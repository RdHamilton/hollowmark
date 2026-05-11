package repository

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
	"time"
)

// ListByAccountIDCursor returns up to limit+1 matches using keyset (cursor)
// pagination. The extra row signals has_more=true to the caller.
//
// When cursorTS and cursorID are both non-zero the query applies the keyset
// predicate (timestamp, id) < (cursorTS, cursorID), restricting results to
// rows that come after the cursor in the default DESC ordering. When cursorTS
// is nil (first page) no keyset predicate is applied.
//
// format may be empty to return all formats.
func (r *MatchesRepository) ListByAccountIDCursor(
	ctx context.Context,
	accountID int64,
	format string,
	cursorTS *time.Time,
	cursorID string,
	limit int,
) ([]MatchRow, error) {
	fetch := limit + 1 // fetch one extra to detect has_more

	var (
		rows *sql.Rows
		err  error
	)

	switch {
	case format != "" && cursorTS != nil:
		const q = `
			SELECT id, format, result, timestamp, duration_seconds, deck_id, rank_before, rank_after,
			       player_wins, opponent_wins
			FROM matches
			WHERE account_id = $1
			  AND lower(format) = lower($2)
			  AND (timestamp < $3 OR (timestamp = $3 AND id < $4))
			ORDER BY timestamp DESC, id DESC
			LIMIT $5`

		rows, err = r.db.QueryContext(ctx, q, accountID, format, *cursorTS, cursorID, fetch)

	case format != "" && cursorTS == nil:
		const q = `
			SELECT id, format, result, timestamp, duration_seconds, deck_id, rank_before, rank_after,
			       player_wins, opponent_wins
			FROM matches
			WHERE account_id = $1 AND lower(format) = lower($2)
			ORDER BY timestamp DESC, id DESC
			LIMIT $3`

		rows, err = r.db.QueryContext(ctx, q, accountID, format, fetch)

	case format == "" && cursorTS != nil:
		const q = `
			SELECT id, format, result, timestamp, duration_seconds, deck_id, rank_before, rank_after,
			       player_wins, opponent_wins
			FROM matches
			WHERE account_id = $1
			  AND (timestamp < $2 OR (timestamp = $2 AND id < $3))
			ORDER BY timestamp DESC, id DESC
			LIMIT $4`

		rows, err = r.db.QueryContext(ctx, q, accountID, *cursorTS, cursorID, fetch)

	default: // format == "" && cursorTS == nil (first page, no filter)
		const q = `
			SELECT id, format, result, timestamp, duration_seconds, deck_id, rank_before, rank_after,
			       player_wins, opponent_wins
			FROM matches
			WHERE account_id = $1
			ORDER BY timestamp DESC, id DESC
			LIMIT $2`

		rows, err = r.db.QueryContext(ctx, q, accountID, fetch)
	}

	if err != nil {
		return nil, err
	}

	defer rows.Close()

	var matches []MatchRow

	for rows.Next() {
		var m MatchRow
		if err := rows.Scan(
			&m.ID, &m.Format, &m.Result, &m.Timestamp,
			&m.DurationSeconds, &m.DeckID, &m.RankBefore, &m.RankAfter,
			&m.PlayerWins, &m.OpponentWins,
		); err != nil {
			return nil, err
		}

		matches = append(matches, m)
	}

	return matches, rows.Err()
}

// MatchRow is a row returned from the matches table for history reads.
type MatchRow struct {
	ID              string
	Format          string
	Result          string
	Timestamp       time.Time
	DurationSeconds *int
	DeckID          *string
	RankBefore      *string
	RankAfter       *string
	PlayerWins      int
	OpponentWins    int
}

// MatchesRepository provides read access to the matches table scoped by account_id.
type MatchesRepository struct {
	db DB
}

// NewMatchesRepository returns a MatchesRepository backed by db.
func NewMatchesRepository(db DB) *MatchesRepository {
	return &MatchesRepository{db: db}
}

