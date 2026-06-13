package projection

// Tests for #1340 — transient FK retry (RC1–RC5).
//
// RC1: keyset pagination — ≥100 transient rows must NOT starve newer rows
// RC2: TTL base = received_at (not occurred_at) — 24h ceiling → DLQ
// RC3: 23503 detected via errors.As+pgconn.PgError, not string match
// RC4: outcomeRetryTransient counted + PostHog metric emitted
// RC5: ResetProjected method on daemonEventStore interface

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/posthog/posthog-go"

	"github.com/RdHamilton/hollowmark/services/bff/internal/storage/repository"
)

// ---------------------------------------------------------------------------
// Fakes specific to transient tests
// ---------------------------------------------------------------------------

// fakeGameRowWriterTransient controls UpsertGameRow return values per-call.
// errors[i] returned on call i; last entry repeated.
type fakeGameRowWriterTransient struct {
	calls   int
	errors  []error
	gameIDs []int64 // returned on success; defaults to 1
}

func (f *fakeGameRowWriterTransient) UpsertGameRow(_ context.Context, _ string, _ int, _ string) (int64, error) {
	i := f.calls
	if i >= len(f.errors) {
		i = len(f.errors) - 1
	}
	f.calls++
	if err := f.errors[i]; err != nil {
		return 0, err
	}
	id := int64(1)
	if i < len(f.gameIDs) {
		id = f.gameIDs[i]
	}
	return id, nil
}

// fakeGameRowWriterAlwaysFKViolation always returns a FK violation from UpsertGameRow.
type fakeGameRowWriterAlwaysFKViolation struct{}

func (f *fakeGameRowWriterAlwaysFKViolation) UpsertGameRow(_ context.Context, _ string, _ int, _ string) (int64, error) {
	return 0, makeFKViolationError()
}

// fakePagedEventStore models a keyset-paginated pending-events store used for RC1.
// ListPendingProjection returns up to limit rows from the first.
// ListPendingProjectionAfter returns rows after (afterTime, afterID).
// MarkProjected and ResetProjected maintain a projected set.
type fakePagedEventStore struct {
	allRows    []repository.DaemonEventRow
	projected  []int64
	projectErr error
}

func (f *fakePagedEventStore) ListPendingProjection(_ context.Context, limit int) ([]repository.DaemonEventRow, error) {
	return f.pageFrom(time.Time{}, 0, limit)
}

func (f *fakePagedEventStore) ListPendingProjectionAfter(_ context.Context, afterTime time.Time, afterID int64, limit int) ([]repository.DaemonEventRow, error) {
	return f.pageFrom(afterTime, afterID, limit)
}

func (f *fakePagedEventStore) pageFrom(afterTime time.Time, afterID int64, limit int) ([]repository.DaemonEventRow, error) {
	var result []repository.DaemonEventRow
	for i := range f.allRows {
		r := f.allRows[i]
		if f.isProjected(r.ID) {
			continue
		}
		if afterTime.IsZero() || r.ReceivedAt.After(afterTime) ||
			(r.ReceivedAt.Equal(afterTime) && r.ID > afterID) {
			result = append(result, r)
			if len(result) == limit {
				break
			}
		}
	}
	return result, nil
}

func (f *fakePagedEventStore) isProjected(id int64) bool {
	for _, pid := range f.projected {
		if pid == id {
			return true
		}
	}
	return false
}

func (f *fakePagedEventStore) MarkProjected(_ context.Context, id int64) error {
	if f.projectErr != nil {
		return f.projectErr
	}
	f.projected = append(f.projected, id)
	return nil
}

func (f *fakePagedEventStore) ResetProjected(_ context.Context, id int64) error {
	out := f.projected[:0]
	for _, pid := range f.projected {
		if pid != id {
			out = append(out, pid)
		}
	}
	f.projected = out
	return nil
}

// fakeEventStoreWithReset extends fakeEventStore with ResetProjected so it
// satisfies the updated daemonEventStore interface (RC5).
type fakeEventStoreWithReset struct {
	fakeEventStore
}

