package projection

// Tests for #1353 — transient retry for projectMatch draft_session_id linkage.
//
// Bug: when match.completed arrives before draft.started, SessionExists returns
// false and the draft_session_id linkage is permanently dropped ("ignoring
// DraftSessionID"). Fix: return transient() so the row stays pending and retries
// after draft.started projects. Same TTL/DLQ ceiling as #1340.
//
// AC1: out-of-order match.completed → event left pending (not marked projected)
// AC2: after draft.started projects (SessionExists → true), retry links correctly
// AC3: in-order arrival (draft.started before match.completed) still links — no regression
// AC4: "ignoring DraftSessionID" silent-drop path replaced with deferral/retry
// AC5: unit test covers the out-of-order path (this file)
// TTL: received_at TTL ceiling → escalates to DLQ (same RC2 as #1340)

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/RdHamilton/hollowmark/services/bff/internal/storage/repository"
)

// makeMatchCompletedPayloadWithDraftSession returns a match.completed JSON
// payload whose draft_session_id field is set to draftSessionID.
func makeMatchCompletedPayloadWithDraftSession(t *testing.T, matchID, draftSessionID string) json.RawMessage {
	t.Helper()
	p := map[string]interface{}{
		"match_id":         matchID,
		"event_id":         "evt-draft-link",
		"event_name":       "QuickDraft_EOE",
		"format":           "draft",
		"result":           "win",
		"player_wins":      2,
		"opponent_wins":    1,
		"player_team_id":   1,
		"draft_session_id": draftSessionID,
	}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("makeMatchCompletedPayloadWithDraftSession: %v", err)
	}
	return b
}

// fakeDraftStoreControlled extends fakeDraftStore with per-call control for
// SessionExists. existsSeq[i] / existsErrSeq[i] returned on i-th call;
// last entry repeated when calls exceed slice length.
// Embeds *fakeDraftStore (pointer) so pointer-receiver methods are promoted.
type fakeDraftStoreControlled struct {
	*fakeDraftStore
	existsCalls  int
	existsSeq    []bool  // exists return value per call
	existsErrSeq []error // error per call (nil = no error)
}

func (f *fakeDraftStoreControlled) SessionExists(_ context.Context, _ int64, _ string) (bool, error) {
	i := f.existsCalls
	if i >= len(f.existsSeq) {
		i = len(f.existsSeq) - 1
	}
	f.existsCalls++
	var err error
	if i < len(f.existsErrSeq) {
		err = f.existsErrSeq[i]
	}
	return f.existsSeq[i], err
}

// ---------------------------------------------------------------------------
// AC1 / AC5: out-of-order arrival → event left pending
// ---------------------------------------------------------------------------

// TestProjectMatch_DraftSessionNotYetProjected_EventLeftPending is the primary
// regression test for #1353.
//
// BEFORE fix: SessionExists → false → "ignoring DraftSessionID" log → match
// written without linkage → MarkProjected called → linkage gone forever.
//
// AFTER fix: SessionExists → false → return transient() → MarkProjected NOT
// called → row stays pending → retries after draft.started projects.
func TestProjectMatch_DraftSessionNotYetProjected_EventLeftPending(t *testing.T) {
	now := time.Now().UTC()
	const draftSessionID = "draft-sess-001"
	payload := makeMatchCompletedPayloadWithDraftSession(t, "match-ooo-001", draftSessionID)

	events := &fakeEventStore{
		pending: []repository.DaemonEventRow{
			{
				ID:         700,
				UserID:     1,
				AccountID:  "acct-ooo",
				EventType:  "match.completed",
				Payload:    payload,
				OccurredAt: now,
				ReceivedAt: now,
				Sequence:   1,
			},
		},
	}
	accounts := &fakeAccountStore{accountID: 50}
	matches := &fakeMatchStore{}
	// SessionExists returns false — draft.started has not yet been projected.
	drafts := &fakeDraftStoreControlled{
		fakeDraftStore: &fakeDraftStore{},
		existsSeq:      []bool{false},
	}

	w := NewWorker(events, accounts, matches, drafts, &fakeCollectionStore{}, &fakeInventoryStore{}, &fakeQuestStore{}, &fakeDeckStore{}, &fakeGamePlayStore{})
	w.RunOnce(context.Background())

	// CRITICAL: MarkProjected must NOT have been called — row stays pending.
	if len(events.projected) != 0 {
		t.Errorf("out-of-order match.completed must leave event pending (MarkProjected must NOT be called); got projected=%v", events.projected)
	}

	// The match itself must NOT have been written while the event is deferred.
	if len(matches.upserts) != 0 {
		t.Errorf("deferred match.completed must not write match row until linkage confirmed; got %d upserts", len(matches.upserts))
	}
}

