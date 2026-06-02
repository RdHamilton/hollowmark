package repository_test

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/RdHamilton/vault-mtg/services/bff/internal/storage/repository"
)

// insertTestMatchFull inserts a match with result and duration_seconds.
func insertTestMatchFull(
	t *testing.T,
	db *sql.DB,
	matchID string,
	accountID int64,
	format string,
	ts time.Time,
	result string,
	durationSecs *int,
) {
	t.Helper()

	_, err := db.ExecContext(
		context.Background(),
		`INSERT INTO matches
			(id, account_id, event_id, event_name, timestamp, player_wins, opponent_wins,
			 player_team_id, format, result, duration_seconds)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		matchID, accountID,
		"evt-"+matchID, "event-"+matchID,
		ts,
		func() int {
			if result == "win" {
				return 1
			}
			return 0
		}(),
		func() int {
			if result == "win" {
				return 0
			}
			return 1
		}(),
		1,
		format, result,
		durationSecs,
	)
	if err != nil {
		t.Fatalf("insertTestMatchFull %q: %v", matchID, err)
	}

	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM matches WHERE id = $1`, matchID)
	})
}

// insertSummaryMatch is a convenience wrapper that inserts a match with a
// fixed duration of 600s.
func insertSummaryMatch(t *testing.T, db *sql.DB, matchID string, accountID int64, ts time.Time, result string) {
	t.Helper()
	dur := 600
	insertTestMatchFull(t, db, matchID, accountID, "Standard", ts, result, &dur)
}

// ─── Empty-data baseline ─────────────────────────────────────────────────────

func TestHistorySummaryRepository_EmptyAccount(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewHistorySummaryRepository(db)

	accountID := insertTestAccount(t, db, "hsum-empty")
	now := time.Now().UTC()

	got, err := repo.GetHistorySummary(context.Background(), accountID, now)
	if err != nil {
		t.Fatalf("GetHistorySummary: %v", err)
	}

	// Today
	if got.Today.Wins != 0 || got.Today.Losses != 0 {
		t.Errorf("today: want 0/0, got %d/%d", got.Today.Wins, got.Today.Losses)
	}
	if got.Today.WinRate != 0.0 {
		t.Errorf("today win_rate: want 0.0, got %f", got.Today.WinRate)
	}

	// This week
	if got.ThisWeek.Wins != 0 || got.ThisWeek.Losses != 0 {
		t.Errorf("this_week: want 0/0, got %d/%d", got.ThisWeek.Wins, got.ThisWeek.Losses)
	}
	if got.ThisWeek.WinRate != 0.0 {
		t.Errorf("this_week win_rate: want 0.0, got %f", got.ThisWeek.WinRate)
	}

	// All time
	if got.AllTime.Wins != 0 || got.AllTime.Losses != 0 {
		t.Errorf("all_time: want 0/0, got %d/%d", got.AllTime.Wins, got.AllTime.Losses)
	}
	if got.AllTime.WinRate != 0.0 {
		t.Errorf("all_time win_rate: want 0.0, got %f", got.AllTime.WinRate)
	}

	// Streak
	if got.Streak.CurrentStreak != 0 {
		t.Errorf("streak: want 0, got %d", got.Streak.CurrentStreak)
	}
	if got.Streak.StreakType != "" {
		t.Errorf("streak_type: want empty, got %q", got.Streak.StreakType)
	}

	// Last match
	if got.LastMatch != nil {
		t.Errorf("last_match: want nil, got %+v", got.LastMatch)
	}
}

// ─── Win streak ──────────────────────────────────────────────────────────────

func TestHistorySummaryRepository_WinStreak(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewHistorySummaryRepository(db)

	accountID := insertTestAccount(t, db, "hsum-wstreak")
	now := time.Now().UTC().Truncate(time.Second)

	// 3 consecutive wins most-recent, then 1 loss older
	insertSummaryMatch(t, db, fmt.Sprintf("hsum-ws-1-%d", accountID), accountID, now.Add(-time.Minute), "win")
	insertSummaryMatch(t, db, fmt.Sprintf("hsum-ws-2-%d", accountID), accountID, now.Add(-2*time.Minute), "win")
	insertSummaryMatch(t, db, fmt.Sprintf("hsum-ws-3-%d", accountID), accountID, now.Add(-3*time.Minute), "win")
	insertSummaryMatch(t, db, fmt.Sprintf("hsum-ws-4-%d", accountID), accountID, now.Add(-4*time.Minute), "loss")

	got, err := repo.GetHistorySummary(context.Background(), accountID, now)
	if err != nil {
		t.Fatalf("GetHistorySummary: %v", err)
	}

	if got.Streak.CurrentStreak != 3 {
		t.Errorf("streak: want 3, got %d", got.Streak.CurrentStreak)
	}
	if got.Streak.StreakType != "W" {
		t.Errorf("streak_type: want W, got %q", got.Streak.StreakType)
	}

	// all_time win_rate: 3 wins / 4 total = 0.75
	wantRate := 3.0 / 4.0
	if got.AllTime.WinRate < wantRate-0.001 || got.AllTime.WinRate > wantRate+0.001 {
		t.Errorf("all_time win_rate: want %.4f, got %.4f", wantRate, got.AllTime.WinRate)
	}

	if got.AllTime.Wins != 3 {
		t.Errorf("all_time wins: want 3, got %d", got.AllTime.Wins)
	}
	if got.AllTime.Losses != 1 {
		t.Errorf("all_time losses: want 1, got %d", got.AllTime.Losses)
	}
}