func (f *fakeEventStoreWithReset) ResetProjected(_ context.Context, id int64) error {
	out := f.projected[:0]
	for _, pid := range f.projected {
		if pid != id {
			out = append(out, pid)
		}
	}
	f.projected = out
	return nil
}

// makeFKViolationError returns a pgconn.PgError{Code:"23503"} wrapped in a
// fmt.Errorf chain — the exact shape pgx/v5 produces for FK violations.
func makeFKViolationError() error {
	return fmt.Errorf("UpsertGameRow: %w", &pgconn.PgError{
		Code:           "23503",
		ConstraintName: "games_match_id_fkey",
		Message:        `insert or update on table "games" violates foreign key constraint "games_match_id_fkey"`,
	})
}

// makeGameEndedPayload returns a match.game_ended JSON payload with card_plays
// so the gameRows/cardPlays code path (UpsertGameRow + InsertCardPlays) fires.
// arena_id must be an int (contract.CardPlayEntry.ArenaID is int).
func makeGameEndedPayload(t *testing.T, matchID string, gameNumber int) json.RawMessage {
	t.Helper()
	p := map[string]interface{}{
		"match_id":        matchID,
		"game_number":     gameNumber,
		"winning_team_id": 1,
		"turn_count":      5,
		"duration_secs":   60,
		"life_changes":    []interface{}{},
		"card_plays": []map[string]interface{}{
			{"arena_id": 12345, "turn_number": 1, "phase": "main1", "player_type": "player", "action_type": "play_card", "zone_from": "hand", "zone_to": "battlefield"},
		},
	}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("makeGameEndedPayload: %v", err)
	}
	return b
}

// ---------------------------------------------------------------------------
// RC3: isFKViolation helper (errors.As + pgconn.PgError)
// ---------------------------------------------------------------------------

// TestIsFKViolation_pgconn23503_DetectedViaErrorsAs verifies that a wrapped
// pgconn.PgError{Code:"23503"} is correctly identified and that a plain error
// with "23503" in the message is NOT (must use errors.As, not string matching).
// RED: isFKViolation does not exist yet.
func TestIsFKViolation_pgconn23503_DetectedViaErrorsAs(t *testing.T) {
	fkErr := makeFKViolationError()
	if !isFKViolation(fkErr) {
		t.Error("isFKViolation: want true for pgconn.PgError{Code:23503}, got false")
	}

	// Plain error with "23503" in text must NOT trigger detection.
	plainErr := fmt.Errorf("error code 23503 in message")
	if isFKViolation(plainErr) {
		t.Error("isFKViolation: want false for plain error with 23503 in text (must use errors.As, not strings)")
	}

	// Unrelated pgconn error must NOT trigger detection.
	otherPgErr := fmt.Errorf("wrapped: %w", &pgconn.PgError{Code: "25001"})
	if isFKViolation(otherPgErr) {
		t.Error("isFKViolation: want false for pgconn error with unrelated code")
	}
}

// ---------------------------------------------------------------------------
// transientErr sentinel
// ---------------------------------------------------------------------------

// TestTransient_WrapAndIsTransient verifies the transientErr sentinel mirrors
// the permanentErr pattern.
// RED: transientErr / transient() / isTransient() do not exist yet.
func TestTransient_WrapAndIsTransient(t *testing.T) {
	inner := fmt.Errorf("fk violation")
	err := transient(inner)

	if !isTransient(err) {
		t.Error("isTransient: want true for wrapped error, got false")
	}
	if err.Error() != "fk violation" {
		t.Errorf("Error(): want %q, got %q", "fk violation", err.Error())
	}
}

func TestTransient_NilIsNil(t *testing.T) {
	if transient(nil) != nil {
		t.Error("transient(nil) must return nil")
	}
}

func TestIsTransient_PlainError_ReturnsFalse(t *testing.T) {
	if isTransient(fmt.Errorf("plain")) {
		t.Error("isTransient: want false for plain error")
	}
}

