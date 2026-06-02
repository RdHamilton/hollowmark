-- Migration 000101 rollback: drop per-turn columns added by the up migration.
--
-- On a DB that had the per-turn schema before this migration the columns were
-- pre-existing; dropping them here would be destructive. Those DBs ran
-- migrations 000030 / 000044 / 000054 which own those columns, so a DOWN on
-- this migration should leave them intact.
--
-- The safe rollback is therefore: drop the partial unique index (it was created
-- by this migration) and drop only the columns that this migration added on
-- per-game-schema DBs. On per-turn-schema DBs the IF EXISTS guard prevents
-- errors but does nothing harmful.
--
-- NOTE: rolling back this migration on a per-turn-schema DB is a no-op for the
-- column drops (columns pre-existed and are owned by 000030/000044/000054).

DROP INDEX IF EXISTS idx_game_plays_unique;

-- Restore account_id NOT NULL constraint.  Only meaningful on per-game-schema
-- DBs; on per-turn-schema DBs this may fail if any NULL account_id rows exist
-- (none should — per-turn rows get '' from the DEFAULT).
ALTER TABLE game_plays ALTER COLUMN account_id SET NOT NULL;

-- Columns added by this migration on per-game-schema DBs only.
-- On per-turn-schema DBs the IF EXISTS guard is the protection; these columns
-- are owned by earlier migrations and restoring them is handled by rolling back
-- those migrations, not this one. A best-effort approach is used here.
ALTER TABLE game_plays DROP COLUMN IF EXISTS sequence_number;
ALTER TABLE game_plays DROP COLUMN IF EXISTS timestamp;
ALTER TABLE game_plays DROP COLUMN IF EXISTS life_to;
ALTER TABLE game_plays DROP COLUMN IF EXISTS life_from;
ALTER TABLE game_plays DROP COLUMN IF EXISTS zone_to;
ALTER TABLE game_plays DROP COLUMN IF EXISTS zone_from;
ALTER TABLE game_plays DROP COLUMN IF EXISTS card_name;
ALTER TABLE game_plays DROP COLUMN IF EXISTS card_id;
ALTER TABLE game_plays DROP COLUMN IF EXISTS action_type;
ALTER TABLE game_plays DROP COLUMN IF EXISTS player_type;
ALTER TABLE game_plays DROP COLUMN IF EXISTS step;
ALTER TABLE game_plays DROP COLUMN IF EXISTS phase;
ALTER TABLE game_plays DROP COLUMN IF EXISTS turn_number;
ALTER TABLE game_plays DROP COLUMN IF EXISTS game_id;
