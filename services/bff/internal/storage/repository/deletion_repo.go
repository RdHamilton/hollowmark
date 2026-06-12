package repository

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
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
// strings across ALL of the user's accounts.  These values must be captured
// BEFORE any deletion so that Step 4a (TEXT-keyed deletes) and Step 6
// (Mailchimp) can proceed even after the accounts rows are removed.
//
// accountIDs must be the complete set of accounts.id values owned by the user
// — as returned by ResolveAllAccountIDs (#1333 fix: was single accountID).
//
// FM-5 (capture email before accounts delete) and the client_id ordering
// hazard are both addressed here.
func (r *DeletionRepository) CapturePreJobData(ctx context.Context, userID int64, accountIDs []int64) (email string, clientIDs []string, err error) {
	// Capture email from users row.
	const emailQ = `SELECT email FROM users WHERE id = $1`
	if err := r.db.QueryRowContext(ctx, emailQ, userID).Scan(&email); err != nil {
		if err == sql.ErrNoRows {
			return "", nil, fmt.Errorf("CapturePreJobData: user %d not found", userID)
		}
		return "", nil, fmt.Errorf("CapturePreJobData: query email: %w", err)
	}

	if len(accountIDs) == 0 {
		return email, nil, nil
	}

	// Capture all MTGA client_id strings across ALL of the user's accounts.
	// accounts.client_id is TEXT — one per accounts row.
	// Uses ANY($1) with a BIGINT[] slice to avoid dynamic SQL.
	const clientIDQ = `SELECT client_id FROM accounts WHERE id = ANY($1) AND client_id IS NOT NULL`
	rows, err := r.db.QueryContext(ctx, clientIDQ, accountIDs)
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

// DeleteTextKeyedRows deletes all rows from TEXT-keyed tables that use MTGA
// client_id strings as their account identifier.  These tables cannot be
// reached by the FK cascade from accounts(id) and must be explicitly deleted.
//
// Tables addressed (FM-3 Step 4a):
//   - daemon_events.account_id TEXT
//   - daemon_api_keys.account_id TEXT
//   - user_play_patterns.account_id TEXT
//   - projection_errors.account_id TEXT
//   - inventory_history.account_id TEXT (incremental path via 000068 only;
//     the fresh-init path via 000054 has BIGINT FK CASCADE — see below)
//
// # inventory_history schema fork (F1 — #1257)
//
// Migration 000068 added account_id as TEXT NOT NULL DEFAULT ” to
// inventory_history on the incremental upgrade path.  Migration 000054 (fresh
// init) created it as BIGINT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE.
// There is no conversion migration between these two paths.
//
// On the TEXT path: this method handles inventory_history via the TEXT ANY($1)
// delete below.  On the BIGINT path: inventory_history is covered by
// HardDeleteAccount (cascade) and/or DeleteExplicitBigintRows.  The method
// gates on information_schema.data_type to avoid a SQLSTATE 22P02 type error.
//
// NOTE: quest_session_tracking is NOT in this list.  Migration 000080 converted
// quest_session_tracking.account_id from TEXT (raw MTGA client_id) to BIGINT FK
// referencing accounts(id) ON DELETE CASCADE.  Attempting to delete it here
// with a text[] binding throws SQLSTATE 22P02 (invalid input syntax for type bigint).
//
// The delete is idempotent — re-running on an already-empty set is a no-op.
func (r *DeletionRepository) DeleteTextKeyedRows(ctx context.Context, clientIDs []string) error {
	if len(clientIDs) == 0 {
		return nil
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

	// quest_session_tracking is intentionally omitted — account_id is BIGINT FK
	// (ON DELETE CASCADE from accounts.id, migration 000080).  It is deleted by
	// HardDeleteAccount (Step 4e), not here.

	const userPlayPatternsQ = `DELETE FROM user_play_patterns WHERE account_id = ANY($1)`
	if _, err := r.db.ExecContext(ctx, userPlayPatternsQ, clientIDs); err != nil {
		return fmt.Errorf("DeleteTextKeyedRows user_play_patterns: %w", err)
	}

	const projectionErrorsQ = `DELETE FROM projection_errors WHERE account_id = ANY($1)`
	if _, err := r.db.ExecContext(ctx, projectionErrorsQ, clientIDs); err != nil {
		return fmt.Errorf("DeleteTextKeyedRows projection_errors: %w", err)
	}

	// inventory_history — TEXT path only (incremental migration via 000068).
	//
	// Gate on information_schema to determine the column data type before
	// issuing a TEXT-array bind.  On the BIGINT path (fresh-init 000054) the
	// column carries a FK CASCADE from accounts(id) so cascade + Step 4-explicit
	// cover it; a TEXT bind against a BIGINT column would throw SQLSTATE 22P02.
	//
	// PROD NOTE: the incremental production path has TEXT; this branch executes
	// on prod and is a no-op on fresh-init (CI) test databases.
	var invHistDataType string
	err := r.db.QueryRowContext(ctx, `
		SELECT COALESCE(data_type, '')
		FROM information_schema.columns
		WHERE table_schema = 'public'
		  AND table_name   = 'inventory_history'
		  AND column_name  = 'account_id'`).Scan(&invHistDataType)
	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("DeleteTextKeyedRows inventory_history data_type lookup: %w", err)
	}
	invHistDataType = strings.ToLower(invHistDataType)
	if invHistDataType == "text" || invHistDataType == "character varying" || invHistDataType == "character" {
		const inventoryHistoryQ = `DELETE FROM inventory_history WHERE account_id = ANY($1)`
		if _, err := r.db.ExecContext(ctx, inventoryHistoryQ, clientIDs); err != nil {
			return fmt.Errorf("DeleteTextKeyedRows inventory_history (TEXT path): %w", err)
		}
	}
	// BIGINT or missing: handled by HardDeleteAccount cascade + DeleteExplicitBigintRows.

	return nil
}

// AnonymizeConsentLog anonymizes consent_log rows in-place by nulling
// ip_address_hash and metadata for ALL of the user's account IDs.
//
// accountIDs is the complete set of accounts.id values owned by the user
// (#1333 fix: was a single accountID — multi-account users had N-1 consent_log
// rows left un-anonymized).
//
// This runs before the accounts hard-delete (Step 4e).  The ON DELETE SET NULL
// cascade on consent_log.account_id (migration #885) then fires when each
// accounts row is deleted, clearing the account_id FK reference.
//
// The consent_log rows are retained (not deleted) — they are compliance
// evidence under Art.7(1) accountability and must not be erased (ADR-056).
func (r *DeletionRepository) AnonymizeConsentLog(ctx context.Context, accountIDs []int64) error {
	if len(accountIDs) == 0 {
		return nil
	}
	const q = `
		UPDATE consent_log
		SET    ip_address_hash = NULL,
		       metadata        = NULL
		WHERE  account_id = ANY($1)`
	if _, err := r.db.ExecContext(ctx, q, accountIDs); err != nil {
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
//  1. ON DELETE CASCADE on BIGINT FK user-keyed tables (collection, decks,
//     drafts, inventory, draft_sessions, quests, user_settings,
//     recommendation_feedback, card_inventory, draft_picks, draft_packs,
//     draft_match_results, game_event_counters, life_change_tracking,
//     matchup_statistics, deck_performance_history, currency_history,
//     match_game_results, quest_session_tracking, and sub-cascades via
//     decks/matches/games).
//     NOTE: game_plays has no FK CASCADE today (000120 deferred it);
//     matches/player_stats/rank_history/collection_history gained FKs in
//  000119. These are covered defense-in-depth by DeleteExplicitBigintRows.
//  2. ON DELETE SET NULL on consent_log.account_id (migration #885).
func (r *DeletionRepository) HardDeleteAccount(ctx context.Context, accountID int64) error {
	const q = `DELETE FROM accounts WHERE id = $1`
	if _, err := r.db.ExecContext(ctx, q, accountID); err != nil {
		return fmt.Errorf("HardDeleteAccount: %w", err)
	}
	return nil
}

// DeleteExplicitBigintRows performs defense-in-depth explicit DELETEs on
// BIGINT-account_id tables that may be cascade-unreachable at some schema
// versions (#1257 — erasure completeness class fix).
//
// Tables covered:
//   - matches          — FK added 000119; absent on pre-119 DBs (P1 incident class).
//   - player_stats     — FK added 000119; absent on pre-119 DBs.
//   - rank_history     — FK added 000119; absent on pre-119 DBs.
//   - collection_history — FK added 000119; absent on pre-119 DBs.
//   - game_plays       — TEXT→BIGINT in 000120; NO FK CASCADE today (deferred).
//   - inventory_history — BIGINT FK CASCADE on fresh-init (000054) path only.
//     See data_type gate below (mirrors 000120's approach for game_plays).
//
// Called AFTER HardDeleteAccount (step4e) so FK cascades have already fired;
// these deletes are idempotent no-ops when cascade already removed the rows.
//
// # inventory_history data_type gate (F1)
//
// On the incremental path (000068): account_id is TEXT — already handled by
// DeleteTextKeyedRows (step4a).  On the fresh-init path (000054): account_id
// is BIGINT NOT NULL FK CASCADE, so issuing a BIGINT WHERE clause here is
// correct.  The gate reads information_schema.data_type at runtime, mirroring
// how migration 000120 handles the game_plays type fork.
func (r *DeletionRepository) DeleteExplicitBigintRows(ctx context.Context, accountID int64) error {
	// Delete from tables with confirmed BIGINT account_id that may lack FK CASCADE.
	// Using $1 for accountID — pgx positional binding, type BIGINT.

	const matchesQ = `DELETE FROM matches WHERE account_id = $1`
	if _, err := r.db.ExecContext(ctx, matchesQ, accountID); err != nil {
		return fmt.Errorf("DeleteExplicitBigintRows matches: %w", err)
	}

	const playerStatsQ = `DELETE FROM player_stats WHERE account_id = $1`
	if _, err := r.db.ExecContext(ctx, playerStatsQ, accountID); err != nil {
		return fmt.Errorf("DeleteExplicitBigintRows player_stats: %w", err)
	}

	const rankHistoryQ = `DELETE FROM rank_history WHERE account_id = $1`
	if _, err := r.db.ExecContext(ctx, rankHistoryQ, accountID); err != nil {
		return fmt.Errorf("DeleteExplicitBigintRows rank_history: %w", err)
	}

	const collectionHistoryQ = `DELETE FROM collection_history WHERE account_id = $1`
	if _, err := r.db.ExecContext(ctx, collectionHistoryQ, accountID); err != nil {
		return fmt.Errorf("DeleteExplicitBigintRows collection_history: %w", err)
	}

	const gamePlaysQ = `DELETE FROM game_plays WHERE account_id = $1`
	if _, err := r.db.ExecContext(ctx, gamePlaysQ, accountID); err != nil {
		return fmt.Errorf("DeleteExplicitBigintRows game_plays: %w", err)
	}

	// inventory_history — data_type-gated (F1).
	//
	// PROD path: TEXT (incremental 000068) — already handled by DeleteTextKeyedRows;
	// the gate returns '' or 'text' here, so this block is skipped on prod.
	// CI / fresh-init path: BIGINT NOT NULL FK CASCADE (000054) — issue BIGINT delete.
	// The gate is a no-op (returns '') if the table/column doesn't exist.
	var invHistDataType string
	err := r.db.QueryRowContext(ctx, `
		SELECT COALESCE(data_type, '')
		FROM information_schema.columns
		WHERE table_schema = 'public'
		  AND table_name   = 'inventory_history'
		  AND column_name  = 'account_id'`).Scan(&invHistDataType)
	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("DeleteExplicitBigintRows inventory_history data_type lookup: %w", err)
	}

	invHistDataType = strings.ToLower(invHistDataType)
	switch invHistDataType {
	case "bigint", "integer", "int8", "int4":
		// BIGINT path (fresh-init 000054): issue BIGINT WHERE clause.
		const inventoryHistoryQ = `DELETE FROM inventory_history WHERE account_id = $1`
		if _, err := r.db.ExecContext(ctx, inventoryHistoryQ, accountID); err != nil {
			return fmt.Errorf("DeleteExplicitBigintRows inventory_history (BIGINT path): %w", err)
		}
	case "text", "character varying", "character":
		// TEXT path (incremental 000068): covered by DeleteTextKeyedRows (step4a).
		// No action needed here.
	case "":
		// Table or column absent — no action.
	default:
		// Unexpected type: fail loudly to surface schema drift.
		return fmt.Errorf("DeleteExplicitBigintRows inventory_history: unexpected data_type %q — manual investigation required", invHistDataType)
	}

	return nil
}

// AssertZeroResiduals queries information_schema.columns for every public base
// table with an account_id or user_id column and asserts zero residual rows for
// ALL of the erased accountIDs and clientIDs (#1333: was a single accountID).
//
// Coverage strategy (C2):
//   - For each identity-keyed base table found, attempts a COUNT query using
//     accountIDs (BIGINT[]) for tables whose account_id is a numeric type, and
//     clientIDs (TEXT[]) for tables whose account_id is TEXT.
//   - Skips the TEXT-keyed count when clientIDs is empty (C2 condition).
//   - Skips the BIGINT-keyed count when accountIDs is empty.
//
// Retention exclusions (C1 / Ray ruling #1257):
//   - deletion_audit_log     — compliance evidence; numeric ID, non-identifiable post-erasure.
//   - restriction_audit_log  — GDPR Art.18 audit trail.
//   - dsr_access_log         — GDPR Art.15 access log.
//   - rectification_audit_log — GDPR Art.16 rectification audit.
//
// On failure: returns a non-nil error listing ALL offending tables (fail-all,
// not fail-first — C2).  Identifiers are sanitized via
// information_schema.columns (trusted system catalog) and quoted with
// QuoteIdentifier to prevent SQL injection from schema names.
func (r *DeletionRepository) AssertZeroResiduals(ctx context.Context, accountIDs []int64, clientIDs []string) error {
	// retainedTables are excluded from the sweep by Ray's retention ruling (#1257).
	retainedTables := map[string]bool{
		"deletion_audit_log":      true,
		"restriction_audit_log":   true,
		"dsr_access_log":          true,
		"rectification_audit_log": true,
	}

	// Query information_schema for all public base tables with account_id or user_id.
	const schemaQ = `
		SELECT c.table_name, c.column_name, c.data_type
		FROM information_schema.columns c
		JOIN information_schema.tables t
		  ON t.table_schema = c.table_schema
		 AND t.table_name   = c.table_name
		WHERE c.table_schema = 'public'
		  AND c.column_name IN ('account_id', 'user_id')
		  AND t.table_type   = 'BASE TABLE'
		ORDER BY c.table_name, c.column_name`

	rows, err := r.db.QueryContext(ctx, schemaQ)
	if err != nil {
		return fmt.Errorf("AssertZeroResiduals: schema query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	type tableCol struct {
		tableName  string
		columnName string
		dataType   string
	}
	var cols []tableCol
	for rows.Next() {
		var tc tableCol
		if err := rows.Scan(&tc.tableName, &tc.columnName, &tc.dataType); err != nil {
			return fmt.Errorf("AssertZeroResiduals: scan schema row: %w", err)
		}
		cols = append(cols, tc)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("AssertZeroResiduals: schema rows.Err: %w", err)
	}

	var offending []string
	for _, tc := range cols {
		if retainedTables[tc.tableName] {
			continue
		}

		dt := strings.ToLower(tc.dataType)
		isTextType := dt == "text" || dt == "character varying" || dt == "character"

		// Skip TEXT-keyed count when clientIDs is empty (C2).
		if isTextType && len(clientIDs) == 0 {
			continue
		}
		// Skip BIGINT-keyed count when accountIDs is empty.
		if !isTextType && len(accountIDs) == 0 {
			continue
		}

		// QuoteIdentifier: table names come from information_schema (trusted catalog),
		// but we still quote to handle any reserved-word names safely.
		quotedTable := quoteIdentifier(tc.tableName)
		quotedCol := quoteIdentifier(tc.columnName)

		var count int64
		if isTextType {
			q := fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE %s = ANY($1)`, quotedTable, quotedCol)
			if err := r.db.QueryRowContext(ctx, q, clientIDs).Scan(&count); err != nil {
				// If the table doesn't exist (e.g. draft_events dropped in 000025),
				// skip it gracefully.
				if strings.Contains(err.Error(), "does not exist") {
					continue
				}
				return fmt.Errorf("AssertZeroResiduals: count %s.%s (TEXT): %w", tc.tableName, tc.columnName, err)
			}
		} else {
			// Numeric type — use BIGINT[] accountIDs with ANY($1).
			q := fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE %s = ANY($1)`, quotedTable, quotedCol)
			if err := r.db.QueryRowContext(ctx, q, accountIDs).Scan(&count); err != nil {
				if strings.Contains(err.Error(), "does not exist") {
					continue
				}
				return fmt.Errorf("AssertZeroResiduals: count %s.%s (BIGINT): %w", tc.tableName, tc.columnName, err)
			}
		}

		if count > 0 {
			offending = append(offending, fmt.Sprintf("%s.%s(%d)", tc.tableName, tc.columnName, count))
		}
	}

	if len(offending) > 0 {
		sort.Strings(offending)
		return fmt.Errorf("AssertZeroResiduals: residual rows found after erasure — manual investigation required: %s",
			strings.Join(offending, ", "))
	}

	return nil
}

// quoteIdentifier wraps an identifier in double-quotes for safe SQL embedding.
// All table/column names passed here come from information_schema (system
// catalog) and are therefore trusted, but quoting is still applied for
// correctness with any reserved-word names.
func quoteIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
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
// in-flight job for this user (completed_at IS NULL), the unique partial
// index idx_deletion_audit_log_active_per_user prevents a second row (#1333:
// the guard is now per-user, not per-account, because a single erasure request
// must cover ALL of a user's accounts — the old per-account index was
// semantically wrong for multi-account users).
//
// account_id in the row is set to the first accountID in accountIDs for
// backward-compatible audit trail logging; all accountIDs are erased by the
// cascade regardless.
//
// Returns (jobID, false, nil) when a new job is created.
// Returns (existingJobID, true, nil) when a concurrent job is already active.
func (r *DeletionRepository) CreateAuditLogEntry(ctx context.Context, clerkUserID string, userID int64, accountIDs []int64) (jobID string, alreadyActive bool, err error) {
	// Use the first accountID for the audit log row (backward-compatible).
	// When accountIDs is empty (user with no accounts), use 0 as sentinel.
	var firstAccountID int64
	if len(accountIDs) > 0 {
		firstAccountID = accountIDs[0]
	}

	const insertQ = `
		INSERT INTO deletion_audit_log (clerk_user_id, user_id, account_id, requested_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (user_id) WHERE completed_at IS NULL DO NOTHING
		RETURNING job_id`

	row := r.db.QueryRowContext(ctx, insertQ, clerkUserID, userID, firstAccountID, time.Now().UTC())
	if err := row.Scan(&jobID); err != nil {
		if err != sql.ErrNoRows {
			return "", false, fmt.Errorf("CreateAuditLogEntry: insert: %w", err)
		}
		// Conflict — an in-flight job exists for this user.  Look it up.
		const lookupQ = `
			SELECT job_id FROM deletion_audit_log
			WHERE  user_id = $1 AND completed_at IS NULL
			LIMIT  1`
		if err2 := r.db.QueryRowContext(ctx, lookupQ, userID).Scan(&jobID); err2 != nil {
			return "", false, fmt.Errorf("CreateAuditLogEntry: lookup active job: %w", err2)
		}
		return jobID, true, nil
	}
	return jobID, false, nil
}

// ResolveAllAccountIDs resolves a Clerk user ID to the internal users.id and
// ALL accounts.id values owned by that user.  It replaces the LIMIT 1 path
// (ResolveUserAndAccount) for the erasure handler so that ALL of the user's
// accounts — and all their account-scoped child data — are erased in a single
// GDPR Art.17 request (#1333).
//
// Returns (userID, []accountIDs, nil).  accountIDs contains every accounts.id
// row where accounts.user_id = users.id.  It is the caller's responsibility to
// assert len(accountIDs) > 0 before proceeding.
//
// Returns a non-nil error when the user is not found (sql.ErrNoRows-wrapped)
// or when a DB error occurs.  Returns (userID, nil, nil) when the user exists
// but has no accounts rows — callers must handle this gracefully.
func (r *DeletionRepository) ResolveAllAccountIDs(ctx context.Context, clerkUserID string) (userID int64, accountIDs []int64, err error) {
	const userQ = `SELECT id FROM users WHERE clerk_user_id = $1`
	if err := r.db.QueryRowContext(ctx, userQ, clerkUserID).Scan(&userID); err != nil {
		if err == sql.ErrNoRows {
			return 0, nil, fmt.Errorf("ResolveAllAccountIDs: user not found for clerk_user_id %s", clerkUserID)
		}
		return 0, nil, fmt.Errorf("ResolveAllAccountIDs: query user: %w", err)
	}

	// Fetch ALL accounts rows for this user — no LIMIT.
	const accountsQ = `SELECT id FROM accounts WHERE user_id = $1 ORDER BY id`
	rows, err := r.db.QueryContext(ctx, accountsQ, userID)
	if err != nil {
		return 0, nil, fmt.Errorf("ResolveAllAccountIDs: query accounts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return 0, nil, fmt.Errorf("ResolveAllAccountIDs: scan account id: %w", err)
		}
		accountIDs = append(accountIDs, id)
	}
	if err := rows.Err(); err != nil {
		return 0, nil, fmt.Errorf("ResolveAllAccountIDs: rows: %w", err)
	}

	return userID, accountIDs, nil
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
