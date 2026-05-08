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

type fakeCollectionStore struct {
	upserts []repository.CardInventoryUpsert
	err     error
}

func (f *fakeCollectionStore) UpsertDelta(_ context.Context, u repository.CardInventoryUpsert) error {
	if f.err != nil {
		return f.err
	}
	f.upserts = append(f.upserts, u)
	return nil
}

type fakeInventoryStore struct {
	err error
}

func (f *fakeInventoryStore) UpsertInventory(_ context.Context, _ repository.InventoryUpsert) error {
	return f.err
}

type fakeQuestStore struct {
	err error
}

func (f *fakeQuestStore) UpsertQuestProgress(_ context.Context, _ repository.QuestProgressUpsert) error {
	return f.err
}

func (f *fakeQuestStore) InsertQuestCompleted(_ context.Context, _ repository.QuestCompletedInsert) error {
	return f.err
}

type fakeDeckStore struct {
	err error
}

func (f *fakeDeckStore) UpsertDeck(_ context.Context, _ repository.DeckUpsert) error {
	return f.err
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
	return NewWorker(events, accounts, matches, drafts, &fakeCollectionStore{}, &fakeInventoryStore{}, &fakeQuestStore{}, &fakeDeckStore{})
}

func newWorkerWithCollection(events *fakeEventStore, accounts *fakeAccountStore, collection *fakeCollectionStore) *Worker {
	return NewWorker(events, accounts, &fakeMatchStore{}, &fakeDraftStore{}, collection, &fakeInventoryStore{}, &fakeQuestStore{}, &fakeDeckStore{})
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

// --- collection.updated tests ---

func TestRunOnce_CollectionUpdated_ProjectsToInventory(t *testing.T) {
	payload := makePayload(t, map[string]interface{}{
		"cards": []map[string]interface{}{
			{"arena_id": 100001, "count": 4},
			{"arena_id": 100002, "count": 2},
		},
		"is_delta": false,
	})

	events := &fakeEventStore{
		pending: []repository.DaemonEventRow{
			{ID: 20, UserID: 1, AccountID: "acct-col", EventType: "collection.updated", Payload: payload, OccurredAt: time.Now()},
		},
	}
	accounts := &fakeAccountStore{accountID: 42}
	collection := &fakeCollectionStore{}

	w := newWorkerWithCollection(events, accounts, collection)
	w.RunOnce(context.Background())

	if len(collection.upserts) != 2 {
		t.Fatalf("expected 2 card upserts, got %d", len(collection.upserts))
	}
	if collection.upserts[0].CardID != 100001 || collection.upserts[0].Count != 4 {
		t.Errorf("unexpected first upsert: %+v", collection.upserts[0])
	}
	if collection.upserts[1].CardID != 100002 || collection.upserts[1].Count != 2 {
		t.Errorf("unexpected second upsert: %+v", collection.upserts[1])
	}
	// All upserts must carry the same snapshot_hash.
	if collection.upserts[0].SnapshotHash == "" {
		t.Error("snapshot_hash must not be empty")
	}
	if collection.upserts[0].SnapshotHash != collection.upserts[1].SnapshotHash {
		t.Errorf("snapshot_hash must be consistent across cards in one event; got %q vs %q",
			collection.upserts[0].SnapshotHash, collection.upserts[1].SnapshotHash)
	}
	// Row must be marked projected.
	if len(events.projected) != 1 || events.projected[0] != 20 {
		t.Errorf("expected row 20 marked projected, got %v", events.projected)
	}
}

func TestRunOnce_CollectionUpdated_AccountIDScoped(t *testing.T) {
	payload := makePayload(t, map[string]interface{}{
		"cards":    []map[string]interface{}{{"arena_id": 200001, "count": 1}},
		"is_delta": true,
	})

	events := &fakeEventStore{
		pending: []repository.DaemonEventRow{
			{ID: 21, UserID: 5, AccountID: "acct-scoped", EventType: "collection.updated", Payload: payload, OccurredAt: time.Now()},
		},
	}
	accounts := &fakeAccountStore{accountID: 99}
	collection := &fakeCollectionStore{}

	w := newWorkerWithCollection(events, accounts, collection)
	w.RunOnce(context.Background())

	if len(collection.upserts) != 1 {
		t.Fatalf("expected 1 upsert, got %d", len(collection.upserts))
	}
	if collection.upserts[0].AccountID != 99 {
		t.Errorf("expected account_id=99, got %d", collection.upserts[0].AccountID)
	}
}

func TestRunOnce_CollectionUpdated_EmptyCards_NoUpsert(t *testing.T) {
	payload := makePayload(t, map[string]interface{}{
		"cards":    []map[string]interface{}{},
		"is_delta": true,
	})

	events := &fakeEventStore{
		pending: []repository.DaemonEventRow{
			{ID: 22, UserID: 1, AccountID: "acct-empty", EventType: "collection.updated", Payload: payload, OccurredAt: time.Now()},
		},
	}
	accounts := &fakeAccountStore{accountID: 10}
	collection := &fakeCollectionStore{}

	w := newWorkerWithCollection(events, accounts, collection)
	w.RunOnce(context.Background())

	if len(collection.upserts) != 0 {
		t.Errorf("expected 0 upserts for empty cards, got %d", len(collection.upserts))
	}
	// Must still be marked projected.
	if len(events.projected) != 1 || events.projected[0] != 22 {
		t.Errorf("expected row 22 marked projected, got %v", events.projected)
	}
}

func TestRunOnce_CollectionUpdated_IdempotentSamePayload(t *testing.T) {
	payload := makePayload(t, map[string]interface{}{
		"cards":    []map[string]interface{}{{"arena_id": 300001, "count": 3}},
		"is_delta": false,
	})

	row := repository.DaemonEventRow{
		ID: 23, UserID: 1, AccountID: "acct-idem", EventType: "collection.updated",
		Payload: payload, OccurredAt: time.Now(),
	}

	events := &fakeEventStore{pending: []repository.DaemonEventRow{row}}
	accounts := &fakeAccountStore{accountID: 10}
	collection := &fakeCollectionStore{}

	w := newWorkerWithCollection(events, accounts, collection)

	// First run.
	w.RunOnce(context.Background())
	firstCount := len(collection.upserts)

	// Reset pending to simulate the same event being re-queued (e.g. daemon retry).
	events.pending = []repository.DaemonEventRow{row}
	events.projected = nil

	// Second run with the same payload.
	w.RunOnce(context.Background())

	// The fake store always accepts; idempotency is enforced by the DB ON CONFLICT.
	// Here we just verify the worker calls UpsertDelta again (DB handles dedup).
	if len(collection.upserts) != firstCount*2 {
		t.Errorf("expected %d total upserts after two runs, got %d", firstCount*2, len(collection.upserts))
	}
	// Snapshot hashes must be identical across both runs.
	if collection.upserts[0].SnapshotHash != collection.upserts[firstCount].SnapshotHash {
		t.Errorf("snapshot_hash must be deterministic; run1=%q run2=%q",
			collection.upserts[0].SnapshotHash, collection.upserts[firstCount].SnapshotHash)
	}
}

func TestRunOnce_CollectionUpdated_MalformedPayload_MarkedProjected(t *testing.T) {
	events := &fakeEventStore{
		pending: []repository.DaemonEventRow{
			{
				ID: 24, UserID: 1, AccountID: "acct-bad", EventType: "collection.updated",
				Payload: json.RawMessage(`not-json`), OccurredAt: time.Now(),
			},
		},
	}
	accounts := &fakeAccountStore{accountID: 10}
	collection := &fakeCollectionStore{}

	w := newWorkerWithCollection(events, accounts, collection)
	w.RunOnce(context.Background())

	if len(events.projected) != 1 || events.projected[0] != 24 {
		t.Errorf("malformed row must be marked projected; got %v", events.projected)
	}
	if len(collection.upserts) != 0 {
		t.Errorf("expected 0 upserts for malformed payload, got %d", len(collection.upserts))
	}
}
