-- Migration 000124: Change deletion_audit_log idempotency guard from per-account
-- to per-user.
--
-- Context (#1333, ADR-080 D4 G1 audit row 4):
--   Under the multi-account model (one Clerk user may own N accounts rows —
--   exactly the situation the 2026-06-12 incident created), the old
--   idx_deletion_audit_log_active_per_account index allowed one in-flight job
--   per ACCOUNT.  Two concurrent erasure requests for the same user (each
--   resolving to a different account_id) could each slip through the conflict
--   guard and launch independent jobs — a correctness and GDPR compliance risk.
--
--   The fix (matching the updated CreateAuditLogEntry ON CONFLICT clause) is to
--   enforce at most one in-flight job per USER.
--
-- Depends on: 000112 (deletion_audit_log + idx_deletion_audit_log_active_per_account).

-- Drop the old per-account unique partial index.
DROP INDEX IF EXISTS idx_deletion_audit_log_active_per_account;

-- Create the new per-user unique partial index.
-- At most one in-flight (completed_at IS NULL) erasure job per user.
CREATE UNIQUE INDEX idx_deletion_audit_log_active_per_user
    ON deletion_audit_log (user_id)
    WHERE completed_at IS NULL;
