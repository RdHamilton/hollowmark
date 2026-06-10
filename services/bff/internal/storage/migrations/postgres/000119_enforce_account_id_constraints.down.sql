-- Migration 000119 down: drop named FK constraints and drop NOT NULL on five tables.
--
-- draft_sessions: drops NOT NULL only.  The FK from migration 000052 (anonymous
-- name, part of the incremental chain) is NOT touched — this down migration only
-- reverts what 000119.up added.  The fresh-install path (000054) inline anonymous
-- FKs are similarly NOT touched; those are part of the consolidated CREATE TABLE,
-- not of this migration.
--
-- Rolling back this migration on a fresh-install DB leaves the inline anonymous
-- FKs intact, which is the correct pre-migration state for that path.

ALTER TABLE matches             DROP CONSTRAINT IF EXISTS fk_matches_account_id;
ALTER TABLE matches             ALTER COLUMN account_id DROP NOT NULL;

ALTER TABLE player_stats        DROP CONSTRAINT IF EXISTS fk_player_stats_account_id;
ALTER TABLE player_stats        ALTER COLUMN account_id DROP NOT NULL;

ALTER TABLE rank_history        DROP CONSTRAINT IF EXISTS fk_rank_history_account_id;
ALTER TABLE rank_history        ALTER COLUMN account_id DROP NOT NULL;

ALTER TABLE collection_history  DROP CONSTRAINT IF EXISTS fk_collection_history_account_id;
ALTER TABLE collection_history  ALTER COLUMN account_id DROP NOT NULL;

-- draft_sessions: drop NOT NULL only; FK from migration 000052 is not touched.
ALTER TABLE draft_sessions      ALTER COLUMN account_id DROP NOT NULL;
