package repository_test

// Integration tests for GamePlaysRepository — the per-turn action read path.
//
// These tests exercise PlaysByMatch, PlaysByGameID, SnapshotsByMatch,
// OpponentCardsByMatch, and MatchExistsForAccount against a real Postgres
// database (DATABASE_URL env var required; tests are skipped without it).
//
// Key coverage goals:
//   - PlaysByMatch returns rows for a match that has per-turn data.
//   - PlaysByMatch returns empty (no error) for a match with no per-turn data.
//   - PlaysByMatch excludes per-game (legacy) rows (game_id IS NULL).
//   - PlaysByGameID returns rows filtered by game_id.
//   - SnapshotsByMatch returns snapshots or empty without error.
//   - OpponentCardsByMatch returns observed cards or empty without error.
//   - MatchExistsForAccount correctly identifies matches by account.
//
// Ref: ADR-050, ticket #659, migration 000101.

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/RdHamilton/hollowmark/services/bff/internal/storage/repository"
)

// ────────────────────────────────────────────────────────────────────────────
// helpers
// ────────────────────────────────────────────────────────────────────────────

// insertGPAccount inserts a minimal accounts row and registers cleanup.
func insertGPAccount(t *testing.T, db *sql.DB, suffix string) int64 {
	t.Helper()
	name := fmt.Sprintf("gplays-account-%s", suffix)
	var id int64
	if err := db.QueryRowContext(
		context.Background(),
		`INSERT INTO accounts (name) VALUES ($1) RETURNING id`, name,
	).Scan(&id); err != nil {
		t.Fatalf("insertGPAccount %q: %v", name, err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM accounts WHERE id = $1`, id)
	})
	return id
}

// insertGPMatch inserts a minimal matches row and registers cleanup.
func insertGPMatch(t *testing.T, db *sql.DB, matchID string, accountID int64) {
	t.Helper()
	_, err := db.ExecContext(
		context.Background(),
		`INSERT INTO matches
			(id, account_id, event_id, event_name, timestamp, player_wins,
			 opponent_wins, player_team_id, format, result)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		 ON CONFLICT (id) DO NOTHING`,
		matchID, accountID, "evt-"+matchID, "evt-name-"+matchID,
		time.Now().UTC(), 1, 0, 1, "Standard", "win",
	)
	if err != nil {
		t.Fatalf("insertGPMatch %q: %v", matchID, err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM matches WHERE id = $1`, matchID)
	})
}

// insertGPGame inserts a minimal games row and returns its id.
func insertGPGame(t *testing.T, db *sql.DB, matchID string, gameNumber int) int64 {
	t.Helper()
	var id int64
	err := db.QueryRowContext(
		context.Background(),
		`INSERT INTO games (match_id, game_number, result)
		 VALUES ($1, $2, 'win')
		 ON CONFLICT (match_id, game_number) DO UPDATE SET result = EXCLUDED.result
		 RETURNING id`,
		matchID, gameNumber,
	).Scan(&id)
	if err != nil {
		t.Fatalf("insertGPGame match=%q game=%d: %v", matchID, gameNumber, err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM games WHERE id = $1`, id)
	})
	return id
}

// insertGamePlayRow inserts one per-turn game_plays row directly and returns
// its id. Used to seed read-path tests without going through InsertCardPlays.
func insertGamePlayRow(t *testing.T, db *sql.DB, gameID int64, matchID string, turn, seq int, playerType, actionType string) int64 {
	t.Helper()
	var id int64
	err := db.QueryRowContext(
		context.Background(),
		`INSERT INTO game_plays
			(game_id, match_id, turn_number, player_type, action_type,
			 timestamp, sequence_number)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 ON CONFLICT (game_id, sequence_number) DO UPDATE SET player_type = EXCLUDED.player_type
		 RETURNING id`,
		gameID, matchID, turn, playerType, actionType,
		time.Now().UTC(), seq,
	).Scan(&id)
	if err != nil {
		t.Fatalf("insertGamePlayRow gameID=%d seq=%d: %v", gameID, seq, err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM game_plays WHERE id = $1`, id)
	})
	return id
}

