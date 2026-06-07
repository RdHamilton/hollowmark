package repository_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/RdHamilton/hollowmark/services/bff/internal/storage/repository"
	"github.com/RdHamilton/hollowmark/services/contract"
)

// insertTestAccountForGamePlay inserts a minimal accounts row and returns its
// auto-assigned id. Removed via t.Cleanup.
func insertTestAccountForGamePlay(t *testing.T, db *sql.DB, suffix string) int64 {
	t.Helper()

	name := fmt.Sprintf("gp-test-account-%s", suffix)
	var id int64
	err := db.QueryRowContext(
		context.Background(),
		`INSERT INTO accounts (name) VALUES ($1) RETURNING id`,
		name,
	).Scan(&id)
	if err != nil {
		t.Fatalf("insertTestAccountForGamePlay %q: %v", name, err)
	}

	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM accounts WHERE id = $1`, id)
	})

	return id
}

// cleanupMatchGameResults deletes match_game_results (and cascaded
// life_change_tracking / game_event_counters) rows for the given account.
func cleanupMatchGameResults(t *testing.T, db *sql.DB, accountID int64) {
	t.Helper()
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM match_game_results WHERE account_id = $1`, accountID)
	})
}

// insertTestMatchForCardPlays inserts a minimal matches row for use in
// InsertCardPlays tests (card plays require a games row, which requires a match).
func insertTestMatchForCardPlays(t *testing.T, db *sql.DB, matchID string, accountID int64) {
	t.Helper()
	_, err := db.ExecContext(
		context.Background(),
		`INSERT INTO matches
			(id, account_id, event_id, event_name, timestamp, player_wins, opponent_wins,
			 player_team_id, format, result)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		 ON CONFLICT (id) DO NOTHING`,
		matchID, accountID, "evt-"+matchID, "event-"+matchID, time.Now().UTC(),
		1, 0, 1, "Standard", "win",
	)
	if err != nil {
		t.Fatalf("insertTestMatchForCardPlays %q: %v", matchID, err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM matches WHERE id = $1`, matchID)
	})
}

// insertTestGameForCardPlays inserts a minimal games row and returns its id.
// Requires the match to exist first.
func insertTestGameForCardPlays(t *testing.T, db *sql.DB, matchID string, gameNumber int) int64 {
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
		t.Fatalf("insertTestGameForCardPlays match=%q game=%d: %v", matchID, gameNumber, err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM games WHERE id = $1`, id)
	})
	return id
}

func TestGamePlayRepository_SingleInsert(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewGamePlayRepository(db)
	ctx := context.Background()

	accountID := insertTestAccountForGamePlay(t, db, "single-insert")
	cleanupMatchGameResults(t, db, accountID)

	now := time.Now().UTC().Truncate(time.Microsecond)

	id, err := repo.InsertGamePlay(ctx, repository.GamePlayInsert{
		AccountID:     accountID,
		MatchID:       "match-si-001",
		GameNumber:    1,
		WinningTeamID: 1,
		TurnCount:     10,
		DurationSecs:  240,
		Sequence:      42,
		OccurredAt:    now,
	})
	if err != nil {
		t.Fatalf("InsertGamePlay: %v", err)
	}
	if id == 0 {
		t.Error("InsertGamePlay returned id=0")
	}

	row, err := repo.GetGamePlay(ctx, accountID, "match-si-001", 1)
	if err != nil {
		t.Fatalf("GetGamePlay: %v", err)
	}

	if row.MatchID != "match-si-001" {
		t.Errorf("match_id: want match-si-001, got %q", row.MatchID)
	}
	if row.GameNumber != 1 {
		t.Errorf("game_number: want 1, got %d", row.GameNumber)
	}
	if row.WinningTeamID != 1 {
		t.Errorf("winning_team_id: want 1, got %d", row.WinningTeamID)
	}
	if row.TurnCount != 10 {
		t.Errorf("turn_count: want 10, got %d", row.TurnCount)
	}
	if row.DurationSecs != 240 {
		t.Errorf("duration_secs: want 240, got %d", row.DurationSecs)
	}
	if row.Sequence != 42 {
		t.Errorf("sequence: want 42, got %d", row.Sequence)
	}
	if row.AccountID != accountID {
		t.Errorf("account_id: want %d, got %d", accountID, row.AccountID)
	}
}

