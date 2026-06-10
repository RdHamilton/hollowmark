-- Migration 000111: add COPPA columns to users table.
--
-- date_of_birth_year: the year of birth supplied during the COPPA age gate (#884).
--   Storing only the year (not full DOB) is the minimum data needed to verify
--   the 13-year threshold and minimises PII surface (GDPR data-minimisation).
--
-- coppa_restricted: set TRUE when date_of_birth_year indicates the user is
--   under 13. Checked by the COPPA gate handler (#884) and by the consent log
--   write path on coppa_gate events.
--
-- These columns live on users (not consent_log) because they describe the
-- account's STANDING RESTRICTION STATUS — not a single consent event.
-- The consent_log records the gate event; users records the outcome.
--
-- The partial index below is for compliance/admin queries scoped to restricted
-- accounts only — no full-table scan needed to find COPPA-restricted users.

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS date_of_birth_year SMALLINT,
    ADD COLUMN IF NOT EXISTS coppa_restricted   BOOLEAN NOT NULL DEFAULT FALSE;

-- Partial index: only index rows where coppa_restricted = TRUE.
-- Keeps the index tiny (only a handful of restricted accounts expected at beta).
CREATE INDEX IF NOT EXISTS users_coppa_restricted_idx
    ON users (coppa_restricted)
    WHERE coppa_restricted = TRUE;