// insertSnapshotRow inserts one game_state_snapshots row directly.
func insertSnapshotRow(t *testing.T, db *sql.DB, gameID int64, matchID string, turn int, activePlayer string) int64 {
	t.Helper()
	var id int64
	err := db.QueryRowContext(
		context.Background(),
		`INSERT INTO game_state_snapshots
			(game_id, match_id, turn_number, active_player, timestamp)
		 VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (game_id, turn_number) DO UPDATE SET active_player = EXCLUDED.active_player
		 RETURNING id`,
		gameID, matchID, turn, activePlayer, time.Now().UTC(),
	).Scan(&id)
	if err != nil {
		t.Fatalf("insertSnapshotRow gameID=%d turn=%d: %v", gameID, turn, err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM game_state_snapshots WHERE id = $1`, id)
	})
	return id
}

// insertOpponentCardRow inserts one opponent_cards_observed row directly.
func insertOpponentCardRow(t *testing.T, db *sql.DB, gameID int64, matchID string, cardID int) int64 {
	t.Helper()
	var id int64
	err := db.QueryRowContext(
		context.Background(),
		`INSERT INTO opponent_cards_observed
			(game_id, match_id, card_id)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (game_id, card_id) DO UPDATE SET times_seen = opponent_cards_observed.times_seen + 1
		 RETURNING id`,
		gameID, matchID, cardID,
	).Scan(&id)
	if err != nil {
		t.Fatalf("insertOpponentCardRow gameID=%d cardID=%d: %v", gameID, cardID, err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM opponent_cards_observed WHERE id = $1`, id)
	})
	return id
}

// ────────────────────────────────────────────────────────────────────────────
// PlaysByMatch
// ────────────────────────────────────────────────────────────────────────────

