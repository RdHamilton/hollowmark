package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/RdHamilton/vault-mtg/services/contract"
)

// GamePlayInsert holds the data needed to insert a single match_game_results row.
type GamePlayInsert struct {
	AccountID     int64
	MatchID       string
	GameNumber    int
	WinningTeamID int
	TurnCount     int
	DurationSecs  int
	Sequence      uint64
	OccurredAt    time.Time
	// Partial indicates the row was emitted before the game was confirmed
	// complete — the GRE buffer hit its flush threshold or the stale sweep
	// evicted it.  Maps to the partial column added in migration 000074.
	Partial bool
	// PlayerOnPlay is true when the local player went first in this game
	// (on the play), false when on the draw. Nil when the daemon could not
	// determine the starting player (stale-sweep partial, pre-#687 events).
	PlayerOnPlay *bool
}

// LifeChangeInsert holds one life-change row to be written to
// life_change_tracking.
type LifeChangeInsert struct {
	AccountID         int64
	MatchGameResultID int64
	TeamID            int
	LifeTotal         int
	Delta             int
	TurnNumber        int
}

// GamePlayRow is returned when reading a match_game_results row.
type GamePlayRow struct {
	ID            int64
	AccountID     int64
	MatchID       string
	GameNumber    int
	WinningTeamID int
	TurnCount     int
	DurationSecs  int
	Sequence      uint64
	OccurredAt    time.Time
	Partial       bool
	// PlayerOnPlay is nil for rows written before migration 000103 or when the
	// daemon could not determine the starting player.
	PlayerOnPlay *bool
}

// GamePlayRepository provides write and read access to match_game_results and
// life_change_tracking, always scoped by account_id.
//
// After ADR-050: match_game_results holds per-game results (one row per
// completed game within a match). game_plays remains the per-turn action log.
type GamePlayRepository struct {
	db DB
}

// NewGamePlayRepository returns a GamePlayRepository backed by db.
func NewGamePlayRepository(db DB) *GamePlayRepository {
	return &GamePlayRepository{db: db}
}

// InsertGamePlay inserts or updates a match_game_results row identified by
// (account_id, match_id, game_number) and returns the row's id.
//
// On conflict the row is updated only when the incoming sequence is strictly
// greater than the stored one, preserving causal ordering across out-of-order
// daemon retransmissions.
func (r *GamePlayRepository) InsertGamePlay(ctx context.Context, ins GamePlayInsert) (int64, error) {
	const q = `
		INSERT INTO match_game_results
			(account_id, match_id, game_number, winning_team_id, turn_count,
			 duration_secs, sequence, occurred_at, partial, player_on_play)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT ON CONSTRAINT uq_match_game_results_account_match_game
		DO UPDATE SET
			winning_team_id = EXCLUDED.winning_team_id,
			turn_count      = EXCLUDED.turn_count,
			duration_secs   = EXCLUDED.duration_secs,
			sequence        = EXCLUDED.sequence,
			occurred_at     = EXCLUDED.occurred_at,
			partial         = EXCLUDED.partial,
			player_on_play  = COALESCE(EXCLUDED.player_on_play, match_game_results.player_on_play)
		WHERE match_game_results.sequence < EXCLUDED.sequence
		RETURNING id`

	var id int64
	err := r.db.QueryRowContext(
		ctx, q,
		ins.AccountID,
		ins.MatchID,
		ins.GameNumber,
		ins.WinningTeamID,
		ins.TurnCount,
		ins.DurationSecs,
		ins.Sequence,
		ins.OccurredAt,
		ins.Partial,
		ins.PlayerOnPlay,
	).Scan(&id)

	if err == sql.ErrNoRows {
		// ON CONFLICT DO UPDATE WHERE clause was false (sequence not greater).
		// Fetch the existing id so callers can still insert life_changes.
		return r.getMatchGameResultID(ctx, ins.AccountID, ins.MatchID, ins.GameNumber)
	}

	return id, err
}

// getMatchGameResultID returns the id of an existing match_game_results row.
func (r *GamePlayRepository) getMatchGameResultID(ctx context.Context, accountID int64, matchID string, gameNumber int) (int64, error) {
	const q = `
		SELECT id FROM match_game_results
		WHERE account_id = $1 AND match_id = $2 AND game_number = $3`

	var id int64
	err := r.db.QueryRowContext(ctx, q, accountID, matchID, gameNumber).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("getMatchGameResultID: %w", err)
	}

	return id, nil
}