// ListByAccountID returns a page of matches for the given account, ordered by
// timestamp DESC.  format may be empty to return all formats.
// Returns rows and total count (for pagination).
func (r *MatchesRepository) ListByAccountID(
	ctx context.Context,
	accountID int64,
	format string,
	page int,
	limit int,
) ([]MatchRow, int, error) {
	offset := (page - 1) * limit

	var (
		rows *sql.Rows
		err  error
	)

	if format != "" {
		const q = `
			SELECT id, format, result, timestamp, duration_seconds, deck_id, rank_before, rank_after,
			       player_wins, opponent_wins
			FROM matches
			WHERE account_id = $1 AND lower(format) = lower($2)
			ORDER BY timestamp DESC
			LIMIT $3 OFFSET $4`

		rows, err = r.db.QueryContext(ctx, q, accountID, format, limit, offset)
	} else {
		const q = `
			SELECT id, format, result, timestamp, duration_seconds, deck_id, rank_before, rank_after,
			       player_wins, opponent_wins
			FROM matches
			WHERE account_id = $1
			ORDER BY timestamp DESC
			LIMIT $2 OFFSET $3`

		rows, err = r.db.QueryContext(ctx, q, accountID, limit, offset)
	}

	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var matches []MatchRow

	for rows.Next() {
		var m MatchRow
		if err := rows.Scan(
			&m.ID, &m.Format, &m.Result, &m.Timestamp,
			&m.DurationSeconds, &m.DeckID, &m.RankBefore, &m.RankAfter,
			&m.PlayerWins, &m.OpponentWins,
		); err != nil {
			return nil, 0, err
		}

		matches = append(matches, m)
	}

	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	total, err := r.countByAccountID(ctx, accountID, format)
	if err != nil {
		return nil, 0, err
	}

	return matches, total, nil
}

func (r *MatchesRepository) countByAccountID(ctx context.Context, accountID int64, format string) (int, error) {
	var total int

	if format != "" {
		const q = `SELECT COUNT(*) FROM matches WHERE account_id = $1 AND lower(format) = lower($2)`
		row := r.db.QueryRowContext(ctx, q, accountID, format)

		if err := row.Scan(&total); err != nil {
			return 0, err
		}
	} else {
		const q = `SELECT COUNT(*) FROM matches WHERE account_id = $1`
		row := r.db.QueryRowContext(ctx, q, accountID)

		if err := row.Scan(&total); err != nil {
			return 0, err
		}
	}

	return total, nil
}

// UpsertMatch inserts or updates a match row.  Used by the projection worker.
func (r *MatchesRepository) UpsertMatch(ctx context.Context, m MatchUpsert) error {
	const q = `
		INSERT INTO matches (
			id, account_id, event_id, event_name, timestamp, duration_seconds,
			player_wins, opponent_wins, player_team_id, deck_id, rank_before, rank_after,
			format, result, result_reason, opponent_name, opponent_id
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)
		ON CONFLICT (id) DO UPDATE
			SET event_name       = EXCLUDED.event_name,
			    timestamp        = EXCLUDED.timestamp,
			    duration_seconds = EXCLUDED.duration_seconds,
			    player_wins      = EXCLUDED.player_wins,
			    opponent_wins    = EXCLUDED.opponent_wins,
			    deck_id          = EXCLUDED.deck_id,
			    rank_before      = EXCLUDED.rank_before,
			    rank_after       = EXCLUDED.rank_after,
			    format           = EXCLUDED.format,
			    result           = EXCLUDED.result,
			    result_reason    = EXCLUDED.result_reason,
			    opponent_name    = EXCLUDED.opponent_name,
			    opponent_id      = EXCLUDED.opponent_id`

	_, err := r.db.ExecContext(
		ctx, q,
		m.ID, m.AccountID, m.EventID, m.EventName, m.Timestamp, m.DurationSeconds,
		m.PlayerWins, m.OpponentWins, m.PlayerTeamID, m.DeckID, m.RankBefore, m.RankAfter,
		m.Format, m.Result, m.ResultReason, m.OpponentName, m.OpponentID,
	)
	return err
}

// MatchUpsert holds the fields needed to write a match row from the projection worker.
type MatchUpsert struct {
	ID              string
	AccountID       int64
	EventID         string
	EventName       string
	Timestamp       time.Time
	DurationSeconds *int
	PlayerWins      int
	OpponentWins    int
	PlayerTeamID    int
	DeckID          *string
	RankBefore      *string
	RankAfter       *string
	Format          string
	Result          string
	ResultReason    *string
	OpponentName    *string
	OpponentID      *string
}

// MatchFilter captures every filterable dimension the Phase 2 /api/v1/matches
// endpoint supports. Zero-valued fields are treated as "no filter on this
// dimension" so callers can pass a partially-populated struct.
type MatchFilter struct {
	StartDate *time.Time
	EndDate   *time.Time
	Format    string
	Formats   []string
	DeckID    string
	Result    string // "win" | "loss" | "draw"
	Page      int
	Limit     int
}

