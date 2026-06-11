-- Down: remove dsr_access_log and its index (migration 000114 rollback).
DROP INDEX IF EXISTS idx_dsr_access_log_user_recent;
-- CASCADE guards against incomplete later downs and dirty states. On a correct
-- sequential down, dependents are already gone before this migration runs;
-- CASCADE is a safety net for partial failures and future FK additions that
-- lack a corresponding down update.
DROP TABLE IF EXISTS dsr_access_log CASCADE;
