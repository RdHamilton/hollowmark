-- Reverse: drop all composite account_id indexes added in 000053 up migration.
-- CONCURRENTLY removed: golang-migrate wraps each migration in a transaction
-- by default; DROP INDEX CONCURRENTLY is forbidden inside a transaction block.

DROP INDEX IF EXISTS idx_matches_account_id_timestamp;
DROP INDEX IF EXISTS idx_matches_account_id_format;
DROP INDEX IF EXISTS idx_matches_account_id_format_timestamp;

DROP INDEX IF EXISTS idx_draft_sessions_account_id_created_at;
DROP INDEX IF EXISTS idx_draft_sessions_account_id_set_code;

DROP INDEX IF EXISTS idx_player_stats_account_id_date;
DROP INDEX IF EXISTS idx_player_stats_account_id_format_date;

DROP INDEX IF EXISTS idx_decks_account_id_modified_at;
DROP INDEX IF EXISTS idx_decks_account_id_format;

DROP INDEX IF EXISTS idx_currency_history_account_id_timestamp_desc;

DROP INDEX IF EXISTS idx_matchup_stats_account_id_format;
DROP INDEX IF EXISTS idx_matchup_stats_account_id_format_archetype;

DROP INDEX IF EXISTS idx_deck_perf_history_account_id_timestamp;
DROP INDEX IF EXISTS idx_deck_perf_history_account_id_format;
