-- Rollback: Remove opponent deck analysis tables
-- CASCADE guards against incomplete later downs and dirty states. On a correct
-- sequential down, dependents are already gone before this migration runs;
-- CASCADE is a safety net for partial failures and future FK additions that
-- lack a corresponding down update.
DROP TABLE IF EXISTS archetype_expected_cards CASCADE;
DROP TABLE IF EXISTS matchup_statistics CASCADE;
DROP TABLE IF EXISTS opponent_deck_profiles CASCADE;
ALTER TABLE IF EXISTS deck_performance_history DROP COLUMN IF EXISTS opponent_cards_seen;
ALTER TABLE IF EXISTS deck_performance_history DROP COLUMN IF EXISTS opponent_confidence;
ALTER TABLE IF EXISTS deck_performance_history DROP COLUMN IF EXISTS opponent_color_identity;