func TestIsTransient_PermanentError_ReturnsFalse(t *testing.T) {
	if isTransient(permanent(fmt.Errorf("perm"))) {
		t.Error("isTransient: want false for permanentErr")
	}
}

// ---------------------------------------------------------------------------
// Core fix: FK violation → event left pending (not marked projected)
// ---------------------------------------------------------------------------

// TestRunOnce_GamePlayEvent_FKViolation_EventLeftPending is the primary
// regression test. Before fix: MarkProjected called unconditionally → event
// silently dropped. After fix: MarkProjected NOT called → row stays pending.
// RED: projectRow calls MarkProjected unconditionally.
func TestRunOnce_GamePlayEvent_FKViolation_EventLeftPending(t *testing.T) {
	now := time.Now().UTC()
	payload := makeGameEndedPayload(t, "match-fk-001", 1)

	events := &fakeEventStoreWithReset{
		fakeEventStore: fakeEventStore{
			pending: []repository.DaemonEventRow{
				{
					ID: 500, UserID: 1, AccountID: "acct-fk",
					EventType: "match.game_ended", Payload: payload,
					OccurredAt: now, ReceivedAt: now, Sequence: 1,
				},
			},
		},
	}
	accounts := &fakeAccountStore{accountID: 10}
	gp := &fakeGamePlayStoreCapturing{}
	// Using the existing fakeCardPlayStoreCapturing from worker_test.go
	cardPlays := &fakeCardPlayStoreCapturing{}
	gameRows := &fakeGameRowWriterTransient{
		errors: []error{makeFKViolationError()},
	}

	w := NewWorker(events, accounts, &fakeMatchStore{}, &fakeDraftStore{},
		&fakeCollectionStore{}, &fakeInventoryStore{}, &fakeQuestStore{}, &fakeDeckStore{}, gp)
	w.WithCardPlayStore(cardPlays)
	w.WithGameRowWriter(gameRows)

	w.RunOnce(context.Background())

	// CRITICAL: MarkProjected must NOT have been called — row stays pending.
	if len(events.projected) != 0 {
		t.Errorf("FK violation must leave event pending (MarkProjected must NOT be called); got projected=%v", events.projected)
	}
	// InsertGamePlay (match_game_results) must have succeeded before UpsertGameRow.
	if len(gp.gamePlayInserts) != 1 {
		t.Errorf("InsertGamePlay (match_game_results) must succeed before FK violation; got %d calls", len(gp.gamePlayInserts))
	}
	// InsertCardPlays must NOT have been called (UpsertGameRow failed).
	if len(cardPlays.calls) != 0 {
		t.Errorf("InsertCardPlays must not be called when UpsertGameRow FK-fails; got %d calls", len(cardPlays.calls))
	}
}

// TestRunOnce_GamePlayEvent_FKViolation_ThenSucceedsOnRetry verifies the
// two-pass scenario: first RunOnce leaves pending, second RunOnce (after
// match.completed projects the matches row) succeeds and marks projected.
// RED: first pass currently marks projected → second pass never happens.
func TestRunOnce_GamePlayEvent_FKViolation_ThenSucceedsOnRetry(t *testing.T) {
	now := time.Now().UTC()
	payload := makeGameEndedPayload(t, "match-retry-001", 1)

	row := repository.DaemonEventRow{
		ID: 501, UserID: 1, AccountID: "acct-retry",
		EventType: "match.game_ended", Payload: payload,
		OccurredAt: now, ReceivedAt: now, Sequence: 2,
	}

	events := &fakeEventStoreWithReset{
		fakeEventStore: fakeEventStore{pending: []repository.DaemonEventRow{row}},
	}
	accounts := &fakeAccountStore{accountID: 11}
	gp := &fakeGamePlayStoreCapturing{}
	cardPlays := &fakeCardPlayStoreCapturing{}
	gameRows := &fakeGameRowWriterTransient{
		// Call 0: FK violation. Call 1: success returning game ID 42.
		errors:  []error{makeFKViolationError(), nil},
		gameIDs: []int64{0, 42},
	}

	w := NewWorker(events, accounts, &fakeMatchStore{}, &fakeDraftStore{},
		&fakeCollectionStore{}, &fakeInventoryStore{}, &fakeQuestStore{}, &fakeDeckStore{}, gp)
	w.WithCardPlayStore(cardPlays)
	w.WithGameRowWriter(gameRows)

	// Pass 1: FK violation → event stays pending.
	w.RunOnce(context.Background())
	if len(events.projected) != 0 {
		t.Fatalf("pass 1: event must stay pending after FK violation; got projected=%v", events.projected)
	}

	// Pass 2: UpsertGameRow succeeds → row marked projected + card plays written.
	w.RunOnce(context.Background())
	if len(events.projected) != 1 || events.projected[0] != 501 {
		t.Errorf("pass 2: event must be marked projected after successful retry; got %v", events.projected)
	}
	if len(cardPlays.calls) != 1 {
		t.Errorf("pass 2: InsertCardPlays must be called once after successful UpsertGameRow; got %d", len(cardPlays.calls))
	}
}

