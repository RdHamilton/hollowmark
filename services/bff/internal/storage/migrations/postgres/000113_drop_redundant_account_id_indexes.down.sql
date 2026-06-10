-- Migration 000113 DOWN: Recreate the three dropped redundant indexes.
--
-- Restores daemon_api_keys_account_id_idx, idx_daemon_api_keys_account_id,
-- and idx_accounts_is_default to their state before the 000113 up migration.
-- These are plain btree indexes — identical definitions to what existed before
-- the drop.

CREATE INDEX IF NOT EXISTS daemon_api_keys_account_id_idx
    ON daemon_api_keys (account_id);

CREATE INDEX IF NOT EXISTS idx_daemon_api_keys_account_id
    ON daemon_api_keys (account_id);

CREATE INDEX IF NOT EXISTS idx_accounts_is_default
    ON accounts (is_default);
