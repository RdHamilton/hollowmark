-- Migration 000106 down: revert the backfill by setting game_plays.account_id
-- back to NULL for rows this migration populated.
--
-- NOTE: this is a best-effort revert. Rows that were already NULL before the
-- up migration are unaffected (they remain NULL). Rows that were explicitly
-- set by InsertCardPlays after the #820 Go fix cannot be distinguished from
-- backfilled rows by this migration — those rows will also be NULL-ed out.
-- This is acceptable for a down migration: the column is nullable and the
-- read path does not depend on it.

UPDATE game_plays gp
SET    account_id = NULL
FROM   games g
JOIN   matches m ON m.id = g.match_id
WHERE  gp.game_id = g.id
  AND  gp.account_id = m.account_id;
