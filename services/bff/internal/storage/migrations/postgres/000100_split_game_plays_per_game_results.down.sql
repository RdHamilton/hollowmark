-- Down migration 000100: restore life_change_tracking and game_event_counters
-- to reference game_plays(id), and drop match_game_results.
--
-- NOTE: this down migration is for staging rollback only. Never run in
-- production without a full wave revert. It assumes 0 rows in all affected
-- tables (same invariant as the up migration).

-- Restore game_event_counters FK to game_plays.
ALTER TABLE game_event_counters
    DROP CONSTRAINT IF EXISTS uq_game_event_counters_result_instance_type_turn;
ALTER TABLE game_event_counters
    DROP COLUMN IF EXISTS match_game_result_id;
ALTER TABLE game_event_counters
    ADD COLUMN game_play_id BIGINT NOT NULL REFERENCES game_plays(id) ON DELETE CASCADE;
ALTER TABLE game_event_counters
    ADD CONSTRAINT uq_game_event_counters_play_instance_type_turn
        UNIQUE (game_play_id, instance_id, counter_type, turn_number);

DROP INDEX IF EXISTS idx_game_event_counters_match_game_result;
CREATE INDEX IF NOT EXISTS idx_game_event_counters_game_play_id
    ON game_event_counters (game_play_id, turn_number);

-- Restore life_change_tracking FK to game_plays.
ALTER TABLE life_change_tracking
    DROP CONSTRAINT IF EXISTS uq_life_change_tracking_result_team_turn;
ALTER TABLE life_change_tracking
    DROP COLUMN IF EXISTS match_game_result_id;
ALTER TABLE life_change_tracking
    ADD COLUMN game_play_id BIGINT NOT NULL REFERENCES game_plays(id) ON DELETE CASCADE;
ALTER TABLE life_change_tracking
    ADD CONSTRAINT uq_life_change_tracking_game_play_team_turn
        UNIQUE (game_play_id, team_id, turn_number);

DROP INDEX IF EXISTS idx_life_change_tracking_match_game_result;
CREATE INDEX IF NOT EXISTS idx_life_change_tracking_game_play
    ON life_change_tracking (game_play_id);

-- Drop the new per-game table.
DROP TABLE IF EXISTS match_game_results CASCADE;