func TestGamePlayRepository_MultiGameSession(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewGamePlayRepository(db)
	ctx := context.Background()

	accountID := insertTestAccountForGamePlay(t, db, "multi-game")
	cleanupMatchGameResults(t, db, accountID)

	base := time.Now().UTC().Truncate(time.Microsecond)

	for i := 1; i <= 3; i++ {
		_, err := repo.InsertGamePlay(ctx, repository.GamePlayInsert{
			AccountID:  accountID,
			MatchID:    "match-multi-001",
			GameNumber: i,
			TurnCount:  5 * i,
			Sequence:   uint64(i),
			OccurredAt: base.Add(time.Duration(i) * time.Minute),
		})
		if err != nil {
			t.Fatalf("InsertGamePlay game %d: %v", i, err)
		}
	}

	rows, err := repo.ListGamePlaysByMatch(ctx, accountID, "match-multi-001")
	if err != nil {
		t.Fatalf("ListGamePlaysByMatch: %v", err)
	}

	if len(rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(rows))
	}

	// Verify ordering by (occurred_at, sequence).
	for i, r := range rows {
		wantGame := i + 1
		if r.GameNumber != wantGame {
			t.Errorf("row[%d] game_number: want %d, got %d", i, wantGame, r.GameNumber)
		}
	}
}

func TestGamePlayRepository_OutOfOrderSequenceReordering(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewGamePlayRepository(db)
	ctx := context.Background()

	accountID := insertTestAccountForGamePlay(t, db, "ooo-seq")
	cleanupMatchGameResults(t, db, accountID)

	base := time.Now().UTC().Truncate(time.Microsecond)

	// Insert game 1 and game 2 in-order first.
	for _, gn := range []int{1, 2} {
		_, err := repo.InsertGamePlay(ctx, repository.GamePlayInsert{
			AccountID:  accountID,
			MatchID:    "match-ooo-001",
			GameNumber: gn,
			TurnCount:  5,
			Sequence:   uint64(10 + gn),
			OccurredAt: base.Add(time.Duration(gn) * time.Minute),
		})
		if err != nil {
			t.Fatalf("InsertGamePlay game %d: %v", gn, err)
		}
	}

	// Re-send game 1 with a lower sequence (stale retransmit).
	// The DB WHERE guard must reject the update.
	_, err := repo.InsertGamePlay(ctx, repository.GamePlayInsert{
		AccountID:  accountID,
		MatchID:    "match-ooo-001",
		GameNumber: 1,
		TurnCount:  99, // stale value — should not overwrite
		Sequence:   5,  // lower than the stored 11
		OccurredAt: base,
	})
	if err != nil {
		t.Fatalf("InsertGamePlay stale retransmit: %v", err)
	}

	row, err := repo.GetGamePlay(ctx, accountID, "match-ooo-001", 1)
	if err != nil {
		t.Fatalf("GetGamePlay after stale retransmit: %v", err)
	}

	// TurnCount must still be 5 (original), not 99 (stale).
	if row.TurnCount != 5 {
		t.Errorf("turn_count after stale retransmit: want 5, got %d", row.TurnCount)
	}
	if row.Sequence != 11 {
		t.Errorf("sequence after stale retransmit: want 11, got %d", row.Sequence)
	}
}

