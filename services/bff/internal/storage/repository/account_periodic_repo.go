package repository

import (
	"context"
	"fmt"
	"time"
)

// PeriodicWinsUpsert holds the daily and weekly win counts written to the
// accounts table from a periodic.updated daemon event projection (#1344).
type PeriodicWinsUpsert struct {
	AccountID  int64
	DailyWins  int
	WeeklyWins int
	UpdatedAt  time.Time
}

// AccountPeriodicRepository writes periodic win counts to the accounts table.
// It targets the daily_wins and weekly_wins columns that already exist from
// migration 000012. No new migration is required.
type AccountPeriodicRepository struct {
	db DB
}

// NewAccountPeriodicRepository returns an AccountPeriodicRepository backed by db.
func NewAccountPeriodicRepository(db DB) *AccountPeriodicRepository {
	return &AccountPeriodicRepository{db: db}
}

// UpsertPeriodicWins writes daily_wins and weekly_wins onto the accounts row
// identified by account_id. The update is an in-place SET — no INSERT is
// attempted because the accounts row must already exist (created by
// GetOrCreateByClientID) before any periodic event can be projected for it.
//
// Scoped by account_id — no cross-tenant writes are possible because the
// caller resolves account_id through GetOrCreateByClientID, which enforces the
// client_id → user_id ownership invariant.
func (r *AccountPeriodicRepository) UpsertPeriodicWins(ctx context.Context, u PeriodicWinsUpsert) error {
	const q = `
		UPDATE accounts
		   SET daily_wins  = $1,
		       weekly_wins = $2,
		       updated_at  = $3
		 WHERE id = $4`

	res, err := r.db.ExecContext(
		ctx, q,
		u.DailyWins,
		u.WeeklyWins,
		u.UpdatedAt,
		u.AccountID,
	)
	if err != nil {
		return fmt.Errorf("UpsertPeriodicWins account_id=%d: %w", u.AccountID, err)
	}

	// Sanity guard: the account row must already exist. Zero rows affected means
	// the account was deleted or the ID is wrong — log, but do not hard-fail
	// (the daemon event has already been ingested; failing here would cause a
	// re-projection that doesn't help the write).
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("UpsertPeriodicWins account_id=%d: no accounts row found (already deleted?)", u.AccountID)
	}

	return nil
}
