-- CASCADE guards against incomplete later downs and dirty states, not against
-- clean sequential downs (on a correct sequential down, dependents are already
-- gone before this migration runs). Specifically:
--   game_plays: 000073.up used CREATE TABLE IF NOT EXISTS, so on the incremental
--   migration path (000030-created table) it was a no-op. 000073.down therefore
--   drops the original 000030-created game_plays (and its life_change_tracking
--   dependent created by this same migration). The CASCADE here guards against
--   any incomplete later down that failed to clean up a dependent, or future
--   FKs added to game_plays without a corresponding down update.
--   See also: 000100.up / 000101.up headers document the IF-NOT-EXISTS trap.
DROP TABLE IF EXISTS life_change_tracking CASCADE;
DROP TABLE IF EXISTS game_plays CASCADE;