func TestGamePlayRepository_LifeChanges_InsertAndCount(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewGamePlayRepository(db)
	ctx := context.Background()

	accountID := insertTestAccountForGamePlay(t, db, "life-changes")
	cleanupMatchGameResults(t, db, accountID)

	matchGameResultID, err := repo.InsertGamePlay(ctx, repository.GamePlayInsert{
		AccountID:  accountID,
		MatchID:    "match-lc-001",
		GameNumber: 1,
		Sequence:   1,
		OccurredAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("InsertGamePlay: %v", err)
	}

	changes := []repository.LifeChangeInsert{
		{AccountID: accountID, MatchGameResultID: matchGameResultID, TeamID: 1, LifeTotal: 20, Delta: 0, TurnNumber: 1},
		{AccountID: accountID, MatchGameResultID: matchGameResultID, TeamID: 2, LifeTotal: 17, Delta: -3, TurnNumber: 2},
		{AccountID: accountID, MatchGameResultID: matchGameResultID, TeamID: 1, LifeTotal: 23, Delta: 3, TurnNumber: 3},
	}

	if err := repo.InsertLifeChanges(ctx, changes); err != nil {
		t.Fatalf("InsertLifeChanges: %v", err)
	}

	n, err := repo.CountLifeChangesByGame(ctx, matchGameResultID)
	if err != nil {
		t.Fatalf("CountLifeChangesByGame: %v", err)
	}
	if n != 3 {
		t.Errorf("life_change_tracking count: want 3, got %d", n)
	}
}

func TestGamePlayRepository_AccountIsolation(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewGamePlayRepository(db)
	ctx := context.Background()

	accountA := insertTestAccountForGamePlay(t, db, "isolation-a")
	accountB := insertTestAccountForGamePlay(t, db, "isolation-b")
	cleanupMatchGameResults(t, db, accountA)
	cleanupMatchGameResults(t, db, accountB)

	const matchID = "match-iso-001"
	now := time.Now().UTC()

	_, err := repo.InsertGamePlay(ctx, repository.GamePlayInsert{
		AccountID: accountA, MatchID: matchID, GameNumber: 1,
		TurnCount: 5, Sequence: 1, OccurredAt: now,
	})
	if err != nil {
		t.Fatalf("InsertGamePlay account A: %v", err)
	}

	_, err = repo.InsertGamePlay(ctx, repository.GamePlayInsert{
		AccountID: accountB, MatchID: matchID, GameNumber: 1,
		TurnCount: 99, Sequence: 1, OccurredAt: now,
	})
	if err != nil {
		t.Fatalf("InsertGamePlay account B: %v", err)
	}

	rowA, err := repo.GetGamePlay(ctx, accountA, matchID, 1)
	if err != nil {
		t.Fatalf("GetGamePlay account A: %v", err)
	}
	rowB, err := repo.GetGamePlay(ctx, accountB, matchID, 1)
	if err != nil {
		t.Fatalf("GetGamePlay account B: %v", err)
	}

	if rowA.TurnCount != 5 {
		t.Errorf("account A turn_count: want 5, got %d", rowA.TurnCount)
	}
	if rowB.TurnCount != 99 {
		t.Errorf("account B turn_count: want 99, got %d", rowB.TurnCount)
	}
}

func TestGamePlayRepository_ListGamePlaysByMatch_OrderedByOccurredAtSequence(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewGamePlayRepository(db)
	ctx := context.Background()

	accountID := insertTestAccountForGamePlay(t, db, "ordering")
	cleanupMatchGameResults(t, db, accountID)

	// Insert game 3, then game 1, then game 2 to verify ORDER BY works.
	base := time.Now().UTC().Truncate(time.Microsecond)

	type gameSeed struct {
		gameNumber int
		seq        uint64
		at         time.Time
	}
	seeds := []gameSeed{
		{3, 30, base.Add(3 * time.Minute)},
		{1, 10, base.Add(1 * time.Minute)},
		{2, 20, base.Add(2 * time.Minute)},
	}

	for _, s := range seeds {
		_, err := repo.InsertGamePlay(ctx, repository.GamePlayInsert{
			AccountID:  accountID,
			MatchID:    "match-order-001",
			GameNumber: s.gameNumber,
			Sequence:   s.seq,
			OccurredAt: s.at,
		})
		if err != nil {
			t.Fatalf("InsertGamePlay game %d: %v", s.gameNumber, err)
		}
	}

	rows, err := repo.ListGamePlaysByMatch(ctx, accountID, "match-order-001")
	if err != nil {
		t.Fatalf("ListGamePlaysByMatch: %v", err)
	}

	if len(rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(rows))
	}

	wantOrder := []int{1, 2, 3}
	for i, r := range rows {
		if r.GameNumber != wantOrder[i] {
			t.Errorf("row[%d]: want game_number=%d, got %d", i, wantOrder[i], r.GameNumber)
		}
	}
}

func TestGamePlayRepository_GetGamePlay_NotFound(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewGamePlayRepository(db)
	ctx := context.Background()

	accountID := insertTestAccountForGamePlay(t, db, "not-found")

	_, err := repo.GetGamePlay(ctx, accountID, "match-nonexistent", 1)
	if err != sql.ErrNoRows {
		t.Errorf("expected sql.ErrNoRows, got %v", err)
	}
}

// --- partial flag integration tests ---

// TestGamePlayRepository_PartialTrue verifies that InsertGamePlay stores
// partial=true when the insert carries Partial:true, and that GetGamePlay
// returns sql.ErrNoRows — partial rows are excluded from read queries.
func TestGamePlayRepository_PartialTrue(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewGamePlayRepository(db)
	ctx := context.Background()

	accountID := insertTestAccountForGamePlay(t, db, "partial-true")
	cleanupMatchGameResults(t, db, accountID)

	now := time.Now().UTC().Truncate(time.Microsecond)

	_, err := repo.InsertGamePlay(ctx, repository.GamePlayInsert{
		AccountID:  accountID,
		MatchID:    "match-partial-001",
		GameNumber: 1,
		Sequence:   1,
		OccurredAt: now,
		Partial:    true,
	})
	if err != nil {
		t.Fatalf("InsertGamePlay: %v", err)
	}

	// After the AND partial = false filter, GetGamePlay on a partial row must
	// return sql.ErrNoRows — partial rows are invisible to callers.
	_, err = repo.GetGamePlay(ctx, accountID, "match-partial-001", 1)
	if err != sql.ErrNoRows {
		t.Errorf("GetGamePlay on partial row: want sql.ErrNoRows, got %v", err)
	}
}

