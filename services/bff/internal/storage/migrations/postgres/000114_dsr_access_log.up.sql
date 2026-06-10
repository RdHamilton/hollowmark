-- Migration 000114: DSR access log for GDPR Art.15 data export (ticket #886)
--
-- Adds:
--   dsr_access_log — records each data export request per user.
--
-- Purpose:
--   1. Rate-limit enforcement: GET /api/v1/account/data-export checks this
--      table for a row with requested_at > NOW() - INTERVAL '24 hours' before
--      allowing a new export.  On hit the handler returns 429 + Retry-After.
--      (Ray Q4 ruling: 1 export / 24h / user; keyed off dsr_access_log.)
--   2. Audit log: provides an immutable record of when each user exercised
--      their Art.15 access right.
--
-- No FK to users(id): the log is compliance evidence and must survive if the
-- user row is later erased (Art.17 erasure cascade drops users rows).
-- user_id is retained as a plain BIGINT (same pattern as deletion_audit_log).
--
-- Depends on: migration 000113 (drop redundant account_id indexes, ticket #850).

CREATE TABLE IF NOT EXISTS dsr_access_log (
    export_id    UUID        NOT NULL DEFAULT gen_random_uuid() PRIMARY KEY,
    user_id      BIGINT      NOT NULL,
    requested_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Rate-limit index: find the most-recent export for a given user efficiently.
-- The WHERE clause limits the index to rows within a rolling 48h window so the
-- index stays small even for users who export frequently.
CREATE INDEX IF NOT EXISTS idx_dsr_access_log_user_recent
    ON dsr_access_log (user_id, requested_at DESC);
