-- Migration 000123 down: remove mailchimp_attempts from waitlist_entries.
ALTER TABLE waitlist_entries DROP COLUMN IF EXISTS mailchimp_attempts;
