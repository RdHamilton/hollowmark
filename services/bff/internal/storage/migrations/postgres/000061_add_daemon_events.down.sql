-- Revert migration 000061: drop the daemon_events table and its index.
DROP INDEX IF EXISTS idx_daemon_events_user_occurred;
-- CASCADE guards against incomplete later downs and dirty states. On a correct
-- sequential down, dependents are already gone before this migration runs;
-- CASCADE is a safety net for partial failures and future FK additions that
-- lack a corresponding down update.
DROP TABLE IF EXISTS daemon_events CASCADE;