// TestGamePlayRepository_GetGamePlay_ExcludesPartial verifies that GetGamePlay
// does not return a row that was inserted with Partial:true.
func TestGamePlayRepository_GetGamePlay_ExcludesPartial(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewGamePlayRepository(db)
	ctx := context.Background()

	accountID := insertTestAccountForGamePlay(t, db, "gp-excl-partial")
	cleanupMatchGameResults(t, db, accountID)

	now := time.Now().UTC().Truncate(time.Microsecond)

	_, err := repo.InsertGamePlay(ctx, repository.GamePlayInsert{
		AccountID:  accountID,
		MatchID:    "match-excl-partial-001",
		GameNumber: 1,
		Sequence:   1,
		OccurredAt: now,
		Partial:    true,
	})
	if err != nil {
		t.Fatalf("InsertGamePlay partial=true: %v", err)
	}

	_, err = repo.GetGamePlay(ctx, accountID, "match-excl-partial-001", 1)
	if err != sql.ErrNoRows {
		t.Errorf("GetGamePlay on partial row: want sql.ErrNoRows, got %v", err)
	}
}

// TestGamePlayRepository_ListGamePlaysByMatch_ExcludesPartial verifies that
// ListGamePlaysByMatch omits rows inserted with Partial:true.
func TestGamePlayRepository_ListGamePlaysByMatch_ExcludesPartial(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewGamePlayRepository(db)
	ctx := context.Background()

	accountID := insertTestAccountForGamePlay(t, db, "list-excl-partial")
	cleanupMatchGameResults(t, db, accountID)

	base := time.Now().UTC().Truncate(time.Microsecond)

	type gameSeed struct {
		gameNumber int
		partial    bool
		seq        uint64
		at         time.Time
	}
	seeds := []gameSeed{
		{1, false, 10, base.Add(1 * time.Minute)},
		{2, true, 20, base.Add(2 * time.Minute)},
		{3, false, 30, base.Add(3 * time.Minute)},
	}

	for _, s := range seeds {
		_, err := repo.InsertGamePlay(ctx, repository.GamePlayInsert{
			AccountID:  accountID,
			MatchID:    "match-list-excl-001",
			GameNumber: s.gameNumber,
			Sequence:   s.seq,
			OccurredAt: s.at,
			Partial:    s.partial,
		})
		if err != nil {
			t.Fatalf("InsertGamePlay game %d: %v", s.gameNumber, err)
		}
	}

	rows, err := repo.ListGamePlaysByMatch(ctx, accountID, "match-list-excl-001")
	if err != nil {
		t.Fatalf("ListGamePlaysByMatch: %v", err)
	}

	if len(rows) != 2 {
		t.Fatalf("expected 2 rows (partial row excluded), got %d", len(rows))
	}

	// Rows must be game_number 1 and 3 — never game_number 2 (partial).
	wantGameNumbers := []int{1, 3}
	for i, r := range rows {
		if r.GameNumber != wantGameNumbers[i] {
			t.Errorf("row[%d]: want game_number=%d, got %d", i, wantGameNumbers[i], r.GameNumber)
		}
		if r.Partial {
			t.Errorf("row[%d] game_number=%d: partial must be false in read results, got true", i, r.GameNumber)
		}
	}
}

// TestGamePlayRepository_PartialFalse verifies that InsertGamePlay stores
// partial=false (the default) when Partial is not set.
func TestGamePlayRepository_PartialFalse(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewGamePlayRepository(db)
	ctx := context.Background()

	accountID := insertTestAccountForGamePlay(t, db, "partial-false")
	cleanupMatchGameResults(t, db, accountID)

	now := time.Now().UTC().Truncate(time.Microsecond)

	_, err := repo.InsertGamePlay(ctx, repository.GamePlayInsert{
		AccountID:  accountID,
		MatchID:    "match-nopartial-001",
		GameNumber: 1,
		Sequence:   1,
		OccurredAt: now,
		Partial:    false,
	})
	if err != nil {
		t.Fatalf("InsertGamePlay: %v", err)
	}

	row, err := repo.GetGamePlay(ctx, accountID, "match-nopartial-001", 1)
	if err != nil {
		t.Fatalf("GetGamePlay: %v", err)
	}
	if row.Partial {
		t.Errorf("Partial: want false, got true")
	}
}

// --- InsertCardPlays integration tests (ADR-050) ---

