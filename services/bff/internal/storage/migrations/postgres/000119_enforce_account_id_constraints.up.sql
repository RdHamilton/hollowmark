-- Migration 000119: enforce account_id NOT NULL + FK → accounts(id) ON DELETE CASCADE
-- on five legacy tenant tables.
-- Ticket: #2545
-- Plan approved: Ray, comment 4674629439
--
-- Tables in scope:
--   matches, player_stats, rank_history, collection_history
--       (account_id BIGINT nullable, no FK — added by migration 000002)
--   draft_sessions
--       (account_id BIGINT nullable, FK present from migration 000052, NOT NULL absent)
--
-- Two-path safety:
--   Incremental path (prod): columns are nullable BIGINT; four tables have no FK.
--   Fresh-install path (000054): columns are already NOT NULL with inline anonymous FKs
--       (matches_account_id_fkey etc).  SET NOT NULL is guarded by
--       information_schema.columns is_nullable pre-check; FK ADD is guarded by
--       duplicate_object; VALIDATE is guarded by constraint-existence check.
--
-- Lock profile:
--   Step 1 backfill UPDATEs:       ROW EXCLUSIVE (row-level; expected 0 NULL rows on prod)
--   Step 2 NULL abort check:       ShareLock (read), sub-second
--   Step 3 SET NOT NULL:           ACCESS EXCLUSIVE, catalog-only write (sub-second when 0 NULLs)
--   Step 4 ADD CONSTRAINT NOT VALID: SHARE ROW EXCLUSIVE (brief, no row scan)
--   Step 4 VALIDATE CONSTRAINT:    SHARE UPDATE EXCLUSIVE (reads+writes proceed concurrently)
--
-- golang-migrate wraps this in a transaction.  If VALIDATE fails (orphan found),
-- the entire migration rolls back cleanly.

-- -----------------------------------------------------------------------
-- Step 1: Backfill NULL account_id to the default account.
-- On the fresh-install path (000054) these are no-ops — zero NULL rows exist.
-- On the incremental path, migration 000002 already backfilled NULLs at the
-- time it ran; any rows created since then came through the BFF (which always
-- resolves account_id before writing).  Expected: 0 rows updated.
-- is_default = TRUE: migration 000104 converted accounts.is_default to BOOLEAN.
-- -----------------------------------------------------------------------
UPDATE matches
    SET account_id = (SELECT id FROM accounts WHERE is_default = TRUE LIMIT 1)
    WHERE account_id IS NULL;

UPDATE player_stats
    SET account_id = (SELECT id FROM accounts WHERE is_default = TRUE LIMIT 1)
    WHERE account_id IS NULL;

UPDATE rank_history
    SET account_id = (SELECT id FROM accounts WHERE is_default = TRUE LIMIT 1)
    WHERE account_id IS NULL;

UPDATE collection_history
    SET account_id = (SELECT id FROM accounts WHERE is_default = TRUE LIMIT 1)
    WHERE account_id IS NULL;

UPDATE draft_sessions
    SET account_id = (SELECT id FROM accounts WHERE is_default = TRUE LIMIT 1)
    WHERE account_id IS NULL;

-- -----------------------------------------------------------------------
-- Step 2: Abort if any NULLs remain (no default account row, or a race
-- produced new NULLs after the backfill above).  P0 safety gate.
-- Orphan policy: ABORT-AND-TRIAGE — no silent deletes (Ray Q4 directive).
-- -----------------------------------------------------------------------
DO $$
DECLARE
    null_matches         BIGINT;
    null_player_stats    BIGINT;
    null_rank_history    BIGINT;
    null_collection_hist BIGINT;
    null_draft_sessions  BIGINT;
BEGIN
    SELECT COUNT(*) INTO null_matches         FROM matches          WHERE account_id IS NULL;
    SELECT COUNT(*) INTO null_player_stats    FROM player_stats     WHERE account_id IS NULL;
    SELECT COUNT(*) INTO null_rank_history    FROM rank_history     WHERE account_id IS NULL;
    SELECT COUNT(*) INTO null_collection_hist FROM collection_history WHERE account_id IS NULL;
    SELECT COUNT(*) INTO null_draft_sessions  FROM draft_sessions   WHERE account_id IS NULL;

    IF null_matches > 0 THEN
        RAISE EXCEPTION
            'matches has % NULL account_id rows after backfill — '
            'check accounts.is_default; manual triage required before re-running',
            null_matches;
    END IF;
    IF null_player_stats > 0 THEN
        RAISE EXCEPTION
            'player_stats has % NULL account_id rows after backfill — '
            'manual triage required',
            null_player_stats;
    END IF;
    IF null_rank_history > 0 THEN
        RAISE EXCEPTION
            'rank_history has % NULL account_id rows after backfill — '
            'manual triage required',
            null_rank_history;
    END IF;
    IF null_collection_hist > 0 THEN
        RAISE EXCEPTION
            'collection_history has % NULL account_id rows after backfill — '
            'manual triage required',
            null_collection_hist;
    END IF;
    IF null_draft_sessions > 0 THEN
        RAISE EXCEPTION
            'draft_sessions has % NULL account_id rows after backfill — '
            'manual triage required',
            null_draft_sessions;
    END IF;
