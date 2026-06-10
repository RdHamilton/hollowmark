-- Migration 000120 down: revert game_plays.account_id from BIGINT back to TEXT.
--
-- NOTE: this is a best-effort revert.  Rows whose '' sentinels were resolved
-- to an account_id in the up migration (Steps 1–2) cannot be individually
-- distinguished from rows that already held a valid bigint value.  The down
-- migration casts all BIGINT values back to TEXT and restores the DEFAULT ''
-- sentinel so the column state matches the pre-000120 incremental path.
--
-- Rows that were NULL before the up migration remain NULL after this down
-- migration (the CAST of NULL is still NULL).
--
-- On fresh-init (BIGINT) path DBs, the column is already BIGINT but was NOT
-- touched by the up migration; this down migration will still run the ALTER
-- (there is no symmetric type guard in the down direction).  That is
-- acceptable: reverting a fresh-init DB from BIGINT to TEXT is not a normal
-- operation, and the fresh-init path is always re-initialised rather than
-- rolled back in practice.

ALTER TABLE game_plays
    ALTER COLUMN account_id TYPE TEXT
    USING account_id::text;

ALTER TABLE game_plays
    ALTER COLUMN account_id SET DEFAULT '';
