package repository_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/RdHamilton/hollowmark/services/bff/internal/storage/repository"
)

// insertTestDraftSession inserts a minimal draft_sessions row for the given account.
// The row (and any associated draft_match_results) is removed via t.Cleanup.
func insertTestDraftSession(t *testing.T, db *sql.DB, sessionID string, accountID int64, setCode string, startTime time.Time) {
	t.Helper()

	_, err := db.ExecContext(
		context.Background(),
		`INSERT INTO draft_sessions
			(id, account_id, event_name, set_code, draft_type, start_time, status)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		sessionID, accountID, "event-"+sessionID, setCode, "PremierDraft", startTime, "completed",
	)
	if err != nil {
		t.Fatalf("insertTestDraftSession %q: %v", sessionID, err)
	}

	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM draft_match_results WHERE session_id = $1`, sessionID)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM draft_sessions WHERE id = $1`, sessionID)
	})
}

// insertTestDraftMatchResult inserts a draft_match_results row for the given session.
// The session cleanup (registered by insertTestDraftSession) handles cascade deletion.
func insertTestDraftMatchResult(t *testing.T, db *sql.DB, sessionID, matchID, result string, ts time.Time) {
	t.Helper()

	_, err := db.ExecContext(
		context.Background(),
		`INSERT INTO draft_match_results
			(session_id, match_id, result, match_timestamp)
		 VALUES ($1, $2, $3, $4)`,
		sessionID, matchID, result, ts,
	)
	if err != nil {
		t.Fatalf("insertTestDraftMatchResult session=%q match=%q: %v", sessionID, matchID, err)
	}
}

// TestDraftSessionsRepository_ListByAccountID_ReturnsOnlyOwnRows verifies
// cross-account isolation: account A cannot see account B's draft sessions.
func TestDraftSessionsRepository_ListByAccountID_ReturnsOnlyOwnRows(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewDraftSessionsRepository(db)

	accountA := insertTestAccount(t, db, "draft-test-account-a")
	accountB := insertTestAccount(t, db, "draft-test-account-b")

	now := time.Now().UTC().Truncate(time.Second)

	insertTestDraftSession(t, db, fmt.Sprintf("ds-iso-a-%d", accountA), accountA, "ONE", now)
	insertTestDraftSession(t, db, fmt.Sprintf("ds-iso-b-%d", accountB), accountB, "ONE", now)

	rows, _, err := repo.ListByAccountID(context.Background(), accountA, "", 1, 100)
	if err != nil {
		t.Fatalf("ListByAccountID: %v", err)
	}

	bID := fmt.Sprintf("ds-iso-b-%d", accountB)

	for _, r := range rows {
		if r.ID == bID {
			t.Errorf("cross-account leak: accountA query returned accountB session %q", r.ID)
		}
	}
}

// TestDraftSessionsRepository_ListByAccountID_EmptyAccount verifies that an
// account with no sessions returns an empty slice (not an error).
func TestDraftSessionsRepository_ListByAccountID_EmptyAccount(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewDraftSessionsRepository(db)

	accountID := insertTestAccount(t, db, "draft-test-empty")

	rows, total, err := repo.ListByAccountID(context.Background(), accountID, "", 1, 10)
	if err != nil {
		t.Fatalf("ListByAccountID: %v", err)
	}

	if len(rows) != 0 {
		t.Errorf("expected 0 rows for empty account, got %d", len(rows))
	}

	if total != 0 {
		t.Errorf("expected total=0 for empty account, got %d", total)
	}
}

// TestDraftSessionsRepository_ListByAccountID_Pagination verifies offset/limit
// paging and ordering (start_time DESC).
func TestDraftSessionsRepository_ListByAccountID_Pagination(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewDraftSessionsRepository(db)

	accountID := insertTestAccount(t, db, "draft-test-pagination")

	base := time.Now().UTC().Truncate(time.Second)

	// Insert 3 sessions at different start_times.
	for i := 0; i < 3; i++ {
		ts := base.Add(time.Duration(i) * time.Hour)
		insertTestDraftSession(t, db, fmt.Sprintf("ds-page-%d-%d", accountID, i), accountID, "BLB", ts)
	}

	// Page 1, limit 2 — expect 2 rows, newest first.
	page1, total, err := repo.ListByAccountID(context.Background(), accountID, "", 1, 2)
	if err != nil {
		t.Fatalf("page 1: %v", err)
	}

	if total != 3 {
		t.Errorf("total: want 3, got %d", total)
	}

	if len(page1) != 2 {
		t.Fatalf("page 1 length: want 2, got %d", len(page1))
	}

	// Newest first: page1[0].StartTime >= page1[1].StartTime.
	if page1[0].StartTime.Before(page1[1].StartTime) {
		t.Errorf("expected DESC order: page1[0]=%v < page1[1]=%v", page1[0].StartTime, page1[1].StartTime)
	}

	// Page 2, limit 2 — expect 1 row.
	page2, _, err := repo.ListByAccountID(context.Background(), accountID, "", 2, 2)
	if err != nil {
		t.Fatalf("page 2: %v", err)
	}

	if len(page2) != 1 {
		t.Errorf("page 2 length: want 1, got %d", len(page2))
	}
}

// TestDraftSessionsRepository_ListByAccountID_SetCodeFilter verifies that the
// optional setCode filter restricts results correctly.
func TestDraftSessionsRepository_ListByAccountID_SetCodeFilter(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewDraftSessionsRepository(db)

	accountID := insertTestAccount(t, db, "draft-test-setcode-filter")

	now := time.Now().UTC().Truncate(time.Second)

	insertTestDraftSession(t, db, fmt.Sprintf("ds-set-one-%d", accountID), accountID, "ONE", now)
	insertTestDraftSession(t, db, fmt.Sprintf("ds-set-blb-%d", accountID), accountID, "BLB", now.Add(-time.Second))

	oneRows, oneTotal, err := repo.ListByAccountID(context.Background(), accountID, "ONE", 1, 10)
	if err != nil {
		t.Fatalf("ListByAccountID (ONE): %v", err)
	}

	if oneTotal != 1 {
		t.Errorf("setCode filter total: want 1, got %d", oneTotal)
	}

	for _, r := range oneRows {
		if r.SetCode != "ONE" {
			t.Errorf("setCode filter returned wrong set %q", r.SetCode)
		}
	}
}

// TestDraftSessionsRepository_ListByAccountID_WinsLossesAggregated verifies that
// the wins/losses columns are computed correctly via the LEFT JOIN on
// draft_match_results.
func TestDraftSessionsRepository_ListByAccountID_WinsLossesAggregated(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewDraftSessionsRepository(db)

	accountID := insertTestAccount(t, db, "draft-test-wl-agg")

	now := time.Now().UTC().Truncate(time.Second)
	sessionID := fmt.Sprintf("ds-wl-%d", accountID)

	insertTestDraftSession(t, db, sessionID, accountID, "MKM", now)

	// Seed 2 wins and 1 loss.
	insertTestDraftMatchResult(t, db, sessionID, "match-w1", "win", now.Add(time.Minute))
	insertTestDraftMatchResult(t, db, sessionID, "match-w2", "win", now.Add(2*time.Minute))
	insertTestDraftMatchResult(t, db, sessionID, "match-l1", "loss", now.Add(3*time.Minute))

	rows, _, err := repo.ListByAccountID(context.Background(), accountID, "", 1, 10)
	if err != nil {
		t.Fatalf("ListByAccountID: %v", err)
	}

	var found *repository.DraftSessionRow

	for i := range rows {
		if rows[i].ID == sessionID {
			found = &rows[i]
			break
		}
	}

	if found == nil {
		t.Fatalf("seeded session %q not found in results", sessionID)
	}

	if found.Wins != 2 {
		t.Errorf("wins: want 2, got %d", found.Wins)
	}

	if found.Losses != 1 {
		t.Errorf("losses: want 1, got %d", found.Losses)
	}
}

// TestDraftSessionsRepository_UpsertDraftSession_InsertAndUpdate verifies that
// UpsertDraftSession creates a new row on first call and updates it on second
// call with the same id.
func TestDraftSessionsRepository_UpsertDraftSession_InsertAndUpdate(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewDraftSessionsRepository(db)

	accountID := insertTestAccount(t, db, "draft-test-upsert")

	sessionID := fmt.Sprintf("ds-upsert-%d", accountID)
	now := time.Now().UTC().Truncate(time.Second)

	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM draft_sessions WHERE id = $1`, sessionID)
	})

	s := repository.DraftSessionUpsert{
		ID:        sessionID,
		AccountID: accountID,
		EventName: "TestEvent",
		SetCode:   "ONE",
		DraftType: "PremierDraft",
		StartTime: now,
		Status:    "in_progress",
	}

	if err := repo.UpsertDraftSession(context.Background(), s); err != nil {
		t.Fatalf("first UpsertDraftSession: %v", err)
	}

	// Update status to completed.
	s.Status = "completed"
	s.TotalPicks = 42

	if err := repo.UpsertDraftSession(context.Background(), s); err != nil {
		t.Fatalf("second UpsertDraftSession: %v", err)
	}

	// Verify status updated and total_picks is GREATEST(42, 0) = 42.
	var status string
	var picks int

	err := db.QueryRowContext(
		context.Background(),
		`SELECT status, total_picks FROM draft_sessions WHERE id = $1`,
		sessionID,
	).Scan(&status, &picks)
	if err != nil {
		t.Fatalf("select after upsert: %v", err)
	}

	if status != "completed" {
		t.Errorf("status: want completed, got %q", status)
	}

	if picks != 42 {
		t.Errorf("total_picks: want 42, got %d", picks)
	}
}

