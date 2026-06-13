package projection

// worker_periodic_test.go — TDD tests for #1344: projectPeriodicUpdated must
// call UpsertPeriodicWins with daily_wins and weekly_wins from the payload;
// projectMasteryUpdated must call UpsertMastery from a standalone mastery.updated
// event (emitted by the daemon alongside inventory.updated).

import (
	"context"
	"testing"
	"time"

	"github.com/RdHamilton/hollowmark/services/bff/internal/storage/repository"
	"github.com/RdHamilton/hollowmark/services/contract"
)

// fakePeriodicStore captures calls to UpsertPeriodicWins for assertion.
type fakePeriodicStore struct {
	upserts []repository.PeriodicWinsUpsert
	err     error
}

func (f *fakePeriodicStore) UpsertPeriodicWins(_ context.Context, u repository.PeriodicWinsUpsert) error {
	if f.err != nil {
		return f.err
	}
	f.upserts = append(f.upserts, u)
	return nil
}

// TestPeriodicUpdated_WritesAccountColumns verifies that a periodic.updated event
// causes the worker to call UpsertPeriodicWins with daily_wins=4 / weekly_wins=7
// (#1344: the Quests page must read these authoritative MTGA values).
func TestPeriodicUpdated_WritesAccountColumns(t *testing.T) {
	payload := makePayload(t, contract.PeriodicUpdatedPayload{
		DailyWins:  4,
		WeeklyWins: 7,
	})

	events := &fakeEventStore{
		pending: []repository.DaemonEventRow{
			{ID: 100, UserID: 1, AccountID: "acct-periodic", EventType: "periodic.updated", Payload: payload, OccurredAt: time.Now()},
		},
	}
	accounts := &fakeAccountStore{accountID: 88}
	periodicStore := &fakePeriodicStore{}

	w := NewWorker(events, accounts, &fakeMatchStore{}, &fakeDraftStore{}, &fakeCollectionStore{}, &fakeInventoryStore{}, &fakeQuestStore{}, &fakeCombinedDeckStore{}, &fakeGamePlayStore{})
	w.WithPeriodicStore(periodicStore)
	w.RunOnce(context.Background())

	if len(periodicStore.upserts) != 1 {
		t.Fatalf("expected 1 UpsertPeriodicWins call, got %d", len(periodicStore.upserts))
	}
	u := periodicStore.upserts[0]
	if u.AccountID != 88 {
		t.Errorf("UpsertPeriodicWins AccountID: got %d, want 88", u.AccountID)
	}
	if u.DailyWins != 4 {
		t.Errorf("UpsertPeriodicWins DailyWins: got %d, want 4", u.DailyWins)
	}
	if u.WeeklyWins != 7 {
		t.Errorf("UpsertPeriodicWins WeeklyWins: got %d, want 7", u.WeeklyWins)
	}
}

// TestPeriodicUpdated_ZeroWins verifies that daily_wins=0 / weekly_wins=0 are
// written (a period reset must write 0, not be silently dropped).
func TestPeriodicUpdated_ZeroWins(t *testing.T) {
	payload := makePayload(t, contract.PeriodicUpdatedPayload{
		DailyWins:  0,
		WeeklyWins: 0,
	})

	events := &fakeEventStore{
		pending: []repository.DaemonEventRow{
			{ID: 101, UserID: 1, AccountID: "acct-periodic-zero", EventType: "periodic.updated", Payload: payload, OccurredAt: time.Now()},
		},
	}
	accounts := &fakeAccountStore{accountID: 89}
	periodicStore := &fakePeriodicStore{}

	w := NewWorker(events, accounts, &fakeMatchStore{}, &fakeDraftStore{}, &fakeCollectionStore{}, &fakeInventoryStore{}, &fakeQuestStore{}, &fakeCombinedDeckStore{}, &fakeGamePlayStore{})
	w.WithPeriodicStore(periodicStore)
	w.RunOnce(context.Background())

	if len(periodicStore.upserts) != 1 {
		t.Fatalf("expected 1 UpsertPeriodicWins call for zero reset, got %d", len(periodicStore.upserts))
	}
	u := periodicStore.upserts[0]
	if u.DailyWins != 0 || u.WeeklyWins != 0 {
		t.Errorf("expected zero wins, got daily=%d weekly=%d", u.DailyWins, u.WeeklyWins)
	}
}

// TestPeriodicUpdated_RowProjected verifies that the daemon_events row is marked
// projected after a successful UpsertPeriodicWins.
func TestPeriodicUpdated_RowProjected(t *testing.T) {
	payload := makePayload(t, contract.PeriodicUpdatedPayload{DailyWins: 2, WeeklyWins: 5})

	events := &fakeEventStore{
		pending: []repository.DaemonEventRow{
			{ID: 102, UserID: 1, AccountID: "acct-periodic-proj", EventType: "periodic.updated", Payload: payload, OccurredAt: time.Now()},
		},
	}
	accounts := &fakeAccountStore{accountID: 90}
	periodicStore := &fakePeriodicStore{}

	w := NewWorker(events, accounts, &fakeMatchStore{}, &fakeDraftStore{}, &fakeCollectionStore{}, &fakeInventoryStore{}, &fakeQuestStore{}, &fakeCombinedDeckStore{}, &fakeGamePlayStore{})
	w.WithPeriodicStore(periodicStore)
	w.RunOnce(context.Background())

	if len(events.projected) != 1 || events.projected[0] != 102 {
		t.Errorf("expected row 102 to be marked projected, got %v", events.projected)
	}
}

