// Package gre manages GRE (Game Rules Engine) session buffers.
// Each session accumulates log entries until a threshold is reached, a stale
// sweep evicts it, or the daemon shuts down.  On any of these conditions the
// buffer is flushed as a partial GamePlayEvent.
package gre

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/RdHamilton/hollowmark/services/daemon/internal/recovery"
	"github.com/getsentry/sentry-go"
)

// FlushFunc is called whenever a session buffer is flushed.  The caller is
// responsible for dispatching the resulting event to the BFF.
//
// matchID is an authoritative match id supplied by the caller (the
// finalMatchResult.matchId parsed by the match.completed detector). It is used
// only as a FALLBACK when the GRE entries do not self-resolve a match_id —
// GRE-derived match_id keeps precedence. It is empty ("") for the
// threshold/stale/shutdown flush paths, which have no authoritative id (#807).
type FlushFunc func(ctx context.Context, sessionID, matchID string, entries []json.RawMessage, partial bool) error

// SessionBuffer holds accumulated GRE log entries for a single MTGA session.
type SessionBuffer struct {
	entries     []json.RawMessage
	lastUpdated time.Time
}

// Manager holds all active GRE session buffers and background sweep goroutine.
type Manager struct {
	mu             sync.Mutex
	sessions       map[string]*SessionBuffer
	flushThreshold int
	staleMinutes   int
	flush          FlushFunc
	sweepInterval  time.Duration // injectable for tests
}

// ManagerConfig configures a Manager.
type ManagerConfig struct {
	// FlushThreshold is the number of entries that triggers a partial flush.
	// Valid range enforced by config.Load; default 500.
	FlushThreshold int

	// StaleMinutes is how long a buffer can sit idle before the sweep evicts it.
	// Default 15.
	StaleMinutes int

	// SweepInterval overrides the goroutine sweep period (default 10 min).
	// Used by tests to shorten the interval.
	SweepInterval time.Duration

	// Flush is called each time a buffer is flushed (threshold hit, stale, or shutdown).
	Flush FlushFunc
}

const defaultSweepInterval = 10 * time.Minute

// NewManager creates a Manager with the given config.
func NewManager(cfg ManagerConfig) *Manager {
	si := cfg.SweepInterval
	if si == 0 {
		si = defaultSweepInterval
	}
	return &Manager{
		sessions:       make(map[string]*SessionBuffer),
		flushThreshold: cfg.FlushThreshold,
		staleMinutes:   cfg.StaleMinutes,
		flush:          cfg.Flush,
		sweepInterval:  si,
	}
}

// Append adds an entry to the named session buffer.
// If the buffer reaches the threshold it is flushed immediately as partial and
// then reset; the new entry begins the next buffer.
func (m *Manager) Append(ctx context.Context, sessionID string, entry json.RawMessage) error {
	m.mu.Lock()
	buf, ok := m.sessions[sessionID]
	if !ok {
		buf = &SessionBuffer{}
		m.sessions[sessionID] = buf
	}
	buf.entries = append(buf.entries, entry)
	buf.lastUpdated = time.Now()

	if len(buf.entries) >= m.flushThreshold {
		toFlush := buf.entries
		// Reset the buffer — new entry begins fresh slice.
		buf.entries = nil
		m.mu.Unlock()

		log.Printf("[gre] session=%s threshold=%d reached — flushing partial", sessionID, m.flushThreshold)
		// No authoritative match_id on the threshold path — pass "" and let the
		// flush self-resolve from the buffered GRE entries.
		return m.flush(ctx, sessionID, "", toFlush, true)
	}
	m.mu.Unlock()
	return nil
}

