-- Migration 000115: GDPR Art.18 Right to Restriction flag (ADR-055 / ticket #890)
--
-- Section A: accounts.account_id_hash
--   The analytics seam (analytics.HaltChecker, introduced in #1597) keys halt
--   lookups on a pre-computed SHA-256 hex[:16] of the internal accounts.id.
--   This column is the indexed lookup target for DBHaltChecker.IsHalted.
--   It was designed in #1597 but not yet persisted; adding it here.
--
-- Section B: users.processing_restricted_at
--   Non-null when Art.18 restriction is active. NULL = unrestricted; non-null =
--   analytics forwarding halted. Does NOT prevent auth or product use (ADR-055).
--
-- Section C: restriction_audit_log
--   Append-only record of every set/clear transition. Retained through erasure
--   (no FK to users/accounts — users row may be erased; audit log survives).
--   Mirrors deletion_audit_log pattern.
--
-- Note: CREATE INDEX does NOT use CONCURRENTLY — golang-migrate wraps each
-- migration in a transaction and CONCURRENTLY cannot run inside a transaction.

-- ── Section A: accounts.account_id_hash ──────────────────────────────────────

ALTER TABLE accounts
    ADD COLUMN IF NOT EXISTS account_id_hash TEXT;

-- Backfill existing rows: sha256(id::text) hex[:16].
-- The function substr + encode(digest(...)) is standard pgcrypto (already
-- required by migration 000051 via CREATE EXTENSION IF NOT EXISTS pgcrypto).
-- Left-pad the integer ID string: no leading zeros needed; the Go side uses
-- strconv.FormatInt(id, 10) which never zero-pads.
UPDATE accounts
   SET account_id_hash = substr(encode(digest(id::text, 'sha256'), 'hex'), 1, 16)
 WHERE account_id_hash IS NULL;

-- Index for fast IsHalted lookups and analytics point-reads.
CREATE INDEX IF NOT EXISTS accounts_account_id_hash_idx
    ON accounts (account_id_hash);

-- ── Section B: users.processing_restricted_at ────────────────────────────────

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS processing_restricted_at TIMESTAMPTZ;

-- Partial index: only restricted rows.
-- Admin/runbook query: "who is currently restricted?" — stays tiny.
-- TIMESTAMPTZ column: must not use = TRUE/FALSE.
CREATE INDEX IF NOT EXISTS users_processing_restricted_at_idx
    ON users (processing_restricted_at)
    WHERE processing_restricted_at IS NOT NULL;

-- ── Section C: restriction_audit_log ─────────────────────────────────────────

CREATE TABLE IF NOT EXISTS restriction_audit_log (
    id            BIGSERIAL    PRIMARY KEY,
    user_id       BIGINT       NOT NULL,   -- users.id at event time (retained post-erasure)
    account_id    BIGINT       NOT NULL,   -- accounts.id at event time
    action        TEXT         NOT NULL CHECK (action IN ('restricted', 'unrestricted')),
    actor         TEXT         NOT NULL CHECK (actor IN ('user', 'admin')),
    restricted_at TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

-- Lookup: all audit entries for a given user, newest first.
CREATE INDEX IF NOT EXISTS restriction_audit_log_user_id_idx
    ON restriction_audit_log (user_id, restricted_at DESC);