// TestDraftSessionsRepository_Interface is a compile-time check.
func TestDraftSessionsRepository_Interface(t *testing.T) {
	var db repository.DB = &fakeDB{}
	repo := repository.NewDraftSessionsRepository(db)

	if repo == nil {
		t.Fatal("NewDraftSessionsRepository returned nil")
	}
}

// ─── ADR-051 integration tests ────────────────────────────────────────────────

// TestSessionExists verifies that SessionExists returns true for a known
// session owned by the account and false for an unknown or cross-account ID.
func TestSessionExists(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewDraftSessionsRepository(db)

	accountID := insertTestAccount(t, db, "session-exists-acct")
	otherAccountID := insertTestAccount(t, db, "session-exists-other-acct")
	now := time.Now().UTC().Truncate(time.Second)
	sessionID := fmt.Sprintf("ds-exists-%d", accountID)
	insertTestDraftSession(t, db, sessionID, accountID, "SOS", now)

	// Known session, correct account — expect true.
	exists, err := repo.SessionExists(context.Background(), accountID, sessionID)
	if err != nil {
		t.Fatalf("SessionExists: %v", err)
	}
	if !exists {
		t.Error("expected SessionExists=true for known session, got false")
	}

	// Known session ID but wrong account — expect false (cross-account isolation).
	crossExists, err := repo.SessionExists(context.Background(), otherAccountID, sessionID)
	if err != nil {
		t.Fatalf("SessionExists cross-account: %v", err)
	}
	if crossExists {
		t.Error("expected SessionExists=false for cross-account check, got true")
	}

	// Unknown session ID — expect false.
	noExists, err := repo.SessionExists(context.Background(), accountID, "nonexistent-session")
	if err != nil {
		t.Fatalf("SessionExists unknown: %v", err)
	}
	if noExists {
		t.Error("expected SessionExists=false for unknown session, got true")
	}
}

