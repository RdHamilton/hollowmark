-- Reverse: remove account_id from telemetry tables
--
-- On the incremental migration path (running full DOWN from HEAD):
--   000073.down (which runs BEFORE this migration descending) drops the
--   game_plays table it created.  However, 000073.up used CREATE TABLE IF
--   NOT EXISTS, so on the incremental path the original game_plays (from
--   000030.up) is the live table and 000073.down is the one that drops it.
--   By the time 000068.down runs, game_plays is already gone.
--
--   inventory, inventory_history, and quests are still present at this point:
--   their droppers are 000023.down and 000010.down, both numbered below 68
--   and therefore running AFTER this migration (descending order).  The
--   IF EXISTS guards are hygiene for re-runability, not required for the
--   current schema.

-- game_plays (may already be absent — see note above)
DROP INDEX IF EXISTS idx_game_plays_account_id;
ALTER TABLE IF EXISTS game_plays DROP COLUMN IF EXISTS account_id;

-- inventory_history
DROP INDEX IF EXISTS idx_inventory_history_account_id;
ALTER TABLE IF EXISTS inventory_history DROP COLUMN IF EXISTS account_id;

-- inventory
DROP INDEX IF EXISTS idx_inventory_account_id;
ALTER TABLE IF EXISTS inventory DROP COLUMN IF EXISTS account_id;

-- quests
DROP INDEX IF EXISTS idx_quests_account_id;
ALTER TABLE IF EXISTS quests DROP COLUMN IF EXISTS account_id;
