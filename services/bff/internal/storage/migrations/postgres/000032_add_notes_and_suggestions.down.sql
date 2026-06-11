-- Drop improvement suggestions table
DROP INDEX IF EXISTS idx_suggestions_dismissed;
DROP INDEX IF EXISTS idx_suggestions_type;
DROP INDEX IF EXISTS idx_suggestions_deck_id;
-- CASCADE guards against incomplete later downs and dirty states. On a correct
-- sequential down, dependents are already gone before this migration runs;
-- CASCADE is a safety net for partial failures and future FK additions that
-- lack a corresponding down update.
DROP TABLE IF EXISTS improvement_suggestions CASCADE;
-- Drop deck notes table
DROP INDEX IF EXISTS idx_deck_notes_category;
DROP INDEX IF EXISTS idx_deck_notes_deck_id;
DROP TABLE IF EXISTS deck_notes CASCADE;
-- Remove notes and rating columns from matches table
ALTER TABLE IF EXISTS matches DROP COLUMN IF EXISTS rating;
ALTER TABLE IF EXISTS matches DROP COLUMN IF EXISTS notes;
