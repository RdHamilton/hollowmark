package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// User is the in-memory representation of a row in the users table.
type User struct {
	ID               int64
	Email            string
	ClerkUserID      *string
	SubscriptionTier string
	CreatedAt        time.Time
}

// UserRepository handles persistence for users rows.
type UserRepository struct {
	db DB
}

// NewUserRepository returns a UserRepository backed by db.
func NewUserRepository(db DB) *UserRepository {
	return &UserRepository{db: db}
}

// GetByClerkUserID returns the user whose clerk_user_id matches, or (nil, nil) if not found.
func (r *UserRepository) GetByClerkUserID(ctx context.Context, clerkUserID string) (*User, error) {
	const q = `
		SELECT id, email, clerk_user_id, subscription_tier, created_at
		FROM   users
		WHERE  clerk_user_id = $1`

	row := r.db.QueryRowContext(ctx, q, clerkUserID)

	var u User
	if err := row.Scan(&u.ID, &u.Email, &u.ClerkUserID, &u.SubscriptionTier, &u.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}

		return nil, fmt.Errorf("GetByClerkUserID: %w", err)
	}

	return &u, nil
}

// UpdateCOPPAColumns sets the date_of_birth_year and coppa_restricted columns
// on the users row identified by userID. Added by migration 000111.
//
// dobYear may be nil when only the flag needs updating without recording a year.
// In practice the COPPA gate handler (#884) always supplies a non-nil dobYear.
func (r *UserRepository) UpdateCOPPAColumns(ctx context.Context, userID int64, dobYear *int16, coppaRestricted bool) error {
	const q = `
		UPDATE users
		SET    date_of_birth_year = $2,
		       coppa_restricted   = $3
		WHERE  id = $1`

	_, err := r.db.ExecContext(ctx, q, userID, dobYear, coppaRestricted)
	if err != nil {
		return fmt.Errorf("UserRepository.UpdateCOPPAColumns: %w", err)
	}
	return nil
}

// UpdateEmail updates the email column for the users row identified by userID.
//
// This is mandatory for GDPR Art.16 Right to Rectification (#888, Ray Issue 1):
// JIT provisioning inserts a "@clerk.local" placeholder that is never auto-synced from
// Clerk. The Art.17 erasure cascade reads users.email at deletion_repo.go:33 — a stale
// value breaks the erasure. Callers must invoke this after InsertRectificationEvent so
// the DB value is always consistent with Clerk's source-of-truth.
func (r *UserRepository) UpdateEmail(ctx context.Context, userID int64, email string) error {
	const q = `UPDATE users SET email = $2 WHERE id = $1`
	if _, err := r.db.ExecContext(ctx, q, userID, email); err != nil {
		return fmt.Errorf("UserRepository.UpdateEmail: %w", err)
	}
	return nil
}

// UpsertByClerkUserID inserts a new user row if clerk_user_id is not known, or returns the
// existing one.  For JIT provisioning the email placeholder "<clerkUserID>@clerk.local" is
// used on insert; it is overwritten when the user provides a real email later.
func (r *UserRepository) UpsertByClerkUserID(ctx context.Context, clerkUserID string) (*User, error) {
	// Use an INSERT … ON CONFLICT DO NOTHING pattern combined with a SELECT so
	// we always return the canonical row regardless of whether we just created it.
	const q = `
		INSERT INTO users (email, clerk_user_id, subscription_tier)
		VALUES ($1, $2, 'free')
		ON CONFLICT (clerk_user_id) WHERE clerk_user_id IS NOT NULL DO NOTHING
		RETURNING id, email, clerk_user_id, subscription_tier, created_at`

	placeholder := clerkUserID + "@clerk.local"
	row := r.db.QueryRowContext(ctx, q, placeholder, clerkUserID)

	var u User
	if err := row.Scan(&u.ID, &u.Email, &u.ClerkUserID, &u.SubscriptionTier, &u.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Row already exists — fetch it.
			return r.GetByClerkUserID(ctx, clerkUserID)
		}

		return nil, fmt.Errorf("UpsertByClerkUserID: %w", err)
	}

	return &u, nil
}
