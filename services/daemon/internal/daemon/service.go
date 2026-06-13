// Package daemon provides the standalone daemon service.
// The daemon reads MTGA Player.log, classifies events, and POSTs them
// to the BFF via contract.DaemonEvent. It never connects to a database.
package daemon

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/RdHamilton/hollowmark/pkg/logparse"
	"github.com/RdHamilton/hollowmark/services/contract"
	"github.com/RdHamilton/hollowmark/services/daemon/internal/classify"
	"github.com/RdHamilton/hollowmark/services/daemon/internal/config"
	"github.com/RdHamilton/hollowmark/services/daemon/internal/credstore"
	"github.com/RdHamilton/hollowmark/services/daemon/internal/dispatch"
	"github.com/RdHamilton/hollowmark/services/daemon/internal/draftstate"
	"github.com/RdHamilton/hollowmark/services/daemon/internal/gre"
	"github.com/RdHamilton/hollowmark/services/daemon/internal/install"
	"github.com/RdHamilton/hollowmark/services/daemon/internal/keychain"
	"github.com/RdHamilton/hollowmark/services/daemon/internal/localapi"
	"github.com/RdHamilton/hollowmark/services/daemon/internal/logreader"
	"github.com/RdHamilton/hollowmark/services/daemon/internal/pkce"
	"github.com/RdHamilton/hollowmark/services/daemon/internal/ratingsclient"
	"github.com/RdHamilton/hollowmark/services/daemon/internal/recovery"
	"github.com/RdHamilton/hollowmark/services/daemon/internal/updatecheck"
	"github.com/getsentry/sentry-go"
	"github.com/google/uuid"
)

// jwtRefreshInterval is how often the run loop checks whether the JWT needs
// refreshing during an active session. It is a variable so tests can shorten it.
var jwtRefreshInterval = time.Hour

// updateCheckInterval is how often the run loop checks for a newer daemon version.
// It is a variable so tests can shorten it.
var updateCheckInterval = 24 * time.Hour

// heartbeatInterval is how often the run loop sends a daemon.heartbeat event to
// the BFF so the health check has a liveness signal even when MTGA is idle.
// It is a variable so tests can shorten it.
var heartbeatInterval = 30 * time.Second

// helperCheckInterval is how often the run loop probes the collection helper
// socket to keep the tray state in sync (e.g. if the user installs or stops
// the helper outside of the Grant Access flow).
var helperCheckInterval = 30 * time.Second

// mtgaDetectInterval is how often idleUntilMTGADetected polls for Player.log
// when MTGA is not installed. Exposed as a var so tests can override it.
var mtgaDetectInterval = 5 * time.Minute

// defaultLogPathFn is the function used to detect the MTGA log path.
// Exposed as a var so tests can override it without touching the real filesystem.
var defaultLogPathFn = logreader.DefaultLogPath

// Service is the top-level daemon service.
type Service struct {
	cfg        *config.Config
	dispatcher *dispatch.Dispatcher
	poller     *logreader.Poller
	sessionID  string
	version    string // build-time version; "dev" skips update checks
	greManager *gre.Manager
	// draftState caches the live draft session(s) the daemon has seen
	// so localapi handlers can answer /drafts/{id}/current-pack and
	// friends without re-reading the log. Populated as draft.pack /
	// draft.pick events flow through handleEntry.
	draftState *draftstate.Store
	// ratings is the daemon's read-through cache for the BFF's
	// /api/v1/draft-ratings/{set}/{format} endpoint. Satisfies both
	// pkg/draftalgo.CardLookup and .RatingsLookup so the localapi
	// draft handlers can grade picks against real 17Lands data.
	// Wired into localAPI via SetDraftLookups; token kept in sync
	// with the dispatcher via SetToken on each JWT rotation.
	ratings *ratingsclient.Client
	// mtgaUserID is the local player's MTGA Arena client ID (e.g.
	// "TESTACCOUNT000000000000001", 26-char crockford-base32-style) extracted
	// from the authenticateResponse["clientId"] log entry. It equals the
	// reservedPlayers[].userId field in matchGameRoomStateChangedEvent, which
	// is the join key used to identify the local player's team so win/loss can
	// be derived. Empty until a player.authenticated event has been processed
	// in this daemon session.
	mtgaUserID string

	// lastDeckID is the most recently observed MTGA Arena deck UUID, extracted
	// from a CourseDeckSummary.DeckId field in a CourseDeck log entry. Arena
	// emits CourseDeck just before a match starts when the player submits their
	// deck to an event. This field is attached to the next match.completed
	// payload so the BFF can link the match row to the correct deck.
	// Empty until a course.deck_submitted event has been processed.
	//
	// Goroutine-safety: protected by handleEntryMu. handleEntry is called from
	// two goroutines — the Run event-loop and an HTTP-spawned replay goroutine
	// (localapi.handleReplay → go s.Replay).  handleEntryMu serializes both
	// callers so writes and reads of lastDeckID are never concurrent.
	lastDeckID string

	// lastCollectionHash is the content hash (sorted arena_id:count) of the most
	// recently DISPATCHED collection.updated snapshot from the log-reader path.
	// handleEntry skips dispatch when an incoming snapshot hashes identically —
	// this is the dedup guard that kills the rc3 idle emit-storm (Arena
	// re-writing an unchanged GetPlayerCardsV3 line ~1-2/sec).
	//
	// Goroutine-safety: protected by handleEntryMu. handleEntry is called from
	// two goroutines — the Run event-loop and an HTTP-spawned replay goroutine
	// (localapi.handleReplay → go s.Replay).  handleEntryMu serializes both
	// callers so writes and reads of lastCollectionHash are never concurrent.
	// The user-triggered memory-scan path (performCollectionSync) is
	// intentionally exempt and does not touch this field.
	lastCollectionHash string
	// pendingBacklogCollection holds the latest collection snapshot observed
	// during the historical-backlog drain on (re)install startup. Backlog
	// snapshots are coalesced (not dispatched per-line) so a replay of the whole
	// Player.log does not flood the BFF; the single held snapshot is flushed when
	// the first live (non-backlog) collection entry arrives. Nil once flushed.
	pendingBacklogCollection *contract.CollectionUpdatedPayload
	pendingBacklogHash       string
	// trayHooks connects the tray icon to the daemon event loop.
	// All fields are optional — nil channels block forever in select (safe no-op).
	trayHooks TrayHooks
	// keychainErrMu guards keychainErr so the goroutine spawned by
	// keychainRefresherAdapter (AC-3, #2135) can write to it safely while the
	// heartbeat ticker reads it on the main run-loop goroutine.
	keychainErrMu sync.Mutex
	// keychainErr is set in New() if keychain.Get() fails at startup.
	// Cleared on retry success inside retryKeychain. When non-nil, Run()
	// calls retryKeychain before starting the event loop.
	// Must be accessed under keychainErrMu after New() returns (the goroutine
	// spawned by keychainRefresherAdapter writes from a different goroutine).
	keychainErr error
	// keychainGet is the function used to read the API key from the OS keychain.
	// Defaults to keychain.Get; overridden in tests for deterministic behaviour.
	keychainGet func() (string, error)
	// eventBuffer is the bounded ring buffer wired into the dispatcher so that
	// events are not silently lost when the BFF is transiently unreachable.
	// Capacity is 1000 (hard-coded for v0.3.3; configurable knob deferred to
	// v0.4.0). Dropped() is sampled on every SetState update and surfaced on
	// /api/v1/system/health as metrics.dispatchDropped.
	eventBuffer *dispatch.RingBuffer

	// driftMu guards the three parse-failure tracking fields below.
	// recordParseFailure acquires the lock on every typed-parse error;
	// snapshotAndResetDrift acquires it once per heartbeat tick to read
	// and zero the state atomically.
	driftMu sync.Mutex
	// parseFailureCount counts typed-parse errors since the last heartbeat.
	parseFailureCount uint32
	// sampleLineHash is the SHA-256 hex[:16] of the most recently failing
	// raw log line. Overwritten on each failure; never the raw line itself.
	sampleLineHash string
	// failedEventTypes accumulates the distinct event-type strings for which
	// at least one parse error occurred since the last heartbeat reset.
	failedEventTypes map[string]struct{}

	// authPaused is true when the daemon has reached the max PKCE attempt cap
	// (#2133 consent loop guard). It is set via WithAuthPaused (called from
	// main.go after loading daemon-state.json) and guards computeAuthStatus.
	// Only cleared when the user explicitly triggers a successful auth or
	// clicks "Retry Setup" (RC3: no timer-based reset). Read-only after Run()
	// starts; never written from goroutines spawned inside Run(). Protected
	// by atomic.Bool to allow safe concurrent reads from the heartbeat ticker.
	authPaused atomic.Bool

	// reauthFunc is the in-process PKCE re-auth callback set via WithReauthFunc.
	// When non-nil and a 401 is received in keychain mode, keychainRefresherAdapter
	// calls this function to trigger an in-process PKCE re-auth flow rather than
	// immediately surfacing ErrReauthRequired to the tray. The function is
	// responsible for updating the dispatcher token on success (via SetToken).
	// Set once before Run() via WithReauthFunc; never mutated after that.
	reauthFunc func(ctx context.Context) error

	// reauthInProgress is a concurrency gate: only ONE PKCE attempt runs at a
	// time, even if multiple 401 responses arrive concurrently (e.g. events
	// processed in rapid succession). The second caller sees reauthInProgress=true
	// and returns ErrReauthRequired immediately without triggering a second PKCE
	// flow — the first flow will update the token for both.
	//
	// CONCURRENCY PRIMITIVE ONLY. Must NEVER appear in computeAuthStatus,
	// any localapi.State field, or any /health response — per Ray Q2 (#2135).
	// Reading or setting this field from application state logic is a bug.
	reauthInProgress atomic.Bool

	// batchBuffer coalesces events from handleEntry into batches before
	// dispatching to the BFF via dispatcher.SendBatch.  Size=25 / 750ms interval
	// per ADR-053.  Forced flushes are triggered for match.game_ended and
	// draft.pick boundary events.  Started by Run; drained by Close in shutdown.
	batchBuffer *dispatch.BatchBuffer

	// handleEntryMu serializes calls to handleEntry across the two callers that
	// can invoke it concurrently:
	//   1. The Run event-loop goroutine (case entry, ok := <-updates).
	//   2. An HTTP-spawned replay goroutine (localapi.handleReplay → go s.Replay).
	// Without this lock both goroutines race on lastDeckID and lastCollectionHash
	// (and any other mutable state inside handleEntry).  The mutex is the
	// minimal correct fix: it preserves the serial-entry-handling semantic that
	// the code already assumes (replay.go § "intentionally single-threaded per
	// session") while eliminating the data race (#732).
	handleEntryMu sync.Mutex

	// bffMu guards the two BFF-failure tracking fields below.
	// recordBFFFailure and clearBFFFailureCounter acquire this lock.
	bffMu sync.Mutex
	// consecutiveBFFFailures counts how many consecutive SendOrBuffer calls
	// have ended in terminal failure (all retries exhausted). Reset to 0 on
	// the next successful dispatch. Included in the heartbeat payload so the
	// BFF can emit daemon.dispatch_degraded when the count exceeds the threshold.
	consecutiveBFFFailures uint32
	// lastBFFStatusCode is the HTTP status code from the most recent terminal
	// BFF failure. 0 for transport-level failures.
	lastBFFStatusCode int
}