// ─── Loss streak ─────────────────────────────────────────────────────────────

func TestHistorySummaryRepository_LossStreak(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewHistorySummaryRepository(db)

	accountID := insertTestAccount(t, db, "hsum-lstreak")
	now := time.Now().UTC().Truncate(time.Second)

	// 2 consecutive losses, then 5 older wins
	insertSummaryMatch(t, db, fmt.Sprintf("hsum-ls-1-%d", accountID), accountID, now.Add(-time.Minute), "loss")
	insertSummaryMatch(t, db, fmt.Sprintf("hsum-ls-2-%d", accountID), accountID, now.Add(-2*time.Minute), "loss")
	for i := 3; i <= 7; i++ {
		insertSummaryMatch(t, db, fmt.Sprintf("hsum-ls-%d-%d", i, accountID), accountID, now.Add(-time.Duration(i)*time.Minute), "win")
	}

	got, err := repo.GetHistorySummary(context.Background(), accountID, now)
	if err != nil {
		t.Fatalf("GetHistorySummary: %v", err)
	}

	if got.Streak.CurrentStreak != 2 {
		t.Errorf("streak: want 2, got %d", got.Streak.CurrentStreak)
	}
	if got.Streak.StreakType != "L" {
		t.Errorf("streak_type: want L, got %q", got.Streak.StreakType)
	}
}

// ─── Today / this-week / all-time window isolation ───────────────────────────

func TestHistorySummaryRepository_TimeWindows(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewHistorySummaryRepository(db)

	accountID := insertTestAccount(t, db, "hsum-windows")

	// Use a fixed "now" so we can place matches precisely.
	// Pick a Wednesday so "this week" starts Monday and includes Wednesday.
	// 2026-06-03 is a Wednesday.
	now := time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC)

	// today = 2026-06-03
	// this_week start (Monday) = 2026-06-01
	// anything older = all_time only

	// 1 win today
	insertSummaryMatch(t, db, fmt.Sprintf("hsum-w-today-%d", accountID), accountID, time.Date(2026, 6, 3, 9, 0, 0, 0, time.UTC), "win")

	// 1 loss earlier this week (Tuesday = 2026-06-02)
	insertSummaryMatch(t, db, fmt.Sprintf("hsum-w-week-%d", accountID), accountID, time.Date(2026, 6, 2, 15, 0, 0, 0, time.UTC), "loss")

	// 1 win last week (2026-05-28)
	insertSummaryMatch(t, db, fmt.Sprintf("hsum-w-old-%d", accountID), accountID, time.Date(2026, 5, 28, 10, 0, 0, 0, time.UTC), "win")

	got, err := repo.GetHistorySummary(context.Background(), accountID, now)
	if err != nil {
		t.Fatalf("GetHistorySummary: %v", err)
	}

	// today: 1 win, 0 losses
	if got.Today.Wins != 1 || got.Today.Losses != 0 {
		t.Errorf("today: want 1W/0L, got %dW/%dL", got.Today.Wins, got.Today.Losses)
	}
	if got.Today.WinRate != 1.0 {
		t.Errorf("today win_rate: want 1.0, got %f", got.Today.WinRate)
	}

	// this_week: 1 win (today) + 1 loss (Tuesday) = 2 matches
	if got.ThisWeek.Wins != 1 || got.ThisWeek.Losses != 1 {
		t.Errorf("this_week: want 1W/1L, got %dW/%dL", got.ThisWeek.Wins, got.ThisWeek.Losses)
	}
	if got.ThisWeek.Matches != 2 {
		t.Errorf("this_week matches: want 2, got %d", got.ThisWeek.Matches)
	}
	wantWeekRate := 0.5
	if got.ThisWeek.WinRate < wantWeekRate-0.001 || got.ThisWeek.WinRate > wantWeekRate+0.001 {
		t.Errorf("this_week win_rate: want 0.5, got %f", got.ThisWeek.WinRate)
	}

	// all_time: 2 wins, 1 loss
	if got.AllTime.Wins != 2 || got.AllTime.Losses != 1 {
		t.Errorf("all_time: want 2W/1L, got %dW/%dL", got.AllTime.Wins, got.AllTime.Losses)
	}
	if got.AllTime.Matches != 3 {
		t.Errorf("all_time matches: want 3, got %d", got.AllTime.Matches)
	}
}

