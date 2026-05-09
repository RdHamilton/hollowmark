-- Migration 000075: create daemon_api_keys table for daemon registration.
-- One API key per Clerk user account (UNIQUE on account_id).
-- device_id uniquely identifies the daemon installation (one device per account).
-- Key is stored as a bcrypt hash; plaintext is returned once on creation and never persisted.
-- See ADR-020 for full design notes.

CREATE TABLE IF NOT EXISTS daemon_api_keys (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id  TEXT        NOT NULL,
    key_hash    TEXT        NOT NULL,
    key_prefix  TEXT        NOT NULL,
    device_id   UUID        NOT NULL,
    platform    TEXT        NOT NULL,
    daemon_ver  TEXT        NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_used   TIMESTAMPTZ,
    revoked_at  TIMESTAMPTZ,
    CONSTRAINT daemon_api_keys_account_id_unique UNIQUE (account_id),
    CONSTRAINT daemon_api_keys_device_id_unique  UNIQUE (device_id)
);

CREATE INDEX IF NOT EXISTS daemon_api_keys_account_id_idx ON daemon_api_keys (account_id);
CREATE INDEX IF NOT EXISTS daemon_api_keys_device_id_idx  ON daemon_api_keys (device_id);
