-- Rollback: Remove Standard legality tracking

-- CASCADE guards against incomplete later downs and dirty states. On a correct
-- sequential down, dependents are already gone before this migration runs;
-- CASCADE is a safety net for partial failures and future FK additions that
-- lack a corresponding down update.
DROP TABLE IF EXISTS standard_config CASCADE;
DROP INDEX IF EXISTS idx_sets_standard;
DROP INDEX IF EXISTS idx_set_cards_legalities;
ALTER TABLE IF EXISTS set_cards DROP COLUMN IF EXISTS legalities;
ALTER TABLE IF EXISTS sets DROP COLUMN IF EXISTS rotation_date;
ALTER TABLE IF EXISTS sets DROP COLUMN IF EXISTS is_standard_legal;
