-- Rollback for migration 000112: GDPR Art.17 erasure cascade schema.
--
-- Reverses in the opposite order of the up migration.

-- 3. Remove daemon_api_keys performance index.
DROP INDEX IF EXISTS idx_daemon_api_keys_account_id;

-- 2. Remove deletion_audit_log table and its indexes.
DROP INDEX IF EXISTS idx_deletion_audit_log_active_per_account;
DROP INDEX IF EXISTS idx_deletion_audit_log_completed_at;
DROP TABLE IF EXISTS deletion_audit_log CASCADE;

-- 1. Remove soft-delete gate column from users.
ALTER TABLE users DROP COLUMN IF EXISTS deleted_at;
