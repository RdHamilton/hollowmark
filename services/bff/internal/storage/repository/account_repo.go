package repository

import (
	"context"
	"database/sql"
	"errors"
)

// AccountRepository resolves accounts for a given DB user_id.
type AccountRepository struct {
	db DB
}

// NewAccountRepository returns an AccountRepository backed by db.
func NewAccountRepository(db DB) *AccountRepository {
	return &AccountRepository{db: db}
}

// GetAccountIDByUserID returns the first accounts.id for the given users.id.
// For v0.2.0, one account per user is assumed (multi-account fan-out is v0.3.0).
// Returns (0, false, nil) when the user has no account row yet.
func (r *AccountRepository) GetAccountIDByUserID(ctx context.Context, userID int64) (int64, bool, error) {
	const q = `SELECT id FROM accounts WHERE user_id = $1 LIMIT 1`

	var accountID int64

	row := r.db.QueryRowContext(ctx, q, userID)
	if err := row.Scan(&accountID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, false, nil
		}

		return 0, false, err
	}

	return accountID, true, nil
}

// GetOrCreateByClientID returns the accounts.id for the given MTGA client_id
// (the raw Arena account string).  If no matching account exists, one is
// inserted linked to userID so projection output is correctly scoped.
func (r *AccountRepository) GetOrCreateByClientID(ctx context.Context, clientID string, userID int64) (int64, error) {
	// Try to find an existing account with this client_id.
	const selectQ = `SELECT id FROM accounts WHERE client_id = $1 LIMIT 1`

	var accountID int64

	row := r.db.QueryRowContext(ctx, selectQ, clientID)
	if err := row.Scan(&accountID); err == nil {
		return accountID, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}

	// Insert a minimal account row.
	const insertQ = `
		INSERT INTO accounts (name, client_id, user_id)
		VALUES ($1, $2, $3)
		ON CONFLICT DO NOTHING
		RETURNING id`

	insertRow := r.db.QueryRowContext(ctx, insertQ, clientID, clientID, userID)
	if err := insertRow.Scan(&accountID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Raced — fetch the row that won the conflict.
			retryRow := r.db.QueryRowContext(ctx, selectQ, clientID)
			if err2 := retryRow.Scan(&accountID); err2 != nil {
				return 0, err2
			}

			return accountID, nil
		}

		return 0, err
	}

	return accountID, nil
}
