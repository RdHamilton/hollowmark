-- Migration 000113 DOWN: Recreate the two dropped redundant indexes.
--
-- Restores daemon_api_keys_account_id_idx and idx_accounts_is_default to
-- their state before the 000113 up migration.  These are plain btree
-- indexes -- identical definitions to what existed before the drop.
--
-- idx_daemon_api_keys_account_id is NOT recreated here because 000113 did
-- not drop it (it was retained to serve the GDPR erasure DELETE path).

CREATE INDEX IF NOT EXISTS daemon_api_keys_account_id_idx
    ON daemon_api_keys (account_id);

CREATE INDEX IF NOT EXISTS idx_accounts_is_default
    ON accounts (is_default);
