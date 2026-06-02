-- Migration 000101: ensure per-turn game_plays columns exist (ADR-050, ticket #659).
--
-- BACKGROUND: migration 000073 used CREATE TABLE IF NOT EXISTS game_plays with a
-- per-game schema (account_id BIGINT, game_number INT, ...). On databases where
-- game_plays did not yet exist (e.g. staging rebuilt after 000073 was written),
-- 000073 was NOT a no-op — it created the table with the per-game schema instead
-- of the per-turn schema from 000030/000054.
--
-- The per-turn read path in gameplays_repo.go SELECTs columns that exist only in
-- the per-turn schema (game_id, turn_number, phase, player_type, etc.). On a DB
-- where game_plays has the per-game schema those columns are absent, causing
-- SQLSTATE 42703 (undefined column) on every GET .../plays/timeline request.
--
-- FIX: ADD COLUMN IF NOT EXISTS for every per-turn column the read path requires.
-- On a DB that already has the per-turn schema (000030/000054 + 000044), every
-- statement is a no-op. On a DB with the per-game schema (staging scenario), the
-- columns are added as nullable (NULL default) so the ALTER does not fail on
-- existing per-game rows. The write path (InsertCardPlays) only inserts rows where
-- game_id IS NOT NULL, so per-game rows are inert.
--
-- UNIQUE INDEX: ensures ON CONFLICT (game_id, sequence_number) DO NOTHING in
-- InsertCardPlays has a backing index. On DBs that already have idx_game_plays_unique
-- from 000047/000054, CREATE UNIQUE INDEX IF NOT EXISTS is a no-op.
--
-- Ref: ADR-050, ticket #659, gameplays_repo.go PlaysByMatch fix.

-- Core FK column — distinguishes per-turn rows (NOT NULL) from legacy per-game rows (NULL).
ALTER TABLE game_plays ADD COLUMN IF NOT EXISTS game_id BIGINT;

-- Per-turn action columns.
ALTER TABLE game_plays ADD COLUMN IF NOT EXISTS turn_number   INTEGER;
ALTER TABLE game_plays ADD COLUMN IF NOT EXISTS phase         TEXT;
ALTER TABLE game_plays ADD COLUMN IF NOT EXISTS step          TEXT;
ALTER TABLE game_plays ADD COLUMN IF NOT EXISTS player_type   TEXT;
ALTER TABLE game_plays ADD COLUMN IF NOT EXISTS action_type   TEXT;
ALTER TABLE game_plays ADD COLUMN IF NOT EXISTS card_id       INTEGER;
ALTER TABLE game_plays ADD COLUMN IF NOT EXISTS card_name     TEXT;
ALTER TABLE game_plays ADD COLUMN IF NOT EXISTS zone_from     TEXT;
ALTER TABLE game_plays ADD COLUMN IF NOT EXISTS zone_to       TEXT;
ALTER TABLE game_plays ADD COLUMN IF NOT EXISTS life_from     INTEGER;
ALTER TABLE game_plays ADD COLUMN IF NOT EXISTS life_to       INTEGER;
ALTER TABLE game_plays ADD COLUMN IF NOT EXISTS timestamp     TIMESTAMPTZ;
ALTER TABLE game_plays ADD COLUMN IF NOT EXISTS sequence_number INTEGER;

-- account_id may be BIGINT NOT NULL on per-game-schema DBs (000073 schema).
-- InsertCardPlays does not supply account_id (scoping flows through games →
-- matches → account_id). Allow NULL so per-turn INSERTs succeed on both
-- schema variants. On per-turn-schema DBs account_id is TEXT NOT NULL DEFAULT ''
-- and already accepts INSERTs without an explicit value; this ALTER changes
-- the constraint on the BIGINT variant to be nullable.
ALTER TABLE game_plays ALTER COLUMN account_id DROP NOT NULL;

-- Partial unique index used by InsertCardPlays ON CONFLICT clause.
-- The WHERE predicate scopes it to per-turn rows only (game_id IS NOT NULL),
-- excluding legacy per-game rows that have game_id = NULL after this migration.
-- On per-turn-schema DBs that already have idx_game_plays_unique from migration
-- 000047/000054 (full index, no WHERE), this CREATE INDEX IF NOT EXISTS is a
-- no-op by name — the full index already covers the ON CONFLICT.
CREATE UNIQUE INDEX IF NOT EXISTS idx_game_plays_unique
    ON game_plays (game_id, sequence_number)
    WHERE game_id IS NOT NULL AND sequence_number IS NOT NULL;
