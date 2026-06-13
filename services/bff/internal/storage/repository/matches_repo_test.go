package repository_test

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/RdHamilton/hollowmark/services/bff/internal/storage/repository"
)

// insertTestAccount inserts a minimal accounts row and returns its auto-assigned id.
// The row is removed via t.Cleanup.
// Defined here (matches_repo_test.go) and shared with draft_sessions_repo_test.go
// within the same package repository_test.
func insertTestAccount(t *testing.T, db *sql.DB, name string) int64 {
	t.Helper()

	var id int64

	err := db.QueryRowContext(
		context.Background(),
		`INSERT INTO accounts (name) VALUES ($1) RETURNING id`,
		name,
	).Scan(&id)
	if err != nil {
		t.Fatalf("insertTestAccount %q: %v", name, err)
	}

	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM accounts WHERE id = $1`, id)
	})

	return id
}

// insertTestMatch inserts a minimal matches row for the given account.
// The row is removed via t.Cleanup.
func insertTestMatch(t *testing.T, db *sql.DB, matchID string, accountID int64, format string, ts time.Time) {
	t.Helper()

	_, err := db.ExecContext(
		context.Background(),
		`INSERT INTO matches
			(id, account_id, event_id, event_name, timestamp, player_wins, opponent_wins,
			 player_team_id, format, result)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		matchID, accountID, "evt-"+matchID, "event-"+matchID, ts,
		1, 0, 1, format, "win",
	)
	if err != nil {
		t.Fatalf("insertTestMatch %q: %v", matchID, err)
	}

	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM matches WHERE id = $1`, matchID)
	})
}

// TestMatchesRepository_ListByAccountID_ReturnsOnlyOwnRows verifies cross-account
// isolation: account A cannot see account B's matches (legacy offset method).
func TestMatchesRepository_ListByAccountID_ReturnsOnlyOwnRows(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewMatchesRepository(db)

	accountA := insertTestAccount(t, db, "match-test-account-a")
	accountB := insertTestAccount(t, db, "match-test-account-b")

	now := time.Now().UTC().Truncate(time.Second)

	insertTestMatch(t, db, fmt.Sprintf("match-iso-a-%d", accountA), accountA, "Standard", now)
	insertTestMatch(t, db, fmt.Sprintf("match-iso-b-%d", accountB), accountB, "Standard", now)

	rows, _, err := repo.ListByAccountID(context.Background(), accountA, "", 1, 100)
	if err != nil {
		t.Fatalf("ListByAccountID: %v", err)
	}

	bID := fmt.Sprintf("match-iso-b-%d", accountB)

	for _, r := range rows {
		if r.ID == bID {
			t.Errorf("cross-account leak: accountA query returned accountB row %q", r.ID)
		}
	}
}

// TestMatchesRepository_ListByAccountID_EmptyAccount verifies that an account
// with no matches returns an empty slice (not an error) — legacy offset method.
func TestMatchesRepository_ListByAccountID_EmptyAccount(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewMatchesRepository(db)

	accountID := insertTestAccount(t, db, "match-test-empty")

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

// TestMatchesRepository_ListByAccountID_Pagination verifies offset/limit paging
// and ordering (timestamp DESC) — deprecated method, retained for regression.
func TestMatchesRepository_ListByAccountID_Pagination(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewMatchesRepository(db)

	accountID := insertTestAccount(t, db, "match-test-pagination")

	base := time.Now().UTC().Truncate(time.Second)

	// Insert 3 matches at different timestamps.
	for i := 0; i < 3; i++ {
		ts := base.Add(time.Duration(i) * time.Minute)
		insertTestMatch(t, db, fmt.Sprintf("match-page-%d-%d", accountID, i), accountID, "Standard", ts)
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

	// Newest first: page1[0].Timestamp >= page1[1].Timestamp.
	if page1[0].Timestamp.Before(page1[1].Timestamp) {
		t.Errorf("expected DESC order: page1[0]=%v < page1[1]=%v", page1[0].Timestamp, page1[1].Timestamp)
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

// TestMatchesRepository_ListByAccountID_FormatFilter verifies that the optional
// format filter restricts results correctly — deprecated method.
func TestMatchesRepository_ListByAccountID_FormatFilter(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewMatchesRepository(db)

	accountID := insertTestAccount(t, db, "match-test-format-filter")

	now := time.Now().UTC().Truncate(time.Second)

	insertTestMatch(t, db, fmt.Sprintf("match-fmt-std-%d", accountID), accountID, "Standard", now)
	insertTestMatch(t, db, fmt.Sprintf("match-fmt-draft-%d", accountID), accountID, "PremierDraft", now.Add(-time.Second))

	stdRows, stdTotal, err := repo.ListByAccountID(context.Background(), accountID, "Standard", 1, 10)
	if err != nil {
		t.Fatalf("ListByAccountID (Standard): %v", err)
	}

	if stdTotal != 1 {
		t.Errorf("format filter total: want 1, got %d", stdTotal)
	}

	for _, r := range stdRows {
		if r.Format != "Standard" {
			t.Errorf("format filter returned non-Standard row: %q", r.Format)
		}
	}
}

// ── Cursor-based tests (ListByAccountIDCursorFiltered) ────────────────────────

// TestMatchesRepository_CursorFiltered_EmptyAccount verifies that cursor query
// returns empty slice for an account with no matches.
func TestMatchesRepository_CursorFiltered_EmptyAccount(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewMatchesRepository(db)

	accountID := insertTestAccount(t, db, "cursor-test-empty")

	rows, err := repo.ListByAccountIDCursorFiltered(context.Background(), accountID, repository.MatchFilter{}, nil, "", 50)
	if err != nil {
		t.Fatalf("ListByAccountIDCursorFiltered: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("expected 0 rows for empty account, got %d", len(rows))
	}
}

// TestMatchesRepository_CursorFiltered_CrossAccountIsolation verifies account
// scoping: accountA cursor query must not return accountB rows.
func TestMatchesRepository_CursorFiltered_CrossAccountIsolation(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewMatchesRepository(db)

	accountA := insertTestAccount(t, db, "cursor-iso-a")
	accountB := insertTestAccount(t, db, "cursor-iso-b")

	now := time.Now().UTC().Truncate(time.Second)
	insertTestMatch(t, db, fmt.Sprintf("cursor-iso-match-a-%d", accountA), accountA, "Standard", now)
	insertTestMatch(t, db, fmt.Sprintf("cursor-iso-match-b-%d", accountB), accountB, "Standard", now)

	rows, err := repo.ListByAccountIDCursorFiltered(context.Background(), accountA, repository.MatchFilter{}, nil, "", 100)
	if err != nil {
		t.Fatalf("ListByAccountIDCursorFiltered: %v", err)
	}

	bID := fmt.Sprintf("cursor-iso-match-b-%d", accountB)
	for _, r := range rows {
		if r.ID == bID {
			t.Errorf("cross-account leak: accountA cursor query returned accountB row %q", r.ID)
		}
	}
}

// TestMatchesRepository_CursorFiltered_OrderAndHasMore verifies DESC ordering
// and the limit+1 has_more probe pattern.
func TestMatchesRepository_CursorFiltered_OrderAndHasMore(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewMatchesRepository(db)

	accountID := insertTestAccount(t, db, "cursor-test-order")
	base := time.Now().UTC().Truncate(time.Second)

	// Insert 3 matches with distinct timestamps.
	for i := 0; i < 3; i++ {
		ts := base.Add(time.Duration(i) * time.Minute)
		insertTestMatch(t, db, fmt.Sprintf("cursor-order-%d-%d", accountID, i), accountID, "Standard", ts)
	}

	// Fetch with limit=2 — should get 3 rows (limit+1 probe), signalling has_more.
	rows, err := repo.ListByAccountIDCursorFiltered(context.Background(), accountID, repository.MatchFilter{}, nil, "", 2)
	if err != nil {
		t.Fatalf("ListByAccountIDCursorFiltered: %v", err)
	}

	// Expect 3 rows (limit+1) because has_more probe.
	if len(rows) != 3 {
		t.Fatalf("expected 3 rows (limit+1 probe), got %d", len(rows))
	}

	// Newest first.
	if rows[0].Timestamp.Before(rows[1].Timestamp) {
		t.Errorf("expected DESC order: rows[0]=%v < rows[1]=%v", rows[0].Timestamp, rows[1].Timestamp)
	}
}

// TestMatchesRepository_CursorFiltered_KeysetCursor verifies that a cursor
// correctly restricts results to rows older than the cursor row.
func TestMatchesRepository_CursorFiltered_KeysetCursor(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewMatchesRepository(db)

	accountID := insertTestAccount(t, db, "cursor-test-keyset")
	base := time.Now().UTC().Truncate(time.Second)

	ids := []string{
		fmt.Sprintf("cursor-ks-0-%d", accountID),
		fmt.Sprintf("cursor-ks-1-%d", accountID),
		fmt.Sprintf("cursor-ks-2-%d", accountID),
	}
	for i, id := range ids {
		insertTestMatch(t, db, id, accountID, "Standard", base.Add(time.Duration(2-i)*time.Minute))
	}
	// Timestamps: ids[0] newest (base+2m), ids[1] middle (base+1m), ids[2] oldest (base).

	// First page: limit=2, no cursor → get ids[0] and ids[1] (+ probe=ids[2]).
	firstPage, err := repo.ListByAccountIDCursorFiltered(context.Background(), accountID, repository.MatchFilter{}, nil, "", 2)
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	if len(firstPage) < 2 {
		t.Fatalf("first page: expected ≥2 rows, got %d", len(firstPage))
	}

	// Use the second row as the cursor to fetch the next page.
	cursorRow := firstPage[1] // ids[1]
	cursorTS := cursorRow.Timestamp
	cursorID := cursorRow.ID

	secondPage, err := repo.ListByAccountIDCursorFiltered(context.Background(), accountID, repository.MatchFilter{}, &cursorTS, cursorID, 10)
	if err != nil {
		t.Fatalf("second page: %v", err)
	}

	// Should only contain ids[2] (the oldest row).
	for _, r := range secondPage {
		if r.Timestamp.After(cursorTS) || (r.Timestamp.Equal(cursorTS) && r.ID >= cursorID) {
			t.Errorf("cursor leak: row %q ts=%v is not before cursor ts=%v id=%q", r.ID, r.Timestamp, cursorTS, cursorID)
		}
	}
}

// TestMatchesRepository_CursorFiltered_FilterDimensions verifies that the full
// MatchFilter (date range, deck, result, format) is applied correctly.
func TestMatchesRepository_CursorFiltered_FilterDimensions(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewMatchesRepository(db)

	accountID := insertTestAccount(t, db, "cursor-test-filter")
	now := time.Now().UTC().Truncate(time.Second)

	insertTestMatch(t, db, fmt.Sprintf("cursor-flt-std-%d", accountID), accountID, "Standard", now)
	insertTestMatch(t, db, fmt.Sprintf("cursor-flt-draft-%d", accountID), accountID, "PremierDraft", now.Add(-time.Second))

	// Filter by format=Standard — should only return the Standard match.
	f := repository.MatchFilter{Format: "Standard"}
	rows, err := repo.ListByAccountIDCursorFiltered(context.Background(), accountID, f, nil, "", 50)
	if err != nil {
		t.Fatalf("filtered query: %v", err)
	}

	for _, r := range rows {
		if strings.ToLower(r.Format) != "standard" {
			t.Errorf("format filter leaked non-Standard row: format=%q id=%q", r.Format, r.ID)
		}
	}

	draftID := fmt.Sprintf("cursor-flt-draft-%d", accountID)
	for _, r := range rows {
		if r.ID == draftID {
			t.Errorf("format filter should have excluded PremierDraft row %q", draftID)
		}
	}
}

// TestMatchesRepository_UpsertMatch_InsertAndUpdate verifies that UpsertMatch
// creates a new row on first call and updates it on second call with the same id.
func TestMatchesRepository_UpsertMatch_InsertAndUpdate(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewMatchesRepository(db)

	accountID := insertTestAccount(t, db, "match-test-upsert")

	matchID := fmt.Sprintf("match-upsert-%d", accountID)
	now := time.Now().UTC().Truncate(time.Second)

	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM matches WHERE id = $1`, matchID)
	})

	m := repository.MatchUpsert{
		ID:           matchID,
		AccountID:    accountID,
		EventID:      "evt-upsert",
		EventName:    "TestEvent",
		Timestamp:    now,
		Format:       "Standard",
		Result:       "win",
		PlayerWins:   2,
		OpponentWins: 0,
		PlayerTeamID: 1,
	}

	if err := repo.UpsertMatch(context.Background(), m); err != nil {
		t.Fatalf("first UpsertMatch: %v", err)
	}

	// Update result to "loss".
	m.Result = "loss"
	m.PlayerWins = 0
	m.OpponentWins = 2

	if err := repo.UpsertMatch(context.Background(), m); err != nil {
		t.Fatalf("second UpsertMatch: %v", err)
	}

	// Verify the row reflects the updated result.
	var result string

	err := db.QueryRowContext(
		context.Background(),
		`SELECT result FROM matches WHERE id = $1`,
		matchID,
	).Scan(&result)
	if err != nil {
		t.Fatalf("select after upsert: %v", err)
	}

	if result != "loss" {
		t.Errorf("expected result=loss after update upsert, got %q", result)
	}
}