// ListByAccountIDFiltered returns a page of matches scoped to accountID,
// filtered by the non-zero fields of f, ordered by timestamp DESC.  Returns
// the page rows and a total count for pagination.
func (r *MatchesRepository) ListByAccountIDFiltered(ctx context.Context, accountID int64, f MatchFilter) ([]MatchRow, int, error) {
	where, args := buildMatchWhere(accountID, f)
	offset := (f.Page - 1) * f.Limit
	args = append(args, f.Limit, offset)

	q := `SELECT id, format, result, timestamp, duration_seconds, deck_id,
	             rank_before, rank_after, player_wins, opponent_wins
	      FROM matches ` + where + `
	      ORDER BY timestamp DESC
	      LIMIT $` + itoa(len(args)-1) + ` OFFSET $` + itoa(len(args))

	rows, err := r.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var matches []MatchRow
	for rows.Next() {
		var m MatchRow
		if err := rows.Scan(
			&m.ID, &m.Format, &m.Result, &m.Timestamp,
			&m.DurationSeconds, &m.DeckID, &m.RankBefore, &m.RankAfter,
			&m.PlayerWins, &m.OpponentWins,
		); err != nil {
			return nil, 0, err
		}
		matches = append(matches, m)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	// Drop pagination args for the count query.
	countWhere, countArgs := buildMatchWhere(accountID, f)
	countQ := "SELECT COUNT(*) FROM matches " + countWhere
	var total int
	if err := r.db.QueryRowContext(ctx, countQ, countArgs...).Scan(&total); err != nil {
		return nil, 0, err
	}
	return matches, total, nil
}

// GetByID returns a single match row scoped to accountID, or nil when the
// row does not exist or belongs to a different account. The "scoped to
// accountID" check is the security boundary — never trust matchID alone.
func (r *MatchesRepository) GetByID(ctx context.Context, accountID int64, matchID string) (*MatchRow, error) {
	const q = `SELECT id, format, result, timestamp, duration_seconds, deck_id,
	                  rank_before, rank_after, player_wins, opponent_wins
	           FROM matches
	           WHERE account_id = $1 AND id = $2`
	row := r.db.QueryRowContext(ctx, q, accountID, matchID)
	var m MatchRow
	err := row.Scan(
		&m.ID, &m.Format, &m.Result, &m.Timestamp,
		&m.DurationSeconds, &m.DeckID, &m.RankBefore, &m.RankAfter,
		&m.PlayerWins, &m.OpponentWins,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// DistinctFormats returns every distinct format the account has matches in,
// sorted alphabetically. Used by the SPA's format-filter dropdown.
func (r *MatchesRepository) DistinctFormats(ctx context.Context, accountID int64) ([]string, error) {
	const q = `SELECT DISTINCT format
	           FROM matches
	           WHERE account_id = $1 AND format <> ''
	           ORDER BY format`
	rows, err := r.db.QueryContext(ctx, q, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var f string
		if err := rows.Scan(&f); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// buildMatchWhere assembles the WHERE clause + args for ListByAccountIDFiltered
// and the matching count query.  Returns "WHERE ..." with $1..$N placeholders
// in the same order as the args slice.
func buildMatchWhere(accountID int64, f MatchFilter) (string, []any) {
	clauses := []string{"account_id = $1"}
	args := []any{accountID}
	next := 2

	if f.StartDate != nil {
		clauses = append(clauses, "timestamp >= $"+itoa(next))
		args = append(args, *f.StartDate)
		next++
	}
	if f.EndDate != nil {
		clauses = append(clauses, "timestamp <= $"+itoa(next))
		args = append(args, *f.EndDate)
		next++
	}
	switch {
	case f.Format != "" && len(f.Formats) > 0:
		clauses = append(clauses, "(lower(format) = lower($"+itoa(next)+") OR lower(format) = ANY($"+itoa(next+1)+"))")
		args = append(args, f.Format, lowerSlice(f.Formats))
		next += 2
	case f.Format != "":
		clauses = append(clauses, "lower(format) = lower($"+itoa(next)+")")
		args = append(args, f.Format)
		next++
	case len(f.Formats) > 0:
		clauses = append(clauses, "lower(format) = ANY($"+itoa(next)+")")
		args = append(args, lowerSlice(f.Formats))
		next++
	}
	if f.DeckID != "" {
		clauses = append(clauses, "deck_id = $"+itoa(next))
		args = append(args, f.DeckID)
		next++
	}
	if f.Result != "" {
		clauses = append(clauses, "lower(result) = lower($"+itoa(next)+")")
		args = append(args, f.Result)
		next++
	}

	return "WHERE " + strings.Join(clauses, " AND "), args
}

func lowerSlice(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		out = append(out, strings.ToLower(s))
	}
	return out
}

// itoa is a small int-to-string helper used inside SQL builders; preferred
// over strconv.Itoa here to avoid an extra import and to keep the SQL
// concatenation readable.
func itoa(i int) string { return strconv.Itoa(i) }
