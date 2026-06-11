-- Migration 000123: add mailchimp_attempts column to waitlist_entries.
-- Ticket: hollowmark-tickets#126
--
-- Design notes:
--   * INTEGER NOT NULL DEFAULT 0 — zero means "no reconciler attempt yet".
--     The handler's initial Mailchimp call is best-effort and does NOT increment
--     this counter; only the reconciler does.
--   * Terminal threshold: reconciler stops retrying when mailchimp_attempts >= 10
--     and sets mailchimp_status = 'terminal'.
--   * Manual recovery of a 'terminal' row requires resetting BOTH
--     mailchimp_status = 'failed' AND mailchimp_attempts = 0 — resetting only
--     one of the two is insufficient to re-enter the reconciler's retry window.
--     Example:
--       UPDATE waitlist_entries
--       SET mailchimp_status = 'failed', mailchimp_attempts = 0, updated_at = now()
--       WHERE id = '<row-uuid>';
--   * IF NOT EXISTS guard makes the migration re-runnable (idempotent).

ALTER TABLE waitlist_entries
    ADD COLUMN IF NOT EXISTS mailchimp_attempts INTEGER NOT NULL DEFAULT 0;

COMMENT ON COLUMN waitlist_entries.mailchimp_attempts IS
    'Number of Mailchimp AddMember attempts made by the reconciler (not the initial handler call). '
    'Terminal threshold: reconciler marks mailchimp_status=''terminal'' when this reaches 10. '
    'Manual recovery requires resetting BOTH mailchimp_status=''failed'' AND mailchimp_attempts=0.';
