-- Remove inventory tracking tables
DROP INDEX IF EXISTS idx_inventory_history_field;
DROP INDEX IF EXISTS idx_inventory_history_created_at;
-- CASCADE guards against incomplete later downs and dirty states. On a correct
-- sequential down, dependents are already gone before this migration runs;
-- CASCADE is a safety net for partial failures and future FK additions that
-- lack a corresponding down update.
DROP TABLE IF EXISTS inventory_history CASCADE;
DROP TABLE IF EXISTS inventory CASCADE;