// New creates a Service from cfg.
func New(cfg *config.Config) *Service {
	// ADR-049 Ticket 2: use the channel-derived identity so the staging daemon
	// reads/writes its own credential slot rather than clobbering the prod entry.
	identity := install.Identity(install.Channel)

	// credStore is the platform credential backend (ADR-081):
	//   darwin  — 0600 file at identity.CredentialFile with keychain migration.
	//   windows — Windows Credential Manager via go-keyring.
	// keychainGetFn wraps credStore.Get so the existing keychainGet field
	// continues to work without changes to any non-call-site code.
	cs := credstore.New(identity.CredentialFile, identity.KeychainService)
	keychainGetFn := cs.Get

	// Resolve the dispatcher bearer token in this priority order:
	//   1. cfg.Keychain == true → load api_key from the credential store (PKCE path).
	//   2. cfg.DaemonJWT (legacy HMAC daemon-JWT path).
	//   3. cfg.APIKey plaintext (pre-keychain-migration legacy path).
	// The PKCE path is the only one that works against the current BFF; the
	// legacy registration endpoint /api/daemon/register is no longer mounted
	// (ADR-009 / #1315). Non-keychain modes dispatch without a refresher.
	token := ""
	var keychainErr error
	switch {
	case cfg.Keychain:
		key, err := keychainGetFn()
		if err != nil {
			keychainErr = err
			log.Printf("[daemon] warn: credential store read failed: %v — will retry on startup", err)
		}
		token = key
	case cfg.DaemonJWT != "":
		token = cfg.DaemonJWT
	default:
		token = cfg.APIKey
	}
	// Bounded ring buffer: capacity 1000, hard-coded for v0.3.3.
	// Configurable knob deferred to v0.4.0 (see #2557 follow-on).
	buf := dispatch.NewRingBuffer(1000)
	d := dispatch.New(cfg.CloudAPIURL, cfg.IngestPath, token).WithBuffer(buf)
	sessionID := fmt.Sprintf("live-%s", uuid.New().String())

	svc := &Service{
		cfg:         cfg,
		dispatcher:  d,
		eventBuffer: buf,
		sessionID:   sessionID,
		version:     "dev",
		draftState:  draftstate.New(),
		keychainErr: keychainErr,
		keychainGet: keychainGetFn,
		ratings: ratingsclient.New(ratingsclient.Config{
			BFFURL: cfg.CloudAPIURL,
			Token:  token,
		}),
	}
	// Wire the keychain refresher. Non-keychain installs do not wire a refresher:
	// the legacy /api/daemon/register endpoint is no longer mounted on the BFF
	// (ADR-009 / #1315), so calling it would produce a 404 loop. Keychain mode
	// uses the in-process PKCE re-auth adapter which recovers from 401 without
	// a daemon restart.
	if cfg.Keychain {
		d.WithRefresher(svc.keychainRefresherAdapter())
	}

	// Build the GRE session manager.  The flush func emits a match.game_ended
	// DaemonEvent carrying the accumulated GRE entries as the raw payload.
	svc.greManager = gre.NewManager(gre.ManagerConfig{
		FlushThreshold: cfg.GRESessionFlushThreshold,
		StaleMinutes:   cfg.GRESessionStaleMinutes,
		Flush:          svc.flushGREBuffer,
	})

	// Wire the BFF-failure callback so terminal dispatch failures are counted,
	// and the success callback so the counter resets on the next confirmed send.
	// The counter is included in the next heartbeat payload so the BFF can emit
	// daemon.dispatch_degraded to PostHog when the count exceeds the threshold.
	// onBFFFailure fires only on "all retries exhausted" — NOT on context
	// cancellation. onBFFSuccess fires only on an actual HTTP 2xx delivery.
	d.WithOnBFFFailure(svc.recordBFFFailure).WithOnBFFSuccess(svc.clearBFFFailureCounter)

	// Build the batch buffer (ADR-053).  The stamp function increments the
	// Dispatcher's monotonic sequence counter and stamps it into the event at
	// Add time — satisfying ADR-013's "sequence stamped at emission" requirement.
	// The flush function calls d.SendBatch which inherits the 429-aware backoff
	// from doSend (PR #816) and re-enqueues per-event on retry exhaustion.
	// Start is called here (not in Run) so the buffer is active for the
	// lifetime of the Service — including in tests that call handleEntry
	// directly without going through Run.  Close is called in Run's shutdown
	// path to drain any remaining queued events before exit.
	svc.batchBuffer = dispatch.NewBatchBuffer(dispatch.BatchBufferConfig{
		Size:     25,
		Interval: 750 * time.Millisecond,
		FlushFn:  d.SendBatch,
		Stamp:    func(e *contract.DaemonEvent) { e.Sequence = d.StampSeq() },
		// OnErrReauthRequired fires when SendBatch receives ErrReauthRequired.
		// It dispatches a daemon.auth_failed event (fire-and-forget, same as the
		// legacy handleEntry ErrReauthRequired branch) so PostHog records the 401.
		OnErrReauthRequired: func() {
			go svc.dispatchAuthFailed(context.Background(), "bff_rejected")
		},
	})
	svc.batchBuffer.Start(context.Background())

	return svc
}

// computeAuthStatus derives the auth_status string from config, the current
// keychain error sentinel, and the auth-paused flag (#2133). It is a pure
// function (no receiver) so it can be tested independently.
//
// Precedence rules (highest priority first):
//  1. authPaused == true → auth_paused, regardless of any other state.
//     auth_paused OUTRANKS keychain_error (RC5, #2133).
//  2. keychainErr != nil → keychain_error, regardless of Keychain or AccountID.
//  3. cfg.AccountID == "" OR cfg.Keychain == false → setup_required.
//  4. cfg.Keychain == true AND cfg.AccountID != "" AND keychainErr == nil → authenticated.
//
// NOTE: s.keychainErr is the single source of truth for the keychain-error
// state. Do NOT introduce a parallel boolean; when #2136 lands its graceful-
// degradation state machine it must continue to set/clear s.keychainErr so this
// derivation picks up the transition automatically on the next heartbeat tick.
func computeAuthStatus(cfg *config.Config, keychainErr error, authPaused bool) string {
	if authPaused {
		return localapi.AuthStatusAuthPaused
	}
	if keychainErr != nil {
		return localapi.AuthStatusKeychainError
	}
	if !cfg.Keychain || cfg.AccountID == "" {
		return localapi.AuthStatusSetupRequired
	}
	return localapi.AuthStatusAuthenticated
}

// flushGREBuffer is the FlushFunc wired into the GRE session manager.
// It parses the accumulated GRE log entries using pkg/logparse and dispatches
// the resulting GamePlayPayload to the BFF as a "match.game_ended" DaemonEvent.
//
// Timing note: this function is called retrospectively — the game has already
// ended (or the buffer threshold was reached) when the flush fires. For v0.3.7
// this is acceptable because the BFF projection is async. Real-time
// gre.game_started emission per-entry is a v0.3.8 enhancement.
//
// Nil-seat degradation: when GetPlayerSeatID returns nil (no connectResp entry
// in the buffer window — typical on stale-sweep flushes), PlayerType defaults to
// "opponent" for all events. The Partial flag is set to true in this case per the
// contract godoc; downstream consumers must treat the event as incomplete.
//
// WinningTeamID is always zero: GRE messages do not carry a final win signal.
// The BFF projection worker cross-references the matches table at projection time.
// matchID is an authoritative match id supplied by the caller (the
// finalMatchResult.matchId from the match.completed detector). It is used only
// as a FALLBACK: GRE-derived gameInfo.matchID keeps precedence when present.
// GRE matchID is sparse in real logs (~8 occurrences across 196 game-state
// lines in the real Player.log), so the explicit fallback is what anchors the
// non-partial game-end flush to a real match (#807). It is empty ("") for the
// threshold/stale/shutdown flush paths.
func (s *Service) flushGREBuffer(ctx context.Context, sessionID, matchID string, entries []json.RawMessage, partial bool) error {
	// Convert raw GRE log JSON stored in the buffer into LogEntry values that
	// pkg/logparse functions can consume.
	logEntries := make([]*logparse.LogEntry, 0, len(entries))
	for _, raw := range entries {
		e := logparse.ParseLine(string(raw))
		if e.IsJSON {
			logEntries = append(logEntries, e)
		}
	}

	// Resolve the player's seat — used to distinguish "player" from "opponent".
	// Returns nil when no connectResp entry is present in the buffer window
	// (stale-sweep flush). ParseGamePlaysResult handles nil gracefully.
	playerConn := logparse.GetPlayerSeatID(logEntries)

	// Single-pass parse: plays, snapshots, opponent cards, counter changes, mulligan.
	result, err := logparse.ParseGamePlaysResult(logEntries, playerConn)
	if err != nil {
		log.Printf("[daemon] warn: flushGREBuffer: ParseGamePlaysResult: %v", err)
	}

	// Build the payload from parsed data.
	// SchemaVersion 2 is the first A1.4 implementation (Ray Q3, vmt-t#613/#614).
	payload := contract.GamePlayPayload{
		Partial:       partial,
		SchemaVersion: 2,
		LifeChanges:   []contract.LifeChangeEntry{},
	}

	// Derive game-level fields and populate LifeChanges + CardPlays.
	maxTurn := 0
	for _, play := range result.Plays {
		if play.TurnNumber > maxTurn {
			maxTurn = play.TurnNumber
		}
		// Derive game-level fields from the first play that has them.
		if payload.MatchID == "" && play.MatchID != "" {
			payload.MatchID = play.MatchID
		}
		if payload.GameNumber == 0 && play.GameNumber > 0 {
			payload.GameNumber = play.GameNumber
		}

		switch play.ActionType {
		case "life_change":
			payload.LifeChanges = append(payload.LifeChanges, contract.LifeChangeEntry{
				TeamID:     play.TeamID,
				LifeTotal:  play.LifeTo,
				Delta:      play.LifeTo - play.LifeFrom,
				TurnNumber: play.TurnNumber,
			})
		default:
			// All non-life-change plays are card plays (zone transitions, attacks, blocks).
			payload.CardPlays = append(payload.CardPlays, contract.CardPlayEntry{
				GameNumber: play.GameNumber,
				TurnNumber: play.TurnNumber,
				Phase:      play.Phase,
				ArenaID:    play.CardID,
				PlayerType: play.PlayerType,
				ActionType: play.ActionType,
				ZoneFrom:   play.ZoneFrom,
				ZoneTo:     play.ZoneTo,
			})
		}
	}

	// TurnCount is the maximum turn number observed.
	if maxTurn > 0 {
		payload.TurnCount = maxTurn
	}

	// Map GameSnapshot → contract.GameSnapshotEntry.
	for _, snap := range result.Snapshots {
		// Backfill MatchID/GameNumber from snapshots if not yet set.
		if payload.MatchID == "" && snap.MatchID != "" {
			payload.MatchID = snap.MatchID
		}
		if payload.GameNumber == 0 && snap.GameNumber > 0 {
			payload.GameNumber = snap.GameNumber
		}
		if snap.TurnNumber > payload.TurnCount {
			payload.TurnCount = snap.TurnNumber
		}
		payload.Snapshots = append(payload.Snapshots, contract.GameSnapshotEntry{
			GameNumber:          snap.GameNumber,
			TurnNumber:          snap.TurnNumber,
			PlayerLife:          snap.PlayerLife,
			OpponentLife:        snap.OpponentLife,
			PlayerCardsInHand:   snap.PlayerCardsInHand,
			OpponentCardsInHand: snap.OpponentCardsInHand,
			PlayerLandsInPlay:   snap.PlayerLandsInPlay,
			OpponentLandsInPlay: snap.OpponentLandsInPlay,
		})
	}

	// Map OpponentCard → contract.OpponentCardEntry.
	for _, oc := range result.OpponentCards {
		payload.OpponentCards = append(payload.OpponentCards, contract.OpponentCardEntry{
			// ArenaID is the MTGA GRPId (grpId) as observed in GRE game objects.
			ArenaID:       oc.CardID,
			ZoneObserved:  oc.ZoneObserved,
			TurnFirstSeen: oc.TurnFirstSeen,
			TimesSeen:     oc.TimesSeen,
		})
	}

	// Map CounterChangeEvent → contract.CounterChangeEntry (#613).
	for _, cc := range result.CounterChanges {
		payload.CounterChanges = append(payload.CounterChanges, contract.CounterChangeEntry{
			InstanceID:  cc.InstanceID,
			ArenaID:     cc.ArenaID,
			CounterType: cc.CounterType,
			Count:       cc.Count,
			Delta:       cc.Delta,
			Controller:  cc.Controller,
			TurnNumber:  cc.TurnNumber,
		})
	}

	// Map MulliganData → contract.MulliganEntry (#614).
	if result.Mulligan != nil {
		payload.Mulligan = &contract.MulliganEntry{
			OpeningHandSize: result.Mulligan.OpeningHandSize,
			MulliganCount:   result.Mulligan.MulliganCount,
			KeptCardIDs:     result.Mulligan.KeptCardIDs,
			BottomedCardIDs: result.Mulligan.BottomedCardIDs,
		}
	}

	// Derive PlayerOnPlay (#687): the first-turn active player is on the play.
	// playerConn is nil on stale-sweep flushes; in that case we leave
	// PlayerOnPlay nil (unknown) rather than making an incorrect determination.
	if result.FirstTurnActivePlayerSeatID > 0 && playerConn != nil {
		onPlay := result.FirstTurnActivePlayerSeatID == playerConn.SeatID
		payload.PlayerOnPlay = &onPlay
	}

	// Match-id fallback (#807): GRE gameInfo.matchID is sparse in real logs, so
	// when the buffered entries did not self-resolve a match_id, fall back to the
	// authoritative id supplied by the caller (finalMatchResult.matchId from the
	// match.completed detector). GRE-derived match_id keeps precedence above.
	if payload.MatchID == "" && matchID != "" {
		payload.MatchID = matchID
	}

	// Emit gre.game_started for each distinct gameNumber found in this buffer.
	// This event is emitted retrospectively (game already over when flush fires).
	// A real-time emission path is a v0.3.8 enhancement.
	if payload.GameNumber > 0 {
		startedPayload := contract.GamePlayPayload{
			MatchID:    payload.MatchID,
			GameNumber: payload.GameNumber,
		}
		startedRaw, marshalErr := json.Marshal(startedPayload)
		if marshalErr == nil {
			startedEvt := contract.DaemonEvent{
				Type:       "gre.game_started",
				AccountID:  s.cfg.AccountID,
				SessionID:  s.sessionID,
				OccurredAt: time.Now().UTC(),
				Payload:    json.RawMessage(startedRaw),
			}
			startedCtx, startedCancel := context.WithTimeout(ctx, 5*time.Second)
			defer startedCancel()
			if dispatchErr := s.dispatcher.SendOrBuffer(startedCtx, startedEvt); dispatchErr != nil {
				log.Printf("[daemon] warn: flushGREBuffer: dispatch gre.game_started: %v", dispatchErr)
			}
		}
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("flushGREBuffer: marshal payload: %w", err)
	}

	evt := contract.DaemonEvent{
		Type:       "match.game_ended",
		AccountID:  s.cfg.AccountID,
		SessionID:  s.sessionID,
		OccurredAt: time.Now().UTC(),
		Payload:    json.RawMessage(raw),
	}

	dispatchCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	return s.dispatcher.SendOrBuffer(dispatchCtx, evt)
}

