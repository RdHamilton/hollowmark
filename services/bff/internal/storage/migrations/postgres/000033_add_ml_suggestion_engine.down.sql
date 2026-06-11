-- Rollback ML Suggestion Engine Schema
-- CASCADE guards against incomplete later downs and dirty states. On a correct
-- sequential down, dependents are already gone before this migration runs;
-- CASCADE is a safety net for partial failures and future FK additions that
-- lack a corresponding down update.
DROP TABLE IF EXISTS ml_model_metadata CASCADE;
DROP TABLE IF EXISTS user_play_patterns CASCADE;
DROP TABLE IF EXISTS card_affinity CASCADE;
DROP TABLE IF EXISTS ml_suggestions CASCADE;
DROP TABLE IF EXISTS card_combination_stats CASCADE;