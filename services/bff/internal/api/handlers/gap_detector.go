// Package handlers provides HTTP request handlers for the BFF service.
package handlers

import (
	"fmt"
	"sync"
)

// GapDetector tracks the last seen sequence number per (account_id, session_id)
// pair and detects gaps in the monotonically-increasing sequence emitted by the
// daemon (ADR-013).
//
// It is in-process only — no database or Redis.  State resets on BFF restart,
// which is acceptable for this observability signal.
type GapDetector struct {
	mu sync.Map // key: "accountID:sessionID" → uint64 (last seen sequence)
}

// gapKey returns the map key for the given account and session pair.
func gapKey(accountID, sessionID string) string {
	return fmt.Sprintf("%s:%s", accountID, sessionID)
}

// Check evaluates seq against the last seen sequence for the given pair.
//
// Three outcomes:
//   - First event for the key: stores seq, returns isGap=false.
//   - Sequential (seq == last+1): updates stored value, returns isGap=false.
//   - Sequence reset (seq < last): treats as new session start, updates stored
//     value, returns isGap=false — NOT logged as a gap.
//   - Gap (seq > last+1): returns isGap=true and the expected value (last+1);
//     stored value is updated so the next event is evaluated from seq, not last.
func (d *GapDetector) Check(accountID, sessionID string, seq uint64) (isGap bool, expected uint64) {
	key := gapKey(accountID, sessionID)

	val, loaded := d.mu.Load(key)
	if !loaded {
		// First event for this pair — establish the baseline.
		d.mu.Store(key, seq)
		return false, 0
	}

	last, _ := val.(uint64)

	switch {
	case seq < last:
		// Sequence reset — treat as new session start, update baseline.
		d.mu.Store(key, seq)
		return false, 0

	case seq == last+1:
		// Sequential — normal case.
		d.mu.Store(key, seq)
		return false, 0

	case seq == last:
		// Duplicate — not a gap, but also not advancing.  Update stored so a
		// subsequent gap relative to this duplicate is still detected.
		d.mu.Store(key, seq)
		return false, 0

	default:
		// seq > last+1 → gap detected.
		d.mu.Store(key, seq)
		return true, last + 1
	}
}