// TestGamePlayRepository_InsertCardPlays_WritesToGamePlays verifies that
// InsertCardPlays writes per-turn rows into game_plays (the per-turn table).
// AC: InsertCardPlays writes to game_plays (per-turn).
func TestGamePlayRepository_InsertCardPlays_WritesToGamePlays(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewGamePlayRepository(db)
	ctx := context.Background()

	accountID := insertTestAccountForGamePlay(t, db, "card-plays-write")
	cleanupMatchGameResults(t, db, accountID)

	const matchID = "match-cp-write-001"
	insertTestMatchForCardPlays(t, db, matchID, accountID)
	gameID := insertTestGameForCardPlays(t, db, matchID, 1)

	entries := []contract.CardPlayEntry{
		{GameNumber: 1, TurnNumber: 1, Phase: "main1", ArenaID: 80001, PlayerType: "player", ActionType: "play_card", ZoneFrom: "hand", ZoneTo: "battlefield"},
		{GameNumber: 1, TurnNumber: 2, Phase: "main1", ArenaID: 80002, PlayerType: "opponent", ActionType: "cast_spell", ZoneFrom: "hand", ZoneTo: "stack"},
		{GameNumber: 1, TurnNumber: 3, Phase: "combat", ArenaID: 80003, PlayerType: "player", ActionType: "attack", ZoneFrom: "battlefield", ZoneTo: "battlefield"},
	}

	now := time.Now().UTC()
	if err := repo.InsertCardPlays(ctx, accountID, gameID, matchID, entries, now); err != nil {
		t.Fatalf("InsertCardPlays: %v", err)
	}

	n, err := repo.CountCardPlaysByGame(ctx, gameID)
	if err != nil {
		t.Fatalf("CountCardPlaysByGame: %v", err)
	}
	if n != 3 {
		t.Errorf("game_plays count: want 3, got %d", n)
	}

	// AC: game_plays.account_id must be populated (ticket #820).
	var storedAccountID int64
	err = db.QueryRowContext(
		ctx,
		`SELECT account_id FROM game_plays WHERE game_id = $1 LIMIT 1`, gameID,
	).Scan(&storedAccountID)
	if err != nil {
		t.Fatalf("read game_plays.account_id: %v", err)
	}
	if storedAccountID != accountID {
		t.Errorf("game_plays.account_id: want %d, got %d — InsertCardPlays must populate account_id", accountID, storedAccountID)
	}
}

// TestGamePlayRepository_InsertCardPlays_Idempotent verifies that replaying
// InsertCardPlays for the same (game_id, sequence_number) does not produce
// duplicate rows (ON CONFLICT DO NOTHING).
func TestGamePlayRepository_InsertCardPlays_Idempotent(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewGamePlayRepository(db)
	ctx := context.Background()

	accountID := insertTestAccountForGamePlay(t, db, "card-plays-idem")
	cleanupMatchGameResults(t, db, accountID)

	const matchID = "match-cp-idem-001"
	insertTestMatchForCardPlays(t, db, matchID, accountID)
	gameID := insertTestGameForCardPlays(t, db, matchID, 1)

	entry := contract.CardPlayEntry{
		GameNumber: 1, TurnNumber: 1, Phase: "main1", ArenaID: 80001,
		PlayerType: "player", ActionType: "play_card", ZoneFrom: "hand", ZoneTo: "battlefield",
	}
	now := time.Now().UTC()

	if err := repo.InsertCardPlays(ctx, accountID, gameID, matchID, []contract.CardPlayEntry{entry}, now); err != nil {
		t.Fatalf("InsertCardPlays first: %v", err)
	}
	// Replay — must not error or produce a duplicate.
	if err := repo.InsertCardPlays(ctx, accountID, gameID, matchID, []contract.CardPlayEntry{entry}, now); err != nil {
		t.Fatalf("InsertCardPlays replay: %v", err)
	}

	n, err := repo.CountCardPlaysByGame(ctx, gameID)
	if err != nil {
		t.Fatalf("CountCardPlaysByGame: %v", err)
	}
	if n != 1 {
		t.Errorf("game_plays count after replay: want 1, got %d", n)
	}
}

// TestGamePlayRepository_InsertCardPlays_Empty verifies that InsertCardPlays
// on an empty slice is a no-op.
func TestGamePlayRepository_InsertCardPlays_Empty(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewGamePlayRepository(db)
	ctx := context.Background()

	if err := repo.InsertCardPlays(ctx, 0, 0, "", nil, time.Now().UTC()); err != nil {
		t.Errorf("InsertCardPlays(nil): want no error, got %v", err)
	}
	if err := repo.InsertCardPlays(ctx, 0, 0, "", []contract.CardPlayEntry{}, time.Now().UTC()); err != nil {
		t.Errorf("InsertCardPlays(empty): want no error, got %v", err)
	}
}