END $$;

-- -----------------------------------------------------------------------
-- Step 3: SET NOT NULL on each column.
--
-- Guarded with information_schema.columns is_nullable pre-check (Ray Q3 directive):
-- only runs the ALTER when the column is actually nullable (incremental path).
-- On the fresh-install path the column is already NOT NULL — this is a clean no-op
-- without swallowing real errors.
-- -----------------------------------------------------------------------
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.columns
               WHERE table_name = 'matches' AND column_name = 'account_id'
                 AND is_nullable = 'YES') THEN
        ALTER TABLE matches ALTER COLUMN account_id SET NOT NULL;
    END IF;
END $$;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.columns
               WHERE table_name = 'player_stats' AND column_name = 'account_id'
                 AND is_nullable = 'YES') THEN
        ALTER TABLE player_stats ALTER COLUMN account_id SET NOT NULL;
    END IF;
END $$;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.columns
               WHERE table_name = 'rank_history' AND column_name = 'account_id'
                 AND is_nullable = 'YES') THEN
        ALTER TABLE rank_history ALTER COLUMN account_id SET NOT NULL;
    END IF;
END $$;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.columns
               WHERE table_name = 'collection_history' AND column_name = 'account_id'
                 AND is_nullable = 'YES') THEN
        ALTER TABLE collection_history ALTER COLUMN account_id SET NOT NULL;
    END IF;
END $$;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.columns
               WHERE table_name = 'draft_sessions' AND column_name = 'account_id'
                 AND is_nullable = 'YES') THEN
        ALTER TABLE draft_sessions ALTER COLUMN account_id SET NOT NULL;
    END IF;
END $$;

-- -----------------------------------------------------------------------
-- Step 4: Add named FK constraints with NOT VALID + existence-guarded VALIDATE.
--
-- NOT VALID: avoids full-table scan under SHARE ROW EXCLUSIVE lock; the lock
-- releases before VALIDATE.  VALIDATE runs under SHARE UPDATE EXCLUSIVE
-- (concurrent reads+writes proceed).
--
-- ADD: DO/EXCEPTION on duplicate_object is idempotent (catches both the
-- fresh-install path where an anonymous FK already exists, and any re-run).
--
-- VALIDATE: guarded by information_schema existence check so a re-run where ADD
-- was a no-op (duplicate_object caught) does not fail with "constraint not found"
-- (Ray Q2 directive).
--
-- draft_sessions: FK already exists from migration 000052 (anonymous name).
-- Skip FK add/validate — NOT NULL in Step 3 is the only change needed.
-- -----------------------------------------------------------------------
DO $$ BEGIN
    ALTER TABLE matches
        ADD CONSTRAINT fk_matches_account_id
        FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE NOT VALID;
EXCEPTION WHEN duplicate_object THEN NULL; END $$;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.table_constraints
               WHERE constraint_name = 'fk_matches_account_id'
                 AND table_name      = 'matches') THEN
        ALTER TABLE matches VALIDATE CONSTRAINT fk_matches_account_id;
    END IF;
END $$;

DO $$ BEGIN
    ALTER TABLE player_stats
        ADD CONSTRAINT fk_player_stats_account_id
        FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE NOT VALID;
EXCEPTION WHEN duplicate_object THEN NULL; END $$;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.table_constraints
               WHERE constraint_name = 'fk_player_stats_account_id'
                 AND table_name      = 'player_stats') THEN
        ALTER TABLE player_stats VALIDATE CONSTRAINT fk_player_stats_account_id;
    END IF;
END $$;

DO $$ BEGIN
    ALTER TABLE rank_history
        ADD CONSTRAINT fk_rank_history_account_id
        FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE NOT VALID;
EXCEPTION WHEN duplicate_object THEN NULL; END $$;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.table_constraints
               WHERE constraint_name = 'fk_rank_history_account_id'
                 AND table_name      = 'rank_history') THEN
        ALTER TABLE rank_history VALIDATE CONSTRAINT fk_rank_history_account_id;
    END IF;
END $$;

DO $$ BEGIN
    ALTER TABLE collection_history
        ADD CONSTRAINT fk_collection_history_account_id
        FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE NOT VALID;
EXCEPTION WHEN duplicate_object THEN NULL; END $$;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.table_constraints
               WHERE constraint_name = 'fk_collection_history_account_id'
                 AND table_name      = 'collection_history') THEN
        ALTER TABLE collection_history VALIDATE CONSTRAINT fk_collection_history_account_id;
    END IF;
END $$;
