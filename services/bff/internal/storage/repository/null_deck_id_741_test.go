// Package repository_test — integration tests for #741:
// BFF read-path graceful degradation on NULL deck_id.
//
// These tests seed real matches rows with deck_id = NULL and assert that each
// of the three player-facing surfaces degrades gracefully:
//
//	AC1: GetDeckPerformance — NULL-deck_id matches are excluded; no 500.
//	AC2: Match History list — rows with NULL deck_id are returned without error;
//	     the DeckID field is nil (not a garbage string).
//	AC3: Match Detail (GetByID) — a match with NULL deck_id is returned without
//	     error; DeckID is nil.
//
// All tests require a live DATABASE_URL; they are skipped in environments where
// that variable is absent (local devs without a local PG, etc.).
package repository_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/RdHamilton/hollowmark/services/bff/internal/storage/repository"
)

// insertTestMatchNullDeckID inserts a matches row with deck_id = NULL and
// cleans it up after the test.
func insertTestMatchNullDeckID(t *testing.T, db *sql.DB, matchID string, accountID int64, format string, ts time.Time, result string) {
	t.Helper()

	_, err := db.ExecContext(
		context.Background(),
		`INSERT INTO matches
			(id, account_id, event_id, event_name, timestamp, player_wins, opponent_wins,
			 player_team_id, format, result, deck_id)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, NULL)`,
		matchID, accountID,
		"evt-"+matchID, "event-"+matchID,
		ts,
		1, 0, 1,
		format, result,
	)
	if err != nil {
		t.Fatalf("insertTestMatchNullDeckID %q: %v", matchID, err)
	}

	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM matches WHERE id = $1`, matchID)
	})
}

// ─── AC1: GetDeckPerformance excludes NULL deck_id rows ───────────────────────

// TestStatsRepository_GetDeckPerformance_NullDeckID_Excluded verifies that
// matches with deck_id = NULL do not appear in GetDeckPerformance and do not
// cause a scan error.  Before the guard (AND m.deck_id IS NOT NULL) was added
// the query would have returned a row with an empty-string DeckID, breaking
// the SPA's per-deck chart.
func TestStatsRepository_GetDeckPerformance_NullDeckID_Excluded(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewStatsRepository(db)

	accountID := insertTestAccount(t, db, "741-dp-null-deck-id")
	base := time.Now().UTC().Truncate(time.Second)

	// Seed three matches with NULL deck_id for this account.
	for i := 0; i < 3; i++ {
		insertTestMatchNullDeckID(
			t, db,
			fmt.Sprintf("741-dp-null-%d-%d", i, accountID),
			accountID, "Standard",
			base.Add(time.Duration(-i)*time.Second),
			"win",
		)
	}

	rows, err := repo.GetDeckPerformance(context.Background(), accountID)
	if err != nil {
		t.Fatalf("GetDeckPerformance returned error for account with only NULL deck_id matches: %v", err)
	}

	// Expect zero rows — NULL deck_id matches must be filtered out, not returned
	// as a row with an empty DeckID string.
	if len(rows) != 0 {
		t.Errorf("GetDeckPerformance: want 0 rows (NULL deck_id filtered), got %d: %+v", len(rows), rows)
	}
}

// TestStatsRepository_GetDeckPerformance_MixedNullAndNonNull verifies that
// when an account has both NULL-deck_id matches and properly-keyed matches,
// only the properly-keyed deck appears in GetDeckPerformance.
func TestStatsRepository_GetDeckPerformance_MixedNullAndNonNull(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewStatsRepository(db)

	accountID := insertTestAccount(t, db, "741-dp-mixed")
	deckID := fmt.Sprintf("test-deck-741-mixed-%d", accountID)
	base := time.Now().UTC().Truncate(time.Second)

	// One match with a real deck_id.
	insertTestMatchWithDeck(
		t, db,
		fmt.Sprintf("741-dp-mixed-keyed-%d", accountID),
		accountID, "Standard", base, deckID, "win",
	)

	// Two matches with NULL deck_id — these must not corrupt or duplicate the
	// real deck row.
	insertTestMatchNullDeckID(
		t, db,
		fmt.Sprintf("741-dp-mixed-null-1-%d", accountID),
		accountID, "Standard",
		base.Add(-time.Second),
		"loss",
	)
	insertTestMatchNullDeckID(
		t, db,
		fmt.Sprintf("741-dp-mixed-null-2-%d", accountID),
		accountID, "Standard",
		base.Add(-2*time.Second),
		"loss",
	)

	rows, err := repo.GetDeckPerformance(context.Background(), accountID)
	if err != nil {
		t.Fatalf("GetDeckPerformance returned error for mixed account: %v", err)
	}

	if len(rows) != 1 {
		t.Fatalf("GetDeckPerformance: want 1 row (the real deck), got %d: %+v", len(rows), rows)
	}

	if rows[0].DeckID != deckID {
		t.Errorf("DeckID: want %q, got %q", deckID, rows[0].DeckID)
	}

	// The NULL-deck_id matches must not inflate the real deck's counts.
	if rows[0].TotalGames != 1 {
		t.Errorf("TotalGames: want 1 (only the keyed match), got %d", rows[0].TotalGames)
	}

	if rows[0].Wins != 1 {
		t.Errorf("Wins: want 1, got %d", rows[0].Wins)
	}
}

// ─── AC2: Match History list renders NULL deck_id without error ───────────────

// TestMatchesRepository_ListCursorFiltered_NullDeckID_Returned verifies that a
// match with deck_id = NULL is returned by ListByAccountIDCursorFiltered without
// a scan error, and that the returned MatchRow has DeckID == nil (not a garbage
// non-nil pointer to an empty string).
func TestMatchesRepository_ListCursorFiltered_NullDeckID_Returned(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewMatchesRepository(db)

	accountID := insertTestAccount(t, db, "741-hist-null-deck-id")
	matchID := fmt.Sprintf("741-hist-null-%d", accountID)
	ts := time.Now().UTC().Truncate(time.Second)

	insertTestMatchNullDeckID(t, db, matchID, accountID, "Standard", ts, "win")

	rows, err := repo.ListByAccountIDCursorFiltered(
		context.Background(),
		accountID,
		repository.MatchFilter{},
		nil, "", 20,
	)
	if err != nil {
		t.Fatalf("ListByAccountIDCursorFiltered returned error for NULL deck_id match: %v", err)
	}

	var found *repository.MatchRow
	for i := range rows {
		if rows[i].ID == matchID {
			found = &rows[i]
			break
		}
	}

	if found == nil {
		t.Fatalf("NULL deck_id match %q not returned in list — must not be filtered out", matchID)
	}

	// DeckID must be nil, not a non-nil pointer to an empty string.
	if found.DeckID != nil {
		t.Errorf("DeckID: want nil for a NULL deck_id match, got %q", *found.DeckID)
	}
}

// TestMatchesRepository_ListCursorFiltered_NullDeckID_MixedPage verifies that a
// page containing a mix of NULL-deck_id and non-NULL-deck_id matches is returned
// without error, with each row's DeckID correctly nil or non-nil.
func TestMatchesRepository_ListCursorFiltered_NullDeckID_MixedPage(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewMatchesRepository(db)

	accountID := insertTestAccount(t, db, "741-hist-mixed")
	deckID := fmt.Sprintf("deck-741-hist-mixed-%d", accountID)
	base := time.Now().UTC().Truncate(time.Second)

	nullMatchID := fmt.Sprintf("741-hist-mixed-null-%d", accountID)
	keyedMatchID := fmt.Sprintf("741-hist-mixed-keyed-%d", accountID)

	insertTestMatchNullDeckID(t, db, nullMatchID, accountID, "Standard", base, "win")
	insertTestMatchWithDeck(t, db, keyedMatchID, accountID, "Standard", base.Add(-time.Second), deckID, "loss")

	rows, err := repo.ListByAccountIDCursorFiltered(
		context.Background(),
		accountID,
		repository.MatchFilter{},
		nil, "", 20,
	)
	if err != nil {
		t.Fatalf("ListByAccountIDCursorFiltered returned error for mixed-deck_id page: %v", err)
	}

	seen := map[string]*repository.MatchRow{}
	for i := range rows {
		seen[rows[i].ID] = &rows[i]
	}

	nullRow, ok := seen[nullMatchID]
	if !ok {
		t.Fatalf("NULL deck_id match %q missing from list results", nullMatchID)
	}
	if nullRow.DeckID != nil {
		t.Errorf("null match DeckID: want nil, got %q", *nullRow.DeckID)
	}

	keyedRow, ok := seen[keyedMatchID]
	if !ok {
		t.Fatalf("keyed deck match %q missing from list results", keyedMatchID)
	}
	if keyedRow.DeckID == nil {
		t.Errorf("keyed match DeckID: want %q, got nil", deckID)
	} else if *keyedRow.DeckID != deckID {
		t.Errorf("keyed match DeckID: want %q, got %q", deckID, *keyedRow.DeckID)
	}
}

// ─── AC3: Match Detail returns NULL deck_id match without error ───────────────

// TestMatchesRepository_GetByID_NullDeckID_Returns200 verifies that GetByID
// returns a match whose deck_id is NULL without error and with DeckID == nil.
// Before the fix, a nil-dereference on DeckID would have panicked the BFF when
// the handler tried to read *row.DeckID (or the scan itself would have failed).
func TestMatchesRepository_GetByID_NullDeckID_Returns200(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewMatchesRepository(db)

	accountID := insertTestAccount(t, db, "741-detail-null-deck-id")
	matchID := fmt.Sprintf("741-detail-null-%d", accountID)
	ts := time.Now().UTC().Truncate(time.Second)

	insertTestMatchNullDeckID(t, db, matchID, accountID, "Standard", ts, "win")

	row, err := repo.GetByID(context.Background(), accountID, matchID)
	if err != nil {
		t.Fatalf("GetByID returned error for NULL deck_id match: %v", err)
	}

	if row == nil {
		t.Fatalf("GetByID returned nil for existing NULL deck_id match %q", matchID)
	}

	if row.DeckID != nil {
		t.Errorf("DeckID: want nil for a NULL deck_id match, got %q", *row.DeckID)
	}
}

// TestMatchesRepository_GetByID_NullDeckID_WrongAccount verifies that the
// account-scoping boundary is maintained for NULL deck_id matches (regression
// guard: the NULL guard must not bypass the account_id filter).
func TestMatchesRepository_GetByID_NullDeckID_WrongAccount(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewMatchesRepository(db)

	accountA := insertTestAccount(t, db, "741-detail-xacct-a")
	accountB := insertTestAccount(t, db, "741-detail-xacct-b")

	matchID := fmt.Sprintf("741-detail-xacct-b-null-%d", accountB)
	ts := time.Now().UTC().Truncate(time.Second)

	insertTestMatchNullDeckID(t, db, matchID, accountB, "Standard", ts, "win")

	// Account A must not see account B's match — even when deck_id is NULL.
	row, err := repo.GetByID(context.Background(), accountA, matchID)
	if err != nil {
		t.Fatalf("GetByID returned error: %v", err)
	}
	if row != nil {
		t.Errorf("cross-account leak: accountA retrieved accountB's NULL-deck_id match %q", matchID)
	}
}