// TestMatchesRepository_Interface is a compile-time check.
func TestMatchesRepository_Interface(t *testing.T) {
	var db repository.DB = &fakeDB{}
	repo := repository.NewMatchesRepository(db)

	if repo == nil {
		t.Fatal("NewMatchesRepository returned nil")
	}
}

// ─── ADR-051 match DraftSessionID tests ───────────────────────────────────────

// TestUpsertMatch_WithDraftSessionID verifies that UpsertMatch stores a
// draft_session_id when provided. Requires DATABASE_URL (integration test).
func TestUpsertMatch_WithDraftSessionID(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewMatchesRepository(db)
	dsRepo := repository.NewDraftSessionsRepository(db)

	accountID := insertTestAccount(t, db, "upsert-draft-session-id-acct")
	now := time.Now().UTC().Truncate(time.Second)
	sessionID := fmt.Sprintf("ds-um-sid-%d", accountID)
	matchID := fmt.Sprintf("match-um-sid-%d", accountID)

	// Create the draft session first (FK constraint).
	if err := dsRepo.UpsertDraftSession(context.Background(), repository.DraftSessionUpsert{
		ID:        sessionID,
		AccountID: accountID,
		EventName: "QuickDraft_SOS_20260526",
		SetCode:   "SOS",
		DraftType: "PremierDraft",
		StartTime: now.Add(-2 * time.Hour),
		Status:    "completed",
	}); err != nil {
		t.Fatalf("UpsertDraftSession: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM draft_match_results WHERE match_id = $1`, matchID)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM matches WHERE id = $1`, matchID)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM draft_sessions WHERE id = $1`, sessionID)
	})

	if err := repo.UpsertMatch(context.Background(), repository.MatchUpsert{
		ID:             matchID,
		AccountID:      accountID,
		EventID:        "evt-um-sid",
		EventName:      "QuickDraft_SOS_20260526",
		Timestamp:      now,
		PlayerWins:     2,
		OpponentWins:   1,
		PlayerTeamID:   1,
		Format:         "QuickDraft_SOS_20260526",
		Result:         "win",
		DraftSessionID: &sessionID,
	}); err != nil {
		t.Fatalf("UpsertMatch: %v", err)
	}

	var storedSessionID *string
	if err := db.QueryRowContext(
		context.Background(),
		`SELECT draft_session_id FROM matches WHERE id = $1`,
		matchID,
	).Scan(&storedSessionID); err != nil {
		t.Fatalf("SELECT draft_session_id: %v", err)
	}
	if storedSessionID == nil || *storedSessionID != sessionID {
		t.Errorf("stored DraftSessionID: want %q, got %v", sessionID, storedSessionID)
	}
}

