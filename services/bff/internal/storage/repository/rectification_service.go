package repository

import (
	"context"
	"database/sql"
	"fmt"
)

// RectificationService coordinates the two-write atomicity requirement for
// GDPR Art.16 profile rectification (#888, Bianca V1 BLOCK).
//
// Email changes require both an audit-log INSERT and a users.email UPDATE to
// succeed or fail together — a partial commit would leave the audit log out of
// sync with the users table, which is a compliance defect.
//
// RectificationService satisfies both the rectificationAuditWriter and the
// profileEmailRectifier interfaces expected by AccountProfileHandler.
type RectificationService struct {
	db        *sql.DB
	auditRepo *RectificationAuditRepository
	userRepo  *UserRepository
}

// NewRectificationService returns a RectificationService backed by db.
// Both the audit INSERT and the email UPDATE run through this service so
// they can share a single *sql.Tx on the email-change path.
func NewRectificationService(db *sql.DB) *RectificationService {
	return &RectificationService{
		db:        db,
		auditRepo: NewRectificationAuditRepository(db),
		userRepo:  NewUserRepository(db),
	}
}

// InsertRectificationEvent appends one row to rectification_audit_log without
// a transaction.  Used for display_name changes (audit-only; no DB email sync).
//
// For email changes, call RectifyProfileTx instead.
func (s *RectificationService) InsertRectificationEvent(
	ctx context.Context,
	userID int64,
	fieldName string,
	oldValueHash *string,
	newValueHash string,
) error {
	return s.auditRepo.InsertRectificationEvent(ctx, userID, fieldName, oldValueHash, newValueHash)
}

// RectifyProfileTx atomically writes one audit-log row and updates users.email
// within a single *sql.Tx.
//
// If either write fails the transaction is rolled back and the error is returned.
// The caller receives a clean error; no partial state is committed.
//
// Parameters:
//   - userID        — internal users.id.
//   - fieldName     — name of the changed field ("email").
//   - oldValueHash  — SHA-256 hex[:16] of the previous value; nil when unknown.
//   - newValueHash  — SHA-256 hex[:16] of the new value; must not be raw PII.
//   - email         — Clerk-verified primary email to write to users.email.
func (s *RectificationService) RectifyProfileTx(
	ctx context.Context,
	userID int64,
	fieldName string,
	oldValueHash *string,
	newValueHash string,
	email string,
) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("RectificationService.RectifyProfileTx BeginTx: %w", err)
	}

	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	const auditQ = `
		INSERT INTO rectification_audit_log
		            (user_id, field_name, old_value_hash, new_value_hash)
		VALUES      ($1, $2, $3, $4)`

	if _, err := tx.ExecContext(ctx, auditQ, userID, fieldName, oldValueHash, newValueHash); err != nil {
		return fmt.Errorf("RectificationService.RectifyProfileTx audit INSERT: %w", err)
	}

	const emailQ = `UPDATE users SET email = $2 WHERE id = $1`
	if _, err := tx.ExecContext(ctx, emailQ, userID, email); err != nil {
		return fmt.Errorf("RectificationService.RectifyProfileTx email UPDATE: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("RectificationService.RectifyProfileTx Commit: %w", err)
	}
	committed = true

	return nil
}