// TestGamePlayRepository_InsertGamePlay_WritesToMatchGameResults verifies that
// InsertGamePlay rows land in match_game_results, not game_plays.
// AC: InsertGamePlay writes to match_game_results (per-game).
func TestGamePlayRepository_InsertGamePlay_WritesToMatchGameResults(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewGamePlayRepository(db)
	ctx := context.Background()

	accountID := insertTestAccountForGamePlay(t, db, "mgr-target")
	cleanupMatchGameResults(t, db, accountID)

	now := time.Now().UTC().Truncate(time.Microsecond)

	id, err := repo.InsertGamePlay(ctx, repository.GamePlayInsert{
		AccountID:     accountID,
		MatchID:       "match-mgr-target-001",
		GameNumber:    1,
		WinningTeamID: 2,
		TurnCount:     15,
		DurationSecs:  600,
		Sequence:      7,
		OccurredAt:    now,
	})
	if err != nil {
		t.Fatalf("InsertGamePlay: %v", err)
	}

	// Verify the row landed in match_game_results (not game_plays).
	var count int
	err = db.QueryRowContext(
		ctx,
		`SELECT COUNT(*) FROM match_game_results WHERE id = $1 AND account_id = $2`,
		id, accountID,
	).Scan(&count)
	if err != nil {
		t.Fatalf("verify match_game_results: %v", err)
	}
	if count != 1 {
		t.Errorf("match_game_results count for inserted id: want 1, got %d", count)
	}

	// Verify game_plays was NOT written to by InsertGamePlay.
	var gpCount int
	err = db.QueryRowContext(
		ctx,
		`SELECT COUNT(*) FROM game_plays WHERE match_id = $1`,
		"match-mgr-target-001",
	).Scan(&gpCount)
	if err != nil {
		t.Fatalf("verify game_plays not written: %v", err)
	}
	if gpCount != 0 {
		t.Errorf("game_plays must not be written by InsertGamePlay: want 0, got %d", gpCount)
	}
}

// TestGamePlayRepository_GameIDByMatchAndNumber_NotFound verifies that
// GameIDByMatchAndNumber returns an error wrapping sql.ErrNoRows when no
// games row exists.
func TestGamePlayRepository_GameIDByMatchAndNumber_NotFound(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewGamePlayRepository(db)
	ctx := context.Background()

	_, err := repo.GameIDByMatchAndNumber(ctx, "no-such-match-xyz", 1)
	if err == nil {
		t.Fatal("expected error for nonexistent (match_id, game_number), got nil")
	}
}

// ---------------------------------------------------------------------------
// Ticket #687: player_on_play column
// ---------------------------------------------------------------------------

func TestGamePlayRepository_InsertAndRead_PlayerOnPlay(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewGamePlayRepository(db)

	accountID := insertTestAccountForGamePlay(t, db, "player-on-play")
	matchID := fmt.Sprintf("gp-onplay-%d", accountID)
	insertTestMatchForCardPlays(t, db, matchID, accountID)
	cleanupMatchGameResults(t, db, accountID)

	onPlay := true
	ins := repository.GamePlayInsert{
		AccountID:     accountID,
		MatchID:       matchID,
		GameNumber:    1,
		WinningTeamID: 1,
		TurnCount:     8,
		DurationSecs:  120,
		Sequence:      1,
		OccurredAt:    time.Now().UTC(),
		Partial:       false,
		PlayerOnPlay:  &onPlay,
	}

	id, err := repo.InsertGamePlay(context.Background(), ins)
	if err != nil {
		t.Fatalf("InsertGamePlay: %v", err)
	}
	if id == 0 {
		t.Fatal("InsertGamePlay returned id 0")
	}

	row, err := repo.GetGamePlay(context.Background(), accountID, matchID, 1)
	if err != nil {
		t.Fatalf("GetGamePlay: %v", err)
	}

	if row.PlayerOnPlay == nil {
		t.Fatal("PlayerOnPlay is nil after insert, want non-nil")
	}
	if !*row.PlayerOnPlay {
		t.Errorf("PlayerOnPlay = false, want true")
	}
}

