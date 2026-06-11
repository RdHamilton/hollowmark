-- Rollback: Remove deck performance tracking tables

DROP INDEX IF EXISTS idx_archetype_card_weights_card;
DROP INDEX IF EXISTS idx_archetype_card_weights_archetype;
-- CASCADE guards against incomplete later downs and dirty states. On a correct
-- sequential down, dependents are already gone before this migration runs;
-- CASCADE is a safety net for partial failures and future FK additions that
-- lack a corresponding down update.
DROP TABLE IF EXISTS archetype_card_weights CASCADE;
DROP INDEX IF EXISTS idx_rec_feedback_type_action;
DROP INDEX IF EXISTS idx_rec_feedback_card;
DROP INDEX IF EXISTS idx_rec_feedback_timestamp;
DROP INDEX IF EXISTS idx_rec_feedback_action;
DROP INDEX IF EXISTS idx_rec_feedback_type;
DROP INDEX IF EXISTS idx_rec_feedback_account;
DROP TABLE IF EXISTS recommendation_feedback CASCADE;
DROP INDEX IF EXISTS idx_deck_archetypes_colors;
DROP INDEX IF EXISTS idx_deck_archetypes_format;
DROP INDEX IF EXISTS idx_deck_archetypes_set;
DROP TABLE IF EXISTS deck_archetypes CASCADE;
DROP INDEX IF EXISTS idx_deck_perf_history_archetype_result;
DROP INDEX IF EXISTS idx_deck_perf_history_result;
DROP INDEX IF EXISTS idx_deck_perf_history_timestamp;
DROP INDEX IF EXISTS idx_deck_perf_history_format;
DROP INDEX IF EXISTS idx_deck_perf_history_archetype;
DROP INDEX IF EXISTS idx_deck_perf_history_deck;
DROP INDEX IF EXISTS idx_deck_perf_history_account;
DROP TABLE IF EXISTS deck_performance_history CASCADE;