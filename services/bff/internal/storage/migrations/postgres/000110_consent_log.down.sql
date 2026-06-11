-- Migration 000110 rollback: drop consent_log table.
-- Indexes are dropped automatically when the table is dropped.
-- CASCADE guards against incomplete later downs and dirty states. On a correct
-- sequential down, dependents are already gone before this migration runs;
-- CASCADE is a safety net for partial failures and future FK additions that
-- lack a corresponding down update.
DROP TABLE IF EXISTS consent_log CASCADE;