// ---------------------------------------------------------------------------
// AC2: retry after draft.started projects → linkage applied
// ---------------------------------------------------------------------------

// TestProjectMatch_DraftSessionExistsOnRetry_LinksCorrectly verifies the
// two-pass scenario:
//
//	Pass 1: SessionExists → false → event stays pending.
//	Pass 2: SessionExists → true (draft.started now projected) → match written
//	        with draft_session_id set + row marked projected.
func TestProjectMatch_DraftSessionExistsOnRetry_LinksCorrectly(t *testing.T) {
	now := time.Now().UTC()
	const draftSessionID = "draft-sess-002"
	payload := makeMatchCompletedPayloadWithDraftSession(t, "match-retry-link-001", draftSessionID)

	row := repository.DaemonEventRow{
		ID:         701,
		UserID:     1,
		AccountID:  "acct-retry-link",
		EventType:  "match.completed",
		Payload:    payload,
		OccurredAt: now,
		ReceivedAt: now,
		Sequence:   2,
	}

	events := &fakeEventStore{pending: []repository.DaemonEventRow{row}}
	accounts := &fakeAccountStore{accountID: 51}
	matches := &fakeMatchStore{}
	// Call 0: false (draft not yet projected). Call 1: true (draft now projected).
	drafts := &fakeDraftStoreControlled{
		fakeDraftStore: &fakeDraftStore{},
		existsSeq:      []bool{false, true},
	}

	w := NewWorker(events, accounts, matches, drafts, &fakeCollectionStore{}, &fakeInventoryStore{}, &fakeQuestStore{}, &fakeDeckStore{}, &fakeGamePlayStore{})

	// Pass 1: SessionExists → false → event stays pending.
	w.RunOnce(context.Background())
	if len(events.projected) != 0 {
		t.Fatalf("pass 1: event must stay pending when session not yet projected; got projected=%v", events.projected)
	}

	// Pass 2: SessionExists → true → match written with draft_session_id.
	w.RunOnce(context.Background())
	if len(events.projected) != 1 || events.projected[0] != 701 {
		t.Errorf("pass 2: event must be marked projected after session exists; got %v", events.projected)
	}
	if len(matches.upserts) != 1 {
		t.Fatalf("pass 2: match upsert must happen after successful retry; got %d", len(matches.upserts))
	}
	if matches.upserts[0].DraftSessionID == nil || *matches.upserts[0].DraftSessionID != draftSessionID {
		t.Errorf("pass 2: match must carry draft_session_id=%q; got %v", draftSessionID, matches.upserts[0].DraftSessionID)
	}
}

// ---------------------------------------------------------------------------
// AC3: in-order arrival regression guard
// ---------------------------------------------------------------------------

// TestProjectMatch_DraftSessionExistsImmediately_LinksOnFirstPass verifies
// that the happy path (draft.started projected before match.completed) still
// works correctly after the fix. No retry needed; event projected in one pass.
func TestProjectMatch_DraftSessionExistsImmediately_LinksOnFirstPass(t *testing.T) {
	now := time.Now().UTC()
	const draftSessionID = "draft-sess-003"
	payload := makeMatchCompletedPayloadWithDraftSession(t, "match-inorder-001", draftSessionID)

	events := &fakeEventStore{
		pending: []repository.DaemonEventRow{
			{
				ID:         702,
				UserID:     1,
				AccountID:  "acct-inorder",
				EventType:  "match.completed",
				Payload:    payload,
				OccurredAt: now,
				ReceivedAt: now,
				Sequence:   3,
			},
		},
	}
	accounts := &fakeAccountStore{accountID: 52}
	matches := &fakeMatchStore{}
	// Session already exists — in-order arrival.
	drafts := &fakeDraftStoreControlled{
		fakeDraftStore: &fakeDraftStore{},
		existsSeq:      []bool{true},
	}

	w := NewWorker(events, accounts, matches, drafts, &fakeCollectionStore{}, &fakeInventoryStore{}, &fakeQuestStore{}, &fakeDeckStore{}, &fakeGamePlayStore{})
	w.RunOnce(context.Background())

	// Single pass: match written with draft_session_id and row marked projected.
	if len(events.projected) != 1 || events.projected[0] != 702 {
		t.Errorf("in-order: event must be marked projected in one pass; got %v", events.projected)
	}
	if len(matches.upserts) != 1 {
		t.Fatalf("in-order: match must be written; got %d upserts", len(matches.upserts))
	}
	if matches.upserts[0].DraftSessionID == nil || *matches.upserts[0].DraftSessionID != draftSessionID {
		t.Errorf("in-order: match must carry draft_session_id=%q; got %v", draftSessionID, matches.upserts[0].DraftSessionID)
	}
}

