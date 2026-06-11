-- Remove data_source columns from existing tables
ALTER TABLE IF EXISTS draft_color_ratings DROP COLUMN IF EXISTS data_source;
ALTER TABLE IF EXISTS draft_card_ratings DROP COLUMN IF EXISTS data_source;

-- Drop dataset_metadata table
-- CASCADE guards against incomplete later downs and dirty states. On a correct
-- sequential down, dependents are already gone before this migration runs;
-- CASCADE is a safety net for partial failures and future FK additions that
-- lack a corresponding down update.
DROP TABLE IF EXISTS dataset_metadata CASCADE;
