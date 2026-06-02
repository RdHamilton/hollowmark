-- Migration 000100: Recreate game_plays with the per-game-result schema.
--
-- Background: the original game_plays table (migration 000030 / consolidated
-- in 000054) stored turn-by-turn action records keyed by game_id FK → games.id.
-- The v0.3.7 BFF projection worker (game_play_repo.go) writes a completely
-- different shape: one row per completed game identified by
-- (account_id, match_id, game_number), with life-change rows in
-- life_change_tracking referencing game_plays.id.
--
-- Migration 000073 attempted to CREATE TABLE IF NOT EXISTS game_plays with the
-- new schema, but that statement was silently skipped on any DB that already
-- had game_plays from the original schema — causing every match.game_ended
-- projection to fail with "column game_number does not exist" (SQLSTATE 42703).
--
-- Fix: drop the old table (safe — no user rows exist on any environment; the
-- old action-log read path in gameplays_repo.go is superseded) and recreate
-- with the correct per-game-result schema that the projection worker and the
-- match-details API expect.
--
-- life_change_tracking already exists from migration 000073 and has the
-- correct schema. Its FK to game_plays.id will be re-established by this
-- migration.

-- Drop old game_plays and its obsolete dependants.
-- life_change_tracking is recreated below so it is dropped first.
DROP TABLE IF EXISTS life_change_tracking;
DROP TABLE IF EXISTS game_plays;

-- Recreate game_plays: one row per completed game within a match.
-- (account_id, match_id, game_number) is the natural unique key.
-- sequence carries the per-session monotonic counter from the DaemonEvent
-- envelope and is used by the projector to enforce causal ordering.
CREATE TABLE game_plays (
    id             BIGSERIAL    PRIMARY KEY,
    account_id     BIGINT       NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    match_id       TEXT         NOT NULL,
    game_number    INT          NOT NULL,
    winning_team_id INT         NOT NULL DEFAULT 0,
    turn_count     INT          NOT NULL DEFAULT 0,
    duration_secs  INT          NOT NULL DEFAULT 0,
    sequence       BIGINT       NOT NULL DEFAULT 0,
    partial        BOOLEAN      NOT NULL DEFAULT FALSE,
    occurred_at    TIMESTAMPTZ  NOT NULL,
    created_at     TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_game_plays_account_match_game
        UNIQUE (account_id, match_id, game_number)
);

CREATE INDEX idx_game_plays_account_match
    ON game_plays (account_id, match_id);

-- Recreate life_change_tracking with FK to the new game_plays.
CREATE TABLE life_change_tracking (
    id          BIGSERIAL    PRIMARY KEY,
    account_id  BIGINT       NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    game_play_id BIGINT      NOT NULL REFERENCES game_plays(id) ON DELETE CASCADE,
    team_id     INT          NOT NULL,
    life_total  INT          NOT NULL,
    delta       INT          NOT NULL,
    turn_number INT          NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_life_change_tracking_game_play_team_turn
        UNIQUE (game_play_id, team_id, turn_number)
);

CREATE INDEX idx_life_change_tracking_game_play
    ON life_change_tracking (game_play_id);
