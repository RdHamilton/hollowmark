-- Migration 000100: split game_plays per-turn from per-game results (ADR-050).
--
-- BACKGROUND: migration 000073 attempted to re-purpose game_plays as a per-game
-- result table, but used CREATE TABLE IF NOT EXISTS — a no-op because the table
-- already existed with the per-turn schema (000030/000054). As a result
-- game_play_repo.go has been crashing with SQLSTATE 42703 (undefined column)
-- on every match.game_ended projection event. Additionally the turn-by-turn
-- writer was never implemented.
--
-- PRE-CONDITION: life_change_tracking and game_event_counters must both have
-- 0 rows before this migration is applied. The FK drop in steps 2 and 3
-- assumes there are no referencing rows. Verify with:
--   SELECT COUNT(*) FROM life_change_tracking;
--   SELECT COUNT(*) FROM game_event_counters;
-- If either returns > 0, STOP and escalate to Ray.
--
-- SAFETY: game_plays is NOT touched. No DROP, no ALTER on game_plays.

-- Step 1: create the new per-game result table.
CREATE TABLE match_game_results (
    id              BIGSERIAL    PRIMARY KEY,
    account_id      BIGINT       NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    match_id        TEXT         NOT NULL,
    game_number     INT          NOT NULL,
    winning_team_id INT          NOT NULL DEFAULT 0,
    turn_count      INT          NOT NULL DEFAULT 0,
    duration_secs   INT          NOT NULL DEFAULT 0,
    sequence        BIGINT       NOT NULL DEFAULT 0,
    occurred_at     TIMESTAMPTZ  NOT NULL,
    partial         BOOLEAN      NOT NULL DEFAULT FALSE,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_match_game_results_account_match_game
        UNIQUE (account_id, match_id, game_number)
);

CREATE INDEX idx_match_game_results_account_match
    ON match_game_results (account_id, match_id);

-- Step 2: reroute life_change_tracking FK from game_plays(id) → match_game_results(id).
ALTER TABLE life_change_tracking
    DROP CONSTRAINT IF EXISTS uq_life_change_tracking_game_play_team_turn;
ALTER TABLE life_change_tracking
    DROP COLUMN game_play_id;
ALTER TABLE life_change_tracking
    ADD COLUMN match_game_result_id BIGINT NOT NULL REFERENCES match_game_results(id) ON DELETE CASCADE;
ALTER TABLE life_change_tracking
    ADD CONSTRAINT uq_life_change_tracking_result_team_turn
        UNIQUE (match_game_result_id, team_id, turn_number);

DROP INDEX IF EXISTS idx_life_change_tracking_game_play;
CREATE INDEX idx_life_change_tracking_match_game_result
    ON life_change_tracking (match_game_result_id);

-- Step 3: reroute game_event_counters FK from game_plays(id) → match_game_results(id).
ALTER TABLE game_event_counters
    DROP CONSTRAINT IF EXISTS uq_game_event_counters_play_instance_type_turn;
ALTER TABLE game_event_counters
    DROP COLUMN game_play_id;
ALTER TABLE game_event_counters
    ADD COLUMN match_game_result_id BIGINT NOT NULL REFERENCES match_game_results(id) ON DELETE CASCADE;
ALTER TABLE game_event_counters
    ADD CONSTRAINT uq_game_event_counters_result_instance_type_turn
        UNIQUE (match_game_result_id, instance_id, counter_type, turn_number);

DROP INDEX IF EXISTS idx_game_event_counters_game_play_id;
CREATE INDEX idx_game_event_counters_match_game_result
    ON game_event_counters (match_game_result_id, turn_number);

-- NOTE: game_plays is NOT touched. No DROP, no ALTER.