// TestInferSessionForMatch_OneCandidate verifies that InferSessionForMatch
// returns the session ID when exactly one completed session matches within the
// 48-hour window.
func TestInferSessionForMatch_OneCandidate(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewDraftSessionsRepository(db)

	accountID := insertTestAccount(t, db, "infer-one-acct")
	matchTime := time.Now().UTC().Truncate(time.Second)
	sessionStart := matchTime.Add(-1 * time.Hour)
	sessionID := fmt.Sprintf("ds-infer-one-%d", accountID)
	eventName := fmt.Sprintf("QuickDraft_SOS_%d", accountID)

	// Insert a completed session matching event_name and time window.
	_, err := db.ExecContext(
		context.Background(),
		`INSERT INTO draft_sessions
			(id, account_id, event_name, set_code, draft_type, start_time, status)
		 VALUES ($1, $2, $3, $4, $5, $6, 'completed')`,
		sessionID, accountID, eventName, "SOS", "PremierDraft", sessionStart,
	)
	if err != nil {
		t.Fatalf("insert draft_sessions: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM draft_sessions WHERE id = $1`, sessionID)
	})

	got, err := repo.InferSessionForMatch(context.Background(), accountID, eventName, matchTime)
	if err != nil {
		t.Fatalf("InferSessionForMatch: %v", err)
	}
	if got != sessionID {
		t.Errorf("InferSessionForMatch: want %q, got %q", sessionID, got)
	}
}

