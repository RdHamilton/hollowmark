package repository

import (
	"context"
	"fmt"
)

// GameEventCounterInsert holds the data for a single game_event_counters row.
type GameEventCounterInsert struct {
	MatchGameResultID int64
	AccountID         int64
	InstanceID        int
	ArenaID           int
	CounterType       string
	Count             int
	Delta             int
	Controller        string
	TurnNumber        int
}

// GameEventCountersRepository provides write access to game_event_counters.
type GameEventCountersRepository struct {
	db DB
}

// NewGameEventCountersRepository returns a GameEventCountersRepository backed by db.
func NewGameEventCountersRepository(db DB) *GameEventCountersRepository {
	return &GameEventCountersRepository{db: db}
}

// InsertCounters bulk-inserts game_event_counters rows.
// ON CONFLICT (match_game_result_id, instance_id, counter_type, turn_number) DO NOTHING
// ensures idempotent replay of the same daemon_events row.
// Each row is account-scoped (account_id).
func (r *GameEventCountersRepository) InsertCounters(ctx context.Context, inserts []GameEventCounterInsert) error {
	if len(inserts) == 0 {
		return nil
	}

	const q = `
		INSERT INTO game_event_counters
			(match_game_result_id, account_id, instance_id, arena_id, counter_type,
			 count, delta, controller, turn_number)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (match_game_result_id, instance_id, counter_type, turn_number) DO NOTHING`

	for i := range inserts {
		ins := inserts[i]
		if _, err := r.db.ExecContext(
			ctx, q,
			ins.MatchGameResultID,
			ins.AccountID,
			ins.InstanceID,
			ins.ArenaID,
			ins.CounterType,
			ins.Count,
			ins.Delta,
			ins.Controller,
			ins.TurnNumber,
		); err != nil {
			return fmt.Errorf("InsertCounters[%d]: %w", i, err)
		}
	}

	return nil
}

// CountByMatchGameResult returns the number of game_event_counters rows for the
// given match_game_result_id.  Used in integration tests.
func (r *GameEventCountersRepository) CountByMatchGameResult(ctx context.Context, matchGameResultID int64) (int, error) {
	const q = `SELECT COUNT(*) FROM game_event_counters WHERE match_game_result_id = $1`

	var n int
	err := r.db.QueryRowContext(ctx, q, matchGameResultID).Scan(&n)

	return n, err
}
