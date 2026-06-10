-- Migration 000110: create consent_log table.
--
-- Records every consent event (signup ToS/PP acceptance, COPPA gate,
-- cookie opt-in/out, install-dialog accept) as an append-only audit trail.
--
-- PII BOUNDARY: no direct identifiers stored verbatim.
--   account_id  — internal int64 FK (not raw Clerk user ID or email)
--   ip_address_hash — SHA-256 hex[:16] of raw IP; never the raw address
--   metadata    — nullable JSONB; application layer hashes/omits PII before write
--
-- APPEND-ONLY: enforced at the application layer (INSERT-only repository
-- interface). No UPDATE or DELETE methods are exposed by ConsentLogRepository.
-- A DB-level trigger may be added in a follow-on ticket if counsel requires
-- DB-level proof; the INSERT-only interface is sufficient for beta.
--
-- RETENTION ON ART.17 ERASURE (coupling with #891):
--   account_id is ON DELETE SET NULL (not RESTRICT, not CASCADE).
--   The #891 erasure cascade (Step 4) will:
--     (a) UPDATE consent_log SET ip_address_hash=NULL, metadata=NULL
--         WHERE account_id=$erased_account_id  -- anonymize PII linkage
--     (b) DELETE FROM accounts WHERE id=$erased_account_id
--         which triggers this SET NULL cascade, nulling account_id.
--   Result: evidence row retained (event_type, tos_version, consented_at);
--   PII linkage (account_id, ip hash, metadata) fully severed.
--
-- INDEX STRATEGY (db-sop.md):
--   account_id is leading in both composite indexes.
--   Primary access: all events for an account ordered by time.
--   Secondary access: latest event of a specific type per account
--   (used by #890 processing-restriction check and COPPA gate).

CREATE TABLE consent_log (
    id                      BIGSERIAL    PRIMARY KEY,
    account_id              BIGINT                              -- nullable: SET NULL on account erasure
                                         REFERENCES accounts(id)
                                         ON DELETE SET NULL,
    event_type              TEXT         NOT NULL
                                         CHECK (event_type IN (
                                             'signup',
                                             'coppa_gate',
                                             'cookie_accept',
                                             'cookie_decline',
                                             'install_dialog'
                                         )),
    tos_version             TEXT,                              -- null for non-ToS events
    privacy_policy_version  TEXT,                              -- null for non-PP events
    ip_address_hash         TEXT,                              -- SHA-256 hex[:16]; null post-erasure or server-initiated
    consented_at            TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    metadata                JSONB                              -- nullable; extensible per event type
);

-- Primary access pattern: all consent events for an account, newest first.
CREATE INDEX consent_log_account_id_consented_at_idx
    ON consent_log (account_id, consented_at DESC);

-- Secondary pattern: latest consent for a specific event type per account
-- (used by #890 restriction check and the COPPA gate).
CREATE INDEX consent_log_account_id_event_type_idx
    ON consent_log (account_id, event_type, consented_at DESC);
