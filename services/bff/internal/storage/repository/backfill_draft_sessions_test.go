package repository_test

// BackfillStaleDraftSessions integration tests — TDD RED phase.
//
// Four correctness invariants:
//   1. A genuinely active session (recent updated_at, low pick count) is NOT closed.
//   2. A session with end_time set (projection bug case) is closed as 'completed'.
//   3. A stale orphaned session (no end_time, full picks, old updated_at) is closed
//      as 'abandoned'.
//   4. Idempotent re-run: second call on an already-backfilled DB returns zero rows.
//   5. Account isolation: AccountID option scopes the backfill; other accounts untouched.
//   6. Already-closed sessions ('completed' / 'abandoned') are not re-processed.

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/RdHamilton/hollowmark/services/bff/internal/storage/repository"
)

// insertInProgressSession inserts a draft_sessions row with status='in_progress',
// the given updated_at, end_time (may be nil), and total_picks.
// Cleanup removes the row and any associated pick/match rows after the test.
func insertInProgressSession(
	t *testing.T,
	db *sql.DB,
	id string,
	accountID int64,
	updatedAt time.Time,
	endTime *time.Time,
	totalPicks int,
) {
	t.Helper()

	_, err := db.ExecContext(
		context.Background(),
		`INSERT INTO draft_sessions
			(id, account_id, event_name, set_code, draft_type, start_time, end_time,
			 status, total_picks, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		id, accountID, "Premier Draft BLB", "BLB", "PremierDraft",
		updatedAt.Add(-2*time.Hour),
		endTime,
		"in_progress",
		totalPicks,
		updatedAt,
	)
	if err != nil {
		t.Fatalf("insertInProgressSession %q: %v", id, err)
	}

	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM draft_match_results WHERE session_id = $1`, id)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM draft_picks WHERE session_id = $1`, id)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM draft_sessions WHERE id = $1`, id)
	})
}

// querySessionStatus returns the current status column of the given draft_sessions row.
func querySessionStatus(t *testing.T, db *sql.DB, id string) string {
	t.Helper()

	var status string

	err := db.QueryRowContext(
		context.Background(),
		`SELECT status FROM draft_sessions WHERE id = $1`,
		id,
	).Scan(&status)
	if err != nil {
		t.Fatalf("querySessionStatus %q: %v", id, err)
	}

	return status
}

// TestBackfillStaleDraftSessions_ActiveSession_NotClosed verifies that a session
// with a very recent updated_at and a low pick count is left as in_progress.
func TestBackfillStaleDraftSessions_ActiveSession_NotClosed(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewDraftSessionsRepository(db)

	accountID := insertTestAccount(t, db, fmt.Sprintf("bf-active-%d", time.Now().UnixNano()))
	id := fmt.Sprintf("ds-active-%d", time.Now().UnixNano())

	// Updated 2 minutes ago — clearly live.
	insertInProgressSession(t, db, id, accountID, time.Now().UTC().Add(-2*time.Minute), nil, 10)

	result, err := repo.BackfillStaleDraftSessions(context.Background(), repository.BackfillOptions{
		StalenessThreshold: 4 * time.Hour,
		MinPicksForStale:   42,
	})
	if err != nil {
		t.Fatalf("BackfillStaleDraftSessions: %v", err)
	}

	if got := querySessionStatus(t, db, id); got != "in_progress" {
		t.Errorf("active session: want status=in_progress, got=%q", got)
	}

	for _, closed := range result.ClosedCompleted {
		if closed == id {
			t.Errorf("active session %q incorrectly appears in ClosedCompleted", id)
		}
	}

	for _, closed := range result.ClosedAbandoned {
		if closed == id {
			t.Errorf("active session %q incorrectly appears in ClosedAbandoned", id)
		}
	}
}