// WithVersion sets the build-time version string used for update checks.
// Call this after New() before Run(). Defaults to "dev" if never called.
func (s *Service) WithVersion(v string) {
	if v != "" {
		s.version = v
	}
}

// setKeychainErr sets s.keychainErr under the mutex. Use this wherever
// keychainErr is written after New() returns (i.e., in any goroutine).
func (s *Service) setKeychainErr(err error) {
	s.keychainErrMu.Lock()
	s.keychainErr = err
	s.keychainErrMu.Unlock()
}

// getKeychainErr returns s.keychainErr under the mutex. Use this wherever
// keychainErr is read concurrently with keychainRefresherAdapter goroutines.
func (s *Service) getKeychainErr() error {
	s.keychainErrMu.Lock()
	defer s.keychainErrMu.Unlock()
	return s.keychainErr
}

// WithReauthFunc wires an in-process PKCE re-auth callback into the service.
// Call this after New() and before Run(). When set, a BFF 401 in keychain mode
// triggers an in-process PKCE re-auth via fn rather than immediately surfacing
// ErrReauthRequired to the tray. The daemon stays running throughout (Ray Q1).
//
// fn must update the dispatcher token on success (via s.dispatcher.SetToken).
// On failure fn should return a non-nil error; ErrReauthFailed will be set on
// s.keychainErr so computeAuthStatus routes to "keychain_error" at the next
// heartbeat tick. The keychain is NOT cleared on failure (Ray Q5).
func (s *Service) WithReauthFunc(fn func(ctx context.Context) error) {
	s.reauthFunc = fn
}

// WithAuthPaused sets the auth-paused flag from daemon-state.json before
// Run() starts (#2133 consent loop guard). When true, the daemon does not
// open a browser or attempt PKCE on startup; computeAuthStatus returns
// AuthStatusAuthPaused until the user explicitly clicks "Retry Setup".
//
// Call this after New() and before Run(), from main.go after loading
// daemon-state.json (RC2: state file is read BEFORE NeedsFirstRunAuth).
func (s *Service) WithAuthPaused(paused bool) {
	s.authPaused.Store(paused)
}

// ClearAuthPaused clears the auth-paused flag. Called after a successful
// PKCE completion or after the user explicitly clicks "Retry Setup" (RC3).
// Callers are responsible for persisting the cleared state to daemon-state.json.
func (s *Service) ClearAuthPaused() {
	s.authPaused.Store(false)
}

// PropagateKeychainToken reads the API key from the OS keychain and wires it
// into the dispatcher and ratings client so subsequent dispatches use the new
// token immediately. It also clears keychainErr so computeAuthStatus reports
// "authenticated" at the next heartbeat tick.
//
// Call this from main.go right after a successful Retry-Setup PKCE flow
// (after ClearAuthPaused(), before Run()) so the token obtained during the
// Retry-Setup browser flow reaches the long-lived dispatcher that Run() uses.
// Without this call, the token set inside runPKCEAuth (via keychain.Set) is
// never wired into the existing dispatcher — Run() would start with an empty
// bearer and every ingest call would return 401 until the next reactive PKCE
// cycle completes.
//
// Binding items (Ray #issuecomment-4582173385):
//   - A1: guards s.ratings != nil before SetToken (New() does not guarantee
//     ratings is non-nil in all test configurations).
//   - A2: treats an empty key returned with a nil error as an error — same
//     guard as retryKeychain (service.go ~line 579) — to avoid silently
//     setting an empty bearer token.
func (s *Service) PropagateKeychainToken() error {
	key, err := s.keychainGet()
	if err != nil {
		s.setKeychainErr(err)
		return fmt.Errorf("PropagateKeychainToken: keychain read: %w", err)
	}
	// A2: empty key with nil error is still unusable — treat it as an error.
	if key == "" {
		e := fmt.Errorf("PropagateKeychainToken: keychain returned empty key")
		s.setKeychainErr(e)
		return e
	}
	s.dispatcher.SetToken(key)
	// A1: guard ratings != nil (not guaranteed in all test configurations).
	if s.ratings != nil {
		s.ratings.SetToken(key)
	}
	// Clear keychainErr so computeAuthStatus reports "authenticated".
	s.setKeychainErr(nil)
	return nil
}

// KeychainReauthRequired is called when the BFF returns 401 in keychain mode.
// It fires the tray hook (if wired) so the UI can prompt the user to
// re-authenticate, then returns dispatch.ErrReauthRequired to signal the
// dispatcher to break its retry loop immediately.
func (s *Service) KeychainReauthRequired(reason string) error {
	if s.trayHooks.SetReauthRequired != nil {
		s.trayHooks.SetReauthRequired(reason)
	}
	return dispatch.ErrReauthRequired
}

