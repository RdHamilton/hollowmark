package repository

import (
	"context"
	"database/sql"
	"time"
)

// BackfillOptions controls the behaviour of BackfillStaleDraftSessions.
type BackfillOptions struct {
	// StalenessThreshold is the minimum age of updated_at for a session to be
	// considered stale.  Sessions updated more recently than this threshold are
	// never touched, regardless of pick count.
	// Recommended production value: 4 hours.
	StalenessThreshold time.Duration

	// MinPicksForStale is the minimum total_picks count required before an
	// orphaned session (no end_time) can be closed as 'abandoned'.  This
	// prevents closing genuinely mid-draft sessions that just happen to be old
	// (e.g. a user who paused mid-pack).
	// Recommended production value: 42 (minimum full-draft pick count for
	// Quick Draft; Premier Draft is 45 but 42 is the conservative floor).
	MinPicksForStale int

	// AccountID, when non-zero, scopes the backfill to a single account.
	// Zero means "all accounts" (global run).
	AccountID int64
}

// BackfillResult summarises what BackfillStaleDraftSessions changed.
type BackfillResult struct {
	// ClosedCompleted holds session IDs promoted from in_progress → completed
	// because end_time was already set (projection bug case).
	ClosedCompleted []string

	// ClosedAbandoned holds session IDs promoted from in_progress → abandoned
	// because they had no end_time but met the staleness + pick-count threshold.
	ClosedAbandoned []string
}

// BackfillStaleDraftSessions is a safe, idempotent one-time repair operation that
// closes draft_sessions rows stuck in 'in_progress' after the draft has actually
// ended.  It should be invoked once manually from an ops endpoint after the #1344
// PR-B fix ships; subsequent calls are safe no-ops.
//
// Two categories of stale rows are closed:
//
//  1. Rows where end_time IS NOT NULL — the projection worker set end_time on a
//     draft.completed event but failed to flip the status.  These are promoted to
//     'completed' because the server confirmed the draft ended.
//
//  2. Rows where end_time IS NULL, total_picks >= opts.MinPicksForStale, and
//     updated_at < NOW() - opts.StalenessThreshold — sessions from old daemon builds
//     that never sent draft.completed.  Promoted to 'abandoned' (not 'completed') to
//     reflect the missing server confirmation.
//
// Safety properties:
//   - Only touches rows with status = 'in_progress'.
//   - When opts.AccountID is non-zero, only that account's rows are touched.
//   - Idempotent: re-running on a fully-backfilled DB returns empty slices.
//   - The two UPDATE predicates are disjoint (end_time IS NOT NULL vs IS NULL),
//     so there is no double-update risk between the two passes.
func (r *DraftSessionsRepository) BackfillStaleDraftSessions(
	ctx context.Context,
	opts BackfillOptions,
) (BackfillResult, error) {
	staleThreshold := time.Now().UTC().Add(-opts.StalenessThreshold)

	var result BackfillResult

	completedIDs, err := r.backfillCompletedByEndTime(ctx, opts.AccountID)
	if err != nil {
		return result, err
	}

	result.ClosedCompleted = completedIDs

	abandonedIDs, err := r.backfillAbandonedOrphans(ctx, opts.AccountID, staleThreshold, opts.MinPicksForStale)
	if err != nil {
		return result, err
	}

	result.ClosedAbandoned = abandonedIDs

	return result, nil
}

// backfillCompletedByEndTime promotes all in_progress sessions that already have
// end_time set to status='completed'.  Returns the affected session IDs.
func (r *DraftSessionsRepository) backfillCompletedByEndTime(ctx context.Context, accountID int64) ([]string, error) {
	var (
		rows *sql.Rows
		err  error
	)

	if accountID != 0 {
		const q = `
			UPDATE draft_sessions
			   SET status = 'completed', updated_at = NOW()
			 WHERE status = 'in_progress'
			   AND end_time IS NOT NULL
			   AND account_id = $1
			RETURNING id`

		rows, err = r.db.QueryContext(ctx, q, accountID)
	} else {
		const q = `
			UPDATE draft_sessions
			   SET status = 'completed', updated_at = NOW()
			 WHERE status = 'in_progress'
			   AND end_time IS NOT NULL
			RETURNING id`

		rows, err = r.db.QueryContext(ctx, q)
	}

	if err != nil {
		return nil, err
	}

	return drainIDRows(rows)
}

// backfillAbandonedOrphans promotes in_progress sessions that have no end_time but
// are old enough and have enough picks to be considered finished-but-lost.
// Returns the affected session IDs.
func (r *DraftSessionsRepository) backfillAbandonedOrphans(
	ctx context.Context,
	accountID int64,
	staleThreshold time.Time,
	minPicks int,
) ([]string, error) {
	var (
		rows *sql.Rows
		err  error
	)

	if accountID != 0 {
		const q = `
			UPDATE draft_sessions
			   SET status = 'abandoned', updated_at = NOW()
			 WHERE status = 'in_progress'
			   AND end_time IS NULL
			   AND total_picks >= $1
			   AND updated_at < $2
			   AND account_id = $3
			RETURNING id`

		rows, err = r.db.QueryContext(ctx, q, minPicks, staleThreshold, accountID)
	} else {
		const q = `
			UPDATE draft_sessions
			   SET status = 'abandoned', updated_at = NOW()
			 WHERE status = 'in_progress'
			   AND end_time IS NULL
			   AND total_picks >= $1
			   AND updated_at < $2
			RETURNING id`

		rows, err = r.db.QueryContext(ctx, q, minPicks, staleThreshold)
	}

	if err != nil {
		return nil, err
	}

	return drainIDRows(rows)
}

// drainIDRows scans a single-TEXT-column Rows result into a string slice.
func drainIDRows(rows *sql.Rows) ([]string, error) {
	defer func() { _ = rows.Close() }()

	var ids []string

	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}

		ids = append(ids, id)
	}

	return ids, rows.Err()
}
