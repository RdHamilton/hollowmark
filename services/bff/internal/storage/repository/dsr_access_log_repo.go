package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// DSRAccessLogRepository handles persistence for dsr_access_log — the audit
// log and rate-limit source for GDPR Art.15 data export requests.
//
// Rate-limit semantics: one export per user per 24-hour window.  The window is
// enforced by CheckRecentExport; RecordExport writes the new row on success.
//
// Compliance note: user_id is stored as a plain BIGINT (no FK to users).  The
// log must survive Art.17 erasure, which deletes the users row.
type DSRAccessLogRepository struct {
	db DB
}

// NewDSRAccessLogRepository returns a DSRAccessLogRepository backed by db.
func NewDSRAccessLogRepository(db DB) *DSRAccessLogRepository {
	return &DSRAccessLogRepository{db: db}
}

// CheckRecentExport looks for a dsr_access_log row for userID within the last
// 24 hours.
//
// Returns:
//   - (false, 0, nil) — no recent export; the caller may proceed.
//   - (true, N, nil) — export within the 24h window; N = seconds until the
//     window expires (for the Retry-After header).
//   - (false, 0, err) — database error.
func (r *DSRAccessLogRepository) CheckRecentExport(ctx context.Context, userID int64) (limited bool, retryAfterSecs int64, err error) {
	const q = `
		SELECT requested_at
		FROM   dsr_access_log
		WHERE  user_id      = $1
		  AND  requested_at > NOW() - INTERVAL '24 hours'
		ORDER  BY requested_at DESC
		LIMIT  1`

	var requestedAt time.Time
	err = r.db.QueryRowContext(ctx, q, userID).Scan(&requestedAt)
	if err == sql.ErrNoRows {
		return false, 0, nil
	}
	if err != nil {
		return false, 0, fmt.Errorf("DSRAccessLogRepository.CheckRecentExport: %w", err)
	}

	// A row was found within the 24h window — compute Retry-After.
	windowExpiry := requestedAt.Add(24 * time.Hour)
	retryAfterSecs = int64(time.Until(windowExpiry).Seconds())
	if retryAfterSecs < 0 {
		retryAfterSecs = 0
	}
	return true, retryAfterSecs, nil
}

// RecordExport inserts a new dsr_access_log row for userID with requested_at
// set to NOW().  Returns the generated export_id UUID.
//
// Callers MUST call CheckRecentExport before RecordExport to honour the
// rate-limit window — RecordExport itself does not enforce the window.
func (r *DSRAccessLogRepository) RecordExport(ctx context.Context, userID int64) (exportID string, err error) {
	const q = `
		INSERT INTO dsr_access_log (user_id)
		VALUES ($1)
		RETURNING export_id`

	err = r.db.QueryRowContext(ctx, q, userID).Scan(&exportID)
	if err != nil {
		return "", fmt.Errorf("DSRAccessLogRepository.RecordExport: %w", err)
	}
	return exportID, nil
}
