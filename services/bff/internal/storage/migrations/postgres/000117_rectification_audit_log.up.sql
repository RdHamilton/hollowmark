-- Migration 000117: rectification audit log for GDPR Art.16 Right to Rectification (#888)
--
-- Adds:
--   rectification_audit_log — one row per profile-field change initiated by a user.
--
-- Purpose:
--   PATCH /api/v1/account/profile writes an audit row AND syncs users.email in one
--   transaction.  This table is the immutable record of every rectification event.
--
-- PII BOUNDARY:
--   user_id     — internal int64 (not raw Clerk user ID or email).
--   field_name  — TEXT; the name of the field changed (e.g. "email", "display_name").
--   old_value_hash — SHA-256 hex[:16] of the old value; never the raw value.
--   new_value_hash — SHA-256 hex[:16] of the new value; never the raw value.
--   Hashing prevents PII from appearing in audit rows while still providing a
--   verifiable record that the value changed (two distinct hashes ≠ no-op).
--
-- RETENTION ON ART.17 ERASURE:
--   No FK to users(id): the audit log is compliance evidence and must survive
--   Art.17 erasure.  user_id is retained as a plain BIGINT (same pattern as
--   deletion_audit_log and dsr_access_log).  The erasure cascade does NOT
--   touch this table — the hashed PII fields contain no reconstructable PII.
--
-- SCOPE:
--   display_name — audit-only (Clerk is the source of truth; not persisted in DB).
--   email        — audit row + mandatory users.email UPDATE in the same txn.
--   date_of_birth_year is explicitly OUT OF SCOPE (COPPA-gated, support-handled;
--   Ray ruling 2026-06-10 issue #888).
--
-- Depends on: migration 000114 (dsr_access_log, ticket #886).

CREATE TABLE IF NOT EXISTS rectification_audit_log (
    id              BIGSERIAL    PRIMARY KEY,
    user_id         BIGINT       NOT NULL,
    field_name      TEXT         NOT NULL,
    old_value_hash  TEXT,        -- SHA-256 hex[:16] of old value; NULL when not available
    new_value_hash  TEXT         NOT NULL,
    changed_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

-- Primary access: all rectification events for a user, newest first.
-- Enables compliance queries: "what did user N change and when?"
CREATE INDEX IF NOT EXISTS idx_rectification_audit_log_user_changed_at
    ON rectification_audit_log (user_id, changed_at DESC);