// keychainRefresherAdapter wraps the keychain 401 recovery as a dispatch.Refresher.
//
// When s.reauthFunc is set (wired via WithReauthFunc from cmd/daemon/main.go),
// it attempts an in-process PKCE re-auth:
//   - The reauthInProgress gate (atomic.Bool) ensures only one PKCE attempt runs
//     at a time; a concurrent caller returns ErrReauthRequired immediately so the
//     first PKCE flow can complete and update the token for both.
//   - A Sentry breadcrumb is added on trigger and outcome (zero-PII payload).
//   - On success: s.keychainErr is cleared so the next heartbeat reports "authenticated".
//   - On failure: s.keychainErr is set to ErrReauthFailed so computeAuthStatus
//     routes to "keychain_error". The keychain is NOT cleared (Ray Q5).
//
// When s.reauthFunc is nil (no WithReauthFunc call), the original behavior is
// preserved: fire the tray hook and return ErrReauthRequired immediately.
func (s *Service) keychainRefresherAdapter() dispatch.Refresher {
	return refresherFunc(func(ctx context.Context) (string, error) {
		if s.reauthFunc == nil {
			// No in-process reauth wired — fall back to tray-hook only.
			return "", s.KeychainReauthRequired("BFF returned 401")
		}

		// Concurrency gate: if a PKCE attempt is already in flight, let it
		// complete and return ErrReauthRequired so this caller waits for the
		// next dispatcher retry cycle with the refreshed token.
		if !s.reauthInProgress.CompareAndSwap(false, true) {
			log.Printf("[daemon] reauth: PKCE already in progress — skipping duplicate attempt")
			return "", dispatch.ErrReauthRequired
		}

		// Run the PKCE flow in a goroutine so the dispatcher's Refresh call
		// returns promptly. ErrReauthRequired breaks the current retry loop;
		// the next inbound event will retry with the fresh token if PKCE succeeds.
		//
		// context.Background() is intentional here: ctx is the dispatcher's
		// 5-second dispatch timeout, which fires long before a user can complete
		// browser-based PKCE auth (typically 10–30s of real interaction). Using
		// ctx would cause guaranteed context.DeadlineExceeded, set ErrReauthFailed
		// permanently, and leave the daemon stuck in keychain_error forever.
		// The PKCE flow manages its own internal deadline; reauthInProgress
		// prevents concurrent goroutines so there is no goroutine-leak risk.
		// (Sarah S-07 P1 fix — #2135)
		go func() {
			defer recovery.RecoverGoroutine("reauth", recovery.CaptureFn(sentry.CurrentHub().CaptureException))
			defer s.reauthInProgress.Store(false)

			sentry.AddBreadcrumb(&sentry.Breadcrumb{
				Category: "reauth",
				Message:  "reactive 401 re-auth triggered",
				Level:    sentry.LevelInfo,
			})

			log.Printf("[daemon] reauth: starting in-process PKCE re-auth (BFF returned 401)")

			if err := s.reauthFunc(context.Background()); err != nil {
				log.Printf("[daemon] reauth: PKCE re-auth failed: %v", err)
				// Emit daemon.auth_failed with the classified reason code so
				// operators can distinguish user-cancellation from wall-clock
				// timeout in PostHog. Fire-and-forget in a goroutine so the
				// reauthInProgress goroutine is not blocked by the 5-second
				// dispatch timeout. Matches the existing bff_rejected pattern
				// at service.go:1166.
				go s.dispatchAuthFailed(context.Background(), classifyPKCEError(err))
				// Fix C (#1328): set authPaused=true so the for-loop in main.go
				// (NeedsFirstRunAuth || authPaused) surfaces "Retry Setup" in the
				// tray. This ensures the user can always manually re-auth after any
				// reactive 401 failure — not just on startup failures.
				// authPaused outranks keychainErr in computeAuthStatus (RC5), so
				// the tray will show "setup_required" rather than "keychain_error".
				s.authPaused.Store(true)
				if s.trayHooks.SetSetupRequired != nil {
					s.trayHooks.SetSetupRequired(true)
				}
				// Set sentinel so computeAuthStatus routes to "keychain_error"
				// at the next heartbeat tick. Do NOT clear the keychain (Ray Q5).
				s.setKeychainErr(ErrReauthFailed)

				sentry.AddBreadcrumb(&sentry.Breadcrumb{
					Category: "reauth",
					Message:  "reactive 401 re-auth failed",
					Level:    sentry.LevelError,
				})
				return
			}

			log.Printf("[daemon] reauth: in-process PKCE re-auth succeeded")
			// Read the fresh token from keychain and wire it into the dispatcher
			// so subsequent events use the new API key immediately.
			// reauthFunc is responsible for storing the new key in the OS keychain
			// before returning nil; we read it back here to keep token management
			// in one place (the keychain is the source of truth in keychain mode).
			if freshKey, kcErr := s.keychainGet(); kcErr == nil && freshKey != "" {
				s.dispatcher.SetToken(freshKey)
				if s.ratings != nil {
					s.ratings.SetToken(freshKey)
				}
			} else {
				log.Printf("[daemon] reauth: warn: could not read fresh key from keychain after reauth: %v", kcErr)
			}
			// Clear keychainErr so computeAuthStatus reports "authenticated"
			// at the next heartbeat tick.
			s.setKeychainErr(nil)

			sentry.AddBreadcrumb(&sentry.Breadcrumb{
				Category: "reauth",
				Message:  "reactive 401 re-auth succeeded",
				Level:    sentry.LevelInfo,
			})
		}()

		return "", dispatch.ErrReauthRequired
	})
}

// refresherFunc is a function type that adapts a plain func to dispatch.Refresher.
type refresherFunc func(ctx context.Context) (string, error)

func (f refresherFunc) Refresh(ctx context.Context) (string, error) {
	return f(ctx)
}

// ErrSetupRequired is returned by retryKeychain when s.keychainErr is
// keychain.ErrNotFound. ErrNotFound is permanent (the api key was never stored
// or the keychain was wiped), so retries are pointless. The caller (Run /
// main.go) must exit immediately; launchd respawn + NeedsFirstRunAuth handles
// the PKCE re-auth on the next boot.
//
// Distinct from the generic "retries exhausted" error so callers can branch on
// error type rather than string comparison.
var ErrSetupRequired = errors.New("keychain: api key not found — setup required")

// ErrReauthFailed is set on s.keychainErr when a PKCE re-auth callback
// (WithReauthFunc) returns an error. It signals computeAuthStatus to route to
// "keychain_error" at the next heartbeat tick, making the failure visible via
// the /health endpoint. The TryAgain tray channel (#2136) can clear this state.
//
// The keychain is NOT cleared on PKCE failure — per Ray's Q5 answer (#2135).
var ErrReauthFailed = errors.New("reauth: PKCE flow failed")

// ProbeTokenLiveness issues a GET /api/v1/health/daemon against bffBaseURL
// using token as the bearer credential and reports whether the token is live.
//
// Returns (true, nil) when the BFF responds with 200 — the token is valid.
// Returns (false, nil) when the BFF responds with 401 or 403 — the token is
// stale, revoked, or issued for a different Clerk instance (the incident cause).
// Returns (true, nil) for any 5xx response — a server-side error does not imply
// the token is invalid; assume valid to avoid spurious PKCE flows during BFF
// downtime.
//
// Call this at daemon startup after reading the keychain token, before entering
// the main event loop (#1326 Fix A). On (false, nil): treat as NeedsFirstRunAuth
// and enter the PKCE re-registration flow.
func ProbeTokenLiveness(ctx context.Context, bffBaseURL, token string) (live bool, err error) {
	url := strings.TrimRight(bffBaseURL, "/") + "/api/v1/health/daemon"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, fmt.Errorf("probe token liveness: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		// Network error: assume token is still valid; BFF may be transiently
		// unreachable. Do not trigger PKCE on a connectivity blip.
		return true, nil
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusOK:
		return true, nil
	case http.StatusUnauthorized, http.StatusForbidden:
		// Token explicitly rejected by the BFF — re-auth required.
		return false, nil
	default:
		// 4xx other than 401/403, 5xx, redirects: assume token is valid.
		return true, nil
	}
}

// keychainMaxRetries is the number of keychain retry attempts before the daemon
// gives up and exits. Exposed as a var so tests can override it.
var keychainMaxRetries = 3

// keychainRetryBase is the base backoff duration for keychain retries. The
// actual wait for attempt N is keychainRetryBase * N (2s, 4s, 8s). Exposed as
// a var so tests can use shorter durations.
//
// NOTE: The ticket AC specified 500ms/1s/2s backoff. Per Ray's plan-review
// (#2136#issuecomment-4566034474), the existing 2s/4s/8s linear schedule is
// correct — the AC was written before retryKeychain existed; code wins.
var keychainRetryBase = 2 * time.Second

// keychainAccessDeniedPollInterval is how often the idle-degraded loop retries
// after exhausted ErrAccessDenied (R1, ADR-081 §Decision 3). Exposed as a var
// so tests can use shorter durations.
var keychainAccessDeniedPollInterval = 30 * time.Second

// retryKeychain retries credential reads with linear backoff (2s/4s/8s),
// surfacing the error state in the tray. Returns nil on success or when the
// context is cancelled. Returns ErrSetupRequired if the error is permanent
// (ErrNotFound).
//
// REV-1: ErrNotFound short-circuit is the FIRST statement — before any tray
// state change. This ensures computeAuthStatus returns "setup_required" (not
// "keychain_error") on the next heartbeat tick.
//
// R1 (ADR-081 / #1345): after retry exhaustion where ALL failures are
// ErrAccessDenied, the function enters an idle-degraded loop rather than
// returning an error. Returning an error would cause Run() → main.go:exit →
// launchd respawn — a new exit loop. Instead the daemon idles with
// SetKeychainError(true) set and polls until ctx is cancelled or TryAgain
// fires + read succeeds. On ctx cancel it returns nil (not an error), so
// main.go does not call os.Exit.
func (s *Service) retryKeychain(ctx context.Context) error {
	// ── REV-1: ErrNotFound short-circuit ──────────────────────────────────────
	// ErrNotFound is permanent (key never stored / credential wiped). Retrying
	// would loop forever. Clear keychainErr so computeAuthStatus routes to
	// "setup_required" rather than "keychain_error", then return the sentinel
	// without touching tray state. Launchd respawn + NeedsFirstRunAuth handles
	// PKCE re-auth on the next boot.
	if errors.Is(s.getKeychainErr(), keychain.ErrNotFound) || errors.Is(s.getKeychainErr(), credstore.ErrNotFound) {
		s.setKeychainErr(nil)
		return ErrSetupRequired
	}

	// ── Transient / access-denied error: surface tray state and retry ─────────
	if s.trayHooks.SetKeychainError != nil {
		s.trayHooks.SetKeychainError(true)
	}
	defer func() {
		if s.trayHooks.SetKeychainError != nil {
			s.trayHooks.SetKeychainError(false)
		}
	}()

	// Track whether every failure in the retry loop was ErrAccessDenied so we
	// know whether to enter the idle-degraded state after exhaustion.
	allAccessDenied := true

	for attempt := 1; attempt <= keychainMaxRetries; attempt++ {
		backoff := keychainRetryBase * time.Duration(attempt)
		log.Printf("[daemon] keychain retry %d/%d in %s", attempt, keychainMaxRetries, backoff)

		select {
		case <-ctx.Done():
			return nil // context cancelled — do not error out (R1 anti-respawn)
		case <-s.trayHooks.TryAgain:
			// User clicked Try Again — retry immediately, skipping backoff.
			log.Printf("[daemon] keychain retry %d/%d triggered by user", attempt, keychainMaxRetries)
		case <-time.After(backoff):
			// Automatic retry after backoff.
		}

		key, err := s.keychainGet()
		if err == nil && key != "" {
			log.Printf("[daemon] keychain retry %d/%d succeeded", attempt, keychainMaxRetries)
			s.setKeychainErr(nil)
			s.dispatcher.SetToken(key)
			if s.ratings != nil {
				s.ratings.SetToken(key)
			}
			return nil
		}
		if !errors.Is(err, credstore.ErrAccessDenied) {
			allAccessDenied = false
		}
		log.Printf("[daemon] keychain retry %d/%d failed: %v", attempt, keychainMaxRetries, err)
	}

	// ── R1: idle-degraded path for exhausted ErrAccessDenied ──────────────────
	// When every retry returned ErrAccessDenied (OS permission / ACL denial,
	// the launchd headless scenario from #1345), we MUST NOT return an error.
	// Returning an error routes to main.go → logAndExitHeadlessKeychain → os.Exit
	// → launchd respawns → repeat (the "respawn loop" Ray's R1 is fixing).
	//
	// Instead: idle with SetKeychainError(true) active and emit telemetry.
	// The daemon stays alive — it just doesn't dispatch any events. This mirrors
	// the idleUntilMTGADetected anti-respawn precedent (service.go:1014).
	//
	// On TryAgain + successful read: recover and return nil.
	// On context cancel: return nil (clean exit — supervisor does NOT respawn).
	if allAccessDenied {
		log.Printf("[daemon] credential access-denied after %d retries — entering idle-degraded state (no os.Exit, ADR-081 R1)", keychainMaxRetries)

		if s.cfg.AccountID != "" {
			go s.dispatchKeychainError(ctx, "access_denied")
		}

		for {
			select {
			case <-ctx.Done():
				return nil // clean exit; launchd/NSSM does NOT respawn
			case <-s.trayHooks.TryAgain:
				log.Printf("[daemon] idle-degraded: TryAgain signal received — probing credential")
			case <-time.After(keychainAccessDeniedPollInterval):
				log.Printf("[daemon] idle-degraded: periodic credential probe")
			}

			key, err := s.keychainGet()
			if err == nil && key != "" {
				log.Printf("[daemon] idle-degraded: credential read succeeded — resuming normal operation")
				s.setKeychainErr(nil)
				s.dispatcher.SetToken(key)
				if s.ratings != nil {
					s.ratings.SetToken(key)
				}
				return nil
			}
			log.Printf("[daemon] idle-degraded: credential still unreadable: %v", err)
		}
	}

	// ── Non-access-denied exhaustion: propagate for non-headless tray handling ─
	// Dispatch daemon.keychain_error only when AccountID is non-empty (post-auth
	// case B per Ray's OQ-1 verdict). Pre-auth keychain failures are unobservable
	// via the BFF emission boundary — the event would have no api_key and never
	// reach the BFF. The correct signal for the pre-auth case is heartbeat-absence.
	if s.cfg.AccountID != "" {
		go s.dispatchKeychainError(ctx, "os_error")
	}

	return fmt.Errorf("keychain unavailable after %d retries", keychainMaxRetries)
}

