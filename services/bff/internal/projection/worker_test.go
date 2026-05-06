package projection

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ramonehamilton/mtga-bff/internal/storage/repository"
)

// --- fakes ---

type fakeEventStore struct {
	pending    []repository.DaemonEventRow
	projected  []int64
	projectErr error
}

func (f *fakeEventStore) ListPendingProjection(_ context.Context, limit int) ([]repository.DaemonEventRow, error) {
	if limit < len(f.pending) {
		return f.pending[:limit], nil
	}
	return f.pending, nil
}

func (f *fakeEventStore) MarkProjected(_ context.Context, id int64) error {
	if f.projectErr != nil {
		return f.projectErr
	}
	f.projected = append(f.projected, id)
	return nil
}

type fakeAccountStore struct {
	accountID int64
	err       error
}

func (f *fakeAccountStore) GetOrCreateByClientID(_ context.Context, _ string, _ int64) (int64, error) {
	return f.accountID, f.err
}

type fakeMatchStore struct {
	upserts []repository.MatchUpsert
	err     error
}

func (f *fakeMatchStore) UpsertMatch(_ context.Context, m repository.MatchUpsert) error {
	if f.err != nil {
		return f.err
	}
	f.upserts = append(f.upserts, m)
	return nil
}

type fakeDraftStore struct {
	upserts []repository.DraftSessionUpsert
	err     error
}

func (f *fakeDraftStore) UpsertDraftSession(_ context.Context, s repository.DraftSessionUpsert) error {
	if f.err != nil {
		return f.err
	}
	f.upserts = append(f.upserts, s)
	return nil
}

// --- helpers ---

func makePayload(t *testing.T, v interface{}) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return b
}

func newWorker(events *fakeEventStore, accounts *fakeAccountStore, matches *fakeMatchStore, drafts *fakeDraftStore) *Worker {
	return NewWorker(events, accounts, matches, drafts)
}

// --- tests ---

func TestRunOnce_MatchCompleted_ProjectsToMatches(t *testing.T) {
	payload := makePayload(t, map[string]interface{}{
		"match_id":       "match-001",
		"event_id":       "evt_abc",
		"event_name":     "Standard_BO1",
		"format":         "Standard",
		"result":         "win",
		"player_wins":    2,
		"opponent_wins":  1,
		"player_team_id": 0,
	})

	events := &fakeEventStore{
		pending: []repository.DaemonEventRow{
			{ID: 1, UserID: 1, AccountID: "acct-1", EventType: "match.completed", Payload: payload, OccurredAt: time.Now()},
		},
	}
	accounts := &fakeAccountStore{accountID: 10}
	matches := &fakeMatchStore{}
	drafts := &fakeDraftStore{}

	w := newWorker(events, accounts, matches, drafts)
	w.RunOnce(context.Background())

	if len(matches.upserts) != 1 {
		t.Fatalf("expected 1 match upsert, got %d", len(matches.upserts))
	}
	if matches.upserts[0].ID != "match-001" {
		t.Errorf("expected match ID match-001, got %q", matches.upserts[0].ID)
	}
	if len(events.projected) != 1 || events.projected[0] != 1 {
		t.Errorf("expected row 1 marked projected, got %v", events.projected)
	}
}

func TestRunOnce_DraftStarted_ProjectsToDraftSessions(t *testing.T) {
	payload := makePayload(t, map[string]interface{}{
		"session_id": "draft-001",
		"event_name": "QuickDraft_EOE",
		"set_code":   "EOE",
		"draft_type": "quick_draft",
		"status":     "in_progress",
	})

	events := &fakeEventStore{
		pending: []repository.DaemonEventRow{
			{ID: 2, UserID: 1, AccountID: "acct-1", EventType: "draft.started", Payload: payload, OccurredAt: time.Now()},
		},
	}
	accounts := &fakeAccountStore{accountID: 10}
	matches := &fakeMatchStore{}
	drafts := &fakeDraftStore{}

	w := newWorker(events, accounts, matches, drafts)
	w.RunOnce(context.Background())

	if len(drafts.upserts) != 1 {
		t.Fatalf("expected 1 draft upsert, got %d", len(drafts.upserts))
	}
	if drafts.upserts[0].ID != "draft-001" {
		t.Errorf("expected session ID draft-001, got %q", drafts.upserts[0].ID)
	}
	if len(events.projected) != 1 {
		t.Errorf("expected 1 row marked projected")
	}
}