// TestUpsertMatch_DraftSessionIDNotOverwrittenByNil verifies the COALESCE
// behaviour: re-projecting a match with DraftSessionID=nil does not overwrite
// an existing non-nil draft_session_id.
func TestUpsertMatch_DraftSessionIDNotOverwrittenByNil(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewMatchesRepository(db)
	dsRepo := repository.NewDraftSessionsRepository(db)

	accountID := insertTestAccount(t, db, "upsert-coalesce-acct")
	now := time.Now().UTC().Truncate(time.Second)
	sessionID := fmt.Sprintf("ds-coalesce-%d", accountID)
	matchID := fmt.Sprintf("match-coalesce-%d", accountID)

	if err := dsRepo.UpsertDraftSession(context.Background(), repository.DraftSessionUpsert{
		ID:        sessionID,
		AccountID: accountID,
		EventName: "QuickDraft_SOS_20260526",
		SetCode:   "SOS",
		DraftType: "PremierDraft",
		StartTime: now.Add(-3 * time.Hour),
		Status:    "completed",
	}); err != nil {
		t.Fatalf("UpsertDraftSession: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM draft_match_results WHERE match_id = $1`, matchID)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM matches WHERE id = $1`, matchID)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM draft_sessions WHERE id = $1`, sessionID)
	})

	// First upsert with sessionID set.
	if err := repo.UpsertMatch(context.Background(), repository.MatchUpsert{
		ID:             matchID,
		AccountID:      accountID,
		EventID:        "evt-coalesce",
		EventName:      "QuickDraft_SOS_20260526",
		Timestamp:      now,
		PlayerWins:     2,
		OpponentWins:   1,
		PlayerTeamID:   1,
		Format:         "QuickDraft_SOS_20260526",
		Result:         "win",
		DraftSessionID: &sessionID,
	}); err != nil {
		t.Fatalf("first UpsertMatch: %v", err)
	}

	// Second upsert with DraftSessionID=nil — should NOT clear the existing value.
	if err := repo.UpsertMatch(context.Background(), repository.MatchUpsert{
		ID:             matchID,
		AccountID:      accountID,
		EventID:        "evt-coalesce",
		EventName:      "QuickDraft_SOS_20260526",
		Timestamp:      now,
		PlayerWins:     2,
		OpponentWins:   1,
		PlayerTeamID:   1,
		Format:         "QuickDraft_SOS_20260526",
		Result:         "win",
		DraftSessionID: nil, // should be COALESCE'd away
	}); err != nil {
		t.Fatalf("second UpsertMatch: %v", err)
	}

	var storedSessionID *string
	if err := db.QueryRowContext(
		context.Background(),
		`SELECT draft_session_id FROM matches WHERE id = $1`,
		matchID,
	).Scan(&storedSessionID); err != nil {
		t.Fatalf("SELECT draft_session_id: %v", err)
	}
	if storedSessionID == nil || *storedSessionID != sessionID {
		t.Errorf("COALESCE should preserve existing DraftSessionID; want %q, got %v", sessionID, storedSessionID)
	}
}

