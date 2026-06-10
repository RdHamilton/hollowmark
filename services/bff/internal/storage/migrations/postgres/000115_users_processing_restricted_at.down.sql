-- Down migration for 000115: GDPR Art.18 Right to Restriction flag
-- Reverses the .up.sql additions in reverse order.

-- Section C
DROP INDEX IF EXISTS restriction_audit_log_user_id_idx;
DROP TABLE IF EXISTS restriction_audit_log CASCADE;

-- Section B
DROP INDEX IF EXISTS users_processing_restricted_at_idx;
ALTER TABLE users DROP COLUMN IF EXISTS processing_restricted_at;

-- Section A
DROP INDEX IF EXISTS accounts_account_id_hash_idx;
ALTER TABLE accounts DROP COLUMN IF EXISTS account_id_hash;
