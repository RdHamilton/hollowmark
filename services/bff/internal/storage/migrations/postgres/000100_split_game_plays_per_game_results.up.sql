-- Migration 000100: split game_plays per-turn from per-game results (ADR-050).
--
-- BACKGROUND: migration 000073 attempted to re-purpose game_plays as a per-game
-- result table, but used CREATE TABLE IF NOT EXISTS — a no-op because the table
-- already existed with the per-turn schema (000030/000054). As a result
-- game_play_repo.go was crashing with SQLSTATE 42703 (undefined column)
-- on every match.game_ended projection event. Additionally the turn-by-turn
-- writer was never implemented.
--
-- PROD-SAFETY HARDENING (v0.3.7 gate, ticket #659):
-- The original 000100 had two failure modes that manifested on staging and
-- threatened the prod deploy:
--
--   1. MISSING TABLE: life_change_tracking and/or game_event_counters may be
--      absent on some DBs (confirmed on staging — the staging BFF deploy failed
--      with "relation does not exist"). Each CREATE TABLE IF NOT EXISTS below
--      recreates the table in its old-schema shape if missing, making all four
--      subsequent ALTERs safe.
--
--   2. ROWS PRESENT: ADD COLUMN ... NOT NULL without a DEFAULT fails if the
--      table has any rows. Existing rows (if any) in life_change_tracking and
--      game_event_counters carry game_play_id FKs that do NOT map to
--      match_game_results — they are orphaned data from a per-turn writer that
--      never successfully executed (ADR-050 §3). TRUNCATE removes them before
--      the column reroute. This is safe and correct: retaining stale rows with
--      broken FKs provides zero value and would violate the new FK constraint.
--
-- END-STATE INVARIANT: this migration produces the identical schema whether
-- applied to (a) a fresh DB, (b) old-schema + 0 rows, (c) old-schema + rows,
-- or (d) a DB where life_change_tracking / game_event_counters are missing.
-- Staging is already at v100 — golang-migrate is version-based, not
-- content-checksum, so this edit only executes on DBs still at v99 (prod +
-- fresh). The end schema matches what staging now has.
--
-- SAFETY: game_plays is NOT touched. No DROP, no ALTER on game_plays.

-- -------------------------------------------------------------------------
-- GUARD: ensure life_change_tracking exists in its old-schema shape.
-- On a DB where the table is present this is a no-op. On a DB where it is
-- absent (staging scenario) this creates it so the subsequent ALTERs succeed.
-- -------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS life_change_tracking (
    id           BIGSERIAL    PRIMARY KEY,
    account_id   BIGINT       NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    game_play_id BIGINT       NOT NULL REFERENCES game_plays(id) ON DELETE CASCADE,
    team_id      INT          NOT NULL,
    life_total   INT          NOT NULL,
    delta        INT          NOT NULL,
    turn_number  INT          NOT NULL DEFAULT 0,
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_life_change_tracking_game_play_team_turn
        UNIQUE (game_play_id, team_id, turn_number)
);

-- -------------------------------------------------------------------------
-- GUARD: ensure game_event_counters exists in its old-schema shape.
-- Same rationale as above.
-- -------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS game_event_counters (
    id             BIGSERIAL    PRIMARY KEY,
    game_play_id   BIGINT       NOT NULL REFERENCES game_plays(id) ON DELETE CASCADE,
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

-- -------------------------------------------------------------------------
-- TRUNCATE: remove any existing rows before adding NOT NULL columns.
-- Rows in these tables reference game_play_id FKs that have no mapping to
-- match_game_results (the per-turn writer never successfully executed —
-- ADR-050 §3). They are orphaned garbage. Retaining them is not possible
-- (the NOT NULL FK reroute below would fail) and not desirable (stale data
-- with broken FKs). TRUNCATE CASCADE removes referencing rows in any child
-- tables too.
-- -------------------------------------------------------------------------
TRUNCATE TABLE life_change_tracking CASCADE;
TRUNCATE TABLE game_event_counters CASCADE;

-- -------------------------------------------------------------------------
-- Step 1: create the new per-game result table.
-- -------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS match_game_results (
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

CREATE INDEX IF NOT EXISTS idx_match_game_results_account_match
    ON match_game_results (account_id, match_id);

-- -------------------------------------------------------------------------
-- Step 2: reroute life_change_tracking FK from game_plays(id) → match_game_results(id).
-- -------------------------------------------------------------------------
ALTER TABLE life_change_tracking
    DROP CONSTRAINT IF EXISTS uq_life_change_tracking_game_play_team_turn;
ALTER TABLE life_change_tracking
    DROP COLUMN IF EXISTS game_play_id;
ALTER TABLE life_change_tracking
    ADD COLUMN IF NOT EXISTS match_game_result_id BIGINT NOT NULL REFERENCES match_game_results(id) ON DELETE CASCADE;
ALTER TABLE life_change_tracking
    ADD CONSTRAINT uq_life_change_tracking_result_team_turn
        UNIQUE (match_game_result_id, team_id, turn_number);

DROP INDEX IF EXISTS idx_life_change_tracking_game_play;
CREATE INDEX IF NOT EXISTS idx_life_change_tracking_match_game_result
    ON life_change_tracking (match_game_result_id);

-- -------------------------------------------------------------------------
-- Step 3: reroute game_event_counters FK from game_plays(id) → match_game_results(id).
-- -------------------------------------------------------------------------
ALTER TABLE game_event_counters
    DROP CONSTRAINT IF EXISTS uq_game_event_counters_play_instance_type_turn;
ALTER TABLE game_event_counters
    DROP COLUMN IF EXISTS game_play_id;
ALTER TABLE game_event_counters
    ADD COLUMN IF NOT EXISTS match_game_result_id BIGINT NOT NULL REFERENCES match_game_results(id) ON DELETE CASCADE;
ALTER TABLE game_event_counters
    ADD CONSTRAINT uq_game_event_counters_result_instance_type_turn
        UNIQUE (match_game_result_id, instance_id, counter_type, turn_number);

DROP INDEX IF EXISTS idx_game_event_counters_game_play_id;
CREATE INDEX IF NOT EXISTS idx_game_event_counters_match_game_result
    ON game_event_counters (match_game_result_id, turn_number);

-- NOTE: game_plays is NOT touched. No DROP, no ALTER.
