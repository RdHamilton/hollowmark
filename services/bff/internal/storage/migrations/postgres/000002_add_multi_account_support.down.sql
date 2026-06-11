-- Rollback multi-account support
-- Removes account_id columns and accounts table

-- Remove account_id from draft_events
DROP INDEX IF EXISTS idx_draft_events_account_id;
ALTER TABLE draft_events DROP COLUMN IF EXISTS account_id;

-- Remove account_id from rank_history
DROP INDEX IF EXISTS idx_rank_history_account_id;
ALTER TABLE IF EXISTS rank_history DROP COLUMN IF EXISTS account_id;

-- Remove account_id from collection_history
DROP INDEX IF EXISTS idx_collection_history_account_id;
ALTER TABLE IF EXISTS collection_history DROP COLUMN IF EXISTS account_id;

-- Recreate collection table without account_id.
-- Guard: collection is dropped by 000054.down (which runs before this migration
-- in descending order). Skip this block entirely if collection is absent.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = 'public' AND table_name = 'collection') THEN
        CREATE TABLE IF NOT EXISTS collection_old (
            card_id INTEGER PRIMARY KEY,
            quantity INTEGER NOT NULL,
            updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
        );
        INSERT INTO collection_old (card_id, quantity, updated_at)
        SELECT card_id, quantity, updated_at FROM collection;
        DROP TABLE IF EXISTS collection CASCADE;
        ALTER TABLE collection_old RENAME TO collection;
    END IF;
END
$$;

-- Remove account_id from decks
DROP INDEX IF EXISTS idx_decks_account_id;
ALTER TABLE IF EXISTS decks DROP COLUMN IF EXISTS account_id;

-- Remove account_id from player_stats
DROP INDEX IF EXISTS idx_player_stats_account_id;
DROP INDEX IF EXISTS idx_player_stats_date_format_account;
-- Guard: player_stats is dropped by 000054.down before this migration runs.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = 'public' AND table_name = 'player_stats') THEN
        CREATE UNIQUE INDEX IF NOT EXISTS idx_player_stats_date_format ON player_stats(date, format);
    END IF;
END
$$;
ALTER TABLE IF EXISTS player_stats DROP COLUMN IF EXISTS account_id;

-- Remove account_id from matches
DROP INDEX IF EXISTS idx_matches_account_id;
ALTER TABLE IF EXISTS matches DROP COLUMN IF EXISTS account_id;

-- Drop accounts table
DROP INDEX IF EXISTS idx_accounts_is_default;
DROP INDEX IF EXISTS idx_accounts_default;
-- CASCADE guards against incomplete later downs and dirty states. On a correct
-- sequential down, dependents are already gone before this migration runs;
-- CASCADE is a safety net for partial failures and future FK additions that
-- lack a corresponding down update.
DROP TABLE IF EXISTS accounts CASCADE;