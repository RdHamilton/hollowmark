-- Drop indexes first
DROP INDEX IF EXISTS idx_draft_match_results_session;
DROP INDEX IF EXISTS idx_draft_match_results_timestamp;
DROP INDEX IF EXISTS idx_draft_archetype_stats_set;
DROP INDEX IF EXISTS idx_draft_community_comparison_set;
DROP INDEX IF EXISTS idx_draft_temporal_trends_period;

-- Drop tables
-- CASCADE guards against incomplete later downs and dirty states. On a correct
-- sequential down, dependents are already gone before this migration runs;
-- CASCADE is a safety net for partial failures and future FK additions that
-- lack a corresponding down update.
DROP TABLE IF EXISTS draft_pattern_analysis CASCADE;
DROP TABLE IF EXISTS draft_temporal_trends CASCADE;
DROP TABLE IF EXISTS draft_community_comparison CASCADE;
DROP TABLE IF EXISTS draft_archetype_stats CASCADE;
DROP TABLE IF EXISTS draft_match_results CASCADE;