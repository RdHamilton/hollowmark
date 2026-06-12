package projection

// worker_deck_summaries_test.go — TDD tests for #1337: projectInventoryUpdated
// must fan out to UpsertDeckSummary (NOT UpsertDeck) for each entry in
// payload.Decks when the inventory.updated event carries a DeckSummaries field.

import (
	"context"
	"testing"
	"time"

	"github.com/RdHamilton/hollowmark/services/bff/internal/storage/repository"
	"github.com/RdHamilton/hollowmark/services/contract"
)

// fakeDeckSummaryStore captures calls to UpsertDeckSummary for assertion.
// It does NOT implement UpsertDeck — verifying that the worker uses the new
// header-only path rather than the card-clobbering full-upsert path.
type fakeDeckSummaryStore struct {
	summaryUpserts []repository.DeckSummaryUpsert
	err            error
}

func (f *fakeDeckSummaryStore) UpsertDeckSummary(_ context.Context, u repository.DeckSummaryUpsert) error {
	if f.err != nil {
		return f.err
	}
	f.summaryUpserts = append(f.summaryUpserts, u)
	return nil
}

// fakeCombinedDeckStore implements both deckStore and deckSummaryStore so the
// same instance can be wired into the Worker and assertions can check both.
type fakeCombinedDeckStore struct {
	fullUpserts    []repository.DeckUpsert
	summaryUpserts []repository.DeckSummaryUpsert
	err            error
}

func (f *fakeCombinedDeckStore) UpsertDeck(_ context.Context, u repository.DeckUpsert) error {
	if f.err != nil {
		return f.err
	}
	f.fullUpserts = append(f.fullUpserts, u)
	return nil
}

func (f *fakeCombinedDeckStore) UpsertDeckSummary(_ context.Context, u repository.DeckSummaryUpsert) error {
	if f.err != nil {
		return f.err
	}
	f.summaryUpserts = append(f.summaryUpserts, u)
	return nil
}

// TestInventoryUpdated_FansOutDeckSummaries verifies that when an
// inventory.updated payload carries a non-empty Decks slice, the worker calls
// UpsertDeckSummary once per deck and NEVER calls UpsertDeck.
func TestInventoryUpdated_FansOutDeckSummaries(t *testing.T) {
	payload := makePayload(t, contract.InventoryUpdatedPayload{
		Gems: 1000,
		Gold: 5000,
		Decks: []contract.DeckSummary{
			{DeckID: "deck-aaa", Name: "Deck A", Format: "Standard"},
			{DeckID: "deck-bbb", Name: "Deck B", Format: "Alchemy"},
			{DeckID: "deck-ccc", Name: "Deck C", Format: ""},
		},
	})

	events := &fakeEventStore{
		pending: []repository.DaemonEventRow{
			{ID: 10, UserID: 1, AccountID: "acct-inv", EventType: "inventory.updated", Payload: payload, OccurredAt: time.Now()},
		},
	}
	accounts := &fakeAccountStore{accountID: 42}
	deckStore := &fakeCombinedDeckStore{}

	w := NewWorker(events, accounts, &fakeMatchStore{}, &fakeDraftStore{}, &fakeCollectionStore{}, &fakeInventoryStore{}, &fakeQuestStore{}, deckStore, &fakeGamePlayStore{})
	w.WithDeckSummaryStore(deckStore)
	w.RunOnce(context.Background())

	// Must have called UpsertDeckSummary 3 times.
	if len(deckStore.summaryUpserts) != 3 {
		t.Fatalf("expected 3 UpsertDeckSummary calls, got %d", len(deckStore.summaryUpserts))
	}

	// Verify deck IDs.
	ids := make(map[string]bool)
	for _, u := range deckStore.summaryUpserts {
		ids[u.DeckID] = true
		if u.AccountID != 42 {
			t.Errorf("UpsertDeckSummary AccountID: got %d, want 42", u.AccountID)
		}
	}
	for _, want := range []string{"deck-aaa", "deck-bbb", "deck-ccc"} {
		if !ids[want] {
			t.Errorf("missing UpsertDeckSummary for deck_id %q", want)
		}
	}

	// Must NOT have called the full UpsertDeck (which deletes deck_cards).
	if len(deckStore.fullUpserts) != 0 {
		t.Errorf("UpsertDeck was called %d time(s) — must not clobber deck_cards via full upsert", len(deckStore.fullUpserts))
	}
}

// TestInventoryUpdated_NoDeckSummaries_NoUpsertDeckSummaryCalls verifies that
// an inventory.updated event with an empty Decks slice (or no Decks field)
// produces zero UpsertDeckSummary calls and continues to project normally.
func TestInventoryUpdated_NoDeckSummaries_NoUpsertDeckSummaryCalls(t *testing.T) {
	payload := makePayload(t, contract.InventoryUpdatedPayload{
		Gems: 500,
		Gold: 200,
		// No Decks
	})

	events := &fakeEventStore{
		pending: []repository.DaemonEventRow{
			{ID: 11, UserID: 1, AccountID: "acct-inv2", EventType: "inventory.updated", Payload: payload, OccurredAt: time.Now()},
		},
	}
	accounts := &fakeAccountStore{accountID: 43}
	deckStore := &fakeCombinedDeckStore{}

	w := NewWorker(events, accounts, &fakeMatchStore{}, &fakeDraftStore{}, &fakeCollectionStore{}, &fakeInventoryStore{}, &fakeQuestStore{}, deckStore, &fakeGamePlayStore{})
	w.WithDeckSummaryStore(deckStore)
	w.RunOnce(context.Background())

	if len(deckStore.summaryUpserts) != 0 {
		t.Errorf("expected 0 UpsertDeckSummary calls with empty Decks, got %d", len(deckStore.summaryUpserts))
	}
	// Row must still be projected.
	if len(events.projected) != 1 {
		t.Errorf("expected row 11 projected, got %v", events.projected)
	}
}