// InsertLifeChanges bulk-inserts life_change_tracking rows for a game.
// Each row is scoped by account_id and references match_game_result_id.
// Duplicate inserts (same match_game_result_id, team_id, turn_number) are
// silently ignored so replaying the same event is safe.
func (r *GamePlayRepository) InsertLifeChanges(ctx context.Context, changes []LifeChangeInsert) error {
	if len(changes) == 0 {
		return nil
	}

	const q = `
		INSERT INTO life_change_tracking
			(account_id, match_game_result_id, team_id, life_total, delta, turn_number)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (match_game_result_id, team_id, turn_number) DO NOTHING`

	for i := range changes {
		c := changes[i]
		if _, err := r.db.ExecContext(
			ctx, q,
			c.AccountID,
			c.MatchGameResultID,
			c.TeamID,
			c.LifeTotal,
			c.Delta,
			c.TurnNumber,
		); err != nil {
			return fmt.Errorf("InsertLifeChanges[%d]: %w", i, err)
		}
	}

	return nil
}

// InsertCardPlays bulk-inserts per-turn card play rows into game_plays.
// accountID is the owning account — written to game_plays.account_id for
// defense-in-depth multi-tenancy hygiene (AC1, ticket #820). The read path
// scopes via games → matches → account_id, so this column is NOT used by any
// current query; it is populated so the column carries the guarantee it implies.
// gameID is the games.id FK resolved from (match_id, game_number).
// matchID is carried on each row for the game_plays.match_id TEXT column.
// occurredAt is used as the per-row timestamp (per-play timestamps are not
// available in the current daemon payload shape).
// ON CONFLICT (game_id, sequence_number) DO NOTHING ensures idempotent replay.
func (r *GamePlayRepository) InsertCardPlays(ctx context.Context, accountID int64, gameID int64, matchID string, entries []contract.CardPlayEntry, occurredAt time.Time) error {
	if len(entries) == 0 {
		return nil
	}

	const q = `
		INSERT INTO game_plays
			(account_id, game_id, match_id, turn_number, phase, player_type, action_type,
			 card_id, card_name, zone_from, zone_to, timestamp, sequence_number)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NULL, $9, $10, $11, $12)
		ON CONFLICT (game_id, sequence_number) DO NOTHING`

	for i := range entries {
		e := entries[i]
		if _, err := r.db.ExecContext(
			ctx, q,
			accountID,
			gameID,
			matchID,
			e.TurnNumber,
			e.Phase,
			e.PlayerType,
			e.ActionType,
			e.ArenaID,
			e.ZoneFrom,
			e.ZoneTo,
			occurredAt,
			i, // sequence_number = index within slice
		); err != nil {
			return fmt.Errorf("InsertCardPlays[%d]: %w", i, err)
		}
	}

	return nil
}

// GetGamePlay returns a single match_game_results row by (account_id, match_id, game_number).
// Partial rows (partial = true) are excluded — they represent incomplete GRE
// events and must not surface as readable game records.
// Returns sql.ErrNoRows when no non-partial row exists.
func (r *GamePlayRepository) GetGamePlay(ctx context.Context, accountID int64, matchID string, gameNumber int) (GamePlayRow, error) {
	const q = `
		SELECT id, account_id, match_id, game_number, winning_team_id,
		       turn_count, duration_secs, sequence, occurred_at, partial, player_on_play
		FROM match_game_results
		WHERE account_id = $1 AND match_id = $2 AND game_number = $3 AND partial = false`

	var row GamePlayRow
	err := r.db.QueryRowContext(ctx, q, accountID, matchID, gameNumber).Scan(
		&row.ID,
		&row.AccountID,
		&row.MatchID,
		&row.GameNumber,
		&row.WinningTeamID,
		&row.TurnCount,
		&row.DurationSecs,
		&row.Sequence,
		&row.OccurredAt,
		&row.Partial,
		&row.PlayerOnPlay,
	)

	return row, err
}

