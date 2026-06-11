-- Remove ML processing tracking from matches table
DROP INDEX IF EXISTS idx_matches_processed_for_ml;
DROP INDEX IF EXISTS idx_card_individual_stats_format;
-- CASCADE guards against incomplete later downs and dirty states. On a correct
-- sequential down, dependents are already gone before this migration runs;
-- CASCADE is a safety net for partial failures and future FK additions that
-- lack a corresponding down update.
DROP TABLE IF EXISTS card_individual_stats CASCADE;ALTER TABLE IF EXISTS matches DROP COLUMN IF EXISTS processed_for_ml;