// FlushAll flushes every non-empty session buffer as partial.  Called on
// SIGTERM/SIGINT before daemon exit.
func (m *Manager) FlushAll(ctx context.Context) {
	m.mu.Lock()
	sessions := make(map[string][]json.RawMessage, len(m.sessions))
	for id, buf := range m.sessions {
		if len(buf.entries) > 0 {
			sessions[id] = buf.entries
		}
	}
	m.sessions = make(map[string]*SessionBuffer)
	m.mu.Unlock()

	for id, entries := range sessions {
		log.Printf("[gre] shutdown flush session=%s entries=%d", id, len(entries))
		if err := m.flush(ctx, id, "", entries, true); err != nil {
			log.Printf("[gre] warn: shutdown flush session=%s: %v", id, err)
		}
	}
}

// FlushSession flushes a single named session's buffer with an explicit,
// authoritative matchID, then resets the buffer. It is invoked by the
// match.completed handler to emit a non-partial game-end flush carrying the
// finalMatchResult.matchId (#807). Unlike the threshold/stale/shutdown paths,
// the caller supplies the match id explicitly so the flush is guaranteed to be
// anchored even when GRE gameInfo.matchID is sparse/absent in the buffer window.
//
// If the session has no buffered entries, FlushSession is a no-op and returns
// nil — a game can complete with its GRE entries already drained by a prior
// threshold flush.
func (m *Manager) FlushSession(ctx context.Context, sessionID, matchID string, partial bool) error {
	m.mu.Lock()
	buf, ok := m.sessions[sessionID]
	if !ok || len(buf.entries) == 0 {
		m.mu.Unlock()
		return nil
	}
	entries := buf.entries
	// Reset the buffer so the next game starts fresh and we never double-flush
	// these entries on a later shutdown/stale sweep.
	buf.entries = nil
	m.mu.Unlock()

	log.Printf("[gre] game-end flush session=%s match_id=%s entries=%d partial=%t",
		sessionID, matchID, len(entries), partial)
	return m.flush(ctx, sessionID, matchID, entries, partial)
}

// RunSweep starts the background stale-buffer sweep goroutine.
// It returns when ctx is cancelled.
func (m *Manager) RunSweep(ctx context.Context) {
	defer recovery.RecoverGoroutine("gre-sweep", recovery.CaptureFn(sentry.CurrentHub().CaptureException))
	ticker := time.NewTicker(m.sweepInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.sweepStale(ctx)
		}
	}
}

// staleEntry captures the data needed to flush a stale session after the lock
// is released.
type staleEntry struct {
	entries     []json.RawMessage
	lastUpdated time.Time
}

// sweepStale evicts and flushes any session buffer whose last-updated time
// is older than staleMinutes.
func (m *Manager) sweepStale(ctx context.Context) {
	cutoff := time.Now().Add(-time.Duration(m.staleMinutes) * time.Minute)

	m.mu.Lock()
	stale := make(map[string]staleEntry)
	for id, buf := range m.sessions {
		if len(buf.entries) > 0 && buf.lastUpdated.Before(cutoff) {
			stale[id] = staleEntry{entries: buf.entries, lastUpdated: buf.lastUpdated}
			delete(m.sessions, id)
		}
	}
	m.mu.Unlock()

	for id, e := range stale {
		log.Printf("[gre] stale sweep flushing session=%s entries=%d idle=%s",
			id, len(e.entries), time.Since(e.lastUpdated).Round(time.Second))
		if err := m.flush(ctx, id, "", e.entries, true); err != nil {
			log.Printf("[gre] warn: stale sweep flush session=%s: %v", id, err)
		}
	}
}

// BufferCount returns the number of active session buffers.  Used in tests.
func (m *Manager) BufferCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sessions)
}

// EntryCount returns the number of buffered entries for a session.  Used in tests.
func (m *Manager) EntryCount(sessionID string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	if buf, ok := m.sessions[sessionID]; ok {
		return len(buf.entries)
	}
	return 0
}

// SetLastUpdated overrides the last-updated timestamp for a session.  Used in tests.
func (m *Manager) SetLastUpdated(sessionID string, t time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	buf, ok := m.sessions[sessionID]
	if !ok {
		return fmt.Errorf("session %q not found", sessionID)
	}
	buf.lastUpdated = t
	return nil
}
