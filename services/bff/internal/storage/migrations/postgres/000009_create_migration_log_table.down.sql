-- Drop migration_log table and indexes
DROP INDEX IF EXISTS idx_migration_log_processed_at;
DROP INDEX IF EXISTS idx_migration_log_old_scryfall_id;
DROP INDEX IF EXISTS idx_migration_log_migration_id;
-- CASCADE guards against incomplete later downs and dirty states. On a correct
-- sequential down, dependents are already gone before this migration runs;
-- CASCADE is a safety net for partial failures and future FK additions that
-- lack a corresponding down update.
DROP TABLE IF EXISTS migration_log CASCADE;