// ---------------------------------------------------------------------------
// AC3: no-draft match (nil DraftSessionID) unaffected
// ---------------------------------------------------------------------------

// TestProjectMatch_NoDraftSessionID_ProjectsNormally verifies that a
// match.completed with no draft_session_id field is unaffected by the fix.
func TestProjectMatch_NoDraftSessionID_ProjectsNormally(t *testing.T) {
	payload := makePayload(t, map[string]interface{}{
		"match_id":       "match-nodraft-001",
		"event_id":       "evt-nodraft",
		"event_name":     "Standard_BO1",
		"format":         "Standard",
		"result":         "win",
		"player_wins":    2,
		"opponent_wins":  1,
		"player_team_id": 1,
	})

	events := &fakeEventStore{
		pending: []repository.DaemonEventRow{
			{
				ID:         703,
				UserID:     1,
				AccountID:  "acct-nodraft",
				EventType:  "match.completed",
				Payload:    payload,
				OccurredAt: time.Now().UTC(),
				ReceivedAt: time.Now().UTC(),
				Sequence:   4,
			},
		},
	}
	accounts := &fakeAccountStore{accountID: 53}
	matches := &fakeMatchStore{}
	// existsSeq irrelevant — SessionExists must not be called for non-draft matches.
	drafts := &fakeDraftStoreControlled{
		fakeDraftStore: &fakeDraftStore{},
		existsSeq:      []bool{false},
	}

	w := NewWorker(events, accounts, matches, drafts, &fakeCollectionStore{}, &fakeInventoryStore{}, &fakeQuestStore{}, &fakeDeckStore{}, &fakeGamePlayStore{})
	w.RunOnce(context.Background())

	if len(events.projected) != 1 || events.projected[0] != 703 {
		t.Errorf("no-draft match must be projected normally; got %v", events.projected)
	}
	if len(matches.upserts) != 1 {
		t.Errorf("no-draft match must produce 1 upsert; got %d", len(matches.upserts))
	}
	if matches.upserts[0].DraftSessionID != nil {
		t.Errorf("no-draft match must have nil DraftSessionID; got %v", matches.upserts[0].DraftSessionID)
	}
}

// ---------------------------------------------------------------------------
// TTL: received_at TTL exceeded → escalate to DLQ (same RC2 pattern as #1340)
// ---------------------------------------------------------------------------

// TestProjectMatch_DraftSessionMissing_ReceivedAtTTLExceeded_EscalatesToDLQ
// verifies that a transient-pending match.completed whose received_at exceeds
// the 24h ceiling is escalated to the DLQ rather than left pending indefinitely.
func TestProjectMatch_DraftSessionMissing_ReceivedAtTTLExceeded_EscalatesToDLQ(t *testing.T) {
	oldOccurredAt := time.Now().UTC().Add(-72 * time.Hour) // 3 days old
	oldReceivedAt := time.Now().UTC().Add(-25 * time.Hour) // >24h → TTL fires

	const draftSessionID = "draft-sess-ttl"
	payload := makeMatchCompletedPayloadWithDraftSession(t, "match-ttl-draft-001", draftSessionID)

	events := &fakeEventStore{
		pending: []repository.DaemonEventRow{
			{
				ID:         704,
				UserID:     1,
				AccountID:  "acct-ttl-draft",
				EventType:  "match.completed",
				Payload:    payload,
				OccurredAt: oldOccurredAt,
				ReceivedAt: oldReceivedAt,
				Sequence:   5,
			},
		},
	}
	accounts := &fakeAccountStore{accountID: 54}
	matches := &fakeMatchStore{}
	// Session never exists — simulates permanently lost draft.started.
	drafts := &fakeDraftStoreControlled{
		fakeDraftStore: &fakeDraftStore{},
		existsSeq:      []bool{false},
	}
	dlq := &fakeDLQStore{}

	w := NewWorker(events, accounts, matches, drafts, &fakeCollectionStore{}, &fakeInventoryStore{}, &fakeQuestStore{}, &fakeDeckStore{}, &fakeGamePlayStore{})
	w.WithDLQ(dlq)
	w.RunOnce(context.Background())

	// TTL exceeded → escalate via DLQ path → row IS marked projected.
	if len(events.projected) != 1 || events.projected[0] != 704 {
		t.Errorf("TTL-exceeded event must be marked projected (DLQ path); got %v", events.projected)
	}
	if len(dlq.inserts) != 1 {
		t.Errorf("TTL-exceeded event must produce 1 DLQ insert; got %d", len(dlq.inserts))
	}
}