// TestRunOnce_GamePlayEvent_NonFKError_StillMarksProjected verifies that a
// non-23503 error from UpsertGameRow does NOT leave the event pending — it is
// still marked projected (existing behaviour preserved for non-FK errors so
// we don't introduce an infinite retry loop for random transient DB errors).
func TestRunOnce_GamePlayEvent_NonFKError_StillMarksProjected(t *testing.T) {
	now := time.Now().UTC()
	payload := makeGameEndedPayload(t, "match-nonfk-001", 1)

	events := &fakeEventStoreWithReset{
		fakeEventStore: fakeEventStore{
			pending: []repository.DaemonEventRow{
				{
					ID: 502, UserID: 1, AccountID: "acct-nonfk",
					EventType: "match.game_ended", Payload: payload,
					OccurredAt: now, ReceivedAt: now, Sequence: 3,
				},
			},
		},
	}
	accounts := &fakeAccountStore{accountID: 12}
	gp := &fakeGamePlayStoreCapturing{}
	gameRows := &fakeGameRowWriterTransient{
		// A generic non-FK error (connection reset, constraint violation from a
		// different constraint, etc.).
		errors: []error{fmt.Errorf("connection reset by peer")},
	}

	w := NewWorker(events, accounts, &fakeMatchStore{}, &fakeDraftStore{},
		&fakeCollectionStore{}, &fakeInventoryStore{}, &fakeQuestStore{}, &fakeDeckStore{}, gp)
	w.WithGameRowWriter(gameRows)

	w.RunOnce(context.Background())

	// Non-FK error → still mark projected (no infinite retry loop).
	if len(events.projected) != 1 || events.projected[0] != 502 {
		t.Errorf("non-FK error must still mark projected; got %v", events.projected)
	}
}

// ---------------------------------------------------------------------------
// RC2: TTL base = received_at, NOT occurred_at
// ---------------------------------------------------------------------------