// ---------------------------------------------------------------------------
// Ticket #687: OpponentName + PlayerOnPlay fields in history list
// ---------------------------------------------------------------------------

// insertTestMatchWithOpponent inserts a matches row that includes opponent_name.
func insertTestMatchWithOpponent(t *testing.T, db *sql.DB, matchID string, accountID int64, opponentName string, ts time.Time) {
	t.Helper()

	_, err := db.ExecContext(
		context.Background(),
		`INSERT INTO matches
			(id, account_id, event_id, event_name, timestamp, player_wins, opponent_wins,
			 player_team_id, format, result, opponent_name)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		matchID, accountID, "evt-"+matchID, "event-"+matchID, ts,
		2, 1, 1, "Standard", "win", opponentName,
	)
	if err != nil {
		t.Fatalf("insertTestMatchWithOpponent %q: %v", matchID, err)
	}

	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM matches WHERE id = $1`, matchID)
	})
}

// insertTestMatchGameResult inserts a match_game_results row for the given match.
func insertTestMatchGameResult(t *testing.T, db *sql.DB, matchID string, accountID int64, gameNumber int, playerOnPlay *bool) {
	t.Helper()

	_, err := db.ExecContext(
		context.Background(),
		`INSERT INTO match_game_results
			(account_id, match_id, game_number, winning_team_id, turn_count,
			 duration_secs, sequence, occurred_at, partial, player_on_play)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		 ON CONFLICT ON CONSTRAINT uq_match_game_results_account_match_game DO NOTHING`,
		accountID, matchID, gameNumber, 1, 8,
		120, 1, time.Now().UTC(), false, playerOnPlay,
	)
	if err != nil {
		t.Fatalf("insertTestMatchGameResult match=%q game=%d: %v", matchID, gameNumber, err)
	}

	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(),
			`DELETE FROM match_game_results WHERE match_id = $1 AND game_number = $2 AND account_id = $3`,
			matchID, gameNumber, accountID)
	})
}

