-- Revert migration 000124: restore per-account idempotency guard.
DROP INDEX IF EXISTS idx_deletion_audit_log_active_per_user;

CREATE UNIQUE INDEX idx_deletion_audit_log_active_per_account
    ON deletion_audit_log (account_id)
    WHERE completed_at IS NULL;