// ListGamePlaysByMatch returns all non-partial match_game_results rows for a match
// ordered by (occurred_at, sequence) — the canonical per-session ordering
// defined in the projection layer v2 spec.
// Partial rows (partial = true) are excluded — they represent incomplete GRE
// events and must not pollute the per-match game list.
func (r *GamePlayRepository) ListGamePlaysByMatch(ctx context.Context, accountID int64, matchID string) ([]GamePlayRow, error) {
	const q = `
		SELECT id, account_id, match_id, game_number, winning_team_id,
		       turn_count, duration_secs, sequence, occurred_at, partial, player_on_play
		FROM match_game_results
		WHERE account_id = $1 AND match_id = $2 AND partial = false
		ORDER BY occurred_at, sequence`

	rows, err := r.db.QueryContext(ctx, q, accountID, matchID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []GamePlayRow
	for rows.Next() {
		var row GamePlayRow
		if err := rows.Scan(
			&row.ID,
			&row.AccountID,
			&row.MatchID,
			&row.GameNumber,
			&row.WinningTeamID,
			&row.TurnCount,
			&row.DurationSecs,
			&row.Sequence,
			&row.OccurredAt,
			&row.Partial,
			&row.PlayerOnPlay,
		); err != nil {
			return nil, err
		}
		out = append(out, row)
	}

	return out, rows.Err()
}

// CountLifeChangesByGame returns the number of life_change_tracking rows for
// the given match_game_result_id.  Used in integration tests.
func (r *GamePlayRepository) CountLifeChangesByGame(ctx context.Context, matchGameResultID int64) (int, error) {
	const q = `SELECT COUNT(*) FROM life_change_tracking WHERE match_game_result_id = $1`

	var n int
	err := r.db.QueryRowContext(ctx, q, matchGameResultID).Scan(&n)

	return n, err
}

// CountCardPlaysByGame returns the number of game_plays rows for the given
// game_id.  Used in integration tests.
func (r *GamePlayRepository) CountCardPlaysByGame(ctx context.Context, gameID int64) (int, error) {
	const q = `SELECT COUNT(*) FROM game_plays WHERE game_id = $1`

	var n int
	err := r.db.QueryRowContext(ctx, q, gameID).Scan(&n)

	return n, err
}

// UpsertGameRow inserts a games row for (match_id, game_number) and returns
// the row's id. ON CONFLICT (match_id, game_number) DO NOTHING — subsequent
// projections of the same (match_id, game_number) pair are idempotent and
// return the existing id.
//
// The games table is the legacy per-game anchor that game_plays.game_id
// references as a foreign key. It must exist before InsertCardPlays can write
// per-turn rows. The projection worker creates this row during match.game_ended
// projection, immediately after writing the match_game_results row.
//
// result is stored as "win" by default (placeholder — the legacy games table
// predates the match_game_results split and is only used as an FK source by
// the per-turn game_plays rows). The column carries a NOT NULL constraint so
// we supply a value even though the real result is derived from matches.result.
func (r *GamePlayRepository) UpsertGameRow(ctx context.Context, matchID string, gameNumber int) (int64, error) {
	const q = `
		INSERT INTO games (match_id, game_number, result)
		VALUES ($1, $2, 'win')
		ON CONFLICT (match_id, game_number) DO UPDATE SET result = games.result
		RETURNING id`

	var id int64
	err := r.db.QueryRowContext(ctx, q, matchID, gameNumber).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("UpsertGameRow match_id=%q game_number=%d: %w", matchID, gameNumber, err)
	}

	return id, nil
}

// GameIDByMatchAndNumber resolves games.id for the given (match_id, game_number)
// pair. Used by the projection worker to obtain the FK required before writing
// per-turn card plays to game_plays.
//
// Returns sql.ErrNoRows when no games row exists yet for the pair — this is
// expected when match.game_ended arrives before match.completed is projected.
// The caller must treat sql.ErrNoRows as a non-fatal skip condition.
func (r *GamePlayRepository) GameIDByMatchAndNumber(ctx context.Context, matchID string, gameNumber int) (int64, error) {
	const q = `SELECT id FROM games WHERE match_id = $1 AND game_number = $2 LIMIT 1`

	var id int64
	err := r.db.QueryRowContext(ctx, q, matchID, gameNumber).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("GameIDByMatchAndNumber match_id=%q game_number=%d: %w", matchID, gameNumber, err)
	}

	return id, nil
}
