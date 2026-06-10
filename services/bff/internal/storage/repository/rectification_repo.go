package repository

import (
	"context"
	"fmt"
)

// RectificationAuditRepository handles persistence for the
// rectification_audit_log table (GDPR Art.16 Right to Rectification, #888).
//
// The table is append-only: no UPDATE or DELETE methods are exposed.
// PII is stored hashed (SHA-256 hex[:16]) — never raw values.
type RectificationAuditRepository struct {
	db DB
}

// NewRectificationAuditRepository returns a RectificationAuditRepository backed by db.
func NewRectificationAuditRepository(db DB) *RectificationAuditRepository {
	return &RectificationAuditRepository{db: db}
}

// InsertRectificationEvent appends one row to rectification_audit_log.
//
// Parameters:
//   - userID        — internal users.id (not the raw Clerk user ID).
//   - fieldName     — name of the changed field (e.g. "email", "display_name").
//   - oldValueHash  — SHA-256 hex[:16] of the old value; nil when not available.
//   - newValueHash  — SHA-256 hex[:16] of the new value; must not be the raw value.
func (r *RectificationAuditRepository) InsertRectificationEvent(
	ctx context.Context,
	userID int64,
	fieldName string,
	oldValueHash *string,
	newValueHash string,
) error {
	const q = `
		INSERT INTO rectification_audit_log
		            (user_id, field_name, old_value_hash, new_value_hash)
		VALUES      ($1, $2, $3, $4)`

	if _, err := r.db.ExecContext(ctx, q, userID, fieldName, oldValueHash, newValueHash); err != nil {
		return fmt.Errorf("RectificationAuditRepository.InsertRectificationEvent: %w", err)
	}

	return nil
}
