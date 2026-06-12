package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// DeckCard represents a single card slot in a deck upsert.
type DeckCard struct {
	ArenaID  int
	Quantity int
}

// DeckUpsert holds the fields written to the decks and deck_cards tables
// from a deck.updated daemon event.
type DeckUpsert struct {
	DeckID    string
	AccountID int64
	Name      string
	Format    string
	Cards     []DeckCard
	UpdatedAt time.Time
}

// DeckSummaryUpsert holds the fields written to the decks table from a
// DeckSummaries fan-out in an inventory.updated event. It intentionally carries
// no Cards field — UpsertDeckSummary never touches deck_cards.
type DeckSummaryUpsert struct {
	DeckID    string
	AccountID int64
	Name      string
	// Format is the Arena deck format string (e.g. "Standard", "Alchemy").
	// When empty, the existing format value in the decks row is preserved.
	Format    string
	UpdatedAt time.Time
}

// DeckProjectorRepository writes deck snapshots to the decks and deck_cards tables.
type DeckProjectorRepository struct {
	db DB
}

// NewDeckProjectorRepository returns a DeckProjectorRepository backed by db.
func NewDeckProjectorRepository(db DB) *DeckProjectorRepository {
	return &DeckProjectorRepository{db: db}
}

// UpsertDeck writes the deck header and replaces all main-board card slots for
// the given deck.  The operation is performed within a transaction:
//  1. Upsert the decks row (keyed on id).
//  2. Delete existing deck_cards rows for this deck on the main board.
//  3. Bulk-insert new deck_cards rows.
//
// account_id is the accounts.id (BIGINT FK) resolved by the projection worker
// via accountStore.GetOrCreateByClientID before calling this method.
func (r *DeckProjectorRepository) UpsertDeck(ctx context.Context, u DeckUpsert) error {
	// Use a transaction so the delete + insert is atomic.
	txer, ok := r.db.(interface {
		BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error)
	})
	if !ok {
		return fmt.Errorf("deck projector: DB does not support transactions")
	}

	tx, err := txer.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}

	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	// 1. Upsert deck header.
	const upsertDeck = `
		INSERT INTO decks (id, account_id, name, format, created_at, modified_at, source)
		VALUES ($1, $2, $3, $4, $5, $5, 'arena')
		ON CONFLICT (id) DO UPDATE
			SET name        = EXCLUDED.name,
			    format      = EXCLUDED.format,
			    modified_at = EXCLUDED.modified_at`

	if _, err = tx.ExecContext(
		ctx, upsertDeck,
		u.DeckID, u.AccountID, u.Name, u.Format, u.UpdatedAt,
	); err != nil {
		return fmt.Errorf("upsert deck header: %w", err)
	}

	// 2. Delete existing main-board cards so we get an exact replacement.
	const deleteCards = `DELETE FROM deck_cards WHERE deck_id = $1 AND board = 'main'`

	if _, err = tx.ExecContext(ctx, deleteCards, u.DeckID); err != nil {
		return fmt.Errorf("delete existing deck_cards: %w", err)
	}

	// 3. Bulk-insert card slots.
	const insertCard = `
		INSERT INTO deck_cards (deck_id, card_id, quantity, board)
		VALUES ($1, $2, $3, 'main')
		ON CONFLICT (deck_id, card_id, board) DO UPDATE
			SET quantity = EXCLUDED.quantity`

	for _, c := range u.Cards {
		if c.Quantity <= 0 {
			continue
		}

		if _, err = tx.ExecContext(ctx, insertCard, u.DeckID, c.ArenaID, c.Quantity); err != nil {
			return fmt.Errorf("insert deck_card arena_id=%d: %w", c.ArenaID, err)
		}
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit deck upsert: %w", err)
	}

	return nil
}

// UpsertDeckSummary writes (or updates) the decks header row from a
// DeckSummaries login-blob entry. It NEVER touches deck_cards — this is the
// key invariant distinguishing it from UpsertDeck.
//
// Format is only overwritten when u.Format is non-empty; an empty value leaves
// whatever format is already stored on the row (Ray amendment 2).
//
// No transaction is needed: this is a single-row upsert.
func (r *DeckProjectorRepository) UpsertDeckSummary(ctx context.Context, u DeckSummaryUpsert) error {
	// Two SQL paths: non-empty format overwrites; empty format coalesces to
	// the existing value so a summary with no Format attribute never clobbers
	// a format we learned from a full DeckUpsertDeckV2 event.
	const upsertWithFormat = `
		INSERT INTO decks (id, account_id, name, format, created_at, modified_at, source)
		VALUES ($1, $2, $3, $4, $5, $5, 'arena')
		ON CONFLICT (id) DO UPDATE
			SET name        = EXCLUDED.name,
			    format      = EXCLUDED.format,
			    modified_at = EXCLUDED.modified_at`

	const upsertNoFormat = `
		INSERT INTO decks (id, account_id, name, format, created_at, modified_at, source)
		VALUES ($1, $2, $3, COALESCE((SELECT format FROM decks WHERE id = $1), 'Unknown'), $4, $4, 'arena')
		ON CONFLICT (id) DO UPDATE
			SET name        = EXCLUDED.name,
			    modified_at = EXCLUDED.modified_at`

	var err error
	if u.Format != "" {
		_, err = r.db.ExecContext(ctx, upsertWithFormat, u.DeckID, u.AccountID, u.Name, u.Format, u.UpdatedAt)
	} else {
		_, err = r.db.ExecContext(ctx, upsertNoFormat, u.DeckID, u.AccountID, u.Name, u.UpdatedAt)
	}
	if err != nil {
		return fmt.Errorf("upsert deck summary deck_id=%s: %w", u.DeckID, err)
	}
	return nil
}
