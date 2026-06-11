-- Rollback migration 000117: drop rectification_audit_log table.
-- CASCADE guards against incomplete later downs and dirty states. On a correct
-- sequential down, dependents are already gone before this migration runs;
-- CASCADE is a safety net for partial failures and future FK additions that
-- lack a corresponding down update.
DROP TABLE IF EXISTS rectification_audit_log CASCADE;