// runUpdateCheck calls updatecheck.CheckWithOptions and swallows any panics.
// When a newer version is found, the tray notification hook fires if wired.
// All errors are already swallowed inside the updatecheck package itself; this
// wrapper ensures the version check can never affect service health.
func (s *Service) runUpdateCheck(ctx context.Context) {
	if s.cfg.DisableUpdateCheck {
		return
	}
	opts := updatecheck.Options{}
	if s.trayHooks.NotifyUpdateAvailable != nil {
		opts.NotifyUpdateAvailable = s.trayHooks.NotifyUpdateAvailable
	}
	updatecheck.CheckWithOptions(ctx, s.cfg.CloudAPIURL, s.version, opts)
}

// idleUntilMTGADetected blocks until defaultLogPathFn succeeds or ctx is cancelled.
// It sets the tray to StatusWaitingForArena on entry and restores it on exit.
// Returns nil when MTGA is detected, context.Canceled when the context is cancelled.
// This prevents the daemon from exiting (and launchd/NSSM from respawning in a tight
// loop) when MTGA Arena is not yet installed on the user's machine (#2568).
func (s *Service) idleUntilMTGADetected(ctx context.Context) error {
	log.Printf("[daemon] MTGA not detected — entering idle mode (polling every %s)", mtgaDetectInterval)

	if s.trayHooks.SetWaitingForArena != nil {
		s.trayHooks.SetWaitingForArena(true)
	}
	defer func() {
		if s.trayHooks.SetWaitingForArena != nil {
			s.trayHooks.SetWaitingForArena(false)
		}
	}()

	ticker := time.NewTicker(mtgaDetectInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if _, err := defaultLogPathFn(); err == nil {
				log.Printf("[daemon] MTGA detected — resuming normal startup")
				return nil
			}
			log.Printf("[daemon] MTGA still not detected — continuing idle poll")
		}
	}
}

// Run starts the daemon, blocks until ctx is cancelled.
func (s *Service) Run(ctx context.Context) error {
	// Phase 0: if the keychain was unavailable at startup, retry before
	// starting the event loop. Returns an error if all retries fail —
	// the caller (main.go) will quit cleanly.
	if s.getKeychainErr() != nil {
		if err := s.retryKeychain(ctx); err != nil {
			return fmt.Errorf("keychain unavailable after retries: %w", err)
		}
	}

	// Run update check once on startup (non-blocking).
	go s.runUpdateCheck(ctx)

	// Sweep stale update-download temp dirs from any previous session that was
	// interrupted by the installer (the installer kills the daemon via schtasks /End
	// mid-session, so deferred cleanup never runs). maxAge=1h is conservative;
	// a fresh installer download completes in seconds on any modern connection.
	// Ray Q4: time.AfterFunc is wrong here because the installer kills the daemon
	// before the timer fires — startup sweep is the correct pattern.
	go updatecheck.CleanStaleTempDirs(os.TempDir(), "vaultmtg-update-", time.Hour)

	logPath := s.cfg.LogPath
	if logPath == "" {
		detected, err := defaultLogPathFn()
		if err != nil {
			// MTGA not installed: idle until Player.log appears rather than
			// exiting. Exiting causes launchd/NSSM to respawn within
			// ThrottleInterval producing an infinite restart loop (#2568).
			if idleErr := s.idleUntilMTGADetected(ctx); idleErr != nil {
				if errors.Is(idleErr, context.Canceled) {
					return nil
				}
				return idleErr
			}
			// Re-attempt detection after idle loop returns (MTGA now present).
			detected, err = defaultLogPathFn()
			if err != nil {
				return fmt.Errorf("detect log path after idle: %w", err)
			}
		}
		logPath = detected
		log.Printf("[daemon] auto-detected log path: %s", logPath)
	}

	if s.cfg.LogPreserveOnStart {
		dst, err := logreader.Snapshot(logPath, s.cfg.LogArchiveDir)
		if err != nil {
			log.Printf("[daemon] warn: log snapshot failed: %v", err)
		} else if dst != "" {
			log.Printf("[daemon] log snapshot saved: %s", dst)
		}
		if err := logreader.PruneSnapshots(s.cfg.LogArchiveDir, s.cfg.LogArchiveMaxAge); err != nil {
			log.Printf("[daemon] warn: prune snapshots failed: %v", err)
		}
	}

	pollerCfg := logreader.DefaultPollerConfig(logPath)
	pollerCfg.Interval = s.cfg.PollInterval
	pollerCfg.UseFileEvents = s.cfg.UseFSNotify
	pollerCfg.ReadFromStart = true

	poller, err := logreader.NewPoller(pollerCfg)
	if err != nil {
		return fmt.Errorf("create poller: %w", err)
	}
	s.poller = poller

	updates := poller.Start()
	errs := poller.Errors()

	// Phase 0 of the daemon-local-API plan: serve a /health endpoint on
	// the channel-derived loopback port (stable=9001, staging=9011) so the
	// SPA's "daemon connected" indicator can detect this process. Deriving
	// the port from install.Channel (rather than the hardcoded DefaultPort)
	// lets a staging daemon coexist with a prod daemon on the same machine
	// without a port collision (#667 / ADR-049 Ticket 5). Non-fatal — if the
	// port is busy (e.g. a previous daemon instance is still draining), the
	// daemon continues with dispatch only.
	startedAt := time.Now().UTC()
	localAPI := localapi.New(install.Identity(install.Channel).LocalAPIPort, localapi.State{
		Version:      s.version,
		SessionID:    s.sessionID,
		StartedAt:    startedAt,
		AccountID:    s.cfg.AccountID,
		CloudAPIURL:  s.cfg.CloudAPIURL,
		BFFReachable: true, // optimistic — flips when a dispatch fails
		AuthStatus:   computeAuthStatus(s.cfg, s.getKeychainErr(), s.authPaused.Load()),
	})
	// Hand the localapi server a read view of the live draft state so
	// /api/v1/drafts/{id}/current-pack, /grade-pick, and /win-probability
	// can answer from in-memory data without a separate parse pass.
	localAPI.SetDraftStore(s.draftState)
	// Wire the ratings client as both CardLookup and RatingsLookup —
	// it satisfies both interfaces. Without this, grade-pick returns
	// "N/A" and win-probability falls back to the neutral baseline.
	localAPI.SetDraftLookups(s.ratings, s.ratings)
	// Wire the replay trigger so POST /api/v1/replay can start a
	// historical log replay that emits replay:* events via the BFF.
	localAPI.SetReplayTrigger(s.Replay)
	// Use the daemon lifecycle context so replay goroutines are cancelled
	// when the daemon stops rather than on HTTP request completion.
	localAPI.WithContext(ctx)
	if err := localAPI.Start(); err != nil {
		log.Printf("[daemon] warn: local API server did not start: %v", err)
	}
	defer func() { _ = localAPI.Stop() }()

	log.Printf("[daemon] started (session=%s cloud_api=%s)", s.sessionID, s.cfg.CloudAPIURL)

	// Check if the privileged collection helper is already installed.
	go s.checkHelperOnStartup(ctx)

	// Start the GRE stale-buffer sweep goroutine.
	go s.greManager.RunSweep(ctx)

	// Periodic JWT refresh: check every jwtRefreshInterval whether the stored
	// token is within the refresh window and re-register if so. This ensures
	// mid-session expiry is handled without requiring a daemon restart.
	jwtTicker := time.NewTicker(jwtRefreshInterval)
	defer jwtTicker.Stop()

	// Periodic version check: every 24 hours, check for a newer daemon release.
	updateTicker := time.NewTicker(updateCheckInterval)
	defer updateTicker.Stop()

	// Periodic liveness heartbeat: every 30 seconds, send a daemon.heartbeat
	// event so the BFF health check has a signal even when MTGA is idle.
	heartbeatTicker := time.NewTicker(heartbeatInterval)
	defer heartbeatTicker.Stop()

	// Periodic helper status check: probe the collection helper socket so the
	// tray reflects current state if the helper is installed or stopped outside
	// of the Grant Access flow.
	helperCheckTicker := time.NewTicker(helperCheckInterval)
	defer helperCheckTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			poller.Stop()
			// Drain the batch buffer before flushing GRE buffers so any
			// in-flight log entries reach the BFF before the session ends.
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
			s.batchBuffer.Close(shutdownCtx)
			shutdownCancel()
			// Flush all non-empty GRE session buffers before exit.
			flushCtx, flushCancel := context.WithTimeout(context.Background(), 10*time.Second)
			s.greManager.FlushAll(flushCtx)
			flushCancel()
			log.Printf("[daemon] stopped")
			return nil

		case <-jwtTicker.C:
			// The legacy /api/daemon/register endpoint is no longer mounted on
			// the BFF (ADR-009 / #1315). Keychain (PKCE) api_keys do not expire;
			// the keychainRefresherAdapter handles 401 recovery without polling.
			// This tick is kept as a no-op placeholder so existing ticker cleanup
			// (defer jwtTicker.Stop()) remains correct.

		case <-updateTicker.C:
			// Run non-blocking; errors are swallowed inside runUpdateCheck.
			go s.runUpdateCheck(ctx)

		case <-heartbeatTicker.C:
			// Refresh the localapi state snapshot so /health reflects the
			// current dispatch_dropped counter. The heartbeat tick is the
			// natural update point — 30-second staleness is acceptable.
			now := time.Now().UTC()
			localAPI.SetState(localapi.State{
				Version:         s.version,
				SessionID:       s.sessionID,
				StartedAt:       startedAt,
				AccountID:       s.cfg.AccountID,
				CloudAPIURL:     s.cfg.CloudAPIURL,
				BFFReachable:    true,
				DispatchDropped: s.eventBuffer.Dropped(),
				LastDispatchAt:  &now,
				AuthStatus:      computeAuthStatus(s.cfg, s.getKeychainErr(), s.authPaused.Load()),
			})

			// Skip when AccountID is not yet set (daemon not authenticated).
			if s.cfg.AccountID == "" {
				continue
			}
			// Snapshot and reset the parse-failure counter so each heartbeat
			// window carries an independent, non-overlapping slice of counts.
			driftCount, driftHash, driftTypes := s.snapshotAndResetDrift()
			// Snapshot BFF failure counter under lock; do not reset it here —
			// the daemon resets it on the next successful SendOrBuffer, not per
			// heartbeat. The BFF decides whether to emit dispatch_degraded.
			s.bffMu.Lock()
			bffFailCount := s.consecutiveBFFFailures
			bffStatusCode := s.lastBFFStatusCode
			s.bffMu.Unlock()
			hbPayload := heartbeatPayload{
				ParseFailureCount:      driftCount,
				SampleLineHash:         driftHash,
				FailedEventTypes:       driftTypes,
				ConsecutiveBFFFailures: bffFailCount,
				LastBFFStatusCode:      bffStatusCode,
			}
			evt, err := dispatch.BuildEvent("daemon.heartbeat", s.cfg.AccountID, s.sessionID, hbPayload)
			if err != nil {
				log.Printf("[daemon] warn: build heartbeat event: %v", err)
				continue
			}
			dispatchCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			if sendErr := s.dispatcher.SendOrBuffer(dispatchCtx, evt); sendErr != nil {
				log.Printf("[daemon] warn: heartbeat dispatch: %v", sendErr)
			}
			cancel()

		case <-helperCheckTicker.C:
			go s.checkHelperOnStartup(ctx)

		case <-s.trayHooks.SyncNow:
			go func() {
				defer func() {
					if r := recover(); r != nil {
						log.Printf("[daemon] panic in performCollectionSync: %v", r)
						// Capture for Sentry. Calls on a nil client are safe
						// no-ops when sentry.Init was not called (#1832).
						sentry.CurrentHub().Recover(r)
					}
				}()
				s.performCollectionSync(ctx)
			}()

		case <-s.trayHooks.GrantAccess:
			go func() {
				defer func() {
					if r := recover(); r != nil {
						log.Printf("[daemon] panic in installCollectionHelper: %v", r)
						sentry.CurrentHub().Recover(r)
					}
				}()
				s.installCollectionHelper()
			}()

		case <-s.trayHooks.InstallUpdate:
			// User clicked the "Update available" tray item. The download and
			// verification happen in a goroutine so the main loop is not blocked.
			// The daemon is the trigger, never the executor: after verification,
			// LaunchInstaller hands off to Installer.app (macOS) or cmd /c start
			// (Windows) and returns immediately. The installer then kills the daemon
			// via schtasks /End mid-session (intentional — see service.go comments
			// on Windows NSIS kill semantics; ADR-036 I-10 invariant).
			go func() {
				defer func() {
					if r := recover(); r != nil {
						log.Printf("[daemon] panic in install update: %v", r)
						sentry.CurrentHub().Recover(r)
					}
				}()
				s.handleInstallUpdate(ctx)
			}()

		case err, ok := <-errs:
			if !ok {
				return nil
			}
			log.Printf("[daemon] poller error: %v", err)

		case entry, ok := <-updates:
			if !ok {
				return nil
			}
			if err := s.handleEntry(ctx, entry); err != nil {
				log.Printf("[daemon] handle entry: %v", err)
			}
		}
	}
}