func TestGamePlayRepository_InsertAndRead_PlayerOnPlay_Draw(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewGamePlayRepository(db)

	accountID := insertTestAccountForGamePlay(t, db, "player-on-draw")
	matchID := fmt.Sprintf("gp-ondraw-%d", accountID)
	insertTestMatchForCardPlays(t, db, matchID, accountID)
	cleanupMatchGameResults(t, db, accountID)

	onPlay := false
	ins := repository.GamePlayInsert{
		AccountID:     accountID,
		MatchID:       matchID,
		GameNumber:    1,
		WinningTeamID: 2,
		TurnCount:     12,
		DurationSecs:  180,
		Sequence:      1,
		OccurredAt:    time.Now().UTC(),
		Partial:       false,
		PlayerOnPlay:  &onPlay,
	}

	id, err := repo.InsertGamePlay(context.Background(), ins)
	if err != nil {
		t.Fatalf("InsertGamePlay: %v", err)
	}
	if id == 0 {
		t.Fatal("InsertGamePlay returned id 0")
	}

	row, err := repo.GetGamePlay(context.Background(), accountID, matchID, 1)
	if err != nil {
		t.Fatalf("GetGamePlay: %v", err)
	}

	if row.PlayerOnPlay == nil {
		t.Fatal("PlayerOnPlay is nil after insert, want non-nil")
	}
	if *row.PlayerOnPlay {
		t.Errorf("PlayerOnPlay = true, want false (player was on draw)")
	}
}

func TestGamePlayRepository_InsertAndRead_PlayerOnPlay_NilPreserved(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewGamePlayRepository(db)

	accountID := insertTestAccountForGamePlay(t, db, "player-on-play-nil")
	matchID := fmt.Sprintf("gp-onplay-nil-%d", accountID)
	insertTestMatchForCardPlays(t, db, matchID, accountID)
	cleanupMatchGameResults(t, db, accountID)

	// Insert without player_on_play (nil = unknown).
	ins := repository.GamePlayInsert{
		AccountID:     accountID,
		MatchID:       matchID,
		GameNumber:    1,
		WinningTeamID: 1,
		TurnCount:     5,
		DurationSecs:  90,
		Sequence:      1,
		OccurredAt:    time.Now().UTC(),
		Partial:       false,
		PlayerOnPlay:  nil,
	}

	id, err := repo.InsertGamePlay(context.Background(), ins)
	if err != nil {
		t.Fatalf("InsertGamePlay: %v", err)
	}
	if id == 0 {
		t.Fatal("InsertGamePlay returned id 0")
	}

	row, err := repo.GetGamePlay(context.Background(), accountID, matchID, 1)
	if err != nil {
		t.Fatalf("GetGamePlay: %v", err)
	}

	if row.PlayerOnPlay != nil {
		t.Errorf("PlayerOnPlay = %v, want nil when not captured", *row.PlayerOnPlay)
	}
}

// TestGamePlayRepository_InsertGamePlay_PlayerOnPlay_COALESCEPreservesKnown
// verifies the COALESCE in the ON CONFLICT DO UPDATE: a second insert with
// nil PlayerOnPlay must NOT overwrite an existing known value.
func TestGamePlayRepository_InsertGamePlay_PlayerOnPlay_COALESCEPreservesKnown(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewGamePlayRepository(db)

	accountID := insertTestAccountForGamePlay(t, db, "player-on-play-coalesce")
	matchID := fmt.Sprintf("gp-coalesce-%d", accountID)
	insertTestMatchForCardPlays(t, db, matchID, accountID)
	cleanupMatchGameResults(t, db, accountID)

	onPlay := true
	first := repository.GamePlayInsert{
		AccountID:     accountID,
		MatchID:       matchID,
		GameNumber:    1,
		WinningTeamID: 1,
		TurnCount:     6,
		DurationSecs:  100,
		Sequence:      1,
		OccurredAt:    time.Now().UTC(),
		Partial:       false,
		PlayerOnPlay:  &onPlay,
	}
	if _, err := repo.InsertGamePlay(context.Background(), first); err != nil {
		t.Fatalf("first InsertGamePlay: %v", err)
	}

	// Second insert (higher sequence) with nil PlayerOnPlay.
	second := repository.GamePlayInsert{
		AccountID:     accountID,
		MatchID:       matchID,
		GameNumber:    1,
		WinningTeamID: 1,
		TurnCount:     7,
		DurationSecs:  105,
		Sequence:      2,
		OccurredAt:    time.Now().UTC(),
		Partial:       false,
		PlayerOnPlay:  nil,
	}
	if _, err := repo.InsertGamePlay(context.Background(), second); err != nil {
		t.Fatalf("second InsertGamePlay: %v", err)
	}

	row, err := repo.GetGamePlay(context.Background(), accountID, matchID, 1)
	if err != nil {
		t.Fatalf("GetGamePlay: %v", err)
	}

	// The known value (true) must survive the nil update via COALESCE.
	if row.PlayerOnPlay == nil {
		t.Fatal("PlayerOnPlay is nil after COALESCE update, want non-nil")
	}
	if !*row.PlayerOnPlay {
		t.Errorf("PlayerOnPlay = false, want true (COALESCE must preserve known value)")
	}
}

// ─── UpsertGameRow tests (Defect A fix) ────────────────────────────────────