// TestMasteryUpdated_CallsUpsertMastery verifies that a standalone mastery.updated
// event calls UpsertMastery once with the correct fields.
func TestMasteryUpdated_CallsUpsertMastery(t *testing.T) {
	payload := makePayload(t, contract.MasteryUpdatedPayload{
		Level:    18,
		PassType: "Standard",
		Max:      80,
	})

	events := &fakeEventStore{
		pending: []repository.DaemonEventRow{
			{ID: 103, UserID: 1, AccountID: "acct-mastery-standalone", EventType: "mastery.updated", Payload: payload, OccurredAt: time.Now()},
		},
	}
	accounts := &fakeAccountStore{accountID: 91}
	masteryStore := &fakeMasteryStore{}

	w := NewWorker(events, accounts, &fakeMatchStore{}, &fakeDraftStore{}, &fakeCollectionStore{}, &fakeInventoryStore{}, &fakeQuestStore{}, &fakeCombinedDeckStore{}, &fakeGamePlayStore{})
	w.WithMasteryStore(masteryStore)
	w.RunOnce(context.Background())

	if len(masteryStore.upserts) != 1 {
		t.Fatalf("expected 1 UpsertMastery call from mastery.updated, got %d", len(masteryStore.upserts))
	}
	u := masteryStore.upserts[0]
	if u.AccountID != 91 {
		t.Errorf("UpsertMastery AccountID: got %d, want 91", u.AccountID)
	}
	if u.MasteryLevel != 18 {
		t.Errorf("UpsertMastery MasteryLevel: got %d, want 18", u.MasteryLevel)
	}
	if u.MasteryPass != "Standard" {
		t.Errorf("UpsertMastery MasteryPass: got %q, want Standard", u.MasteryPass)
	}
	if u.MasteryMax != 80 {
		t.Errorf("UpsertMastery MasteryMax: got %d, want 80", u.MasteryMax)
	}
}

// TestMasteryUpdated_RowProjected verifies that the daemon_events row is marked
// projected after a successful standalone mastery.updated projection.
func TestMasteryUpdated_RowProjected(t *testing.T) {
	payload := makePayload(t, contract.MasteryUpdatedPayload{Level: 5, PassType: "Basic", Max: 80})

	events := &fakeEventStore{
		pending: []repository.DaemonEventRow{
			{ID: 104, UserID: 1, AccountID: "acct-mastery-proj", EventType: "mastery.updated", Payload: payload, OccurredAt: time.Now()},
		},
	}
	accounts := &fakeAccountStore{accountID: 92}
	masteryStore := &fakeMasteryStore{}

	w := NewWorker(events, accounts, &fakeMatchStore{}, &fakeDraftStore{}, &fakeCollectionStore{}, &fakeInventoryStore{}, &fakeQuestStore{}, &fakeCombinedDeckStore{}, &fakeGamePlayStore{})
	w.WithMasteryStore(masteryStore)
	w.RunOnce(context.Background())

	if len(events.projected) != 1 || events.projected[0] != 104 {
		t.Errorf("expected row 104 projected, got %v", events.projected)
	}
}

// TestMasteryUpdated_NoMasteryStore_MarkedProjected verifies that when the
// mastery store is not wired, a mastery.updated event is still marked projected
// (graceful no-op, not a hard fail).
func TestMasteryUpdated_NoMasteryStore_MarkedProjected(t *testing.T) {
	payload := makePayload(t, contract.MasteryUpdatedPayload{Level: 1, PassType: "Basic", Max: 80})

	events := &fakeEventStore{
		pending: []repository.DaemonEventRow{
			{ID: 105, UserID: 1, AccountID: "acct-mastery-nowire", EventType: "mastery.updated", Payload: payload, OccurredAt: time.Now()},
		},
	}
	accounts := &fakeAccountStore{accountID: 93}

	// Do NOT wire a mastery store.
	w := NewWorker(events, accounts, &fakeMatchStore{}, &fakeDraftStore{}, &fakeCollectionStore{}, &fakeInventoryStore{}, &fakeQuestStore{}, &fakeCombinedDeckStore{}, &fakeGamePlayStore{})
	w.RunOnce(context.Background())

	// Row must be marked projected (event accepted, mastery write skipped).
	if len(events.projected) != 1 || events.projected[0] != 105 {
		t.Errorf("expected row 105 projected even with no mastery store, got %v", events.projected)
	}
}