// handleInstallUpdate is the goroutine body for the InstallUpdate tray action.
// It fetches the latest version info, downloads + verifies the installer, then
// launches it. All errors are logged and swallowed.
//
// NOTE: The daemon will be killed by the installer mid-session (Windows: NSIS
// calls schtasks /End; macOS: pkg postinstall unloads and reloads launchd).
// This is intentional and not a bug.
func (s *Service) handleInstallUpdate(ctx context.Context) {
	log.Printf("[daemon] install-update: starting download and verification")

	// Fetch the latest version info to get download URLs.
	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, s.cfg.CloudAPIURL+"/daemon/version", nil)
	if err != nil {
		log.Printf("[daemon] install-update: failed to build version request: %v", err)
		return
	}
	req.Header.Set("User-Agent", fmt.Sprintf("vaultmtg-daemon/%s", s.version))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("[daemon] install-update: version fetch failed: %v", err)
		return
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		log.Printf("[daemon] install-update: version check returned %d", resp.StatusCode)
		return
	}
	var vr updatecheck.VersionResponse
	if err := json.NewDecoder(resp.Body).Decode(&vr); err != nil {
		log.Printf("[daemon] install-update: version decode failed: %v", err)
		return
	}

	if vr.Latest == "" {
		log.Printf("[daemon] install-update: empty latest version from BFF")
		return
	}

	// Downgrade protection.
	d := updatecheck.NewDownloader(updatecheck.DownloaderConfig{})
	if err := d.CheckDowngrade(s.version, vr.Latest); err != nil {
		log.Printf("[daemon] install-update: %v", err)
		return
	}

	if vr.Sha256SumsURL == "" || vr.AttestationURL == "" {
		log.Printf("[daemon] install-update: missing SHA256SUMS/attestation URL fields in version response (BFF not updated yet)")
		return
	}

	// Select the platform-specific installer asset URL (per-platform binary, not the
	// HTML release page). Empty return means this OS is unsupported or the BFF has
	// not yet been updated to expose per-platform URLs — abort cleanly.
	installerURL := updatecheck.SelectInstallerURL(&vr, runtime.GOOS)
	if installerURL == "" {
		log.Printf("[daemon] install-update: no installer URL for platform %s in version response (BFF not updated yet or unsupported OS)", runtime.GOOS)
		return
	}

	parentDir := os.TempDir()

	// Download the SHA256SUMS file.
	sumsPath, err := d.DownloadToTempDir(vr.Sha256SumsURL, parentDir)
	if err != nil {
		log.Printf("[daemon] install-update: download SHA256SUMS failed: %v", err)
		return
	}
	sumsDir := filepath.Dir(sumsPath)
	defer func() { _ = os.RemoveAll(sumsDir) }()

	// Download the signature file.
	sigPath, err := d.DownloadToTempDir(vr.AttestationURL, parentDir)
	if err != nil {
		log.Printf("[daemon] install-update: download signature failed: %v", err)
		return
	}
	sigDir := filepath.Dir(sigPath)
	defer func() { _ = os.RemoveAll(sigDir) }()

	// Download the platform-specific installer binary.
	installerPath, err := d.DownloadToTempDir(installerURL, parentDir)
	if err != nil {
		log.Printf("[daemon] install-update: download installer failed: %v", err)
		return
	}
	installerDir := filepath.Dir(installerPath)
	// NOTE: do NOT defer RemoveAll for the installer dir — the installer kills
	// the daemon before any defer runs. CleanStaleTempDirs handles cleanup on
	// the next daemon startup (Ray Q4 pattern).

	// Verify both trust anchor (minisign signature) and SHA-256 checksum.
	// Never launch unless BOTH pass (ADR-036 I-10).
	installerFilename := filepath.Base(installerPath)
	if err := updatecheck.VerifyBoth(d, installerPath, installerFilename, sumsPath, sigPath, ""); err != nil {
		log.Printf("[daemon] install-update: verification failed — installer rejected: %v", err)
		_ = os.RemoveAll(installerDir)
		return
	}

	log.Printf("[daemon] install-update: verification passed — launching installer v%s", vr.Latest)
	if err := updatecheck.LaunchInstaller(installerPath); err != nil {
		log.Printf("[daemon] install-update: launch failed: %v", err)
		_ = os.RemoveAll(installerDir)
		return
	}
	// Installer launched. The daemon will be killed by the installer.
	log.Printf("[daemon] install-update: installer launched; daemon will exit when installer takes over")
}

// heartbeatPayload is the JSON body of a daemon.heartbeat event.
// When parse failures have occurred since the last heartbeat, ParseFailureCount
// is non-zero and the drift fields are populated; the BFF inspects these to
// emit a daemon.log_format_drift PostHog event (per ADR-027 §OQ-5, #2569).
// ConsecutiveBFFFailures is the number of consecutive SendOrBuffer terminal
// failures since the last success; the BFF emits daemon.dispatch_degraded when
// this counter is >= dispatchDegradedThreshold (#2139).
type heartbeatPayload struct {
	// From #2569: parse-failure drift detection.
	ParseFailureCount uint32   `json:"parse_failure_count"`
	SampleLineHash    string   `json:"sample_line_hash,omitempty"`
	FailedEventTypes  []string `json:"failed_event_types,omitempty"`
	// From #2139: BFF dispatch degradation signal.
	ConsecutiveBFFFailures uint32 `json:"consecutive_bff_failures,omitempty"`
	LastBFFStatusCode      int    `json:"last_bff_status_code,omitempty"`
}

// recordParseFailure increments the per-heartbeat parse-failure counter,
// overwrites sampleLineHash with a SHA-256 hex[:16] of rawLine, and adds
// eventType to the failedEventTypes set. The raw line is never stored.
// This method is safe to call concurrently; driftMu is acquired internally.
func (s *Service) recordParseFailure(eventType, rawLine string) {
	sum := sha256.Sum256([]byte(rawLine))
	hash := fmt.Sprintf("%x", sum)[:16]

	s.driftMu.Lock()
	s.parseFailureCount++
	s.sampleLineHash = hash
	if s.failedEventTypes == nil {
		s.failedEventTypes = make(map[string]struct{})
	}
	s.failedEventTypes[eventType] = struct{}{}
	s.driftMu.Unlock()
}

// snapshotAndResetDrift reads the three drift fields under the lock, zeroes
// them, and returns copies to the caller. Called once per heartbeat tick so
// each heartbeat window carries an independent, non-overlapping slice of counts.
func (s *Service) snapshotAndResetDrift() (count uint32, hash string, types []string) {
	s.driftMu.Lock()
	count = s.parseFailureCount
	hash = s.sampleLineHash
	for et := range s.failedEventTypes {
		types = append(types, et)
	}
	s.parseFailureCount = 0
	s.sampleLineHash = ""
	s.failedEventTypes = nil
	s.driftMu.Unlock()

	sort.Strings(types)
	return count, hash, types
}

// dispatchDegradedThreshold is the consecutive-failure count at which the daemon
// considers BFF ingest "degraded". Three consumers:
//  1. recordBFFFailure — emits a Sentry warning event at the transition edge.
//  2. recordBFFFailure — calls trayHooks.SetSyncDegraded(true) at the edge so
//     the tray flips from "Tracking" to "Sync issues — games may not be saving".
//  3. The heartbeat payload mirrors consecutiveBFFFailures to the BFF
//     (service.go heartbeat section) so the server-side health endpoint can
//     reflect the same semantic.
//
// Mirrors the BFF-side dispatchDegradedThreshold
// (services/bff/internal/api/handlers/ingest.go) so the two systems agree on
// what "degraded" means. Held as a daemon-local constant to avoid a
// daemon→bff package import. (#1234 Ray amendment §1)
const dispatchDegradedThreshold = uint32(3)