// ─── win_rate=0.0 when no matches (no divide-by-zero) ────────────────────────

func TestHistorySummaryRepository_WinRateZeroWhenNoMatches(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewHistorySummaryRepository(db)

	accountID := insertTestAccount(t, db, "hsum-zerowr")
	now := time.Now().UTC()

	got, err := repo.GetHistorySummary(context.Background(), accountID, now)
	if err != nil {
		t.Fatalf("GetHistorySummary: %v", err)
	}

	for _, period := range []struct {
		name string
		wr   float64
	}{
		{"today", got.Today.WinRate},
		{"this_week", got.ThisWeek.WinRate},
		{"all_time", got.AllTime.WinRate},
	} {
		if period.wr != 0.0 {
			t.Errorf("%s win_rate with zero matches: want 0.0, got %f", period.name, period.wr)
		}
	}
}

// ─── last_match null when no matches ─────────────────────────────────────────

func TestHistorySummaryRepository_LastMatchNullWhenEmpty(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewHistorySummaryRepository(db)

	accountID := insertTestAccount(t, db, "hsum-lmnull")
	now := time.Now().UTC()

	got, err := repo.GetHistorySummary(context.Background(), accountID, now)
	if err != nil {
		t.Fatalf("GetHistorySummary: %v", err)
	}

	if got.LastMatch != nil {
		t.Errorf("last_match: want nil for empty account, got %+v", got.LastMatch)
	}
}

// ─── last_match fields ────────────────────────────────────────────────────────

func TestHistorySummaryRepository_LastMatchFields(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewHistorySummaryRepository(db)

	accountID := insertTestAccount(t, db, "hsum-lmfields")

	// Place the match exactly 120 seconds before now.
	now := time.Now().UTC().Truncate(time.Second)
	matchTS := now.Add(-120 * time.Second)

	dur := 600
	matchID := fmt.Sprintf("hsum-lm-1-%d", accountID)
	_, err := db.ExecContext(
		context.Background(),
		`INSERT INTO matches
			(id, account_id, event_id, event_name, timestamp, player_wins, opponent_wins,
			 player_team_id, format, result, duration_seconds)
		 VALUES ($1, $2, $3, $4, $5, 1, 0, 1, 'Standard', 'win', $6)`,
		matchID, accountID,
		"evt-"+matchID, "event-"+matchID,
		matchTS, dur,
	)
	if err != nil {
		t.Fatalf("insert match: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DELETE FROM matches WHERE id = $1`, matchID)
	})

	got, err := repo.GetHistorySummary(context.Background(), accountID, now)
	if err != nil {
		t.Fatalf("GetHistorySummary: %v", err)
	}

	if got.LastMatch == nil {
		t.Fatal("last_match: want non-nil")
	}
	if got.LastMatch.Result != "win" {
		t.Errorf("last_match.result: want win, got %q", got.LastMatch.Result)
	}
	// elapsed should be ~120 seconds (allow ±2s for clock jitter)
	if got.LastMatch.ElapsedSeconds < 118 || got.LastMatch.ElapsedSeconds > 122 {
		t.Errorf("last_match.elapsed_seconds: want ~120, got %d", got.LastMatch.ElapsedSeconds)
	}
	// No opponent_name set → OpponentArchetype should be nil
	if got.LastMatch.OpponentArchetype != nil {
		t.Errorf("last_match.opponent_archetype: want nil when not set, got %q", *got.LastMatch.OpponentArchetype)
	}
}

// ─── cross-account isolation ─────────────────────────────────────────────────

func TestHistorySummaryRepository_CrossAccountIsolation(t *testing.T) {
	db := openTestDB(t)
	repo := repository.NewHistorySummaryRepository(db)

	accountA := insertTestAccount(t, db, "hsum-iso-a")
	accountB := insertTestAccount(t, db, "hsum-iso-b")

	now := time.Now().UTC().Truncate(time.Second)

	// Insert 5 wins for account B only.
	for i := 0; i < 5; i++ {
		insertSummaryMatch(t, db, fmt.Sprintf("hsum-iso-b-%d-%d", i, accountB), accountB, now.Add(-time.Duration(i)*time.Minute), "win")
	}

	// Account A query must return zeros.
	got, err := repo.GetHistorySummary(context.Background(), accountA, now)
	if err != nil {
		t.Fatalf("GetHistorySummary: %v", err)
	}

	if got.AllTime.Wins != 0 || got.AllTime.Losses != 0 {
		t.Errorf("cross-account leak: accountA all_time shows %dW/%dL from accountB", got.AllTime.Wins, got.AllTime.Losses)
	}
	if got.Streak.CurrentStreak != 0 {
		t.Errorf("cross-account leak: accountA streak = %d", got.Streak.CurrentStreak)
	}
	if got.LastMatch != nil {
		t.Errorf("cross-account leak: accountA last_match is non-nil")
	}
}
