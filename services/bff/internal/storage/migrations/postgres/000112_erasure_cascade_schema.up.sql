-- Migration 000112: GDPR Art.17 erasure cascade schema (ADR-056 / ticket #891)
--
-- Adds:
--   1. users.deleted_at TIMESTAMPTZ — soft-delete gate (Step 1 of erasure cascade).
--   2. deletion_audit_log table — tracks erasure job status for AC7 runbook + legal
--      evidence.  Rows are RETAINED (not erased) — they contain no PII and are
--      compliance evidence under legitimate interests (ADR-056 §Risks).
--   3. Index on daemon_api_keys(account_id) — absent index causing 114k seq scans
--      (DB health check 2026-06-10).  The Step 4a cascade DELETE uses this column.
--
-- Depends on: migration 000110 (consent_log + ON DELETE SET NULL, ticket #885).
-- This PR (#891) must merge AFTER #885.

-- 1. Soft-delete gate column on users.
ALTER TABLE users ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;

-- 2. Erasure audit log.
--
--    job_id        — UUID primary key, returned in the 202 response body.
--    clerk_user_id — Clerk identity (retained for legal evidence; not erased).
--    user_id       — internal users.id at time of request.
--    account_id    — internal accounts.id at time of request.
--    requested_at  — timestamp of the erasure request.
--    completed_at  — set by Step 8 on success; NULL = in-progress or failed.
--
-- NO FK to users(id) or accounts(id): the cascade deletes those rows; the audit
-- log must survive independently as compliance evidence.
CREATE TABLE IF NOT EXISTS deletion_audit_log (
    job_id        UUID        NOT NULL DEFAULT gen_random_uuid() PRIMARY KEY,
    clerk_user_id TEXT        NOT NULL,
    user_id       BIGINT      NOT NULL,
    account_id    BIGINT      NOT NULL,
    requested_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at  TIMESTAMPTZ
);

-- Index for AC7 runbook queries: find incomplete jobs.
CREATE INDEX IF NOT EXISTS idx_deletion_audit_log_completed_at
    ON deletion_audit_log (completed_at)
    WHERE completed_at IS NULL;

-- 3. Performance index: daemon_api_keys(account_id TEXT).
--
--    The Step 4a erasure DELETE runs:
--      DELETE FROM daemon_api_keys WHERE account_id = ANY($client_ids)
--    Without an index, this is a full table scan (114k seq_scans observed,
--    0.1% idx_usage_pct — DB health check 2026-06-10).
--
--    Note: CONCURRENTLY is NOT used here per migration SOP (golang-migrate wraps
--    migrations in transactions; CONCURRENTLY cannot run inside a transaction).
--    This migration runs at deploy time when the BFF is stopped; a brief lock is
--    acceptable.
CREATE INDEX IF NOT EXISTS idx_daemon_api_keys_account_id
    ON daemon_api_keys (account_id);