// TestGamePlayRepository_UpsertGameRow_CreatesRow verifies that UpsertGameRow
// inserts a games row and returns a non-zero id. This is the FK anchor required
// by game_plays.game_id before InsertCardPlays can write per-turn rows.
func TestGamePlayRepository_UpsertGameRow_CreatesRow(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewGamePlayRepository(db)
	ctx := context.Background()

	accountID := insertTestAccountForGamePlay(t, db, "upsert-game-row")
	const matchID = "match-ugr-001"
	insertTestMatchForCardPlays(t, db, matchID, accountID)

	id, err := repo.UpsertGameRow(ctx, matchID, 1)
	if err != nil {
		t.Fatalf("UpsertGameRow: %v", err)
	}
	if id == 0 {
		t.Error("UpsertGameRow returned id=0")
	}

	// Verify the row is readable by GameIDByMatchAndNumber.
	resolvedID, err := repo.GameIDByMatchAndNumber(ctx, matchID, 1)
	if err != nil {
		t.Fatalf("GameIDByMatchAndNumber after UpsertGameRow: %v", err)
	}
	if resolvedID != id {
		t.Errorf("GameIDByMatchAndNumber: want %d, got %d", id, resolvedID)
	}

	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM games WHERE id = $1`, id)
	})
}

// TestGamePlayRepository_UpsertGameRow_Idempotent verifies that calling
// UpsertGameRow twice for the same (match_id, game_number) returns the same id
// and does not produce a duplicate row.
func TestGamePlayRepository_UpsertGameRow_Idempotent(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewGamePlayRepository(db)
	ctx := context.Background()

	accountID := insertTestAccountForGamePlay(t, db, "upsert-game-row-idem")
	const matchID = "match-ugr-idem-001"
	insertTestMatchForCardPlays(t, db, matchID, accountID)

	id1, err := repo.UpsertGameRow(ctx, matchID, 2)
	if err != nil {
		t.Fatalf("UpsertGameRow first: %v", err)
	}

	id2, err := repo.UpsertGameRow(ctx, matchID, 2)
	if err != nil {
		t.Fatalf("UpsertGameRow second: %v", err)
	}

	if id1 != id2 {
		t.Errorf("UpsertGameRow idempotency: first id=%d, second id=%d — expected same id", id1, id2)
	}

	// Count rows — must be exactly 1.
	var count int
	if err := db.QueryRowContext(
		ctx,
		`SELECT COUNT(*) FROM games WHERE match_id = $1 AND game_number = $2`, matchID, 2,
	).Scan(&count); err != nil {
		t.Fatalf("count games rows: %v", err)
	}
	if count != 1 {
		t.Errorf("games row count after two UpsertGameRow calls: want 1, got %d", count)
	}

	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM games WHERE id = $1`, id1)
	})
}

// TestGamePlayRepository_UpsertGameRow_ThenInsertCardPlays verifies the full
// Defect A fix path: UpsertGameRow followed by InsertCardPlays succeeds and
// writes card play rows that are countable via CountCardPlaysByGame.
func TestGamePlayRepository_UpsertGameRow_ThenInsertCardPlays(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewGamePlayRepository(db)
	ctx := context.Background()

	accountID := insertTestAccountForGamePlay(t, db, "upsert-then-cp")
	const matchID = "match-utcp-001"
	insertTestMatchForCardPlays(t, db, matchID, accountID)

	// UpsertGameRow provides the FK anchor.
	gameID, err := repo.UpsertGameRow(ctx, matchID, 1)
	if err != nil {
		t.Fatalf("UpsertGameRow: %v", err)
	}

	entries := []contract.CardPlayEntry{
		{GameNumber: 1, TurnNumber: 1, Phase: "main1", ArenaID: 90001, PlayerType: "player", ActionType: "play_card", ZoneFrom: "hand", ZoneTo: "battlefield"},
		{GameNumber: 1, TurnNumber: 2, Phase: "main1", ArenaID: 90002, PlayerType: "opponent", ActionType: "cast_spell", ZoneFrom: "hand", ZoneTo: "stack"},
	}

	now := time.Now().UTC()
	if err := repo.InsertCardPlays(ctx, accountID, gameID, matchID, entries, now); err != nil {
		t.Fatalf("InsertCardPlays after UpsertGameRow: %v", err)
	}

	n, err := repo.CountCardPlaysByGame(ctx, gameID)
	if err != nil {
		t.Fatalf("CountCardPlaysByGame: %v", err)
	}
	if n != 2 {
		t.Errorf("game_plays count: want 2, got %d", n)
	}

	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM game_plays WHERE game_id = $1`, gameID)
		_, _ = db.ExecContext(context.Background(), `DELETE FROM games WHERE id = $1`, gameID)
	})
}