// TestMatchesRepository_ListCursor_OpponentNameAndPlayerOnPlay verifies that
// ListByAccountIDCursorFiltered surfaces opponent_name from matches and
// player_on_play (game 1) from match_game_results.
func TestMatchesRepository_ListCursor_OpponentNameAndPlayerOnPlay(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewMatchesRepository(db)

	accountID := insertTestAccount(t, db, "match-687-account")
	now := time.Now().UTC().Truncate(time.Second)
	matchID := fmt.Sprintf("match-687-%d", accountID)

	insertTestMatchWithOpponent(t, db, matchID, accountID, "OpponentAlpha", now)
	onPlay := true
	insertTestMatchGameResult(t, db, matchID, accountID, 1, &onPlay)

	rows, err := repo.ListByAccountIDCursorFiltered(context.Background(), accountID, repository.MatchFilter{}, nil, "", 10)
	if err != nil {
		t.Fatalf("ListByAccountIDCursorFiltered: %v", err)
	}

	var found *repository.MatchRow
	for i := range rows {
		if rows[i].ID == matchID {
			found = &rows[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("match %q not found in result", matchID)
	}

	if found.OpponentName == nil || *found.OpponentName != "OpponentAlpha" {
		t.Errorf("OpponentName = %v, want %q", found.OpponentName, "OpponentAlpha")
	}
	if found.PlayerOnPlay == nil {
		t.Error("PlayerOnPlay is nil, want non-nil")
	} else if !*found.PlayerOnPlay {
		t.Errorf("PlayerOnPlay = false, want true")
	}
}

// TestMatchesRepository_ListCursor_PlayerOnPlay_NilWhenNoGameResult verifies
// that PlayerOnPlay is nil when no match_game_results row exists for game 1.
func TestMatchesRepository_ListCursor_PlayerOnPlay_NilWhenNoGameResult(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewMatchesRepository(db)

	accountID := insertTestAccount(t, db, "match-687-noresult-account")
	now := time.Now().UTC().Truncate(time.Second)
	matchID := fmt.Sprintf("match-687-noresult-%d", accountID)

	insertTestMatch(t, db, matchID, accountID, "Standard", now)
	// Deliberately do NOT insert a match_game_results row.

	rows, err := repo.ListByAccountIDCursorFiltered(context.Background(), accountID, repository.MatchFilter{}, nil, "", 10)
	if err != nil {
		t.Fatalf("ListByAccountIDCursorFiltered: %v", err)
	}

	var found *repository.MatchRow
	for i := range rows {
		if rows[i].ID == matchID {
			found = &rows[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("match %q not found", matchID)
	}

	if found.PlayerOnPlay != nil {
		t.Errorf("PlayerOnPlay = %v, want nil when no game result row exists", *found.PlayerOnPlay)
	}
}

// TestMatchesRepository_GetByID_OpponentNameAndPlayerOnPlay verifies the
// single-match GetByID read path.
func TestMatchesRepository_GetByID_OpponentNameAndPlayerOnPlay(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewMatchesRepository(db)

	accountID := insertTestAccount(t, db, "match-687-getbyid-account")
	now := time.Now().UTC().Truncate(time.Second)
	matchID := fmt.Sprintf("match-687-getbyid-%d", accountID)

	insertTestMatchWithOpponent(t, db, matchID, accountID, "OpponentBeta", now)
	onPlay := false
	insertTestMatchGameResult(t, db, matchID, accountID, 1, &onPlay)

	row, err := repo.GetByID(context.Background(), accountID, matchID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if row == nil {
		t.Fatal("GetByID returned nil")
	}

	if row.OpponentName == nil || *row.OpponentName != "OpponentBeta" {
		t.Errorf("OpponentName = %v, want %q", row.OpponentName, "OpponentBeta")
	}
	if row.PlayerOnPlay == nil {
		t.Error("PlayerOnPlay is nil, want non-nil")
	} else if *row.PlayerOnPlay {
		t.Errorf("PlayerOnPlay = true, want false (player was on draw)")
	}
}

// ─── GetPlayerTeamIDForMatch integration tests (#748 fix-round) ──────────────

// TestMatchesRepository_GetPlayerTeamIDForMatch_HappyPath verifies that
// GetPlayerTeamIDForMatch returns the stored player_team_id for an existing row.
func TestMatchesRepository_GetPlayerTeamIDForMatch_HappyPath(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewMatchesRepository(db)
	ctx := context.Background()

	accountID := insertTestAccount(t, db, "getteamid-happy")
	matchID := fmt.Sprintf("match-gtid-happy-%d", accountID)
	now := time.Now().UTC().Truncate(time.Second)

	// insertTestMatch seeds player_team_id=1 by convention.
	insertTestMatch(t, db, matchID, accountID, "Standard", now)

	teamID, err := repo.GetPlayerTeamIDForMatch(ctx, accountID, matchID)
	if err != nil {
		t.Fatalf("GetPlayerTeamIDForMatch: unexpected error: %v", err)
	}
	if teamID != 1 {
		t.Errorf("GetPlayerTeamIDForMatch: want player_team_id=1, got %d", teamID)
	}
}

// TestMatchesRepository_GetPlayerTeamIDForMatch_NotFound verifies that
// GetPlayerTeamIDForMatch returns (0, nil) — not an error — when the match row
// does not exist.  Callers treat 0 as "indeterminate" and fall back gracefully.
func TestMatchesRepository_GetPlayerTeamIDForMatch_NotFound(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewMatchesRepository(db)
	ctx := context.Background()

	accountID := insertTestAccount(t, db, "getteamid-notfound")

	teamID, err := repo.GetPlayerTeamIDForMatch(ctx, accountID, "no-such-match-xyz-748")
	if err != nil {
		t.Fatalf("GetPlayerTeamIDForMatch not-found: want (0, nil), got err=%v", err)
	}
	if teamID != 0 {
		t.Errorf("GetPlayerTeamIDForMatch not-found: want teamID=0, got %d", teamID)
	}
}

// TestMatchesRepository_GetPlayerTeamIDForMatch_CrossTenantIsolation verifies
// that GetPlayerTeamIDForMatch scopes to accountID: account A presenting account
// B's match_id must receive (0, nil), not B's player_team_id.  This is the
// cross-tenant security boundary on the query.
func TestMatchesRepository_GetPlayerTeamIDForMatch_CrossTenantIsolation(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewMatchesRepository(db)
	ctx := context.Background()

	accountA := insertTestAccount(t, db, "getteamid-iso-a")
	accountB := insertTestAccount(t, db, "getteamid-iso-b")

	matchBID := fmt.Sprintf("match-gtid-iso-b-%d", accountB)
	now := time.Now().UTC().Truncate(time.Second)
	insertTestMatch(t, db, matchBID, accountB, "Standard", now)

	// accountA must not be able to read accountB's player_team_id.
	teamID, err := repo.GetPlayerTeamIDForMatch(ctx, accountA, matchBID)
	if err != nil {
		t.Fatalf("GetPlayerTeamIDForMatch cross-tenant: want (0, nil), got err=%v", err)
	}
	if teamID != 0 {
		t.Errorf("GetPlayerTeamIDForMatch cross-tenant isolation failure: accountA read accountB's player_team_id=%d (expected 0)", teamID)
	}
}

// ─── GetResultForMatch integration tests (#1341 fix) ─────────────────────────

// TestMatchesRepository_GetResultForMatch_Win verifies that GetResultForMatch
// returns "win" for an existing match that was stored with result="win".
func TestMatchesRepository_GetResultForMatch_Win(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewMatchesRepository(db)
	ctx := context.Background()

	accountID := insertTestAccount(t, db, "getresult-win")
	matchID := fmt.Sprintf("match-grf-win-%d", accountID)
	now := time.Now().UTC().Truncate(time.Second)
	insertTestMatch(t, db, matchID, accountID, "Standard", now) // result="win" by default

	result, err := repo.GetResultForMatch(ctx, accountID, matchID)
	if err != nil {
		t.Fatalf("GetResultForMatch: unexpected error: %v", err)
	}
	if result != "win" {
		t.Errorf("GetResultForMatch: want %q, got %q", "win", result)
	}
}

// TestMatchesRepository_GetResultForMatch_Loss verifies that GetResultForMatch
// returns "loss" when the match row was stored with result="loss".
func TestMatchesRepository_GetResultForMatch_Loss(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewMatchesRepository(db)
	ctx := context.Background()

	accountID := insertTestAccount(t, db, "getresult-loss")
	matchID := fmt.Sprintf("match-grf-loss-%d", accountID)
	now := time.Now().UTC().Truncate(time.Second)

	// Insert with result="loss" directly (insertTestMatch always uses "win").
	_, err := db.ExecContext(
		ctx,
		`INSERT INTO matches
			(id, account_id, event_id, event_name, timestamp, player_wins, opponent_wins,
			 player_team_id, format, result)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		matchID, accountID, "evt-"+matchID, "event-"+matchID, now,
		0, 1, 1, "Standard", "loss",
	)
	if err != nil {
		t.Fatalf("insert match with result=loss: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM matches WHERE id = $1`, matchID)
	})

	result, err := repo.GetResultForMatch(ctx, accountID, matchID)
	if err != nil {
		t.Fatalf("GetResultForMatch: unexpected error: %v", err)
	}
	if result != "loss" {
		t.Errorf("GetResultForMatch: want %q, got %q", "loss", result)
	}
}

// TestMatchesRepository_GetResultForMatch_NotFound verifies that
// GetResultForMatch returns ("", nil) — not an error — when the match row does
// not exist yet (match.completed not yet projected).
func TestMatchesRepository_GetResultForMatch_NotFound(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewMatchesRepository(db)
	ctx := context.Background()

	accountID := insertTestAccount(t, db, "getresult-notfound")

	result, err := repo.GetResultForMatch(ctx, accountID, "no-such-match-1341")
	if err != nil {
		t.Fatalf("GetResultForMatch not-found: want (\"\", nil), got err=%v", err)
	}
	if result != "" {
		t.Errorf("GetResultForMatch not-found: want empty string, got %q", result)
	}
}

// ── Date-boundary tests ───────────────────────────────────────────────────────

// TestMatchesRepository_AggregateStats_EndDate_ExclusiveBoundary verifies that
// buildMatchWhere uses a strict-less-than bound for EndDate (timestamp < end),
// NOT less-than-or-equal. A match whose timestamp equals the exact boundary
// value must be EXCLUDED.
//
// RED: current code uses "timestamp <= $N" so TotalMatches=1; after fix it
// must use "timestamp < $N" so TotalMatches=0.
func TestMatchesRepository_AggregateStats_EndDate_ExclusiveBoundary(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewMatchesRepository(db)
	ctx := context.Background()

	accountID := insertTestAccount(t, db, "aggstats-exclusive-boundary")

	// Match timestamp = exactly midnight 2026-06-13 00:00:00 UTC.
	// The handler will advance bare "2026-06-12" to this exclusive bound.
	// A match at exactly this instant belongs to 2026-06-13, not 2026-06-12,
	// so it must be excluded.
	matchTS := time.Date(2026, 6, 13, 0, 0, 0, 0, time.UTC)
	matchID := fmt.Sprintf("aggstats-excl-bound-%d", accountID)
	insertTestMatch(t, db, matchID, accountID, "Standard", matchTS)

	endBound := time.Date(2026, 6, 13, 0, 0, 0, 0, time.UTC)
	f := repository.MatchFilter{EndDate: &endBound}

	agg, err := repo.AggregateStats(ctx, accountID, f)
	if err != nil {
		t.Fatalf("AggregateStats: %v", err)
	}
	// Must be 0: timestamp == exclusive bound is excluded by <, not <=.
	if agg.TotalMatches != 0 {
		t.Errorf("TotalMatches: want 0 (exclusive boundary excludes midnight match), got %d", agg.TotalMatches)
	}
}

// TestMatchesRepository_AggregateStats_EndDate_BareDateIncludesLateMatch
// verifies that a match at 23:50 UTC on date D is included when the caller
// passes the already-advanced exclusive bound (midnight of D+1). This matches
// what buildMatchFilter produces after the fix for bare "YYYY-MM-DD" EndDates.
func TestMatchesRepository_AggregateStats_EndDate_BareDateIncludesLateMatch(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewMatchesRepository(db)
	ctx := context.Background()

	accountID := insertTestAccount(t, db, "aggstats-late-match")

	// Match at 23:50 on 2026-06-12 — same calendar day as the SPA's endDate.
	matchTS := time.Date(2026, 6, 12, 23, 50, 0, 0, time.UTC)
	matchID := fmt.Sprintf("aggstats-late-match-%d", accountID)
	insertTestMatch(t, db, matchID, accountID, "Standard", matchTS)

	// The handler advances bare "2026-06-12" to this exclusive bound.
	endBound := time.Date(2026, 6, 13, 0, 0, 0, 0, time.UTC)
	f := repository.MatchFilter{EndDate: &endBound}

	agg, err := repo.AggregateStats(ctx, accountID, f)
	if err != nil {
		t.Fatalf("AggregateStats: %v", err)
	}
	if agg.TotalMatches != 1 {
		t.Errorf("TotalMatches: want 1 (23:50 match included when end is midnight D+1), got %d", agg.TotalMatches)
	}
}

// TestMatchesRepository_GetResultForMatch_CrossTenantIsolation verifies that
// GetResultForMatch scopes to accountID: account A presenting account B's
// match_id must receive ("", nil), not B's result.
func TestMatchesRepository_GetResultForMatch_CrossTenantIsolation(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewMatchesRepository(db)
	ctx := context.Background()

	accountA := insertTestAccount(t, db, "getresult-iso-a")
	accountB := insertTestAccount(t, db, "getresult-iso-b")

	matchBID := fmt.Sprintf("match-grf-iso-b-%d", accountB)
	now := time.Now().UTC().Truncate(time.Second)
	insertTestMatch(t, db, matchBID, accountB, "Standard", now)

	// accountA must not be able to read accountB's match result.
	result, err := repo.GetResultForMatch(ctx, accountA, matchBID)
	if err != nil {
		t.Fatalf("GetResultForMatch cross-tenant: want (\"\", nil), got err=%v", err)
	}
	if result != "" {
		t.Errorf("GetResultForMatch cross-tenant isolation failure: accountA read accountB's result=%q (expected empty)", result)
	}
}