// TestRunOnce_GamePlayEvent_ReceivedAtTTLExceeded_EscalatesToDLQ verifies that
// a FK-transient event whose received_at exceeds the 24h ceiling is escalated
// to the DLQ rather than left pending indefinitely.
// RED: no TTL check exists yet.
func TestRunOnce_GamePlayEvent_ReceivedAtTTLExceeded_EscalatesToDLQ(t *testing.T) {
	oldOccurredAt := time.Now().UTC().Add(-72 * time.Hour) // 3 days old
	oldReceivedAt := time.Now().UTC().Add(-25 * time.Hour) // >24h → TTL fires

	payload := makeGameEndedPayload(t, "match-ttl-001", 1)

	events := &fakeEventStoreWithReset{
		fakeEventStore: fakeEventStore{
			pending: []repository.DaemonEventRow{
				{
					ID: 503, UserID: 1, AccountID: "acct-ttl",
					EventType:  "match.game_ended",
					Payload:    payload,
					OccurredAt: oldOccurredAt,
					ReceivedAt: oldReceivedAt,
					Sequence:   4,
				},
			},
		},
	}
	accounts := &fakeAccountStore{accountID: 13}
	gp := &fakeGamePlayStoreCapturing{}
	gameRows := &fakeGameRowWriterTransient{
		errors: []error{makeFKViolationError()},
	}
	dlq := &fakeDLQStore{}
	ph := &fakePostHogClient{}

	w := NewWorker(events, accounts, &fakeMatchStore{}, &fakeDraftStore{},
		&fakeCollectionStore{}, &fakeInventoryStore{}, &fakeQuestStore{}, &fakeDeckStore{}, gp)
	// cardPlays must be wired so UpsertGameRow (and thus the FK violation) is reached.
	w.WithCardPlayStore(&fakeCardPlayStoreCapturing{})
	w.WithGameRowWriter(gameRows)
	w.WithDLQ(dlq)
	w.WithPostHogClient(ph)

	w.RunOnce(context.Background())

	// TTL exceeded → escalate via DLQ path → row IS marked projected.
	if len(events.projected) != 1 || events.projected[0] != 503 {
		t.Errorf("TTL-exceeded event must be marked projected (DLQ path); got %v", events.projected)
	}
	if len(dlq.inserts) != 1 {
		t.Errorf("TTL-exceeded event must produce 1 DLQ insert; got %d", len(dlq.inserts))
	}
}

// TestRunOnce_GamePlayEvent_OccurredAtOld_ReceivedAtFresh_NotDLQd verifies RC2:
// TTL uses received_at, NOT occurred_at. A daemon replay sends old occurred_at
// but fresh received_at — the event must retry, not be DLQd.
// RED: if TTL checked occurred_at, this would incorrectly DLQ a live event.
func TestRunOnce_GamePlayEvent_OccurredAtOld_ReceivedAtFresh_NotDLQd(t *testing.T) {
	oldOccurredAt := time.Now().UTC().Add(-72 * time.Hour)    // 3-day-old daemon replay
	freshReceivedAt := time.Now().UTC().Add(-5 * time.Minute) // just received

	payload := makeGameEndedPayload(t, "match-ttl-fresh-001", 1)

	events := &fakeEventStoreWithReset{
		fakeEventStore: fakeEventStore{
			pending: []repository.DaemonEventRow{
				{
					ID: 504, UserID: 1, AccountID: "acct-ttl-fresh",
					EventType:  "match.game_ended",
					Payload:    payload,
					OccurredAt: oldOccurredAt,
					ReceivedAt: freshReceivedAt,
					Sequence:   5,
				},
			},
		},
	}
	accounts := &fakeAccountStore{accountID: 14}
	gp := &fakeGamePlayStoreCapturing{}
	gameRows := &fakeGameRowWriterTransient{
		errors: []error{makeFKViolationError()},
	}
	dlq := &fakeDLQStore{}

	w := NewWorker(events, accounts, &fakeMatchStore{}, &fakeDraftStore{},
		&fakeCollectionStore{}, &fakeInventoryStore{}, &fakeQuestStore{}, &fakeDeckStore{}, gp)
	// cardPlays must be wired so UpsertGameRow (and thus the FK violation) is reached.
	w.WithCardPlayStore(&fakeCardPlayStoreCapturing{})
	w.WithGameRowWriter(gameRows)
	w.WithDLQ(dlq)

	w.RunOnce(context.Background())

	// Fresh received_at → must NOT DLQ, must stay pending.
	if len(events.projected) != 0 {
		t.Errorf("fresh received_at FK event must stay pending; got projected=%v", events.projected)
	}
	if len(dlq.inserts) != 0 {
		t.Errorf("fresh received_at FK event must NOT be DLQd; got %d DLQ inserts", len(dlq.inserts))
	}
}

// ---------------------------------------------------------------------------
// RC4: outcomeRetryTransient + PostHog metric
// ---------------------------------------------------------------------------

