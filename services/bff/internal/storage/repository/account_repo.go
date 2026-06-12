package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/RdHamilton/hollowmark/services/bff/internal/analytics"
	"github.com/RdHamilton/hollowmark/services/bff/internal/observability"
)

// AccountRow holds the display columns from the accounts table for a single
// account row.  Used by GetByUserID.
type AccountRow struct {
	ID           int64
	Name         string
	ScreenName   sql.NullString
	ClientID     sql.NullString
	IsDefault    bool
	DailyWins    int
	WeeklyWins   int
	MasteryLevel int
	MasteryPass  sql.NullString
	MasteryMax   int
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// DuplicateAccountRow is one result row from CheckDuplicateAccounts.
// It represents a user_id that has more than one accounts row.
type DuplicateAccountRow struct {
	UserID int64
	Count  int64
}

// ErrCrosstenantAccount is returned by GetOrCreateByClientID when the supplied
// client_id resolves to an account that belongs to a different user_id.  The
// caller must treat this as an authorization failure and skip the write.
var ErrCrosstenantAccount = errors.New("client_id belongs to a different user")

// AccountRepository resolves accounts for a given DB user_id.
type AccountRepository struct {
	db              DB
	analyticsClient *analytics.Client
}

// NewAccountRepository returns an AccountRepository backed by db.
// Optionally chain WithAnalyticsClient to enable duplicate-account metrics.
func NewAccountRepository(db DB) *AccountRepository {
	return &AccountRepository{db: db}
}

// WithAnalyticsClient wires an analytics.Client for duplicate-account
// observability (D7.2, ticket #1335).  Returns the same pointer so it can be
// chained: repository.NewAccountRepository(db).WithAnalyticsClient(ac).
func (r *AccountRepository) WithAnalyticsClient(ac *analytics.Client) *AccountRepository {
	r.analyticsClient = ac
	return r
}

// GetByUserID returns the first accounts row for the given users.id, including
// all display columns the SPA's models.Account type needs.
// Returns (nil, false, nil) when the user has no account row yet (first-run
// state — daemon has not paired yet).
func (r *AccountRepository) GetByUserID(ctx context.Context, userID int64) (*AccountRow, bool, error) {
	const q = `
		SELECT id, name, screen_name, client_id, is_default,
		       daily_wins, weekly_wins, mastery_level, mastery_pass, mastery_max,
		       created_at, updated_at
		FROM   accounts
		WHERE  user_id = $1
		LIMIT  1`

	row := r.db.QueryRowContext(ctx, q, userID)

	var a AccountRow
	err := row.Scan(
		&a.ID, &a.Name, &a.ScreenName, &a.ClientID, &a.IsDefault,
		&a.DailyWins, &a.WeeklyWins, &a.MasteryLevel, &a.MasteryPass, &a.MasteryMax,
		&a.CreatedAt, &a.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, nil
		}
		observability.ReportError(ctx, err, map[string]string{"component": "db", "table": "accounts"})
		return nil, false, err
	}

	return &a, true, nil
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
		observability.ReportError(ctx, err, map[string]string{"component": "db", "table": "accounts"})
		return 0, false, err
	}

	return accountID, true, nil
}

// GetOrCreateByClientID returns the accounts.id for the given MTGA client_id
// (the raw Arena account string), verifying that the resolved account belongs
// to userID.  If the client_id is registered under a different user_id,
// ErrCrosstenantAccount is returned and no INSERT is attempted — this prevents
// a daemon authenticated as user A from writing into user B's tenant.
//
// If no account row exists for the client_id, a new one is created linked to
// userID (the legitimate first-run path).
//
// D7.2 (ticket #1335): after a successful INSERT, the method checks whether the
// user now has more than one accounts row. If so it emits a WARN log and an
// analytics event (Operational=true, bypasses Art.18 halt). The INSERT itself
// is not rolled back — detection, not prevention.
func (r *AccountRepository) GetOrCreateByClientID(ctx context.Context, clientID string, userID int64) (int64, error) {
	// Fetch both id and owner so we can detect cross-tenant attempts in a
	// single round-trip.
	const selectQ = `SELECT id, user_id FROM accounts WHERE client_id = $1 LIMIT 1`

	var accountID, ownerUserID int64

	row := r.db.QueryRowContext(ctx, selectQ, clientID)

	switch err := row.Scan(&accountID, &ownerUserID); {
	case err == nil:
		// Account exists — verify it belongs to the authenticated user.
		if ownerUserID != userID {
			return 0, fmt.Errorf("%w: client_id=%s authenticated_user=%d owner_user=%d",
				ErrCrosstenantAccount, clientID, userID, ownerUserID)
		}

		return accountID, nil

	case errors.Is(err, sql.ErrNoRows):
		// Account does not exist yet — fall through to INSERT.

	default:
		observability.ReportError(ctx, err, map[string]string{"component": "db", "table": "accounts"})
		return 0, err
	}

	// Insert a minimal account row linked to the authenticated user.
	// account_id_hash is populated at INSERT time from users.clerk_user_id via a
	// subquery — this ensures DBHaltChecker.IsHalted can match the runtime value
	// produced by identityhash.HashAccountID(clerkUserID) (same SHA-256 hex[:16]
	// formula).  Rows whose user has a NULL clerk_user_id get a NULL hash; those
	// are backfilled when the user authenticates and the daemon re-pairs.
	const insertQ = `
		INSERT INTO accounts (name, client_id, user_id, account_id_hash)
		SELECT $1, $2, $3,
		       CASE WHEN u.clerk_user_id IS NOT NULL
		            THEN substr(encode(digest(u.clerk_user_id, 'sha256'), 'hex'), 1, 16)
		            ELSE NULL
		       END
		  FROM users u
		 WHERE u.id = $3
		ON CONFLICT DO NOTHING
		RETURNING id, account_id_hash`

	var accountIDHash sql.NullString
	insertRow := r.db.QueryRowContext(ctx, insertQ, clientID, clientID, userID)
	if err := insertRow.Scan(&accountID, &accountIDHash); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Another goroutine raced us to the INSERT and won the conflict.
			// Re-read with user_id check.
			retryRow := r.db.QueryRowContext(ctx, selectQ, clientID)
			if err2 := retryRow.Scan(&accountID, &ownerUserID); err2 != nil {
				observability.ReportError(ctx, err2, map[string]string{"component": "db", "table": "accounts"})
				return 0, err2
			}

			if ownerUserID != userID {
				return 0, fmt.Errorf("%w: client_id=%s authenticated_user=%d owner_user=%d",
					ErrCrosstenantAccount, clientID, userID, ownerUserID)
			}

			return accountID, nil
		}

		observability.ReportError(ctx, err, map[string]string{"component": "db", "table": "accounts"})
		return 0, err
	}

	// D7.2: check for duplicates after a successful INSERT. A count > 1 means
	// the user now owns more than one accounts row — the incident class that
	// caused the 2026-06-12 P0. Emit WARN + operational analytics event.
	r.emitDuplicateWarnIfNeeded(ctx, userID, accountIDHash.String)

	return accountID, nil
}

