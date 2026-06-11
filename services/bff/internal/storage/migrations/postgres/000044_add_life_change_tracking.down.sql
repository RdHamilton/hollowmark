-- Rollback: Remove life change tracking from game_plays
--
-- On the incremental migration path (running full DOWN from HEAD):
--   000073.down (which runs BEFORE this migration descending) drops the
--   game_plays table it created.  However, 000073.up used CREATE TABLE IF
--   NOT EXISTS, so on the incremental path the original game_plays (from
--   000030.up) is the live table and 000073.down is the one that drops it.
--   By the time 000044.down runs, game_plays is already gone.
--   IF EXISTS guards make this down idempotent under that ordering.
ALTER TABLE IF EXISTS game_plays DROP COLUMN IF EXISTS life_to;
ALTER TABLE IF EXISTS game_plays DROP COLUMN IF EXISTS life_from;