// TestRunOnce_GamePlayEvent_FKViolation_EmitsRetryTransientMetric verifies that
// a transient FK violation emits a projection.retry_transient PostHog metric
// with a hashed account_id (never raw PII).
// RED: no outcomeRetryTransient or metric exists yet.
func TestRunOnce_GamePlayEvent_FKViolation_EmitsRetryTransientMetric(t *testing.T) {
	now := time.Now().UTC()
	payload := makeGameEndedPayload(t, "match-metric-001", 1)

	events := &fakeEventStoreWithReset{
		fakeEventStore: fakeEventStore{
			pending: []repository.DaemonEventRow{
				{
					ID: 505, UserID: 1, AccountID: "acct-metric",
					EventType: "match.game_ended", Payload: payload,
					OccurredAt: now, ReceivedAt: now, Sequence: 6,
				},
			},
		},
	}
	accounts := &fakeAccountStore{accountID: 15}
	gp := &fakeGamePlayStoreCapturing{}
	gameRows := &fakeGameRowWriterTransient{
		errors: []error{makeFKViolationError()},
	}
	ph := &fakePostHogClient{}

	w := NewWorker(events, accounts, &fakeMatchStore{}, &fakeDraftStore{},
		&fakeCollectionStore{}, &fakeInventoryStore{}, &fakeQuestStore{}, &fakeDeckStore{}, gp)
	// cardPlays must be wired so UpsertGameRow (and thus the FK violation) is reached.
	w.WithCardPlayStore(&fakeCardPlayStoreCapturing{})
	w.WithGameRowWriter(gameRows)
	w.WithPostHogClient(ph)

	w.RunOnce(context.Background())

	// Exactly one PostHog event must be emitted.
	if len(ph.captured) != 1 {
		t.Fatalf("want 1 PostHog event for transient retry, got %d", len(ph.captured))
	}
	cap, ok := ph.captured[0].(posthog.Capture)
	if !ok {
		t.Fatalf("expected posthog.Capture, got %T", ph.captured[0])
	}
	if cap.Event != "projection.retry_transient" {
		t.Errorf("Event: want projection.retry_transient, got %q", cap.Event)
	}
	if cap.DistinctId == "" {
		t.Error("DistinctId must not be empty (must be hashed account_id)")
	}
	if _, has := cap.Properties["account_id_hash"]; !has {
		t.Error("Properties must include account_id_hash")
	}
	if _, has := cap.Properties["account_id"]; has {
		t.Error("Properties must NOT include raw account_id (PII)")
	}
	if _, has := cap.Properties["event_type"]; !has {
		t.Error("Properties must include event_type")
	}
}

// ---------------------------------------------------------------------------
// RC1: keyset pagination — starvation guard
// ---------------------------------------------------------------------------