// emitDuplicateWarnIfNeeded counts the accounts rows for userID after a fresh
// INSERT. If count > 1 it logs at WARN level and emits an operational analytics
// event so the anomaly is detectable via PostHog and any downstream alerting.
//
// Errors from the count query or from the analytics emission are logged but not
// returned — the INSERT already succeeded and the caller must not be blocked on
// monitoring side-effects.
func (r *AccountRepository) emitDuplicateWarnIfNeeded(ctx context.Context, userID int64, accountIDHash string) {
	// Guard: if account_id_hash was NULL at INSERT (users.clerk_user_id absent),
	// emitting a WARN or PostHog event with an empty hash produces misleading
	// observability noise. Skip silently — the duplicate-row itself is still
	// written and will surface via the D7.1 daily canary.
	if accountIDHash == "" {
		return
	}

	const countQ = `SELECT COUNT(*) FROM accounts WHERE user_id = $1`

	var cnt int64
	if err := r.db.QueryRowContext(ctx, countQ, userID).Scan(&cnt); err != nil {
		log.Printf("[WARN] account_repo: duplicate-check count query failed for user_id (hashed): err=%v", err)
		return
	}

	if cnt <= 1 {
		return
	}

	// WARN log — user_id is not logged directly; we identify the account by
	// its hash so no raw PII reaches structured logs.
	log.Printf("[WARN] account_repo: duplicate accounts detected account_id_hash=%s duplicate_count=%d",
		accountIDHash, cnt)

	if r.analyticsClient == nil {
		return
	}

	if err := r.analyticsClient.Capture(
		ctx,
		accountIDHash,
		analytics.EventAccountDuplicateDetected,
		map[string]any{
			"account_id_hash": accountIDHash,
			"duplicate_count": cnt,
		},
		analytics.CaptureOptions{Operational: true},
	); err != nil {
		log.Printf("[WARN] account_repo: analytics emit failed for duplicate-account event: %v", err)
	}
}

// CheckDuplicateAccounts runs the D7.1 canary query:
//
//	SELECT user_id, count(*) FROM accounts GROUP BY user_id HAVING count(*) > 1
//
// It returns one DuplicateAccountRow per user_id with more than one accounts row.
// An empty slice means no duplicates were found. This method is also the D5
// pre-flight gate for the ADR-080 persona-keying epic — run it manually before
// the ADR-080 contract flip deploys.
func (r *AccountRepository) CheckDuplicateAccounts(ctx context.Context) ([]DuplicateAccountRow, error) {
	const q = `
		SELECT user_id, count(*)
		FROM   accounts
		GROUP  BY user_id
		HAVING count(*) > 1`

	rows, err := r.db.QueryContext(ctx, q)
	if err != nil {
		observability.ReportError(ctx, err, map[string]string{"component": "db", "table": "accounts"})
		return nil, fmt.Errorf("CheckDuplicateAccounts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var result []DuplicateAccountRow
	for rows.Next() {
		var d DuplicateAccountRow
		if err := rows.Scan(&d.UserID, &d.Count); err != nil {
			observability.ReportError(ctx, err, map[string]string{"component": "db", "table": "accounts"})
			return nil, fmt.Errorf("CheckDuplicateAccounts: scan: %w", err)
		}
		result = append(result, d)
	}
	if err := rows.Err(); err != nil {
		observability.ReportError(ctx, err, map[string]string{"component": "db", "table": "accounts"})
		return nil, fmt.Errorf("CheckDuplicateAccounts: rows: %w", err)
	}

	return result, nil
}
