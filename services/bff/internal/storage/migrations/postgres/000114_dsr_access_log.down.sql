-- Down: remove dsr_access_log and its index (migration 000114 rollback).
DROP INDEX IF EXISTS idx_dsr_access_log_user_recent;
DROP TABLE IF EXISTS dsr_access_log;
