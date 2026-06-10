package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// DeletionRepository handles all database operations for the GDPR Art.17
// erasure cascade (ADR-056).  It satisfies the erasure.DBOps interface.
//
// Every method is scoped to the authenticated principal — no cross-tenant
// reads or writes are possible.
type DeletionRepository struct {
	db DB
}

// NewDeletionRepository returns a DeletionRepository backed by db.
func NewDeletionRepository(db DB) *DeletionRepository {
	return &DeletionRepository{db: db}
}

// CapturePreJobData returns the user's email address and all MTGA client_id
// strings associated with the account.  These values must be captured BEFORE
// any deletion so that Step 4a (TEXT-keyed deletes) and Step 6 (Mailchimp)
// can proceed even after the accounts row is removed.
//
// FM-5 (capture email before accounts delete) and the client_id ordering
// hazard are both addressed here.
func (r *DeletionRepository) CapturePreJobData(ctx context.Context, userID, accountID int64) (email string, clientIDs []string, err error) {
	// Capture email from users row.
	const emailQ = `SELECT email FROM users WHERE id = $1`
	if err := r.db.QueryRowContext(ctx, emailQ, userID).Scan(&email); err != nil {
		if err == sql.ErrNoRows {
			return "", nil, fmt.Errorf("CapturePreJobData: user %d not found", userID)
		}
		return "", nil, fmt.Errorf("CapturePreJobData: query email: %w", err)
	}

	// Capture all MTGA client_id strings for this account.
	// accounts.client_id is TEXT — one per accounts row in the current schema.
	const clientIDQ = `SELECT client_id FROM accounts WHERE id = $1 AND client_id IS NOT NULL`
	rows, err := r.db.QueryContext(ctx, clientIDQ, accountID)
	if err != nil {
		return "", nil, fmt.Errorf("CapturePreJobData: query client_ids: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var cid sql.NullString
		if err := rows.Scan(&cid); err != nil {
			return "", nil, fmt.Errorf("CapturePreJobData: scan client_id: %w", err)
		}
		if cid.Valid && cid.String != "" {
			clientIDs = append(clientIDs, cid.String)
		}
	}
	if err := rows.Err(); err != nil {
		return "", nil, fmt.Errorf("CapturePreJobData: rows: %w", err)
	}

	return email, clientIDs, nil
}

// SoftDeleteUser sets users.deleted_at = NOW() to block new daemon ingest
// writes before the 202 response is returned (FM-1 prerequisite).
// The UPDATE is idempotent — a second call on an already-soft-deleted user
// is a no-op (WHERE deleted_at IS NULL).
func (r *DeletionRepository) SoftDeleteUser(ctx context.Context, userID int64) error {
	const q = `UPDATE users SET deleted_at = NOW() WHERE id = $1 AND deleted_at IS NULL`
	if _, err := r.db.ExecContext(ctx, q, userID); err != nil {
		return fmt.Errorf("SoftDeleteUser: %w", err)
	}
	return nil
}

// DeleteTextKeyedRows deletes all rows from the five no-FK TEXT-keyed tables
// that use MTGA client_id strings as their account identifier.  These tables
// cannot be reached by the FK cascade from accounts(id), so they must be
// explicitly deleted.
//
// Tables addressed (FM-3 Step 4a):
//   - daemon_events.account_id TEXT
//   - daemon_api_keys.account_id TEXT
//   - quest_session_tracking.account_id TEXT
//   - user_play_patterns.account_id TEXT
//   - projection_errors.account_id TEXT
//
// The delete is idempotent — re-running on an already-empty set is a no-op.
func (r *DeletionRepository) DeleteTextKeyedRows(ctx context.Context, clientIDs []string) error {
	if len(clientIDs) == 0 {
		return nil
	}

	// Build a $N parameter list for ANY($1, $2, ...).
	// pgx uses positional $N placeholders, not ?.
	args := make([]any, len(clientIDs))
	for i, id := range clientIDs {
		args[i] = id
	}

	// Use ANY with a single TEXT[] parameter for simplicity and correctness.
	// Passing a []string as $1 and using = ANY($1) avoids building dynamic SQL.
	const daemonEventsQ = `DELETE FROM daemon_events WHERE account_id = ANY($1)`
	if _, err := r.db.ExecContext(ctx, daemonEventsQ, clientIDs); err != nil {
		return fmt.Errorf("DeleteTextKeyedRows daemon_events: %w", err)
	}

	const daemonAPIKeysQ = `DELETE FROM daemon_api_keys WHERE account_id = ANY($1)`
	if _, err := r.db.ExecContext(ctx, daemonAPIKeysQ, clientIDs); err != nil {
		return fmt.Errorf("DeleteTextKeyedRows daemon_api_keys: %w", err)
	}

	const questSessionQ = `DELETE FROM quest_session_tracking WHERE account_id = ANY($1)`
	if _, err := r.db.ExecContext(ctx, questSessionQ, clientIDs); err != nil {
		return fmt.Errorf("DeleteTextKeyedRows quest_session_tracking: %w", err)
	}

	const userPlayPatternsQ = `DELETE FROM user_play_patterns WHERE account_id = ANY($1)`
	if _, err := r.db.ExecContext(ctx, userPlayPatternsQ, clientIDs); err != nil {
		return fmt.Errorf("DeleteTextKeyedRows user_play_patterns: %w", err)
	}

	const projectionErrorsQ = `DELETE FROM projection_errors WHERE account_id = ANY($1)`
	if _, err := r.db.ExecContext(ctx, projectionErrorsQ, clientIDs); err != nil {
		return fmt.Errorf("DeleteTextKeyedRows projection_errors: %w", err)
	}

	return nil
}

// AnonymizeConsentLog anonymizes consent_log rows in-place by nulling
// ip_address_hash and metadata for the given account_id.
//
// This runs before the accounts hard-delete (Step 4e).  The ON DELETE SET NULL
// cascade on consent_log.account_id (migration #885) then fires when the
// accounts row is deleted, clearing the account_id FK reference.
//
// The consent_log rows are retained (not deleted) — they are compliance
// evidence under Art.7(1) accountability and must not be erased (ADR-056).
func (r *DeletionRepository) AnonymizeConsentLog(ctx context.Context, accountID int64) error {
	const q = `
		UPDATE consent_log
		SET    ip_address_hash = NULL,
		       metadata        = NULL
		WHERE  account_id = $1`
	if _, err := r.db.ExecContext(ctx, q, accountID); err != nil {
		return fmt.Errorf("AnonymizeConsentLog: %w", err)
	}
	return nil
}

// DeleteWaitlistEntry deletes waitlist_entries rows by email (CITEXT match,
// case-insensitive).  Addresses the residual PII identified in counsel §IV.C.
func (r *DeletionRepository) DeleteWaitlistEntry(ctx context.Context, email string) error {
	const q = `DELETE FROM waitlist_entries WHERE email = $1`
	if _, err := r.db.ExecContext(ctx, q, email); err != nil {
		return fmt.Errorf("DeleteWaitlistEntry: %w", err)
	}
	return nil
}

// HardDeleteUser deletes the users row for userID.
// The users(id) FK cascade removes all api_keys rows.
// Must run before HardDeleteAccount (accounts.user_id FK references users.id).
func (r *DeletionRepository) HardDeleteUser(ctx context.Context, userID int64) error {
	const q = `DELETE FROM users WHERE id = $1`
	if _, err := r.db.ExecContext(ctx, q, userID); err != nil {
		return fmt.Errorf("HardDeleteUser: %w", err)
	}
	return nil
}

// HardDeleteAccount deletes the accounts row for accountID.
//
// This fires two database-level cascades:
//  1. ON DELETE CASCADE on all BIGINT FK user-keyed tables (25+ tables per
//     ADR-056 FM-3 enumeration — collection, matches, decks, drafts, inventory,
//     game_plays, rank_history, draft_events, draft_sessions, quests,
//     user_settings, recommendation_feedback, card_inventory, draft_picks,
//     draft_packs, draft_match_results, game_event_counters, life_change_tracking,
//     matchup_statistics, deck_performance_history, currency_history, player_stats,
//     match_game_results, and all their sub-cascades via decks/matches/games).
//  2. ON DELETE SET NULL on consent_log.account_id (migration #885).
func (r *DeletionRepository) HardDeleteAccount(ctx context.Context, accountID int64) error {
	const q = `DELETE FROM accounts WHERE id = $1`
	if _, err := r.db.ExecContext(ctx, q, accountID); err != nil {
		return fmt.Errorf("HardDeleteAccount: %w", err)
	}
	return nil
}

// RecordJobComplete marks the deletion_audit_log row as complete by setting
// completed_at = NOW() for the given job_id.
func (r *DeletionRepository) RecordJobComplete(ctx context.Context, jobID string) error {
	const q = `UPDATE deletion_audit_log SET completed_at = NOW() WHERE job_id = $1`
	if _, err := r.db.ExecContext(ctx, q, jobID); err != nil {
		return fmt.Errorf("RecordJobComplete: %w", err)
	}
	return nil
}

// CreateAuditLogEntry inserts a new row in deletion_audit_log and returns the
// assigned job_id.  Called by the handler before dispatching the goroutine.
//
// Idempotency: if a concurrent DELETE /api/v1/account has already created an
// in-flight job for this account (completed_at IS NULL), the unique partial
// index idx_deletion_audit_log_active_per_account prevents a second row.
// The INSERT uses ON CONFLICT DO NOTHING; if no row is returned, the caller
// looks up and returns the existing in-flight job_id.
//
// Returns (jobID, false, nil) when a new job is created.
// Returns (existingJobID, true, nil) when a concurrent job is already active.
func (r *DeletionRepository) CreateAuditLogEntry(ctx context.Context, clerkUserID string, userID, accountID int64) (jobID string, alreadyActive bool, err error) {
	const insertQ = `
		INSERT INTO deletion_audit_log (clerk_user_id, user_id, account_id, requested_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (account_id) WHERE completed_at IS NULL DO NOTHING
		RETURNING job_id`

	row := r.db.QueryRowContext(ctx, insertQ, clerkUserID, userID, accountID, time.Now().UTC())
	if err := row.Scan(&jobID); err != nil {
		if err != sql.ErrNoRows {
			return "", false, fmt.Errorf("CreateAuditLogEntry: insert: %w", err)
		}
		// Conflict — an in-flight job exists.  Look it up.
		const lookupQ = `
			SELECT job_id FROM deletion_audit_log
			WHERE  account_id = $1 AND completed_at IS NULL
			LIMIT  1`
		if err2 := r.db.QueryRowContext(ctx, lookupQ, accountID).Scan(&jobID); err2 != nil {
			return "", false, fmt.Errorf("CreateAuditLogEntry: lookup active job: %w", err2)
		}
		return jobID, true, nil
	}
	return jobID, false, nil
}

// GetJobStatus returns the status of an erasure job by job_id, scoped to the
// caller's clerk_user_id.  Returns (nil, nil) if no row matches — either the
// job does not exist OR it belongs to a different user.  This prevents IDOR:
// a caller can only read their own jobs.
func (r *DeletionRepository) GetJobStatus(ctx context.Context, jobID, clerkUserID string) (*DeletionJobStatus, error) {
	const q = `
		SELECT job_id, requested_at, completed_at
		FROM   deletion_audit_log
		WHERE  job_id = $1
		  AND  clerk_user_id = $2`

	row := r.db.QueryRowContext(ctx, q, jobID, clerkUserID)

	var j DeletionJobStatus
	var completedAt sql.NullTime
	if err := row.Scan(&j.JobID, &j.RequestedAt, &completedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("GetJobStatus: %w", err)
	}
	if completedAt.Valid {
		j.CompletedAt = &completedAt.Time
	}
	return &j, nil
}

// DeletionJobStatus is the in-memory representation of a deletion_audit_log row.
type DeletionJobStatus struct {
	JobID       string
	RequestedAt time.Time
	CompletedAt *time.Time
}
