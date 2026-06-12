package projection

// worker_mastery_test.go — TDD tests for #1338: projectInventoryUpdated must
// fan out to UpsertMastery when the inventory.updated payload carries a
// non-nil Mastery field. Mirrors the WithDeckSummaryStore pattern from #1337.

import (
	"context"
	"testing"
	"time"

	"github.com/RdHamilton/hollowmark/services/bff/internal/storage/repository"
	"github.com/RdHamilton/hollowmark/services/contract"
)

// fakeMasteryStore captures calls to UpsertMastery for assertion.
type fakeMasteryStore struct {
	upserts []repository.MasteryUpsert
	err     error
}

func (f *fakeMasteryStore) UpsertMastery(_ context.Context, u repository.MasteryUpsert) error {
	if f.err != nil {
		return f.err
	}
	f.upserts = append(f.upserts, u)
	return nil
}

// TestInventoryUpdated_FansOutMastery verifies that when an inventory.updated
// payload carries a non-nil Mastery field, the worker calls UpsertMastery once
// with the correct fields.
func TestInventoryUpdated_FansOutMastery(t *testing.T) {
	mastery := &contract.MasteryInfo{
		Level:    42,
		PassType: "Standard",
		Max:      80,
	}
	payload := makePayload(t, contract.InventoryUpdatedPayload{
		Gems:    1000,
		Gold:    5000,
		Mastery: mastery,
	})

	events := &fakeEventStore{
		pending: []repository.DaemonEventRow{
			{ID: 20, UserID: 1, AccountID: "acct-mastery", EventType: "inventory.updated", Payload: payload, OccurredAt: time.Now()},
		},
	}
	accounts := &fakeAccountStore{accountID: 55}
	masteryStore := &fakeMasteryStore{}

	w := NewWorker(events, accounts, &fakeMatchStore{}, &fakeDraftStore{}, &fakeCollectionStore{}, &fakeInventoryStore{}, &fakeQuestStore{}, &fakeCombinedDeckStore{}, &fakeGamePlayStore{})
	w.WithMasteryStore(masteryStore)
	w.RunOnce(context.Background())

	if len(masteryStore.upserts) != 1 {
		t.Fatalf("expected 1 UpsertMastery call, got %d", len(masteryStore.upserts))
	}
	u := masteryStore.upserts[0]
	if u.AccountID != 55 {
		t.Errorf("UpsertMastery AccountID: got %d, want 55", u.AccountID)
	}
	if u.MasteryLevel != 42 {
		t.Errorf("UpsertMastery MasteryLevel: got %d, want 42", u.MasteryLevel)
	}
	if u.MasteryPass != "Standard" {
		t.Errorf("UpsertMastery MasteryPass: got %q, want %q", u.MasteryPass, "Standard")
	}
	if u.MasteryMax != 80 {
		t.Errorf("UpsertMastery MasteryMax: got %d, want 80", u.MasteryMax)
	}
}

// TestInventoryUpdated_NoMastery_NoUpsertMasteryCalls verifies that an
// inventory.updated event with a nil Mastery field produces zero UpsertMastery
// calls and continues to project normally.
func TestInventoryUpdated_NoMastery_NoUpsertMasteryCalls(t *testing.T) {
	payload := makePayload(t, contract.InventoryUpdatedPayload{
		Gems: 500,
		Gold: 200,
		// No Mastery
	})

	events := &fakeEventStore{
		pending: []repository.DaemonEventRow{
			{ID: 21, UserID: 1, AccountID: "acct-mastery2", EventType: "inventory.updated", Payload: payload, OccurredAt: time.Now()},
		},
	}
	accounts := &fakeAccountStore{accountID: 56}
	masteryStore := &fakeMasteryStore{}

	w := NewWorker(events, accounts, &fakeMatchStore{}, &fakeDraftStore{}, &fakeCollectionStore{}, &fakeInventoryStore{}, &fakeQuestStore{}, &fakeCombinedDeckStore{}, &fakeGamePlayStore{})
	w.WithMasteryStore(masteryStore)
	w.RunOnce(context.Background())

	if len(masteryStore.upserts) != 0 {
		t.Errorf("expected 0 UpsertMastery calls with nil Mastery, got %d", len(masteryStore.upserts))
	}
	// Row must still be projected.
	if len(events.projected) != 1 {
		t.Errorf("expected row 21 projected, got %v", events.projected)
	}
}

// TestInventoryUpdated_MasteryStoreError_SoftFail verifies that a UpsertMastery
// failure is a soft failure: the inventory projection continues (row is marked
// projected) and other fan-outs are not blocked.
func TestInventoryUpdated_MasteryStoreError_SoftFail(t *testing.T) {
	mastery := &contract.MasteryInfo{
		Level:    1,
		PassType: "Basic",
		Max:      80,
	}
	payload := makePayload(t, contract.InventoryUpdatedPayload{
		Gems:    100,
		Mastery: mastery,
	})

	events := &fakeEventStore{
		pending: []repository.DaemonEventRow{
			{ID: 22, UserID: 1, AccountID: "acct-mastery3", EventType: "inventory.updated", Payload: payload, OccurredAt: time.Now()},
		},
	}
	accounts := &fakeAccountStore{accountID: 57}
	masteryStore := &fakeMasteryStore{err: context.DeadlineExceeded}

	w := NewWorker(events, accounts, &fakeMatchStore{}, &fakeDraftStore{}, &fakeCollectionStore{}, &fakeInventoryStore{}, &fakeQuestStore{}, &fakeCombinedDeckStore{}, &fakeGamePlayStore{})
	w.WithMasteryStore(masteryStore)
	w.RunOnce(context.Background())

	// Row must still be projected despite the mastery error (soft failure).
	if len(events.projected) != 1 {
		t.Errorf("expected row 22 projected despite mastery error, got %v", events.projected)
	}
}
