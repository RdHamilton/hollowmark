-- Rollback migration 000122: drop utm_content and utm_term from waitlist_entries.

ALTER TABLE waitlist_entries
    DROP COLUMN IF EXISTS utm_content,
    DROP COLUMN IF EXISTS utm_term;
