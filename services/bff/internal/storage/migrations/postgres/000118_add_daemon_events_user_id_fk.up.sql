-- Migration 000118: add FK daemon_events.user_id → users(id) ON DELETE CASCADE
-- Ticket: #2546
-- Plan approved: Ray, comment 4674634550
--
-- The daemon_events table (created by migration 000061) has user_id BIGINT NOT NULL
-- but no FK constraint.  This migration adds the missing FK.
--
-- Erasure paths after this migration:
--   1. ACCOUNT-DELETE path (existing, unchanged):
--      DeletionRepository.DeleteTextKeyedRows (deletion_repo.go:111) deletes
--      daemon_events rows by account_id TEXT = ANY($1) — the MTGA Arena client_id.
--      This path is unaffected by this migration.
--   2. USER-DELETE path (new, added here):
--      DeletionRepository.HardDeleteUser (deletion_repo.go:172) deletes the users
--      row; this FK cascade now also removes all daemon_events rows for that user_id.
--      The two paths are additive defense-in-depth — not redundant.
--
-- Lock profile:
--   - Orphan pre-check DO block: ShareLock (read); sub-second.
--   - ADD CONSTRAINT ... NOT VALID: SHARE ROW EXCLUSIVE; brief (no row scan).
--   - VALIDATE CONSTRAINT: SHARE UPDATE EXCLUSIVE; concurrent reads+writes proceed.
--
-- No maintenance window required per Ray's PLAN_VERDICT approval (Q3).
-- golang-migrate wraps this in a transaction; a VALIDATE failure rolls back cleanly.

-- -----------------------------------------------------------------------
-- Step 1: Abort if any orphan user_id rows exist (user_id references a
-- non-existent users.id).  Uses NOT EXISTS rather than NOT IN to avoid
-- the NULL-subquery hazard (users.id is BIGSERIAL PK and cannot be NULL
-- in practice, but NOT EXISTS is the correct default regardless).
--
-- Expected count on prod: 0.
-- If this fires: do NOT silently delete.  Raise and require manual triage.
-- -----------------------------------------------------------------------
DO $$
DECLARE
    orphan_count BIGINT;
BEGIN
    SELECT COUNT(*) INTO orphan_count
    FROM   daemon_events de
    WHERE  NOT EXISTS (
               SELECT 1 FROM users u WHERE u.id = de.user_id
           );

    IF orphan_count > 0 THEN
        RAISE EXCEPTION
            'daemon_events has % orphan user_id rows (no matching users.id) — '
            'resolve manually before adding FK. Diagnostic: '
            'SELECT user_id, COUNT(*) FROM daemon_events de '
            'WHERE NOT EXISTS (SELECT 1 FROM users u WHERE u.id = de.user_id) '
            'GROUP BY user_id;',
            orphan_count;
    END IF;
END $$;

-- -----------------------------------------------------------------------
-- Step 2: Add FK with NOT VALID (no full-table scan under heavy lock).
-- DO/EXCEPTION on duplicate_object makes this idempotent if re-run after
-- a partial failure.
-- -----------------------------------------------------------------------
DO $$ BEGIN
    ALTER TABLE daemon_events
        ADD CONSTRAINT fk_daemon_events_user_id
        FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE NOT VALID;
EXCEPTION WHEN duplicate_object THEN NULL; END $$;

-- -----------------------------------------------------------------------
-- Step 3: Validate constraint.
-- Guarded by an existence check so a re-run where Step 2 was a no-op
-- (duplicate_object caught) does not fail with "constraint does not exist".
-- SHARE UPDATE EXCLUSIVE — concurrent reads+writes proceed.
-- -----------------------------------------------------------------------
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM   information_schema.table_constraints
        WHERE  constraint_name = 'fk_daemon_events_user_id'
          AND  table_name      = 'daemon_events'
    ) THEN
        ALTER TABLE daemon_events VALIDATE CONSTRAINT fk_daemon_events_user_id;
    END IF;
END $$;