// TestRunOnce_KeysetPagination_TransientRowsDoNotStarveNewerRows verifies that
// when exactly batchSize (100) transient-pending game_ended rows queue ahead of
// a match.completed row, the worker pages past them within a single tick and
// still projects the match.completed row.
//
// Before fix: runOnce fetches one page of 100 → ID 101 never seen → livelock.
// After fix:  runOnce keyset-paginates past transient rows → ID 101 projected.
// RED: current runOnce fetches a single fixed batch.
func TestRunOnce_KeysetPagination_TransientRowsDoNotStarveNewerRows(t *testing.T) {
	now := time.Now().UTC()

	// 100 transient-pending game_ended rows (all will FK-violate).
	allRows := make([]repository.DaemonEventRow, 0, 101)
	for i := 1; i <= 100; i++ {
		payload := makeGameEndedPayload(t, fmt.Sprintf("match-%03d", i), 1)
		allRows = append(allRows, repository.DaemonEventRow{
			ID:         int64(i),
			UserID:     1,
			AccountID:  "acct-starve",
			EventType:  "match.game_ended",
			Payload:    payload,
			OccurredAt: now.Add(time.Duration(i) * time.Second),
			ReceivedAt: now.Add(time.Duration(i) * time.Second),
			Sequence:   uint64(i),
		})
	}

	// Row 101: match.completed — placed AFTER the 100 stuck rows (received_at > all others).
	matchPayload := makePayload(t, map[string]interface{}{
		"match_id":       "match-001",
		"event_id":       "evt-resolving",
		"event_name":     "Standard_BO1",
		"format":         "Standard",
		"result":         "win",
		"player_wins":    2,
		"opponent_wins":  1,
		"player_team_id": 1,
	})
	allRows = append(allRows, repository.DaemonEventRow{
		ID:         101,
		UserID:     1,
		AccountID:  "acct-starve",
		EventType:  "match.completed",
		Payload:    matchPayload,
		OccurredAt: now.Add(101 * time.Second),
		ReceivedAt: now.Add(101 * time.Second),
		Sequence:   101,
	})

	store := &fakePagedEventStore{allRows: allRows}
	accounts := &fakeAccountStore{accountID: 20}
	matches := &fakeMatchStore{}
	gp := &fakeGamePlayStoreCapturing{}
	gameRows := &fakeGameRowWriterAlwaysFKViolation{}

	w := NewWorker(store, accounts, matches, &fakeDraftStore{},
		&fakeCollectionStore{}, &fakeInventoryStore{}, &fakeQuestStore{}, &fakeDeckStore{}, gp)
	// cardPlays must be wired so UpsertGameRow (and thus the FK violation) is reached.
	w.WithCardPlayStore(&fakeCardPlayStoreCapturing{})
	w.WithGameRowWriter(gameRows)

	w.RunOnce(context.Background())

	// match.completed (ID=101) must have been projected in the same tick.
	matchProjected := false
	for _, id := range store.projected {
		if id == 101 {
			matchProjected = true
			break
		}
	}
	if !matchProjected {
		t.Errorf("keyset pagination: match.completed (id=101) must be projected in same tick with 100 transient rows ahead; projected=%v", store.projected)
	}

	// The 100 transient rows must NOT be marked projected (they stay pending).
	for _, id := range store.projected {
		if id >= 1 && id <= 100 {
			t.Errorf("keyset: transient row id=%d must NOT be projected; projected=%v", id, store.projected)
			break
		}
	}
}

// ---------------------------------------------------------------------------
// RC5: ResetProjected on daemonEventStore interface
// ---------------------------------------------------------------------------

// TestDaemonEventStore_ResetProjectedInterface verifies that daemonEventStore
// declares ResetProjected and that the updated fakes satisfy it.
// RED: daemonEventStore interface does not include ResetProjected yet.
func TestDaemonEventStore_ResetProjectedInterface(t *testing.T) {
	// Compile-time check: if daemonEventStore does not include ResetProjected,
	// this line will not compile (type assertion on interface).
	var _ daemonEventStore = &fakeEventStoreWithReset{}
	var _ daemonEventStore = &fakePagedEventStore{}
}

// TestResetProjected_ResetsToQueryable verifies that after MarkProjected then
// ResetProjected, the row is no longer in the projected set (projected_at=NULL).
func TestResetProjected_ResetsToQueryable(t *testing.T) {
	store := &fakeEventStoreWithReset{
		fakeEventStore: fakeEventStore{
			pending: []repository.DaemonEventRow{
				{ID: 600, EventType: "match.game_ended"},
			},
		},
	}

	if err := store.MarkProjected(context.Background(), 600); err != nil {
		t.Fatalf("MarkProjected: %v", err)
	}
	if len(store.projected) != 1 {
		t.Fatalf("want 1 projected after MarkProjected, got %d", len(store.projected))
	}

	if err := store.ResetProjected(context.Background(), 600); err != nil {
		t.Fatalf("ResetProjected: %v", err)
	}
	for _, id := range store.projected {
		if id == 600 {
			t.Error("after ResetProjected, row must not appear in projected set")
		}
	}
}
