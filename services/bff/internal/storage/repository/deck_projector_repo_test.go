package repository_test

// deck_projector_repo_test.go — TDD integration tests for #1337.
//
// AC (Ray amendments):
//  1. UpsertDeckSummary upserts the decks row but NEVER touches deck_cards.
//  2. Existing deck_cards rows survive a bulk DeckSummaries upsert unchanged.
//  3. Only overwrites format when the incoming value is non-empty.

import (
	"context"
	"testing"
	"time"

	"github.com/RdHamilton/hollowmark/services/bff/internal/storage/repository"
)

// TestUpsertDeckSummary_InsertsNewDeck verifies that UpsertDeckSummary creates
// a new decks row when the deck_id does not already exist.
func TestUpsertDeckSummary_InsertsNewDeck(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewDeckProjectorRepository(db)
	ctx := context.Background()

	accountID := insertTestAccount(t, db, "deck-summary-insert")
	deckID := "deck-summary-new-001"
	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM decks WHERE id = $1`, deckID)
	})

	err := repo.UpsertDeckSummary(ctx, repository.DeckSummaryUpsert{
		DeckID:    deckID,
		AccountID: accountID,
		Name:      "New Deck",
		Format:    "Standard",
		UpdatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("UpsertDeckSummary: %v", err)
	}

	// Verify deck row exists.
	var name, format string
	err = db.QueryRowContext(ctx, `SELECT name, format FROM decks WHERE id = $1 AND account_id = $2`, deckID, accountID).Scan(&name, &format)
	if err != nil {
		t.Fatalf("SELECT after UpsertDeckSummary: %v", err)
	}
	if name != "New Deck" {
		t.Errorf("name: got %q want %q", name, "New Deck")
	}
	if format != "Standard" {
		t.Errorf("format: got %q want %q", format, "Standard")
	}
}

// TestUpsertDeckSummary_NeverTouchesDeckCards is the AC test for Ray's
// amendment 1 and 3: a bulk DeckSummaries upsert must leave ALL existing
// deck_cards rows for a deck completely unchanged.
func TestUpsertDeckSummary_NeverTouchesDeckCards(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewDeckProjectorRepository(db)
	ctx := context.Background()

	accountID := insertTestAccount(t, db, "deck-summary-card-preservation")
	deckID := insertTestDeck(t, db, accountID, "card-preservation")

	// Seed 3 deck_cards rows before the upsert.
	insertTestDeckCard(t, db, deckID, 70001, false)
	insertTestDeckCard(t, db, deckID, 70002, false)
	insertTestDeckCard(t, db, deckID, 70003, true)

	// Count cards before.
	var countBefore int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM deck_cards WHERE deck_id = $1`, deckID).Scan(&countBefore); err != nil {
		t.Fatalf("count before: %v", err)
	}
	if countBefore != 3 {
		t.Fatalf("setup failed: expected 3 cards before, got %d", countBefore)
	}

	// Perform 3 consecutive UpsertDeckSummary calls (mimicking a bulk fan-out
	// over DeckSummaries — this is the exact clobber scenario Ray identified).
	for i := 0; i < 3; i++ {
		err := repo.UpsertDeckSummary(ctx, repository.DeckSummaryUpsert{
			DeckID:    deckID,
			AccountID: accountID,
			Name:      "Updated Name",
			Format:    "Standard",
			UpdatedAt: time.Now().UTC(),
		})
		if err != nil {
			t.Fatalf("UpsertDeckSummary iteration %d: %v", i, err)
		}
	}

	// Count cards after — MUST be unchanged.
	var countAfter int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM deck_cards WHERE deck_id = $1`, deckID).Scan(&countAfter); err != nil {
		t.Fatalf("count after: %v", err)
	}
	if countAfter != 3 {
		t.Errorf("deck_cards clobber detected: expected 3 cards after bulk upsert, got %d", countAfter)
	}

	// Verify individual card rows survive intact.
	type cardRow struct {
		cardID  int
		qty     int
		board   string
		isDraft bool
	}
	rows, err := db.QueryContext(ctx, `SELECT card_id, quantity, board, from_draft_pick FROM deck_cards WHERE deck_id = $1 ORDER BY card_id`, deckID)
	if err != nil {
		t.Fatalf("query deck_cards: %v", err)
	}
	defer rows.Close()

	var cards []cardRow
	for rows.Next() {
		var c cardRow
		if err := rows.Scan(&c.cardID, &c.qty, &c.board, &c.isDraft); err != nil {
			t.Fatalf("scan: %v", err)
		}
		cards = append(cards, c)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows.Err: %v", err)
	}

	if len(cards) != 3 {
		t.Fatalf("expected 3 card rows, got %d", len(cards))
	}
	if cards[0].cardID != 70001 || cards[1].cardID != 70002 || cards[2].cardID != 70003 {
		t.Errorf("card IDs changed: got %v", cards)
	}
	if cards[2].isDraft != true {
		t.Errorf("from_draft_pick on card 70003: got false, want true")
	}
}

// TestUpsertDeckSummary_EmptyFormat_DoesNotOverwriteExistingFormat verifies
// Ray's amendment 2: when DeckSummaryUpsert.Format is empty, the existing
// format value in the decks row must be preserved.
func TestUpsertDeckSummary_EmptyFormat_DoesNotOverwriteExistingFormat(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewDeckProjectorRepository(db)
	ctx := context.Background()

	accountID := insertTestAccount(t, db, "deck-summary-format-guard")
	deckID := insertTestDeck(t, db, accountID, "format-guard")
	// insertTestDeck inserts with format="standard" — verify that assumption.
	var existingFormat string
	if err := db.QueryRowContext(ctx, `SELECT format FROM decks WHERE id = $1`, deckID).Scan(&existingFormat); err != nil {
		t.Fatalf("read existing format: %v", err)
	}

	// Upsert with empty format.
	err := repo.UpsertDeckSummary(ctx, repository.DeckSummaryUpsert{
		DeckID:    deckID,
		AccountID: accountID,
		Name:      "Name Update Only",
		Format:    "", // empty — must not overwrite
		UpdatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("UpsertDeckSummary with empty format: %v", err)
	}

	var formatAfter string
	if err := db.QueryRowContext(ctx, `SELECT format FROM decks WHERE id = $1`, deckID).Scan(&formatAfter); err != nil {
		t.Fatalf("read format after: %v", err)
	}
	if formatAfter != existingFormat {
		t.Errorf("format overwritten with empty value: got %q, want %q (existing)", formatAfter, existingFormat)
	}
}

// TestUpsertDeckSummary_NonEmptyFormat_UpdatesFormat verifies the positive case
// for Ray's amendment 2: a non-empty format DOES overwrite the existing value.
func TestUpsertDeckSummary_NonEmptyFormat_UpdatesFormat(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewDeckProjectorRepository(db)
	ctx := context.Background()

	accountID := insertTestAccount(t, db, "deck-summary-format-update")
	deckID := insertTestDeck(t, db, accountID, "format-update")

	err := repo.UpsertDeckSummary(ctx, repository.DeckSummaryUpsert{
		DeckID:    deckID,
		AccountID: accountID,
		Name:      "Updated Deck",
		Format:    "Alchemy",
		UpdatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("UpsertDeckSummary: %v", err)
	}

	var formatAfter string
	if err := db.QueryRowContext(ctx, `SELECT format FROM decks WHERE id = $1`, deckID).Scan(&formatAfter); err != nil {
		t.Fatalf("read format after: %v", err)
	}
	if formatAfter != "Alchemy" {
		t.Errorf("format not updated: got %q, want %q", formatAfter, "Alchemy")
	}
}
