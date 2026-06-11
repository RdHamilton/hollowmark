package repository

import (
	"context"
	"database/sql"
)

// WaitlistEntry is the in-memory representation of a waitlist_entries row.
type WaitlistEntry struct {
	ID              string
	Email           string
	MailchimpStatus string
	UTMSource       *string
	UTMMedium       *string
	UTMCampaign     *string
	UTMContent      *string
	UTMTerm         *string
	Referrer        *string
}

// waitlistDB is the minimal DB interface required by WaitlistRepository.
type waitlistDB interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// WaitlistRepository handles persistence for the waitlist_entries table.
type WaitlistRepository struct {
	db waitlistDB
}

// NewWaitlistRepository returns a repository backed by db.
func NewWaitlistRepository(db waitlistDB) *WaitlistRepository {
	return &WaitlistRepository{db: db}
}

// InsertIfNew inserts a new waitlist_entries row for email and attribution fields.
// It uses a CTE to atomically insert (ON CONFLICT DO NOTHING) and then count the
// total rows, returning the 1-based position. Returns (id, position, true, nil)
// when a new row was created, or ("", 0, false, nil) when the email already existed
// (ON CONFLICT DO NOTHING → no row returned from the INSERT).
// The initial mailchimp_status is 'failed' per the table DEFAULT; the happy
// path calls UpdateMailchimpStatus afterwards.
func (r *WaitlistRepository) InsertIfNew(
	ctx context.Context,
	email string,
	utmSource, utmMedium, utmCampaign *string,
	utmContent, utmTerm *string,
	referrer *string,
) (id string, position int64, created bool, err error) {
	// The CTE inserts the row (DO NOTHING on conflict) and RETURNING gives us the
	// new id. The outer SELECT counts total rows — this is the 1-based position for
	// the new signup. If the INSERT produces no row (conflict), the CTE is empty and
	// QueryRowContext returns sql.ErrNoRows.
	const q = `
		WITH inserted AS (
			INSERT INTO waitlist_entries
				(email, utm_source, utm_medium, utm_campaign, utm_content, utm_term, referrer)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			ON CONFLICT (email) DO NOTHING
			RETURNING id
		)
		SELECT inserted.id, (SELECT COUNT(*) FROM waitlist_entries) AS position
		FROM inserted`

	row := r.db.QueryRowContext(ctx, q, email, utmSource, utmMedium, utmCampaign, utmContent, utmTerm, referrer)
	if err := row.Scan(&id, &position); err == sql.ErrNoRows {
		return "", 0, false, nil
	} else if err != nil {
		return "", 0, false, err
	}
	return id, position, true, nil
}

// UpdateMailchimpStatus sets mailchimp_status and bumps updated_at for the row
// with the given id. status is expected to be "subscribed" or "failed".
func (r *WaitlistRepository) UpdateMailchimpStatus(ctx context.Context, id, status string) error {
	const q = `
		UPDATE waitlist_entries
		SET    mailchimp_status = $2, updated_at = now()
		WHERE  id = $1`

	_, err := r.db.ExecContext(ctx, q, id, status)
	return err
}

// FailedWaitlistEntry is a minimal projection of waitlist_entries rows used
// by the reconciler: it only needs the row ID and email address.
type FailedWaitlistEntry struct {
	ID    string
	Email string
}

// ListFailedWaitlistEntries returns up to limit rows where
// mailchimp_status = 'failed' AND mailchimp_attempts < maxAttempts (10),
// ordered by created_at ASC so oldest signups are retried first.
// The partial index on (mailchimp_status) WHERE mailchimp_status = 'failed'
// (migration 000086) makes this query efficient.
func (r *WaitlistRepository) ListFailedWaitlistEntries(ctx context.Context, limit int) ([]FailedWaitlistEntry, error) {
	const q = `
		SELECT id, email
		FROM   waitlist_entries
		WHERE  mailchimp_status = 'failed'
		  AND  mailchimp_attempts < 10
		ORDER  BY created_at ASC
		LIMIT  $1`

	rows, err := r.db.QueryContext(ctx, q, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []FailedWaitlistEntry
	for rows.Next() {
		var e FailedWaitlistEntry
		if err := rows.Scan(&e.ID, &e.Email); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// MarkWaitlistSubscribed sets mailchimp_status = 'subscribed' and bumps
// updated_at for the row with the given id.
// Called by the reconciler after a successful AddMember call.
func (r *WaitlistRepository) MarkWaitlistSubscribed(ctx context.Context, id string) error {
	const q = `
		UPDATE waitlist_entries
		SET    mailchimp_status = 'subscribed', updated_at = now()
		WHERE  id = $1`
	_, err := r.db.ExecContext(ctx, q, id)
	return err
}

// IncrementAttemptsAndMaybeTerminate atomically increments mailchimp_attempts
// and, when the new value reaches or exceeds maxAttempts, sets
// mailchimp_status = 'terminal'.
//
// A1 guard (Ray's binding amendment): the WHERE clause includes
// mailchimp_status = 'failed' so a concurrent handler goroutine's success
// (which flips status to 'subscribed') cannot be overwritten by the reconciler.
// If the row is no longer 'failed' the UPDATE is silently a no-op.
//
// Manual recovery of a 'terminal' row requires resetting BOTH
// mailchimp_status = 'failed' AND mailchimp_attempts = 0.
func (r *WaitlistRepository) IncrementAttemptsAndMaybeTerminate(ctx context.Context, id string, maxAttempts int) error {
	const q = `
		UPDATE waitlist_entries
		SET    mailchimp_attempts = mailchimp_attempts + 1,
		       mailchimp_status   = CASE
		                                WHEN mailchimp_attempts + 1 >= $2 THEN 'terminal'
		                                ELSE mailchimp_status
		                            END,
		       updated_at         = now()
		WHERE  id = $1
		  AND  mailchimp_status = 'failed'`
	_, err := r.db.ExecContext(ctx, q, id, maxAttempts)
	return err
}