// TestInferSessionForMatch_MultipleAmbiguous verifies that InferSessionForMatch
// returns ("", nil) when multiple sessions match — ambiguity is not guessed.
func TestInferSessionForMatch_MultipleAmbiguous(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewDraftSessionsRepository(db)

	accountID := insertTestAccount(t, db, "infer-ambig-acct")
	matchTime := time.Now().UTC().Truncate(time.Second)
	eventName := fmt.Sprintf("QuickDraft_AMB_%d", accountID)

	for i := 0; i < 2; i++ {
		sessID := fmt.Sprintf("ds-infer-ambig-%d-%d", accountID, i)
		sessStart := matchTime.Add(-time.Duration(i+1) * time.Hour)
		_, err := db.ExecContext(
			context.Background(),
			`INSERT INTO draft_sessions
				(id, account_id, event_name, set_code, draft_type, start_time, status)
			 VALUES ($1, $2, $3, $4, $5, $6, 'completed')`,
			sessID, accountID, eventName, "AMB", "PremierDraft", sessStart,
		)
		if err != nil {
			t.Fatalf("insert draft_sessions[%d]: %v", i, err)
		}
		iCopy := i
		t.Cleanup(func() {
			_, _ = db.ExecContext(context.Background(), `DELETE FROM draft_sessions WHERE id = $1`,
				fmt.Sprintf("ds-infer-ambig-%d-%d", accountID, iCopy))
		})
	}

	got, err := repo.InferSessionForMatch(context.Background(), accountID, eventName, matchTime)
	if err != nil {
		t.Fatalf("InferSessionForMatch ambiguous: %v", err)
	}
	if got != "" {
		t.Errorf("expected empty string for ambiguous inference, got %q", got)
	}
}

// TestInsertDraftMatchResult_Idempotent verifies that calling InsertDraftMatchResult
// twice with the same (session_id, match_id) does not return an error (ON CONFLICT
// DO NOTHING).
func TestInsertDraftMatchResult_Idempotent(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewDraftSessionsRepository(db)

	accountID := insertTestAccount(t, db, "dmr-idempotent-acct")
	now := time.Now().UTC().Truncate(time.Second)
	sessionID := fmt.Sprintf("ds-dmr-%d", accountID)
	matchID := fmt.Sprintf("match-dmr-%d", accountID)

	insertTestDraftSession(t, db, sessionID, accountID, "SOS", now)
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM draft_match_results WHERE match_id = $1`, matchID)
	})

	ins := repository.DraftMatchResultInsert{
		SessionID:      sessionID,
		MatchID:        matchID,
		Result:         "win",
		GameWins:       2,
		GameLosses:     1,
		MatchTimestamp: now,
	}

	if err := repo.InsertDraftMatchResult(context.Background(), ins); err != nil {
		t.Fatalf("first InsertDraftMatchResult: %v", err)
	}
	// Second call must succeed (ON CONFLICT DO NOTHING).
	if err := repo.InsertDraftMatchResult(context.Background(), ins); err != nil {
		t.Fatalf("second InsertDraftMatchResult (idempotency): %v", err)
	}

	var count int
	if err := db.QueryRowContext(
		context.Background(),
		`SELECT COUNT(*) FROM draft_match_results WHERE session_id = $1 AND match_id = $2`,
		sessionID, matchID,
	).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 row after idempotent insert, got %d", count)
	}
}

// ─── format_type / is_trophy integration tests (Prof additions) ─────────────

// TestUpsertDraftSession_FormatType verifies that UpsertDraftSession stores the
// supplied FormatType and that a partial upsert (empty FormatType) preserves the
// existing value rather than overwriting it with the default.
func TestUpsertDraftSession_FormatType(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewDraftSessionsRepository(db)

	accountID := insertTestAccount(t, db, "draft-fmt-acct")
	sessionID := fmt.Sprintf("ds-fmt-%d", accountID)
	now := time.Now().UTC().Truncate(time.Second)

	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM draft_sessions WHERE id = $1`, sessionID)
	})

	// INSERT with FormatType = "premier_draft".
	if err := repo.UpsertDraftSession(context.Background(), repository.DraftSessionUpsert{
		ID:         sessionID,
		AccountID:  accountID,
		EventName:  "PremierDraft_BLB",
		SetCode:    "BLB",
		DraftType:  "premier_draft",
		StartTime:  now,
		Status:     "in_progress",
		FormatType: "premier_draft",
	}); err != nil {
		t.Fatalf("first UpsertDraftSession: %v", err)
	}

	var ft string
	if err := db.QueryRowContext(
		context.Background(),
		`SELECT format_type FROM draft_sessions WHERE id = $1`, sessionID,
	).Scan(&ft); err != nil {
		t.Fatalf("select after insert: %v", err)
	}
	if ft != "premier_draft" {
		t.Errorf("format_type after insert: want premier_draft, got %q", ft)
	}

	// Partial upsert with empty FormatType must NOT clobber the stored value.
	if err := repo.UpsertDraftSession(context.Background(), repository.DraftSessionUpsert{
		ID:         sessionID,
		AccountID:  accountID,
		StartTime:  now,
		Status:     "in_progress",
		TotalPicks: 1,
		// FormatType intentionally empty
	}); err != nil {
		t.Fatalf("partial UpsertDraftSession: %v", err)
	}

	if err := db.QueryRowContext(
		context.Background(),
		`SELECT format_type FROM draft_sessions WHERE id = $1`, sessionID,
	).Scan(&ft); err != nil {
		t.Fatalf("select after partial upsert: %v", err)
	}
	if ft != "premier_draft" {
		t.Errorf("format_type after partial upsert: want premier_draft (preserved), got %q", ft)
	}
}