// TestProjectMatch_DraftSessionMissing_ReceivedAtFresh_NotDLQd mirrors RC2 from
// #1340: daemon replay has old occurred_at but fresh received_at — must retry,
// not be DLQ'd.
func TestProjectMatch_DraftSessionMissing_ReceivedAtFresh_NotDLQd(t *testing.T) {
	oldOccurredAt := time.Now().UTC().Add(-72 * time.Hour)    // 3-day-old daemon replay
	freshReceivedAt := time.Now().UTC().Add(-5 * time.Minute) // just received

	const draftSessionID = "draft-sess-fresh"
	payload := makeMatchCompletedPayloadWithDraftSession(t, "match-ttl-fresh-draft-001", draftSessionID)

	events := &fakeEventStore{
		pending: []repository.DaemonEventRow{
			{
				ID:         705,
				UserID:     1,
				AccountID:  "acct-ttl-fresh-draft",
				EventType:  "match.completed",
				Payload:    payload,
				OccurredAt: oldOccurredAt,
				ReceivedAt: freshReceivedAt,
				Sequence:   6,
			},
		},
	}
	accounts := &fakeAccountStore{accountID: 55}
	matches := &fakeMatchStore{}
	drafts := &fakeDraftStoreControlled{
		fakeDraftStore: &fakeDraftStore{},
		existsSeq:      []bool{false},
	}
	dlq := &fakeDLQStore{}

	w := NewWorker(events, accounts, matches, drafts, &fakeCollectionStore{}, &fakeInventoryStore{}, &fakeQuestStore{}, &fakeDeckStore{}, &fakeGamePlayStore{})
	w.WithDLQ(dlq)
	w.RunOnce(context.Background())

	// Fresh received_at → must NOT DLQ, must stay pending.
	if len(events.projected) != 0 {
		t.Errorf("fresh received_at must stay pending; got projected=%v", events.projected)
	}
	if len(dlq.inserts) != 0 {
		t.Errorf("fresh received_at must NOT be DLQd; got %d DLQ inserts", len(dlq.inserts))
	}
}

// ---------------------------------------------------------------------------
// No regression on #1340: game_plays (match.game_ended) transient path intact
// ---------------------------------------------------------------------------

// TestProjectGamePlayEvent_FKViolation_StillTransientAfterMatchFix confirms
// the existing #1340 transient path for match.game_ended is unaffected by the
// #1353 fix. Both projectors independently return transient().
func TestProjectGamePlayEvent_FKViolation_StillTransientAfterMatchFix(t *testing.T) {
	now := time.Now().UTC()
	payload := makeGameEndedPayload(t, "match-cross-001", 1)

	events := &fakeEventStore{
		pending: []repository.DaemonEventRow{
			{
				ID:         706,
				UserID:     1,
				AccountID:  "acct-cross",
				EventType:  "match.game_ended",
				Payload:    payload,
				OccurredAt: now,
				ReceivedAt: now,
				Sequence:   7,
			},
		},
	}
	accounts := &fakeAccountStore{accountID: 56}
	gp := &fakeGamePlayStoreCapturing{}
	cardPlays := &fakeCardPlayStoreCapturing{}
	gameRows := &fakeGameRowWriterTransient{
		errors: []error{makeFKViolationError()},
	}

	w := NewWorker(events, accounts, &fakeMatchStore{}, &fakeDraftStoreControlled{fakeDraftStore: &fakeDraftStore{}, existsSeq: []bool{true}},
		&fakeCollectionStore{}, &fakeInventoryStore{}, &fakeQuestStore{}, &fakeDeckStore{}, gp)
	w.WithCardPlayStore(cardPlays)
	w.WithGameRowWriter(gameRows)

	w.RunOnce(context.Background())

	// FK violation on game_ended → event stays pending (same as #1340).
	if len(events.projected) != 0 {
		t.Errorf("#1340 path must still leave game_ended pending on FK violation; got projected=%v", events.projected)
	}
}
