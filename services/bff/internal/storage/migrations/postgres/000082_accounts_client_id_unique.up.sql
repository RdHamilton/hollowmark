-- Migration: add UNIQUE constraint on accounts.client_id
--
-- Context: GetOrCreateByClientID uses ON CONFLICT DO NOTHING when inserting a
-- new account row.  Without a UNIQUE constraint on client_id that clause has no
-- conflict target to act on, meaning concurrent inserts with the same client_id
-- can silently produce duplicate rows instead of coalescing.  This migration
-- adds the missing constraint so ON CONFLICT DO NOTHING behaves as intended and
-- the cross-tenant ownership check in the retry path is always reached when a
-- race occurs.
--
-- The DO $$ block is idempotent: if the constraint already exists (e.g. applied
-- manually or by a future consolidation migration) the block is a no-op.

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM   pg_constraint
        WHERE  conrelid = 'accounts'::regclass
        AND    conname   = 'accounts_client_id_unique'
    ) THEN
        ALTER TABLE accounts
            ADD CONSTRAINT accounts_client_id_unique UNIQUE (client_id);
    END IF;
END;
$$;