// TestUpsertDraftSession_IsTrophy_SetOnCompletion verifies that IsTrophy=true
// is stored when a session completes with wins >= 7, and that a subsequent
// partial upsert with IsTrophy=nil does not clear it.
func TestUpsertDraftSession_IsTrophy_SetOnCompletion(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewDraftSessionsRepository(db)

	accountID := insertTestAccount(t, db, "draft-trophy-acct")
	sessionID := fmt.Sprintf("ds-trophy-%d", accountID)
	now := time.Now().UTC().Truncate(time.Second)

	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM draft_sessions WHERE id = $1`, sessionID)
	})

	tru := true

	// INSERT then update to completed with IsTrophy=true.
	if err := repo.UpsertDraftSession(context.Background(), repository.DraftSessionUpsert{
		ID:        sessionID,
		AccountID: accountID,
		EventName: "QuickDraft_SOS",
		SetCode:   "SOS",
		StartTime: now,
		Status:    "in_progress",
	}); err != nil {
		t.Fatalf("first UpsertDraftSession: %v", err)
	}

	endTime := now.Add(2 * time.Hour)
	if err := repo.UpsertDraftSession(context.Background(), repository.DraftSessionUpsert{
		ID:        sessionID,
		AccountID: accountID,
		EventName: "QuickDraft_SOS",
		SetCode:   "SOS",
		StartTime: now,
		EndTime:   &endTime,
		Status:    "completed",
		IsTrophy:  &tru,
	}); err != nil {
		t.Fatalf("completion UpsertDraftSession: %v", err)
	}

	var isTrophy bool
	if err := db.QueryRowContext(
		context.Background(),
		`SELECT is_trophy FROM draft_sessions WHERE id = $1`, sessionID,
	).Scan(&isTrophy); err != nil {
		t.Fatalf("select after completion: %v", err)
	}
	if !isTrophy {
		t.Error("is_trophy: want true after completion with 7 wins, got false")
	}

	// Partial upsert with nil IsTrophy must NOT clear the trophy flag.
	if err := repo.UpsertDraftSession(context.Background(), repository.DraftSessionUpsert{
		ID:        sessionID,
		AccountID: accountID,
		StartTime: now,
		Status:    "completed",
		// IsTrophy nil — must preserve existing TRUE
	}); err != nil {
		t.Fatalf("partial UpsertDraftSession after trophy: %v", err)
	}

	if err := db.QueryRowContext(
		context.Background(),
		`SELECT is_trophy FROM draft_sessions WHERE id = $1`, sessionID,
	).Scan(&isTrophy); err != nil {
		t.Fatalf("select after partial upsert: %v", err)
	}
	if !isTrophy {
		t.Error("is_trophy: want true (sticky), got false after partial upsert with nil IsTrophy")
	}
}

// TestGetWinsForSession verifies that GetWinsForSession counts only 'win'
// rows in draft_match_results for the given session.
func TestGetWinsForSession(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewDraftSessionsRepository(db)

	accountID := insertTestAccount(t, db, "draft-wins-acct")
	now := time.Now().UTC().Truncate(time.Second)
	sessionID := fmt.Sprintf("ds-wins-%d", accountID)

	insertTestDraftSession(t, db, sessionID, accountID, "SOS", now)

	// 3 wins, 1 loss.
	insertTestDraftMatchResult(t, db, sessionID, "mw1", "win", now.Add(time.Minute))
	insertTestDraftMatchResult(t, db, sessionID, "mw2", "win", now.Add(2*time.Minute))
	insertTestDraftMatchResult(t, db, sessionID, "mw3", "win", now.Add(3*time.Minute))
	insertTestDraftMatchResult(t, db, sessionID, "ml1", "loss", now.Add(4*time.Minute))

	wins, err := repo.GetWinsForSession(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("GetWinsForSession: %v", err)
	}
	if wins != 3 {
		t.Errorf("wins: want 3, got %d", wins)
	}
}

// TestGetWinsForSession_Trophy verifies that GetWinsForSession returns >= 7
// for a trophy session (enabling the projection worker to set is_trophy=true).
func TestGetWinsForSession_Trophy(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewDraftSessionsRepository(db)

	accountID := insertTestAccount(t, db, "draft-trophy-wins-acct")
	now := time.Now().UTC().Truncate(time.Second)
	sessionID := fmt.Sprintf("ds-trophy-wins-%d", accountID)

	insertTestDraftSession(t, db, sessionID, accountID, "SOS", now)

	for i := 0; i < 7; i++ {
		insertTestDraftMatchResult(t, db, sessionID, fmt.Sprintf("mw-%d", i), "win", now.Add(time.Duration(i+1)*time.Minute))
	}
	insertTestDraftMatchResult(t, db, sessionID, "ml1", "loss", now.Add(8*time.Minute))

	wins, err := repo.GetWinsForSession(context.Background(), sessionID)
	if err != nil {
		t.Fatalf("GetWinsForSession trophy: %v", err)
	}
	if wins < 7 {
		t.Errorf("wins: want >= 7 for trophy, got %d", wins)
	}
}

// TestListByAccountID_FormatTypeAndIsTrophy verifies that format_type and
// is_trophy are returned correctly by ListByAccountID.
func TestListByAccountID_FormatTypeAndIsTrophy(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewDraftSessionsRepository(db)

	accountID := insertTestAccount(t, db, "draft-list-fmt-trophy-acct")
	now := time.Now().UTC().Truncate(time.Second)
	sessionID := fmt.Sprintf("ds-list-fmt-%d", accountID)

	// Insert a session with format_type=premier_draft and is_trophy=true directly.
	_, err := db.ExecContext(
		context.Background(),
		`INSERT INTO draft_sessions
			(id, account_id, event_name, set_code, draft_type, start_time, status, format_type, is_trophy)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		sessionID, accountID, "PremierDraft_BLB", "BLB", "PremierDraft", now, "completed",
		"premier_draft", true,
	)
	if err != nil {
		t.Fatalf("insert draft_sessions: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM draft_sessions WHERE id = $1`, sessionID)
	})

	rows, _, err := repo.ListByAccountID(context.Background(), accountID, "", 1, 10)
	if err != nil {
		t.Fatalf("ListByAccountID: %v", err)
	}

	var found *repository.DraftSessionRow
	for i := range rows {
		if rows[i].ID == sessionID {
			found = &rows[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("seeded session %q not found in results", sessionID)
	}
	if found.FormatType != "premier_draft" {
		t.Errorf("FormatType: want premier_draft, got %q", found.FormatType)
	}
	if !found.IsTrophy {
		t.Error("IsTrophy: want true, got false")
	}
}

// TestInsertDraftPick_Idempotent verifies that InsertDraftPick with the same
// (session_id, pack_number, pick_number) twice does not return an error.
func TestInsertDraftPick_Idempotent(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewDraftSessionsRepository(db)

	accountID := insertTestAccount(t, db, "dp-idempotent-acct")
	now := time.Now().UTC().Truncate(time.Second)
	sessionID := fmt.Sprintf("ds-dp-%d", accountID)

	insertTestDraftSession(t, db, sessionID, accountID, "SOS", now)
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM draft_picks WHERE session_id = $1`, sessionID)
	})

	ins := repository.DraftPickInsert{
		SessionID:  sessionID,
		PackNumber: 0,
		PickNumber: 0,
		CardID:     "102704",
		Timestamp:  now,
	}

	if err := repo.InsertDraftPick(context.Background(), ins); err != nil {
		t.Fatalf("first InsertDraftPick: %v", err)
	}
	// Second call must succeed (ON CONFLICT DO NOTHING).
	if err := repo.InsertDraftPick(context.Background(), ins); err != nil {
		t.Fatalf("second InsertDraftPick (idempotency): %v", err)
	}

	var count int
	if err := db.QueryRowContext(
		context.Background(),
		`SELECT COUNT(*) FROM draft_picks WHERE session_id = $1`,
		sessionID,
	).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 pick row after idempotent insert, got %d", count)
	}
}
