-- Rollback: Remove deck permutation tracking

DROP INDEX IF EXISTS idx_decks_current_permutation;
ALTER TABLE IF EXISTS decks DROP COLUMN IF EXISTS current_permutation_id;
-- CASCADE guards against incomplete later downs and dirty states. On a correct
-- sequential down, dependents are already gone before this migration runs;
-- CASCADE is a safety net for partial failures and future FK additions that
-- lack a corresponding down update.
DROP TABLE IF EXISTS deck_permutations CASCADE;