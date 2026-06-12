package repository

import (
	"context"
	"fmt"
	"time"
)

// MasteryUpsert holds the mastery pass fields written to the accounts table
// from an inventory.updated daemon event projection.
type MasteryUpsert struct {
	AccountID    int64
	MasteryLevel int
	MasteryPass  string
	MasteryMax   int
	UpdatedAt    time.Time
}

// AccountMasteryRepository writes mastery pass state to the accounts table.
// It targets the mastery_level, mastery_pass, and mastery_max columns that
// already exist from migration 000013. No new migration is required.
type AccountMasteryRepository struct {
	db DB
}

// NewAccountMasteryRepository returns an AccountMasteryRepository backed by db.
func NewAccountMasteryRepository(db DB) *AccountMasteryRepository {
	return &AccountMasteryRepository{db: db}
}

// UpsertMastery writes mastery_level, mastery_pass, and mastery_max onto the
// accounts row identified by account_id. The update is an in-place SET — no
// INSERT is attempted because the accounts row must already exist (created by
// GetOrCreateByClientID) before any inventory event can be projected for it.
//
// Scoped by account_id — no cross-tenant writes are possible because the
// caller resolves account_id through GetOrCreateByClientID, which enforces the
// client_id → user_id ownership invariant.
func (r *AccountMasteryRepository) UpsertMastery(ctx context.Context, u MasteryUpsert) error {
	const q = `
		UPDATE accounts
		   SET mastery_level = $1,
		       mastery_pass  = $2,
		       mastery_max   = $3,
		       updated_at    = $4
		 WHERE id = $5`

	res, err := r.db.ExecContext(
		ctx, q,
		u.MasteryLevel,
		u.MasteryPass,
		u.MasteryMax,
		u.UpdatedAt,
		u.AccountID,
	)
	if err != nil {
		return fmt.Errorf("UpsertMastery account_id=%d: %w", u.AccountID, err)
	}

	// Sanity guard: the account row must already exist. Zero rows affected means
	// the account was deleted or the ID is wrong — log, but do not hard-fail
	// (the daemon event has already been ingested and the inventory upsert has
	// already succeeded; failing here would cause a re-projection that repeats
	// the inventory upsert without helping the mastery write).
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("UpsertMastery account_id=%d: no accounts row found (already deleted?)", u.AccountID)
	}

	return nil
}