// TestGamePlaysRepository_PlaysByMatch_ReturnsRows verifies that PlaysByMatch
// returns the expected per-turn rows for a match that has data.
func TestGamePlaysRepository_PlaysByMatch_ReturnsRows(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewGamePlaysRepository(db)
	ctx := context.Background()

	accountID := insertGPAccount(t, db, "pbm-rows")
	matchID := "gpr-match-001"
	insertGPMatch(t, db, matchID, accountID)
	gameID := insertGPGame(t, db, matchID, 1)

	// Insert 3 per-turn rows.
	for seq := range 3 {
		insertGamePlayRow(t, db, gameID, matchID, 1, seq, "player", "play_card")
	}

	rows, err := repo.PlaysByMatch(ctx, accountID, matchID)
	if err != nil {
		t.Fatalf("PlaysByMatch: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("want 3 rows, got %d", len(rows))
	}
	for _, r := range rows {
		if r.GameID != gameID {
			t.Errorf("game_id: want %d, got %d", gameID, r.GameID)
		}
		if r.MatchID != matchID {
			t.Errorf("match_id: want %q, got %q", matchID, r.MatchID)
		}
		if r.PlayerType != "player" {
			t.Errorf("player_type: want %q, got %q", "player", r.PlayerType)
		}
	}
}

// TestGamePlaysRepository_PlaysByMatch_EmptyForNoData verifies that
// PlaysByMatch returns an empty slice (not an error) when the match has no
// per-turn rows. This covers the "Game Timeline graceful empty state" case:
// a match recorded before InsertCardPlays was wired in should return []
// so the timeline panel shows "No game play data available" instead of 500.
func TestGamePlaysRepository_PlaysByMatch_EmptyForNoData(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewGamePlaysRepository(db)
	ctx := context.Background()

	accountID := insertGPAccount(t, db, "pbm-empty")
	matchID := "gpr-match-empty-001"
	insertGPMatch(t, db, matchID, accountID)
	// No game_plays rows inserted.

	rows, err := repo.PlaysByMatch(ctx, accountID, matchID)
	if err != nil {
		t.Fatalf("PlaysByMatch returned error on empty match: %v — want nil (no error)", err)
	}
	if len(rows) != 0 {
		t.Fatalf("want 0 rows, got %d", len(rows))
	}
}

// TestGamePlaysRepository_PlaysByMatch_ExcludesLegacyPerGameRows verifies
// that PlaysByMatch returns 0 rows for game_plays rows where game_id IS NULL
// (legacy per-game rows that predate the ADR-050 split). This prevents a
// runtime scan error (NULL → int64) on staging-variant DBs.
//
// The test only applies on DBs where migration 000101 has been applied (the
// game_id column must exist). It is skipped if the column is absent.
func TestGamePlaysRepository_PlaysByMatch_ExcludesLegacyPerGameRows(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewGamePlaysRepository(db)
	ctx := context.Background()

	// Skip if game_id column doesn't exist (DB hasn't run 000101 yet — that's
	// fine for fresh per-turn-schema DBs that never needed it, but integration
	// CI always runs full migrations so this covers both paths).
	var hasGameID bool
	err := db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_name = 'game_plays' AND column_name = 'game_id'
		)`).Scan(&hasGameID)
	if err != nil {
		t.Fatalf("checking game_id column existence: %v", err)
	}
	if !hasGameID {
		t.Skip("game_id column not present — migration 000101 not yet applied")
	}

	accountID := insertGPAccount(t, db, "pbm-legacy")
	matchID := "gpr-match-legacy-001"
	insertGPMatch(t, db, matchID, accountID)

	// Insert a row with game_id = NULL (simulates a legacy per-game row after
	// migration 000101 adds the column to a per-game-schema DB).
	_, err = db.ExecContext(
		ctx, `
		INSERT INTO game_plays (match_id, game_id, turn_number, player_type, action_type, timestamp, sequence_number)
		VALUES ($1, NULL, 1, 'player', 'play_card', NOW(), 99)`,
		matchID,
	)
	if err != nil {
		// If the INSERT fails (e.g. NOT NULL constraint on game_id), the column
		// is NOT NULL — no legacy rows can exist, skip.
		t.Skipf("cannot insert game_id=NULL row (game_id NOT NULL constraint): %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(ctx, `DELETE FROM game_plays WHERE match_id = $1 AND game_id IS NULL`, matchID)
	})

	rows, err := repo.PlaysByMatch(ctx, accountID, matchID)
	if err != nil {
		t.Fatalf("PlaysByMatch with legacy row: %v — want nil (not a scan error on NULL game_id)", err)
	}
	if len(rows) != 0 {
		t.Fatalf("want 0 rows (legacy per-game row excluded), got %d", len(rows))
	}
}

// TestGamePlaysRepository_PlaysByMatch_CrossAccountIsolation verifies that
// PlaysByMatch does not return rows belonging to a different account.
func TestGamePlaysRepository_PlaysByMatch_CrossAccountIsolation(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewGamePlaysRepository(db)
	ctx := context.Background()

	accountA := insertGPAccount(t, db, "pbm-acct-a")
	accountB := insertGPAccount(t, db, "pbm-acct-b")
	matchID := "gpr-match-xacct-001"
	insertGPMatch(t, db, matchID, accountA) // match belongs to account A
	gameID := insertGPGame(t, db, matchID, 1)
	insertGamePlayRow(t, db, gameID, matchID, 1, 0, "player", "attack")

	// Account B should see nothing for this match.
	rows, err := repo.PlaysByMatch(ctx, accountB, matchID)
	if err != nil {
		t.Fatalf("PlaysByMatch cross-account: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("cross-account isolation: want 0 rows for accountB, got %d", len(rows))
	}

	// Account A should see the row.
	rowsA, err := repo.PlaysByMatch(ctx, accountA, matchID)
	if err != nil {
		t.Fatalf("PlaysByMatch account A: %v", err)
	}
	if len(rowsA) != 1 {
		t.Fatalf("want 1 row for accountA, got %d", len(rowsA))
	}
}

// ────────────────────────────────────────────────────────────────────────────
// PlaysByGameID
// ────────────────────────────────────────────────────────────────────────────

// TestGamePlaysRepository_PlaysByGameID_ReturnsRows verifies per-game
// scoped reads.
func TestGamePlaysRepository_PlaysByGameID_ReturnsRows(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewGamePlaysRepository(db)
	ctx := context.Background()

	accountID := insertGPAccount(t, db, "pbgid-rows")
	matchID := "gpr-match-bygame-001"
	insertGPMatch(t, db, matchID, accountID)
	gameID := insertGPGame(t, db, matchID, 1)
	game2ID := insertGPGame(t, db, matchID, 2)

	insertGamePlayRow(t, db, gameID, matchID, 1, 0, "player", "land_drop")
	insertGamePlayRow(t, db, gameID, matchID, 2, 1, "opponent", "attack")
	insertGamePlayRow(t, db, game2ID, matchID, 1, 0, "player", "play_card") // different game

	rows, err := repo.PlaysByGameID(ctx, accountID, gameID)
	if err != nil {
		t.Fatalf("PlaysByGameID: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("want 2 rows for gameID=%d, got %d", gameID, len(rows))
	}
	for _, r := range rows {
		if r.GameID != gameID {
			t.Errorf("game_id: want %d, got %d", gameID, r.GameID)
		}
	}
}

// ────────────────────────────────────────────────────────────────────────────
// SnapshotsByMatch
// ────────────────────────────────────────────────────────────────────────────

// TestGamePlaysRepository_SnapshotsByMatch_EmptyForNoData verifies that
// SnapshotsByMatch returns empty (not error) for a match with no snapshots.
func TestGamePlaysRepository_SnapshotsByMatch_EmptyForNoData(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewGamePlaysRepository(db)
	ctx := context.Background()

	accountID := insertGPAccount(t, db, "snaps-empty")
	matchID := "gpr-match-snaps-empty-001"
	insertGPMatch(t, db, matchID, accountID)

	snaps, err := repo.SnapshotsByMatch(ctx, accountID, matchID, 0)
	if err != nil {
		t.Fatalf("SnapshotsByMatch empty: %v", err)
	}
	if len(snaps) != 0 {
		t.Fatalf("want 0 snapshots, got %d", len(snaps))
	}
}

// TestGamePlaysRepository_SnapshotsByMatch_ReturnsRows verifies snapshot
// reads with and without the optional game_id filter.
func TestGamePlaysRepository_SnapshotsByMatch_ReturnsRows(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewGamePlaysRepository(db)
	ctx := context.Background()

	accountID := insertGPAccount(t, db, "snaps-rows")
	matchID := "gpr-match-snaps-001"
	insertGPMatch(t, db, matchID, accountID)
	gameID := insertGPGame(t, db, matchID, 1)
	game2ID := insertGPGame(t, db, matchID, 2)

	insertSnapshotRow(t, db, gameID, matchID, 1, "player")
	insertSnapshotRow(t, db, gameID, matchID, 2, "opponent")
	insertSnapshotRow(t, db, game2ID, matchID, 1, "player")

	// All snapshots for the match.
	all, err := repo.SnapshotsByMatch(ctx, accountID, matchID, 0)
	if err != nil {
		t.Fatalf("SnapshotsByMatch all: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("want 3 snapshots, got %d", len(all))
	}

	// Filtered by gameID.
	filtered, err := repo.SnapshotsByMatch(ctx, accountID, matchID, gameID)
	if err != nil {
		t.Fatalf("SnapshotsByMatch filtered: %v", err)
	}
	if len(filtered) != 2 {
		t.Fatalf("want 2 snapshots for gameID=%d, got %d", gameID, len(filtered))
	}
}

// ────────────────────────────────────────────────────────────────────────────
// OpponentCardsByMatch
// ────────────────────────────────────────────────────────────────────────────

// TestGamePlaysRepository_OpponentCardsByMatch_EmptyForNoData verifies that
// OpponentCardsByMatch returns empty (not error) for a match with no data.
func TestGamePlaysRepository_OpponentCardsByMatch_EmptyForNoData(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewGamePlaysRepository(db)
	ctx := context.Background()

	accountID := insertGPAccount(t, db, "opp-empty")
	matchID := "gpr-match-opp-empty-001"
	insertGPMatch(t, db, matchID, accountID)

	cards, err := repo.OpponentCardsByMatch(ctx, accountID, matchID)
	if err != nil {
		t.Fatalf("OpponentCardsByMatch empty: %v", err)
	}
	if len(cards) != 0 {
		t.Fatalf("want 0 cards, got %d", len(cards))
	}
}

// TestGamePlaysRepository_OpponentCardsByMatch_ReturnsRows verifies opponent
// card reads.
func TestGamePlaysRepository_OpponentCardsByMatch_ReturnsRows(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewGamePlaysRepository(db)
	ctx := context.Background()

	accountID := insertGPAccount(t, db, "opp-rows")
	matchID := "gpr-match-opp-001"
	insertGPMatch(t, db, matchID, accountID)
	gameID := insertGPGame(t, db, matchID, 1)

	insertOpponentCardRow(t, db, gameID, matchID, 12345)
	insertOpponentCardRow(t, db, gameID, matchID, 67890)

	cards, err := repo.OpponentCardsByMatch(ctx, accountID, matchID)
	if err != nil {
		t.Fatalf("OpponentCardsByMatch: %v", err)
	}
	if len(cards) != 2 {
		t.Fatalf("want 2 cards, got %d", len(cards))
	}
}

// ────────────────────────────────────────────────────────────────────────────
// MatchExistsForAccount
// ────────────────────────────────────────────────────────────────────────────

// TestGamePlaysRepository_MatchExistsForAccount verifies the account-scoped
// existence check.
func TestGamePlaysRepository_MatchExistsForAccount(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewGamePlaysRepository(db)
	ctx := context.Background()

	accountA := insertGPAccount(t, db, "mefa-a")
	accountB := insertGPAccount(t, db, "mefa-b")
	matchID := "gpr-match-mefa-001"
	insertGPMatch(t, db, matchID, accountA)

	ok, err := repo.MatchExistsForAccount(ctx, accountA, matchID)
	if err != nil {
		t.Fatalf("MatchExistsForAccount accountA: %v", err)
	}
	if !ok {
		t.Error("want true for accountA, got false")
	}

	ok, err = repo.MatchExistsForAccount(ctx, accountB, matchID)
	if err != nil {
		t.Fatalf("MatchExistsForAccount accountB: %v", err)
	}
	if ok {
		t.Error("want false for accountB (cross-account), got true")
	}

	ok, err = repo.MatchExistsForAccount(ctx, accountA, "nonexistent-match-id")
	if err != nil {
		t.Fatalf("MatchExistsForAccount nonexistent: %v", err)
	}
	if ok {
		t.Error("want false for nonexistent match, got true")
	}
}