// recordBFFFailure increments the consecutive-BFF-failure counter and records
// the last status code. Called by the onBFFFailure callback wired into the
// Dispatcher in New(). Safe to call concurrently; bffMu is held internally.
//
// On the transition into a multi-failure streak (count == dispatchDegradedThreshold):
//   - emits a Sentry warning event so degraded-BFF episodes surface in the
//     crash aggregator (only at the threshold, not on every failure);
//   - calls trayHooks.SetSyncDegraded(true) so the tray reflects actual ingest
//     health rather than local process state (#1234).
//
// The tray hook is called outside bffMu (no lock nesting with statusMu in tray.App).
// The hook is nil-safe per the existing pattern (collection.go:167).
func (s *Service) recordBFFFailure(statusCode int) {
	s.bffMu.Lock()
	s.consecutiveBFFFailures++
	count := s.consecutiveBFFFailures
	s.lastBFFStatusCode = statusCode
	s.bffMu.Unlock()

	if count == dispatchDegradedThreshold {
		sentry.WithScope(func(scope *sentry.Scope) {
			scope.SetTag("event", "daemon.dispatch_degraded")
			scope.SetTag("bff_status_code", fmt.Sprintf("%d", statusCode))
			scope.SetLevel(sentry.LevelWarning)
			sentry.CaptureMessage(fmt.Sprintf(
				"daemon.dispatch_degraded count=%d status=%d", count, statusCode,
			))
		})
		// Edge-fire: only transition the tray at the threshold, not on every
		// subsequent failure (Ray amendment §3, #1234).
		if s.trayHooks.SetSyncDegraded != nil {
			s.trayHooks.SetSyncDegraded(true)
		}
	}
}

// clearBFFFailureCounter resets the consecutive-failure counter and status code
// to zero. Called after a successful SendOrBuffer. Safe to call concurrently.
//
// Recovery edge-fire (Ray amendment §3, #1234): the tray hook SetSyncDegraded(false)
// is called only when the prior count was >= dispatchDegradedThreshold (i.e. the
// tray was actually in the degraded state). Calling it on every success would
// spam SetStatus once per dispatch on the success path.
func (s *Service) clearBFFFailureCounter() {
	s.bffMu.Lock()
	prior := s.consecutiveBFFFailures
	s.consecutiveBFFFailures = 0
	s.lastBFFStatusCode = 0
	s.bffMu.Unlock()

	if prior >= dispatchDegradedThreshold {
		if s.trayHooks.SetSyncDegraded != nil {
			s.trayHooks.SetSyncDegraded(false)
		}
	}
}

// authFailedPayload is the JSON body of a daemon.auth_failed dispatch event.
// reason is one of: "bff_rejected", "pkce_timeout", "pkce_cancelled",
// "pkce_token_exchange_failed". The latter is emitted when the Clerk token
// endpoint rejects the authorization code (e.g. HTTP 4xx "invalid_grant") and
// was added in #2172 as the third code in the cb4a4c15 [#88] taxonomy.
// BFFStatusCode is populated only when reason is "bff_rejected"; it carries the
// raw HTTP status (401, 403, etc.) for operator routing on the dashboard.
type authFailedPayload struct {
	Reason        string `json:"reason"`
	BFFStatusCode int    `json:"bff_status_code,omitempty"`
	Platform      string `json:"platform"`
	DaemonVersion string `json:"daemon_version"`
}

// keychainErrorPayload is the JSON body of a daemon.keychain_error dispatch event.
// ErrorType is one of: "not_found", "os_error".
type keychainErrorPayload struct {
	ErrorType     string `json:"error_type"`
	Platform      string `json:"platform"`
	DaemonVersion string `json:"daemon_version"`
}

// dispatchAuthFailed sends a daemon.auth_failed event to the BFF via a
// transient dispatcher that has NO refresher set. This is intentional:
// dispatching telemetry about an auth failure must not itself trigger the
// auth-failure tray hook again (which would happen if we used s.dispatcher in
// keychain mode and the BFF returned 401 for the telemetry event). A no-refresher
// dispatcher will retry up to 3 times and buffer on exhaustion — correct
// behaviour for a telemetry event that the BFF may briefly be unable to accept.
// reason must be one of: "bff_rejected", "pkce_timeout", "pkce_cancelled",
// "pkce_token_exchange_failed" (added #2172). For "bff_rejected", lastBFFStatusCode
// is read under the lock and included as
// bff_status_code. This is best-effort — errors are logged and swallowed.
func (s *Service) dispatchAuthFailed(ctx context.Context, reason string) {
	s.bffMu.Lock()
	statusCode := s.lastBFFStatusCode
	s.bffMu.Unlock()

	p := authFailedPayload{
		Reason:        reason,
		Platform:      runtime.GOOS,
		DaemonVersion: s.version,
	}
	if reason == "bff_rejected" {
		p.BFFStatusCode = statusCode
	}

	// Capture the failure as a Sentry exception so beta-time auth regressions
	// surface in the crash aggregator alongside any related panics. The
	// PostHog event (dispatched below) covers user-impact analytics; Sentry
	// covers exception-side root-cause investigation. #1832.
	sentry.WithScope(func(scope *sentry.Scope) {
		scope.SetTag("event", "daemon.auth_failed")
		scope.SetTag("reason", reason)
		if reason == "bff_rejected" {
			scope.SetTag("bff_status_code", fmt.Sprintf("%d", statusCode))
		}
		sentry.CaptureMessage(fmt.Sprintf("daemon.auth_failed reason=%s", reason))
	})

	evt, err := dispatch.BuildEvent("daemon.auth_failed", s.cfg.AccountID, s.sessionID, p)
	if err != nil {
		log.Printf("[daemon] warn: build auth_failed event: %v", err)
		return
	}
	// Use a transient dispatcher without a refresher so that if the BFF
	// returns 401 for this telemetry event, we retry silently and buffer —
	// without re-triggering the keychain reauth tray hook a second time.
	// Token() returns the primary dispatcher's current bearer token.
	d := dispatch.New(s.cfg.CloudAPIURL, s.cfg.IngestPath, s.dispatcher.Token()).
		WithBuffer(s.eventBuffer)
	dispatchCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := d.SendOrBuffer(dispatchCtx, evt); err != nil {
		log.Printf("[daemon] warn: dispatch auth_failed event: %v", err)
	}
}

// classifyPKCEError maps a PKCE error to the appropriate daemon.auth_failed
// reason code. Precedence (highest first):
//
//  1. context.Canceled (bare or wrapped) — user dismissed the browser window
//     → "pkce_cancelled".
//  2. pkce.ErrTokenExchange (wrapped via %w in pkce.Run) — Clerk token endpoint
//     rejected the authorization code (e.g. HTTP 4xx "invalid_grant") →
//     "pkce_token_exchange_failed". Detected via errors.Is; never strings.Contains.
//  3. All other errors — wall-clock timeout, port-bind failure, etc. →
//     "pkce_timeout" (safe default).
//
// Commit cb4a4c15 [#88] established the two-code taxonomy (pkce_cancelled,
// pkce_timeout). This function extends it with pkce_token_exchange_failed (#2172).
func classifyPKCEError(err error) string {
	if errors.Is(err, context.Canceled) {
		return "pkce_cancelled"
	}
	if errors.Is(err, pkce.ErrTokenExchange) {
		return "pkce_token_exchange_failed"
	}
	return "pkce_timeout"
}

// dispatchKeychainError sends a daemon.keychain_error event to the BFF via a
// transient no-refresher dispatcher (same pattern as dispatchAuthFailed).
// errorType must be one of: "not_found", "os_error".
// This is best-effort — errors are logged and swallowed.
func (s *Service) dispatchKeychainError(ctx context.Context, errorType string) {
	p := keychainErrorPayload{
		ErrorType:     errorType,
		Platform:      runtime.GOOS,
		DaemonVersion: s.version,
	}
	// Capture for Sentry alongside the PostHog telemetry event. #1832.
	sentry.WithScope(func(scope *sentry.Scope) {
		scope.SetTag("event", "daemon.keychain_error")
		scope.SetTag("error_type", errorType)
		sentry.CaptureMessage(fmt.Sprintf("daemon.keychain_error type=%s", errorType))
	})

	evt, err := dispatch.BuildEvent("daemon.keychain_error", s.cfg.AccountID, s.sessionID, p)
	if err != nil {
		log.Printf("[daemon] warn: build keychain_error event: %v", err)
		return
	}
	d := dispatch.New(s.cfg.CloudAPIURL, s.cfg.IngestPath, s.dispatcher.Token()).
		WithBuffer(s.eventBuffer)
	dispatchCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := d.SendOrBuffer(dispatchCtx, evt); err != nil {
		log.Printf("[daemon] warn: dispatch keychain_error event: %v", err)
	}
}

// draftScenePayload is the enriched payload emitted for draft.started and
// draft.completed events (#1344 PR-B). The BFF's projectDraftSession requires
// session_id to be non-empty; without enrichment both scene-change events fall
// through to raw entry.JSON (no session_id) and are permanently rejected.
//
// Fields match the BFF's draftPayload struct (worker.go) — ADDITIVE-ONLY,
// no ADR-079 contract bump (payload fields are a superset of the existing
// draftPayload contract; BFF already accepts all these fields).
type draftScenePayload struct {
	SessionID string `json:"session_id"`
	EventName string `json:"event_name"` // CourseName, e.g. "QuickDraft_EOE_20260612"
	SetCode   string `json:"set_code"`   // e.g. "EOE"
	DraftType string `json:"draft_type"` // e.g. "QuickDraft"
}

