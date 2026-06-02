package repository

import (
	"context"
	"database/sql"
	"time"
)

// HistorySummaryPeriod holds win/loss counts and the computed win-rate for
// a single time window (today, this_week, or all_time).
type HistorySummaryPeriod struct {
	Wins    int
	Losses  int
	WinRate float64
	// Matches is the total count of wins+losses for this period.
	// (separate from WinRate so the handler can expose it for non-today periods)
	Matches int
}

// HistoryStreakInfo is appended to the all_time period.
type HistoryStreakInfo struct {
	CurrentStreak int
	StreakType    string // "W" | "L" | "" when no matches
}

// LastMatchInfo describes the most recent match for the summary footer.
// OpponentArchetype is nil when no archetype is recorded.
type LastMatchInfo struct {
	Result            string
	OpponentArchetype *string
	ElapsedSeconds    int
}

// HistorySummaryResult is the complete data set returned by
// GetHistorySummary. The handler maps this to the JSON contract.
type HistorySummaryResult struct {
	Today     HistorySummaryPeriod
	ThisWeek  HistorySummaryPeriod
	AllTime   HistorySummaryPeriod
	Streak    HistoryStreakInfo
	LastMatch *LastMatchInfo
}

// HistorySummaryRepository computes the data needed for the
// GET /api/v1/history/summary endpoint.  All queries are scoped
// by account_id.
type HistorySummaryRepository struct {
	db DB
}

// NewHistorySummaryRepository returns a HistorySummaryRepository backed by db.
func NewHistorySummaryRepository(db DB) *HistorySummaryRepository {
	return &HistorySummaryRepository{db: db}
}

// GetHistorySummary runs the three aggregate queries and a streak walk for
// the given account. "this_week" uses calendar week (Monday 00:00 UTC to the
// end of the current week) — consistent with the label and with how Prof
// defined it ("this week" framing, not rolling 7d).
//
// All queries read from the matches table only; no migration is needed.
// win_rate = wins / (wins + losses), 0.0 when both are zero.
func (r *HistorySummaryRepository) GetHistorySummary(ctx context.Context, accountID int64, now time.Time) (HistorySummaryResult, error) {
	var result HistorySummaryResult

	// ── Aggregate: today + this week + all-time in one pass ──────────────────
	// date_trunc('week', ...) truncates to Monday 00:00 UTC (ISO week start).
	const aggQ = `
		SELECT
			COUNT(*) FILTER (
				WHERE lower(result) IN ('win','loss')
				  AND timestamp >= date_trunc('day', $2::timestamptz)
			) AS today_total,
			COUNT(*) FILTER (
				WHERE lower(result) = 'win'
				  AND timestamp >= date_trunc('day', $2::timestamptz)
			) AS today_wins,
			COUNT(*) FILTER (
				WHERE lower(result) = 'loss'
				  AND timestamp >= date_trunc('day', $2::timestamptz)
			) AS today_losses,
			COUNT(*) FILTER (
				WHERE lower(result) IN ('win','loss')
				  AND timestamp >= date_trunc('week', $2::timestamptz)
			) AS week_total,
			COUNT(*) FILTER (
				WHERE lower(result) = 'win'
				  AND timestamp >= date_trunc('week', $2::timestamptz)
			) AS week_wins,
			COUNT(*) FILTER (
				WHERE lower(result) = 'loss'
				  AND timestamp >= date_trunc('week', $2::timestamptz)
			) AS week_losses,
			COUNT(*) FILTER (WHERE lower(result) IN ('win','loss'))   AS all_total,
			COUNT(*) FILTER (WHERE lower(result) = 'win')             AS all_wins,
			COUNT(*) FILTER (WHERE lower(result) = 'loss')            AS all_losses
		FROM matches
		WHERE account_id = $1`

	var (
		todayTotal, todayWins, todayLosses int
		weekTotal, weekWins, weekLosses    int
		allTotal, allWins, allLosses       int
	)

	if err := r.db.QueryRowContext(ctx, aggQ, accountID, now).Scan(
		&todayTotal, &todayWins, &todayLosses,
		&weekTotal, &weekWins, &weekLosses,
		&allTotal, &allWins, &allLosses,
	); err != nil {
		return result, err
	}

	result.Today = HistorySummaryPeriod{
		Wins:    todayWins,
		Losses:  todayLosses,
		WinRate: safeWinRate(todayWins, todayLosses),
		Matches: todayTotal,
	}
	result.ThisWeek = HistorySummaryPeriod{
		Wins:    weekWins,
		Losses:  weekLosses,
		WinRate: safeWinRate(weekWins, weekLosses),
		Matches: weekTotal,
	}
	result.AllTime = HistorySummaryPeriod{
		Wins:    allWins,
		Losses:  allLosses,
		WinRate: safeWinRate(allWins, allLosses),
		Matches: allTotal,
	}

	// ── Streak: walk most-recent matches DESC, count consecutive same result ─
	// Fetch only the result column for the most recent 200 matches — far more
	// than any realistic streak, keeps the query cheap.
	const streakQ = `
		SELECT lower(result)
		FROM matches
		WHERE account_id = $1
		  AND lower(result) IN ('win','loss')
		ORDER BY timestamp DESC, id DESC
		LIMIT 200`

	streakRows, err := r.db.QueryContext(ctx, streakQ, accountID)
	if err != nil {
		return result, err
	}
	defer func() { _ = streakRows.Close() }()

	var (
		streakCount int
		streakType  string
		firstResult string
	)

	for streakRows.Next() {
		var res string
		if err := streakRows.Scan(&res); err != nil {
			return result, err
		}
		if firstResult == "" {
			firstResult = res
			streakType = resultToStreakType(res)
		}
		if res == firstResult {
			streakCount++
		} else {
			break
		}
	}
	if err := streakRows.Err(); err != nil {
		return result, err
	}

	result.Streak = HistoryStreakInfo{
		CurrentStreak: streakCount,
		StreakType:    streakType,
	}

	// ── Last match ────────────────────────────────────────────────────────────
	const lastQ = `
		SELECT lower(result), opponent_name, duration_seconds, timestamp
		FROM matches
		WHERE account_id = $1
		  AND lower(result) IN ('win','loss')
		ORDER BY timestamp DESC, id DESC
		LIMIT 1`

	var (
		lastResult    string
		lastOpponent  sql.NullString
		lastDuration  sql.NullInt64
		lastTimestamp time.Time
	)

	err = r.db.QueryRowContext(ctx, lastQ, accountID).Scan(
		&lastResult, &lastOpponent, &lastDuration, &lastTimestamp,
	)
	if err == sql.ErrNoRows {
		result.LastMatch = nil
	} else if err != nil {
		return result, err
	} else {
		elapsed := int(now.Sub(lastTimestamp).Seconds())
		if elapsed < 0 {
			elapsed = 0
		}
		lm := &LastMatchInfo{
			Result:         lastResult,
			ElapsedSeconds: elapsed,
		}
		if lastOpponent.Valid && lastOpponent.String != "" {
			lm.OpponentArchetype = &lastOpponent.String
		}
		result.LastMatch = lm
	}

	return result, nil
}

// safeWinRate computes wins / (wins+losses), returning 0.0 on zero denominator.
func safeWinRate(wins, losses int) float64 {
	total := wins + losses
	if total == 0 {
		return 0.0
	}
	return float64(wins) / float64(total)
}

// resultToStreakType maps "win"/"loss" to "W"/"L". Any other value (draw,
// unknown) returns "".
func resultToStreakType(r string) string {
	switch r {
	case "win":
		return "W"
	case "loss":
		return "L"
	default:
		return ""
	}
}
