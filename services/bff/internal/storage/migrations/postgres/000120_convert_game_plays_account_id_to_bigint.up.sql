-- Migration 000120: convert game_plays.account_id from TEXT to BIGINT on
-- incremental-upgrade DBs (ticket #1185).
--
-- BACKGROUND
-- ----------
-- On the incremental-migration path (DBs predating the 000054 consolidated
-- schema):
--
--   000030: game_plays created WITHOUT an account_id column.
--   000068: ADD COLUMN account_id TEXT NOT NULL DEFAULT ''
--           → column born as TEXT; DEFAULT '' active.
--   000101: ALTER COLUMN account_id DROP NOT NULL
--           → column stays TEXT, now nullable; DEFAULT '' NOT removed.
--   000106: ADD COLUMN IF NOT EXISTS account_id BIGINT
--           → no-op (column already exists as TEXT).
--
-- On the fresh-init path (000054 consolidated schema):
--   000054: game_plays.account_id BIGINT from the start.
--   000068: no-op by name.
--
-- Pre-#820 INSERTs omitted account_id from the column list; those rows took
-- the DEFAULT '' on the TEXT/incremental path.  Migration 000106's backfill
-- (WHERE account_id IS NULL) skipped them — they persist as '' in prod today.
--
-- game_play_repo.go's InsertCardPlays binds accountID int64 as $1.  Against
-- a TEXT column (OID 25), pgx v5/stdlib (QueryExecModeCacheStatement) has no
-- int64→text encode plan → the reported error:
--   "InsertCardPlays[0]: unable to encode N into text format for text (OID 25):
--    cannot find encode plan"
--
-- FIX
-- ---
-- A PL/pgSQL DO block gates all mutations on the column being TEXT so the
-- migration is a guaranteed no-op on the fresh-init (BIGINT) path.
--
-- On the TEXT path:
--   Step 1. Resolve '' sentinels to their owning account via games→matches,
--           same join pattern as 000106.  Rows whose chain is intact get the
--           correct BIGINT account_id (cast to text for the UPDATE; the ALTER
--           in Step 3 will cast the whole column).
--   Step 2. Null-out any '' rows that could not be resolved (orphan rows with
--           no game→match→account chain).  These become NULL; the column is
--           nullable post-000101 so this is safe.
--   Step 3. ALTER COLUMN account_id TYPE BIGINT USING NULLIF(btrim(account_id), '')::bigint.
--           NULLIF handles any '' that slipped through (defensive); btrim
--           catches whitespace-only values; a valid bigint string casts cleanly.
--
-- NOT NULL is intentionally NOT added here (consistent with 000106 and the
-- Ray REQUIRED CHANGE 3 ruling).  A follow-on migration tightens the
-- constraint after prod is verified clean of NULLs.
--
-- PRE-APPLY VERIFICATION (run on prod RDS before applying — Ray REQUIRED CHANGE 2)
-- ---------------------------------------------------------------------------------
-- Gate on column being text first, then run:
--
--   SELECT
--     count(*) FILTER (WHERE btrim(COALESCE(account_id, '')) = '')          AS empty_string_rows,
--     count(*) FILTER (WHERE account_id IS NOT NULL
--                        AND btrim(account_id) <> ''
--                        AND account_id !~ '^[0-9]+$')                      AS uncastable_rows
--   FROM game_plays;
--
-- empty_string_rows > 0: EXPECTED and handled by Steps 1–2.
-- uncastable_rows > 0:   HARD STOP — investigate before applying; do not proceed.
-- Paste the output into the PR Local Verification section before applying.

DO $$
DECLARE
  col_type TEXT;
BEGIN
  -- Determine the current data type of game_plays.account_id.
  -- information_schema is used (not pg_attribute) because this migration
  -- targets the real production table, not a temp table.
  SELECT data_type
    INTO col_type
    FROM information_schema.columns
   WHERE table_schema = 'public'
     AND table_name   = 'game_plays'
     AND column_name  = 'account_id';

  -- Only proceed if the column is TEXT (incremental/prod path).
  -- On the fresh-init (BIGINT) path the block exits immediately — true no-op.
  IF col_type IN ('text', 'character varying', 'character') THEN

    -- Step 1: resolve '' sentinels left by pre-#820 inserts on the TEXT path.
    -- 000101 dropped NOT NULL but NOT the DEFAULT '', and 000106's IS NULL
    -- backfill skipped these '' rows, so they persist in prod today.
    UPDATE game_plays gp
       SET account_id = m.account_id::text
      FROM games    g
      JOIN matches  m ON m.id = g.match_id
     WHERE gp.game_id  = g.id
       AND btrim(COALESCE(gp.account_id, '')) = '';

    -- Step 2: null-out any '' rows that could not be resolved (orphan rows
    -- with no games→matches chain).  Column is nullable post-000101.
    UPDATE game_plays
       SET account_id = NULL
     WHERE btrim(account_id) = '';

    -- Step 3a: drop the DEFAULT '' that 000068 set and 000101 never removed.
    -- PostgreSQL cannot automatically cast DEFAULT '' to BIGINT, so the default
    -- must be removed before the ALTER COLUMN TYPE.  Post-000101 the column is
    -- nullable, so dropping the default is safe; new rows insert NULL when
    -- account_id is omitted (the pre-#820 behaviour is no longer triggered).
    ALTER TABLE game_plays ALTER COLUMN account_id DROP DEFAULT;

    -- Step 3b: cast TEXT column to BIGINT.
    -- NULLIF(btrim(account_id), '') handles any residual empties defensively.
    -- A valid bigint string (e.g. '42') casts cleanly.
    -- NULL values pass through unchanged.
    EXECUTE 'ALTER TABLE game_plays
               ALTER COLUMN account_id TYPE BIGINT
               USING NULLIF(btrim(account_id), '''')::bigint';

  END IF;
END$$;
