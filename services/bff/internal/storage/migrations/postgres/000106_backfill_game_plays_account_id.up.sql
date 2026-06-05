-- Migration 000106: backfill game_plays.account_id for existing NULL rows.
--
-- BACKGROUND: InsertCardPlays never supplied account_id in its INSERT statement
-- (ticket #820). Migration 000101 dropped the NOT NULL constraint so that
-- per-turn INSERTs would succeed on both schema variants; the side effect is
-- that every row written before the #820 fix has account_id = NULL.
--
-- FIX: populate account_id from the owning account via the games → matches join
-- for any rows where account_id IS NULL. This is idempotent — rows already
-- populated (after the Go fix ships) are not touched.
--
-- SAFETY: game_plays.account_id is nullable (migration 000101). This UPDATE
-- does not add or drop any column, constraint, or index — no table rewrite.
-- The WHERE account_id IS NULL predicate means zero rows are touched on DBs
-- where all rows are already backfilled; cost is proportional to NULL count.
--
-- NOTE: this migration does NOT add a NOT NULL constraint. A future migration
-- may tighten the constraint once the backfill has been verified in prod and
-- no new NULL-account_id INSERTs are possible (the Go fix gates that).

-- Ensure account_id column exists on the 000054-consolidated-schema init path.
-- On incremental-migration DBs where the column already exists, this is a no-op.
ALTER TABLE game_plays ADD COLUMN IF NOT EXISTS account_id BIGINT;

UPDATE game_plays gp
SET    account_id = m.account_id
FROM   games g
JOIN   matches m ON m.id = g.match_id
WHERE  gp.game_id = g.id
  AND  gp.account_id IS NULL;