func TestRunOnce_MalformedPayload_MarkedProjectedNoDestinationRow(t *testing.T) {
	events := &fakeEventStore{
		pending: []repository.DaemonEventRow{
			{
				ID: 3, UserID: 1, AccountID: "acct-1", EventType: "match.completed",
				Payload: json.RawMessage(`{"bad":"shape"}`), OccurredAt: time.Now(),
			},
		},
	}
	accounts := &fakeAccountStore{accountID: 10}
	matches := &fakeMatchStore{}
	drafts := &fakeDraftStore{}

	w := newWorker(events, accounts, matches, drafts)
	w.RunOnce(context.Background())

	// Row must be marked projected even though payload was bad.
	if len(events.projected) != 1 || events.projected[0] != 3 {
		t.Errorf("malformed row must be marked projected; got %v", events.projected)
	}
	// No match must have been written.
	if len(matches.upserts) != 0 {
		t.Errorf("expected 0 match upserts for malformed payload, got %d", len(matches.upserts))
	}
}

func TestRunOnce_UnknownEventType_MarkedProjected(t *testing.T) {
	events := &fakeEventStore{
		pending: []repository.DaemonEventRow{
			{
				ID: 4, UserID: 1, AccountID: "acct-1", EventType: "sync.collection",
				Payload: json.RawMessage(`{}`), OccurredAt: time.Now(),
			},
		},
	}
	accounts := &fakeAccountStore{accountID: 10}
	matches := &fakeMatchStore{}
	drafts := &fakeDraftStore{}

	w := newWorker(events, accounts, matches, drafts)
	w.RunOnce(context.Background())

	if len(events.projected) != 1 || events.projected[0] != 4 {
		t.Errorf("unknown event must be marked projected; got %v", events.projected)
	}
}

func TestRunOnce_Idempotent_SecondRunNoNewRows(t *testing.T) {
	payload := makePayload(t, map[string]interface{}{
		"match_id":       "match-idem",
		"event_id":       "evt_idem",
		"event_name":     "Standard_BO1",
		"format":         "Standard",
		"result":         "win",
		"player_wins":    2,
		"opponent_wins":  1,
		"player_team_id": 0,
	})

	row := repository.DaemonEventRow{
		ID: 5, UserID: 1, AccountID: "acct-1", EventType: "match.completed",
		Payload: payload, OccurredAt: time.Now(),
	}

	events := &fakeEventStore{pending: []repository.DaemonEventRow{row}}
	accounts := &fakeAccountStore{accountID: 10}
	matches := &fakeMatchStore{}
	drafts := &fakeDraftStore{}

	w := newWorker(events, accounts, matches, drafts)

	// First run — projects the row.
	w.RunOnce(context.Background())
	firstCount := len(matches.upserts)

	// Clear pending so the second run sees nothing new (simulates projected_at being set).
	events.pending = nil

	// Second run — nothing pending, so no additional upserts.
	w.RunOnce(context.Background())

	if len(matches.upserts) != firstCount {
		t.Errorf("second runOnce produced additional upserts; first=%d total=%d", firstCount, len(matches.upserts))
	}
}

func TestRunOnce_MixedTypes_AllMarkedProjected(t *testing.T) {
	matchPayload := makePayload(t, map[string]interface{}{
		"match_id":       "m1",
		"event_id":       "e1",
		"event_name":     "Standard",
		"format":         "Standard",
		"result":         "loss",
		"player_wins":    1,
		"opponent_wins":  2,
		"player_team_id": 0,
	})
	draftPayload := makePayload(t, map[string]interface{}{
		"session_id": "d1",
		"event_name": "QuickDraft",
		"set_code":   "BRO",
		"draft_type": "quick_draft",
		"status":     "in_progress",
	})

	events := &fakeEventStore{
		pending: []repository.DaemonEventRow{
			{ID: 10, UserID: 1, AccountID: "a", EventType: "match.completed", Payload: matchPayload, OccurredAt: time.Now()},
			{ID: 11, UserID: 1, AccountID: "a", EventType: "draft.started", Payload: draftPayload, OccurredAt: time.Now()},
			{ID: 12, UserID: 1, AccountID: "a", EventType: "unknown.type", Payload: json.RawMessage(`{}`), OccurredAt: time.Now()},
		},
	}
	accounts := &fakeAccountStore{accountID: 10}
	matches := &fakeMatchStore{}
	drafts := &fakeDraftStore{}

	w := newWorker(events, accounts, matches, drafts)
	w.RunOnce(context.Background())

	if len(events.projected) != 3 {
		t.Errorf("expected all 3 rows projected, got %d: %v", len(events.projected), events.projected)
	}
	if len(matches.upserts) != 1 {
		t.Errorf("expected 1 match upsert, got %d", len(matches.upserts))
	}
	if len(drafts.upserts) != 1 {
		t.Errorf("expected 1 draft upsert, got %d", len(drafts.upserts))
	}
}
