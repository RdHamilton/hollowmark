-- Migration 000122: add utm_content and utm_term columns to waitlist_entries.
-- Ticket: hollowmark-tickets#130
--
-- Design notes:
--   * Nullable TEXT columns — matching the type/constraints of utm_source,
--     utm_medium, and utm_campaign added in migration 000086.
--   * No NOT NULL constraint — historical rows (before this migration) will
--     be NULL, which is the correct representation.
--   * No default value — NULL is the intended sentinel for "not provided".
--   * IF NOT EXISTS guards make the migration re-runnable (idempotent).

ALTER TABLE waitlist_entries
    ADD COLUMN IF NOT EXISTS utm_content TEXT,
    ADD COLUMN IF NOT EXISTS utm_term    TEXT;
