package repository

import (
	"context"
	"encoding/json"
	"time"
)

// DaemonEventRow is a single row from the daemon_events table.
type DaemonEventRow struct {
	ID          int64
	UserID      int64
	AccountID   string
	EventType   string
	Payload     json.RawMessage
	OccurredAt  time.Time
	ReceivedAt  time.Time
	EventID     *string
	ProjectedAt *time.Time
	Sequence    uint64
}

// DaemonEventsRepository persists daemon events to the daemon_events table.
type DaemonEventsRepository struct {
	db DB
}

// NewDaemonEventsRepository returns a DaemonEventsRepository backed by db.
func NewDaemonEventsRepository(db DB) *DaemonEventsRepository {
	return &DaemonEventsRepository{db: db}
}

// Insert writes a daemon event row scoped to the given user_id and account_id.
// occurred_at is stored as-is; received_at defaults to NOW() via the column default.
// eventID is the daemon-issued idempotency key (may be empty string for legacy rows).
// When eventID is non-empty the unique index (user_id, event_id) prevents duplicate inserts.
// sequence is the monotonically-increasing counter from the daemon (ADR-013).
func (r *DaemonEventsRepository) Insert(
	ctx context.Context,
	userID int64,
	accountID string,
	eventType string,
	payload json.RawMessage,
	occurredAt time.Time,
	eventID string,
	sequence uint64,
) error {
	// Normalise empty eventID to NULL so the partial unique index
	// (WHERE event_id IS NOT NULL) does not deduplicate rows without a key.
	var nullableEventID *string
	if eventID != "" {
		nullableEventID = &eventID
	}

	const q = `
		INSERT INTO daemon_events (user_id, account_id, event_type, payload, occurred_at, event_id, sequence)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT DO NOTHING`

	_, err := r.db.ExecContext(ctx, q, userID, accountID, eventType, payload, occurredAt, nullableEventID, sequence)

	return err
}

// ListByUserID returns up to limit daemon event rows for the given user,
// ordered newest-first.  It never returns rows belonging to other users.
func (r *DaemonEventsRepository) ListByUserID(
	ctx context.Context,
	userID int64,
	limit int,
) ([]DaemonEventRow, error) {
	const q = `
		SELECT id, user_id, account_id, event_type, payload, occurred_at, received_at,
		       event_id, projected_at
		FROM daemon_events
		WHERE user_id = $1
		ORDER BY occurred_at DESC
		LIMIT $2`

	rows, err := r.db.QueryContext(ctx, q, userID, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var events []DaemonEventRow

	for rows.Next() {
		var e DaemonEventRow

		if err := rows.Scan(
			&e.ID, &e.UserID, &e.AccountID, &e.EventType,
			&e.Payload, &e.OccurredAt, &e.ReceivedAt,
			&e.EventID, &e.ProjectedAt,
		); err != nil {
			return nil, err
		}

		events = append(events, e)
	}

	return events, rows.Err()
}

// ListPendingProjection returns up to limit daemon_events rows that have not
// yet been projected (projected_at IS NULL), ordered by received_at ASC so
// events are projected in ingest order.
func (r *DaemonEventsRepository) ListPendingProjection(
	ctx context.Context,
	limit int,
) ([]DaemonEventRow, error) {
	const q = `
		SELECT id, user_id, account_id, event_type, payload, occurred_at, received_at,
		       event_id, projected_at
		FROM daemon_events
		WHERE projected_at IS NULL
		ORDER BY received_at ASC
		LIMIT $1`

	rows, err := r.db.QueryContext(ctx, q, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var events []DaemonEventRow

	for rows.Next() {
		var e DaemonEventRow

		if err := rows.Scan(
			&e.ID, &e.UserID, &e.AccountID, &e.EventType,
			&e.Payload, &e.OccurredAt, &e.ReceivedAt,
			&e.EventID, &e.ProjectedAt,
		); err != nil {
			return nil, err
		}

		events = append(events, e)
	}

	return events, rows.Err()
}

// MarkProjected sets projected_at = NOW() for the given daemon_events row.
func (r *DaemonEventsRepository) MarkProjected(ctx context.Context, id int64) error {
	const q = `UPDATE daemon_events SET projected_at = NOW() WHERE id = $1`
	_, err := r.db.ExecContext(ctx, q, id)
	return err
}

// ListPendingProjectionAfter returns up to limit daemon_events rows that have
// not yet been projected (projected_at IS NULL) and whose (received_at, id) is
// strictly greater than (afterTime, afterID). Rows are ordered by
// (received_at ASC, id ASC) — the same ordering as ListPendingProjection.
//
// This method enables the worker to keyset-paginate within a single tick,
// advancing past transient-pending rows so they cannot starve newer events
// (RC1 starvation guard, fix for #1340).
func (r *DaemonEventsRepository) ListPendingProjectionAfter(
	ctx context.Context,
	afterTime time.Time,
	afterID int64,
	limit int,
) ([]DaemonEventRow, error) {
	const q = `
		SELECT id, user_id, account_id, event_type, payload, occurred_at, received_at,
		       event_id, projected_at
		FROM daemon_events
		WHERE projected_at IS NULL
		  AND (received_at, id) > ($1, $2)
		ORDER BY received_at ASC, id ASC
		LIMIT $3`

	rows, err := r.db.QueryContext(ctx, q, afterTime, afterID, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var events []DaemonEventRow

	for rows.Next() {
		var e DaemonEventRow

		if err := rows.Scan(
			&e.ID, &e.UserID, &e.AccountID, &e.EventType,
			&e.Payload, &e.OccurredAt, &e.ReceivedAt,
			&e.EventID, &e.ProjectedAt,
		); err != nil {
			return nil, err
		}

		events = append(events, e)
	}

	return events, rows.Err()
}

// ResetProjected sets projected_at = NULL for the given daemon_events row,
// returning it to the pending queue. Used by the ops backfill script to
// re-project silently-dropped match.game_ended events whose matches rows
// now exist (fix for #1340, RC5).
//
// The backfill caller is responsible for selecting only rows where the
// corresponding matches row EXISTS — otherwise the row will fail transiently
// again and immediately be re-pended on the next tick.
//
// This is an ops/admin method, NOT a schema migration. Backfill execution
// is tracked in a separate ticket per the #1340 out-of-scope decision.
func (r *DaemonEventsRepository) ResetProjected(ctx context.Context, id int64) error {
	const q = `UPDATE daemon_events SET projected_at = NULL WHERE id = $1`
	_, err := r.db.ExecContext(ctx, q, id)
	return err
}

// HasRecentEventByUserID returns true when the given user has at least one
// daemon_events row with received_at within the last window duration.
// This is used by the health endpoint to determine whether the daemon is
// actively connected (i.e. heartbeating).
func (r *DaemonEventsRepository) HasRecentEventByUserID(ctx context.Context, userID int64, window time.Duration) (bool, error) {
	const q = `
		SELECT EXISTS (
			SELECT 1
			FROM daemon_events
			WHERE user_id = $1
			  AND received_at >= NOW() - ($2 * INTERVAL '1 second')
		)`

	seconds := int64(window.Seconds())
	row := r.db.QueryRowContext(ctx, q, userID, seconds)

	var exists bool
	if err := row.Scan(&exists); err != nil {
		return false, err
	}

	return exists, nil
}