// handleEntry classifies a log entry and dispatches it to the BFF.
//
// Concurrency: handleEntry is called from two goroutines:
//  1. The Run event-loop goroutine (case entry, ok := <-updates).
//  2. An HTTP-spawned replay goroutine (localapi.handleReplay → go s.Replay →
//     iterates the log calling handleEntry on each entry).
//
// handleEntryMu serializes both callers so that mutable fields such as
// lastDeckID and lastCollectionHash are accessed by exactly one goroutine at a
// time.  This preserves the serial-entry-handling semantic that the surrounding
// logic already requires (a course.deck_submitted write must be read atomically
// by the next match.completed without interleaving from the other caller).
func (s *Service) handleEntry(ctx context.Context, entry *logreader.LogEntry) error {
	s.handleEntryMu.Lock()
	defer s.handleEntryMu.Unlock()

	if entry == nil || !entry.IsJSON {
		return nil
	}

	eventType := classifyEntry(entry)
	if eventType == "" {
		// Not a tracked event type
		return nil
	}

	// For known event types, use typed payloads so the BFF receives validated,
	// well-typed JSON rather than the raw map[string]interface{} from the log.
	var payload interface{}
	switch eventType {
	case "draft.pack":
		// Re-discriminate Premier vs BotDraft on the key signature. Premier
		// (#338) = Draft.Notify (draftId + PackCards). BotDraft (#337) =
		// CurrentModule=BotDraft + stringified Payload, handled by the else branch.
		var p *logreader.DraftPackPayload
		var err error
		if _, hasDraftID := entry.JSON["draftId"]; hasDraftID {
			p, err = logreader.ParsePremierDraftNotify(entry)
		} else {
			p, err = logreader.ParseBotDraftStatusPack(entry)
		}
		if err != nil {
			log.Printf("[daemon] warn: parse draft pack: %v", err)
			s.recordParseFailure(eventType, entry.Raw)
			payload = entry.JSON
		} else {
			payload = p
			// Mirror the typed payload into in-memory draftstate so
			// localapi handlers can serve current-pack / grade-pick /
			// win-probability without re-parsing the log.
			if s.draftState != nil {
				s.draftState.HandlePack(p)
			}
		}
	case "draft.pick":
		// Premier (#338) = EventPlayerDraftMakePick (request string with
		// DraftId). BotDraft (#337) = BotDraftDraftPick (request string with
		// PickInfo), handled by the else branch.
		var p *logreader.DraftPickPayload
		var err error
		if req, hasReq := entry.JSON["request"].(string); hasReq && strings.Contains(req, `"DraftId"`) {
			p, err = logreader.ParsePremierDraftMakePick(entry)
		} else {
			p, err = logreader.ParseBotDraftPick(entry)
		}
		if err != nil {
			log.Printf("[daemon] warn: parse draft pick: %v", err)
			s.recordParseFailure(eventType, entry.Raw)
			payload = entry.JSON
		} else {
			if s.draftState != nil {
				s.draftState.HandlePick(p)
				// Attach the active session ID so the BFF can associate
				// this pick with the correct draft_sessions row.
				if sess, ok := s.draftState.Get("current"); ok {
					p.SessionID = sess.ID
				}
			}
			payload = p
		}
	case "inventory.updated":
		p, err := logreader.ParseInventoryEntry(entry)
		if err != nil {
			log.Printf("[daemon] warn: parse inventory: %v", err)
			s.recordParseFailure(eventType, entry.Raw)
			payload = entry.JSON
		} else {
			payload = p
		}
	case "quest.progress":
		p, err := logreader.ParseQuestProgressEntry(entry)
		if err != nil {
			log.Printf("[daemon] warn: parse quest progress: %v", err)
			s.recordParseFailure(eventType, entry.Raw)
			payload = entry.JSON
		} else {
			payload = p
		}
	case "quest.completed":
		p, err := logreader.ParseQuestCompletedEntry(entry)
		if err != nil {
			log.Printf("[daemon] warn: parse quest completed: %v", err)
			s.recordParseFailure(eventType, entry.Raw)
			payload = entry.JSON
		} else {
			payload = p
		}
	case "collection.updated":
		p, err := logreader.ParseCollectionEntry(entry)
		if err != nil {
			log.Printf("[daemon] warn: parse collection: %v", err)
			s.recordParseFailure(eventType, entry.Raw)
			payload = entry.JSON
			break
		}

		h := collectionContentHash(p)

		// Historical-backlog coalescing: on a (re)install the daemon replays the
		// whole Player.log from byte 0, surfacing every past GetPlayerCardsV3
		// snapshot as a backlog entry. Hold only the latest one instead of
		// dispatching each — it is flushed when the first live entry arrives.
		if entry.FromBacklog {
			s.pendingBacklogCollection = p
			s.pendingBacklogHash = h
			return nil
		}

		// First live entry: flush any coalesced backlog snapshot so the BFF
		// learns the current collection exactly once, then fall through to dedup
		// the live entry against it.
		if s.pendingBacklogCollection != nil {
			if s.pendingBacklogHash != s.lastCollectionHash {
				if derr := s.dispatchCollection(ctx, s.pendingBacklogCollection); derr != nil {
					return derr
				}
				s.lastCollectionHash = s.pendingBacklogHash
			}
			s.pendingBacklogCollection = nil
			s.pendingBacklogHash = ""
		}

		// Dedup guard: skip dispatch when the snapshot is byte-for-byte the same
		// collection as the last one we sent (the rc3 idle-storm vector).
		if h == s.lastCollectionHash {
			return nil
		}
		s.lastCollectionHash = h
		payload = p
	case "deck.updated":
		p, err := logreader.ParseDeckEntry(entry)
		if err != nil {
			log.Printf("[daemon] warn: parse deck: %v", err)
			s.recordParseFailure(eventType, entry.Raw)
			payload = entry.JSON
		} else if p == nil {
			// nil, nil means the deck was silently skipped (e.g. precon deck).
			return nil
		} else {
			payload = p
		}
	case "player.authenticated":
		// Cache the local player's MTGA Arena client ID so subsequent
		// match.completed events can determine win/loss from reservedPlayers.
		// In 2026.59.20 the authenticateResponse contains clientId, sessionId,
		// and screenName — there is no accountId or userId key. clientId is the
		// join key: it equals reservedPlayers[].userId in match events.
		if resp, ok := entry.JSON["authenticateResponse"].(map[string]interface{}); ok {
			if uid, ok := resp["clientId"].(string); ok && uid != "" {
				s.mtgaUserID = uid
				log.Printf("[daemon] cached MTGA user ID from authenticateResponse")
			}
		}
		payload = entry.JSON

	case "course.deck_submitted":
		// Cache the deck UUID so it can be attached to the next match.completed.
		// Arena emits CourseDeck just before a match starts; the daemon holds the
		// most recent DeckId in memory and attaches it when match.completed fires.
		deckID, err := logreader.ParseCourseDeckEntry(entry)
		if err != nil {
			log.Printf("[daemon] warn: parse course deck: %v", err)
			s.recordParseFailure(eventType, entry.Raw)
		} else {
			s.lastDeckID = deckID
			log.Printf("[daemon] cached deck ID from CourseDeck: %s", deckID)
		}
		// course.deck_submitted is daemon-internal bookkeeping only.
		// Do not dispatch it to the BFF.
		return nil

	case "match.completed":
		p, err := logreader.ParseMatchCompletedEntry(entry, s.mtgaUserID)
		if err != nil {
			log.Printf("[daemon] warn: parse match completed: %v", err)
			s.recordParseFailure(eventType, entry.Raw)
			payload = entry.JSON
		} else {
			// Attach the cached deck ID when available. The deck ID was captured
			// from the most recent CourseDeck entry, which Arena emits just before
			// the match starts. Clear after attaching so a missed CourseDeck does
			// not spuriously link a stale deck to a future match.
			if s.lastDeckID != "" {
				p.DeckID = s.lastDeckID
				s.lastDeckID = ""
			}
			// Attach DraftSessionID when the active in-memory session's
			// CourseName matches the match Format (event_name) and the
			// session was updated within 48 hours.
			if s.draftState != nil {
				if sess, ok := s.draftState.Get("current"); ok {
					if sess.CourseName == p.Format &&
						time.Since(sess.UpdatedAt) < 48*time.Hour {
						id := sess.ID
						p.DraftSessionID = &id
					}
				}
			}

			// Flush the GRE session as a non-partial game-end event, anchored to
			// the authoritative finalMatchResult.matchId (#807). This is the
			// first-and-only writer of game_plays card-play rows (partial flushes
			// are discarded by the BFF projection worker), so it completes the
			// #2943 read-side chain and populates the Match-Detail timeline.
			// No-op when the session buffer is already empty (entries drained by
			// an earlier threshold flush).
			if p.MatchID != "" {
				if flushErr := s.greManager.FlushSession(ctx, s.sessionID, p.MatchID, false); flushErr != nil {
					log.Printf("[daemon] warn: match.completed GRE game-end flush match_id=%s: %v", p.MatchID, flushErr)
				}
			}

			payload = p
		}
	case "draft.started", "draft.completed":
		// Enrich scene-change events with the active draftstate session so the
		// BFF's projectDraftSession can identify the session (#1344 PR-B).
		//
		// Without enrichment both events fall through to entry.JSON
		// ({fromSceneName, toSceneName, context} — no session_id) and
		// projectDraftSession permanently rejects them. The practical result:
		//   • draft.started  — session never opened via this event path.
		//   • draft.completed — NO draft session ever transitions to
		//     status=completed; all users see a perpetual ACTIVE DRAFT card.
		//
		// When no session is active (daemon restarted before any pack event),
		// fall back to entry.JSON. The BFF will permanent-reject the entry
		// (existing guard), which is correct — there is nothing to close.
		if s.draftState != nil {
			if sess, ok := s.draftState.Get("current"); ok {
				payload = draftScenePayload{
					SessionID: sess.ID,
					EventName: sess.CourseName,
					SetCode:   sess.SetCode,
					DraftType: sess.Format,
				}
			}
		}
		if payload == nil {
			payload = entry.JSON
		}
	case "greToClientEvent":
		// GRE entries are never dispatched individually — they are buffered in
		// the GRE session manager and flushed as a single "match.game_ended"
		// event when the buffer reaches its threshold, on stale-sweep eviction,
		// or on shutdown.  Using entry.Raw preserves the original log line
		// exactly so logparse.ParseLine can re-parse it during flush.
		return s.greManager.Append(ctx, s.sessionID, json.RawMessage(entry.Raw))
	default:
		payload = entry.JSON
	}

	evt, err := dispatch.BuildEvent(eventType, s.cfg.AccountID, s.sessionID, payload)
	if err != nil {
		return fmt.Errorf("build event: %w", err)
	}

	// Enqueue into the batch buffer (ADR-053).  The buffer assigns the sequence
	// number, coalesces events into size=25 / 750ms batches, and flushes via
	// dispatcher.SendBatch.  Add is non-blocking — the actual HTTP send is
	// handled by the batch buffer's background goroutine.
	s.batchBuffer.Add(evt)

	// Boundary events: force an immediate flush so the BFF receives the batch
	// promptly without waiting for the next size or interval trigger.
	// match.game_ended  — game result just produced (player expects UI update).
	// draft.pick        — pick made; grade-pick / win-probability must see it fast.
	// draft.started     — session opens; SPA draft panel must activate immediately.
	// draft.completed   — session closes; Home ACTIVE DRAFT card must clear promptly.
	switch eventType {
	case "match.game_ended", "draft.pick", "draft.started", "draft.completed":
		s.batchBuffer.FlushNow()
	}

	// NOTE: ErrReauthRequired is no longer surfaced here because Add is
	// non-blocking.  The Dispatcher's onBFFFailure callback still fires on
	// terminal failures; 401 recovery is handled inside the keychainRefresherAdapter
	// wired to the Dispatcher in New().
	return nil
}

// collectionContentHash returns a stable SHA-256 hex digest of a collection
// snapshot's contents (sorted arena_id:count pairs). Two snapshots with the same
// set of cards and counts produce the same hash regardless of map iteration
// order, which is what the dedup guard relies on.
func collectionContentHash(p *contract.CollectionUpdatedPayload) string {
	if p == nil {
		return ""
	}
	pairs := make([]string, 0, len(p.Cards))
	for _, c := range p.Cards {
		pairs = append(pairs, strconv.Itoa(c.ArenaID)+":"+strconv.Itoa(c.Count))
	}
	sort.Strings(pairs)
	sum := sha256.Sum256([]byte(strings.Join(pairs, ",")))
	return hex.EncodeToString(sum[:])
}

// dispatchCollection builds and sends a collection.updated event for the given
// payload. It mirrors the dispatch tail of handleEntry but is reused for the
// flushed backlog snapshot. Reauth handling matches handleEntry's contract.
func (s *Service) dispatchCollection(ctx context.Context, p *contract.CollectionUpdatedPayload) error {
	evt, err := dispatch.BuildEvent("collection.updated", s.cfg.AccountID, s.sessionID, p)
	if err != nil {
		return fmt.Errorf("build collection event: %w", err)
	}
	dispatchCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := s.dispatcher.SendOrBuffer(dispatchCtx, evt); err != nil {
		if errors.Is(err, dispatch.ErrReauthRequired) {
			go s.dispatchAuthFailed(context.Background(), "bff_rejected")
			return nil
		}
		return err
	}
	return nil
}

// classifyEntry maps a log entry to a semantic event type string.
// Returns "" if the entry is not a tracked event.
//
// This is a package-level shim so tests in package daemon can call
// classifyEntry without change. All logic lives in internal/classify.
func classifyEntry(entry *logreader.LogEntry) string {
	return classify.ClassifyEntry(entry)
}
