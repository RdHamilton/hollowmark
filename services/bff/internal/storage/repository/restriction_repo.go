package repository

import (
	"context"
	"database/sql"
	"fmt"
)

// RestrictionRepository handles all database operations for the GDPR Art.18
// Right to Restriction (ADR-055, ticket #890).
//
// The table is append-only: no UPDATE or DELETE methods are exposed on the
// audit log. The flag on users is a nullable timestamp managed by
// SetProcessingRestriction and ClearProcessingRestriction.
//
// DBHaltChecker (also in this file) implements the analytics.HaltChecker
// interface using this same DB connection — it is the production-wired
// implementation that replaces analytics.NewNoopHaltChecker() in cmd/main.go.
type RestrictionRepository struct {
	db DB
}

// NewRestrictionRepository returns a RestrictionRepository backed by db.
func NewRestrictionRepository(db DB) *RestrictionRepository {
	return &RestrictionRepository{db: db}
}

// SetProcessingRestriction sets processing_restricted_at = NOW() for the
// users row identified by userID. Idempotent — calling it twice is a no-op
// for the timestamp (it updates to a fresh NOW() on each call, which is
// acceptable for restriction transitions).
func (r *RestrictionRepository) SetProcessingRestriction(ctx context.Context, userID int64) error {
	const q = `UPDATE users SET processing_restricted_at = NOW() WHERE id = $1`
	if _, err := r.db.ExecContext(ctx, q, userID); err != nil {
		return fmt.Errorf("RestrictionRepository.SetProcessingRestriction: %w", err)
	}
	return nil
}

// ClearProcessingRestriction sets processing_restricted_at = NULL for the
// users row identified by userID. Idempotent — clearing an already-NULL
// column is a no-op.
func (r *RestrictionRepository) ClearProcessingRestriction(ctx context.Context, userID int64) error {
	const q = `UPDATE users SET processing_restricted_at = NULL WHERE id = $1`
	if _, err := r.db.ExecContext(ctx, q, userID); err != nil {
		return fmt.Errorf("RestrictionRepository.ClearProcessingRestriction: %w", err)
	}
	return nil
}

// InsertAuditLogEntry appends one row to restriction_audit_log.
//
// action must be 'restricted' or 'unrestricted' (enforced by DB CHECK).
// actor must be 'user' or 'admin' (enforced by DB CHECK).
//
// The table has no FK to users(id) or accounts(id) so it survives the Art.17
// erasure hard-delete (same pattern as deletion_audit_log).
func (r *RestrictionRepository) InsertAuditLogEntry(ctx context.Context, userID, accountID int64, action, actor string) error {
	const q = `
		INSERT INTO restriction_audit_log
		            (user_id, account_id, action, actor)
		VALUES      ($1, $2, $3, $4)`

	if _, err := r.db.ExecContext(ctx, q, userID, accountID, action, actor); err != nil {
		return fmt.Errorf("RestrictionRepository.InsertAuditLogEntry: %w", err)
	}
	return nil
}

// ─── DBHaltChecker ────────────────────────────────────────────────────────────

// DBHaltChecker implements analytics.HaltChecker using the users table.
// It is the production replacement for analytics.NewNoopHaltChecker() and
// must be wired into analytics.NewClient() in cmd/main.go (#890).
//
// IsHalted performs one indexed point-read via users.processing_restricted_at
// joined through accounts.account_id_hash → users.id. It is safe for
// concurrent use; each call opens a new query on the shared pool.
//
// Fail-closed contract: on any DB error, IsHalted returns (false, err).
// The caller (analytics.Client.Capture) treats a non-nil error as halted —
// it does NOT forward to PostHog. Persist (Insert) and SSE broadcast remain
// unconditional in the caller. This is the Art.18 fail-closed ruling from
// Ray's approval comment on #890.
type DBHaltChecker struct {
	db DB
}

// NewDBHaltChecker returns a DBHaltChecker backed by db.
func NewDBHaltChecker(db DB) *DBHaltChecker {
	return &DBHaltChecker{db: db}
}

// IsHalted reports whether analytics forwarding is halted for the given
// account_id_hash. It returns (false, err) on any DB error — the caller
// is responsible for treating errors as halted (fail-closed contract).
//
// The lookup joins accounts.account_id_hash (indexed, added by migration 000115)
// to users.processing_restricted_at.  No reverse-hash computation is needed.
func (c *DBHaltChecker) IsHalted(ctx context.Context, accountIDHash string) (bool, error) {
	const q = `
		SELECT u.processing_restricted_at IS NOT NULL
		  FROM accounts a
		  JOIN users u ON u.id = a.user_id
		 WHERE a.account_id_hash = $1
		 LIMIT 1`

	var restricted bool
	err := c.db.QueryRowContext(ctx, q, accountIDHash).Scan(&restricted)
	if err != nil {
		if err == sql.ErrNoRows {
			// No account row for this hash — not restricted.
			return false, nil
		}
		return false, fmt.Errorf("DBHaltChecker.IsHalted: %w", err)
	}
	return restricted, nil
}