// TestBackfillStaleDraftSessions_SessionWithEndTime_ClosedAsCompleted verifies that
// an in_progress session with end_time already set is promoted to 'completed'.
// This is the primary bug case from #1344 PR-B: projection set end_time but
// draft.completed never fired the status flip.
func TestBackfillStaleDraftSessions_SessionWithEndTime_ClosedAsCompleted(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewDraftSessionsRepository(db)

	accountID := insertTestAccount(t, db, fmt.Sprintf("bf-endtime-%d", time.Now().UnixNano()))
	id := fmt.Sprintf("ds-endtime-%d", time.Now().UnixNano())

	endTime := time.Now().UTC().Add(-6 * time.Hour)
	updatedAt := time.Now().UTC().Add(-6 * time.Hour)
	insertInProgressSession(t, db, id, accountID, updatedAt, &endTime, 45)

	result, err := repo.BackfillStaleDraftSessions(context.Background(), repository.BackfillOptions{
		StalenessThreshold: 4 * time.Hour,
		MinPicksForStale:   42,
	})
	if err != nil {
		t.Fatalf("BackfillStaleDraftSessions: %v", err)
	}

	if got := querySessionStatus(t, db, id); got != "completed" {
		t.Errorf("end_time session: want status=completed, got=%q", got)
	}

	found := false

	for _, closed := range result.ClosedCompleted {
		if closed == id {
			found = true
			break
		}
	}

	if !found {
		t.Errorf("end_time session %q not found in ClosedCompleted; got=%v", id, result.ClosedCompleted)
	}
}

// TestBackfillStaleDraftSessions_OrphanedStaleSession_ClosedAsAbandoned verifies
// that an in_progress session with no end_time, a full pick count (>= MinPicksForStale),
// and an old updated_at is closed as 'abandoned'.
// This covers Ramone's case: old daemon that never sent draft.completed.
func TestBackfillStaleDraftSessions_OrphanedStaleSession_ClosedAsAbandoned(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewDraftSessionsRepository(db)

	accountID := insertTestAccount(t, db, fmt.Sprintf("bf-orphan-%d", time.Now().UnixNano()))
	id := fmt.Sprintf("ds-orphan-%d", time.Now().UnixNano())

	// 10 hours old, 45 picks, no end_time.
	updatedAt := time.Now().UTC().Add(-10 * time.Hour)
	insertInProgressSession(t, db, id, accountID, updatedAt, nil, 45)

	result, err := repo.BackfillStaleDraftSessions(context.Background(), repository.BackfillOptions{
		StalenessThreshold: 4 * time.Hour,
		MinPicksForStale:   42,
	})
	if err != nil {
		t.Fatalf("BackfillStaleDraftSessions: %v", err)
	}

	if got := querySessionStatus(t, db, id); got != "abandoned" {
		t.Errorf("orphan session: want status=abandoned, got=%q", got)
	}

	found := false

	for _, closed := range result.ClosedAbandoned {
		if closed == id {
			found = true
			break
		}
	}

	if !found {
		t.Errorf("orphan session %q not found in ClosedAbandoned; got=%v", id, result.ClosedAbandoned)
	}
}

// TestBackfillStaleDraftSessions_Idempotent verifies that running the backfill
// twice leaves statuses stable and the second run closes zero rows.
func TestBackfillStaleDraftSessions_Idempotent(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewDraftSessionsRepository(db)

	accountID := insertTestAccount(t, db, fmt.Sprintf("bf-idem-%d", time.Now().UnixNano()))

	endTime := time.Now().UTC().Add(-8 * time.Hour)
	updatedAt := time.Now().UTC().Add(-8 * time.Hour)
	idWithEnd := fmt.Sprintf("ds-idem-end-%d", time.Now().UnixNano())
	idOrphan := fmt.Sprintf("ds-idem-orphan-%d", time.Now().UnixNano())

	insertInProgressSession(t, db, idWithEnd, accountID, updatedAt, &endTime, 45)
	insertInProgressSession(t, db, idOrphan, accountID, updatedAt, nil, 45)

	opts := repository.BackfillOptions{
		StalenessThreshold: 4 * time.Hour,
		MinPicksForStale:   42,
	}

	// First run.
	result1, err := repo.BackfillStaleDraftSessions(context.Background(), opts)
	if err != nil {
		t.Fatalf("BackfillStaleDraftSessions run 1: %v", err)
	}

	if len(result1.ClosedCompleted)+len(result1.ClosedAbandoned) == 0 {
		t.Fatal("run 1: expected at least one row closed, got none")
	}

	// Second run must be a no-op.
	result2, err := repo.BackfillStaleDraftSessions(context.Background(), opts)
	if err != nil {
		t.Fatalf("BackfillStaleDraftSessions run 2: %v", err)
	}

	if len(result2.ClosedCompleted) != 0 {
		t.Errorf("idempotent run 2: ClosedCompleted want 0, got %d: %v", len(result2.ClosedCompleted), result2.ClosedCompleted)
	}

	if len(result2.ClosedAbandoned) != 0 {
		t.Errorf("idempotent run 2: ClosedAbandoned want 0, got %d: %v", len(result2.ClosedAbandoned), result2.ClosedAbandoned)
	}

	// Statuses must be stable.
	if got := querySessionStatus(t, db, idWithEnd); got != "completed" {
		t.Errorf("after run 2: idWithEnd want completed, got=%q", got)
	}

	if got := querySessionStatus(t, db, idOrphan); got != "abandoned" {
		t.Errorf("after run 2: idOrphan want abandoned, got=%q", got)
	}
}

