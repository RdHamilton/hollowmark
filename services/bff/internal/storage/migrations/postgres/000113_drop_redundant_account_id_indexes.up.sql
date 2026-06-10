-- Migration 000113: Drop redundant plain account_id indexes (ticket #850)
--
-- Background:
--   Three plain btree indexes are superseded by more specific partial indexes
--   that already exist and serve the hot-path queries.  Keeping the plain indexes
--   adds write overhead on every INSERT/UPDATE/DELETE with no query benefit.
--
-- Dropped indexes and their superseding replacements:
--
--   daemon_api_keys_account_id_idx    btree(account_id)
--     Created: migration 000085.
--     Superseded by: daemon_api_keys_account_active_idx
--                    btree(account_id) WHERE revoked_at IS NULL
--     Scan count on staging (2026-06-10): 0.
--
--   idx_daemon_api_keys_account_id    btree(account_id)
--     Created: migration 000112 (erasure cascade #891).
--     Exact duplicate of daemon_api_keys_account_id_idx.
--     Scan count on staging (2026-06-10): 0.
--
--   idx_accounts_is_default           btree(is_default)
--     Created: migration 000054 / 000104.
--     Superseded by: idx_accounts_default
--                    unique btree(is_default) WHERE is_default = true
--     Scan count on staging (2026-06-10): 0.
--
-- Note: DROP INDEX CONCURRENTLY cannot run inside a transaction block.
-- golang-migrate wraps each migration in a transaction; CONCURRENTLY is
-- therefore never used in migration files (migration SOP §3).  At deploy
-- time the BFF is stopped, so a brief AccessShareLock is acceptable.

DROP INDEX IF EXISTS daemon_api_keys_account_id_idx;
DROP INDEX IF EXISTS idx_daemon_api_keys_account_id;
DROP INDEX IF EXISTS idx_accounts_is_default;
