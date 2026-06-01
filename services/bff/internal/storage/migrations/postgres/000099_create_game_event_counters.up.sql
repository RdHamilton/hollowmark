-- Migration 000099: create game_event_counters table (ADR-046 A2.1, vmt-t#613).
--
-- Stores per-permanent counter mutations extracted from GRE game state diffs.
-- The daemon collects CounterChangeEntry records in GamePlayPayload.counter_changes
-- and the BFF projection worker INSERTs them here synchronously on ingest.
--
-- game_play_id is a FK to game_plays.id (ON DELETE CASCADE so counter rows
-- are removed when the parent game row is deleted).
-- account_id is a FK to accounts.id matching the game_plays pattern.
--
-- Ray addendum: UNIQUE(game_play_id, instance_id, counter_type, turn_number)
-- enforced at the DDL level so the projection INSERT can use ON CONFLICT DO NOTHING
-- for idempotent replay.
--
-- Acceptance: vmt-t#613

CREATE TABLE IF NOT EXISTS game_event_counters (
    id             BIGSERIAL    PRIMARY KEY,
    game_play_id   BIGINT       NOT NULL
                   REFERENCES game_plays(id) ON DELETE CASCADE,
    account_id     BIGINT       NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    instance_id    INT          NOT NULL,
    arena_id       INT          NOT NULL,
    counter_type   TEXT         NOT NULL,
    count          INT          NOT NULL,
    delta          INT          NOT NULL,
    controller     TEXT         NOT NULL,
    turn_number    INT          NOT NULL,
    created_at     TIMESTAMPTZ  NOT NULL DEFAULT NOW(),

    CONSTRAINT uq_game_event_counters_play_instance_type_turn
        UNIQUE (game_play_id, instance_id, counter_type, turn_number)
);

-- Primary access pattern: all counter rows for a game play, ordered by turn.
CREATE INDEX IF NOT EXISTS idx_game_event_counters_game_play_id
    ON game_event_counters (game_play_id, turn_number);

-- Account-scoped audit / back-fill queries.
CREATE INDEX IF NOT EXISTS idx_game_event_counters_account_id
    ON game_event_counters (account_id);