// TestBackfillStaleDraftSessions_AccountIsolation verifies that when BackfillOptions.AccountID
// is non-zero, only that account's rows are touched; other accounts are untouched.
func TestBackfillStaleDraftSessions_AccountIsolation(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewDraftSessionsRepository(db)

	nano := time.Now().UnixNano()
	accountA := insertTestAccount(t, db, fmt.Sprintf("bf-iso-a-%d", nano))
	accountB := insertTestAccount(t, db, fmt.Sprintf("bf-iso-b-%d", nano))

	updatedAt := time.Now().UTC().Add(-10 * time.Hour)
	idA := fmt.Sprintf("ds-iso-a-%d", nano)
	idB := fmt.Sprintf("ds-iso-b-%d", nano)

	// Both sessions are stale with full picks — both would qualify globally.
	insertInProgressSession(t, db, idA, accountA, updatedAt, nil, 45)
	insertInProgressSession(t, db, idB, accountB, updatedAt, nil, 45)

	// Scope the backfill to account A only.
	_, err := repo.BackfillStaleDraftSessions(context.Background(), repository.BackfillOptions{
		StalenessThreshold: 4 * time.Hour,
		MinPicksForStale:   42,
		AccountID:          accountA,
	})
	if err != nil {
		t.Fatalf("BackfillStaleDraftSessions: %v", err)
	}

	if got := querySessionStatus(t, db, idA); got != "abandoned" {
		t.Errorf("account A session: want abandoned, got=%q", got)
	}

	// Account B must be untouched.
	if got := querySessionStatus(t, db, idB); got != "in_progress" {
		t.Errorf("account B session (different account): want in_progress, got=%q", got)
	}
}

// TestBackfillStaleDraftSessions_AlreadyClosedSessions_Untouched verifies that rows
// already in 'completed' or 'abandoned' status are not re-processed or returned.
func TestBackfillStaleDraftSessions_AlreadyClosedSessions_Untouched(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewDraftSessionsRepository(db)

	nano := time.Now().UnixNano()
	accountID := insertTestAccount(t, db, fmt.Sprintf("bf-closed-%d", nano))
	updatedAt := time.Now().UTC().Add(-20 * time.Hour)
	startTime := updatedAt.Add(-2 * time.Hour)

	idCompleted := fmt.Sprintf("ds-already-completed-%d", nano)
	idAbandoned := fmt.Sprintf("ds-already-abandoned-%d", nano)

	for _, row := range []struct {
		id     string
		status string
	}{
		{idCompleted, "completed"},
		{idAbandoned, "abandoned"},
	} {
		_, err := db.ExecContext(
			context.Background(),
			`INSERT INTO draft_sessions
				(id, account_id, event_name, set_code, draft_type, start_time,
				 status, total_picks, updated_at)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			row.id, accountID, "Premier Draft BLB", "BLB", "PremierDraft",
			startTime, row.status, 45, updatedAt,
		)
		if err != nil {
			t.Fatalf("insert %s session: %v", row.status, err)
		}

		closedID := row.id // capture for closure

		t.Cleanup(func() {
			_, _ = db.ExecContext(context.Background(), `DELETE FROM draft_sessions WHERE id = $1`, closedID)
		})
	}

	result, err := repo.BackfillStaleDraftSessions(context.Background(), repository.BackfillOptions{
		StalenessThreshold: 4 * time.Hour,
		MinPicksForStale:   42,
	})
	if err != nil {
		t.Fatalf("BackfillStaleDraftSessions: %v", err)
	}

	for _, id := range result.ClosedCompleted {
		if id == idCompleted || id == idAbandoned {
			t.Errorf("already-closed session %q appears in ClosedCompleted", id)
		}
	}

	for _, id := range result.ClosedAbandoned {
		if id == idCompleted || id == idAbandoned {
			t.Errorf("already-closed session %q appears in ClosedAbandoned", id)
		}
	}

	// Statuses must be unchanged.
	if got := querySessionStatus(t, db, idCompleted); got != "completed" {
		t.Errorf("already-completed: want completed, got=%q", got)
	}

	if got := querySessionStatus(t, db, idAbandoned); got != "abandoned" {
		t.Errorf("already-abandoned: want abandoned, got=%q", got)
	}
}
