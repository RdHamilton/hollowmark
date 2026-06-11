-- Rollback: Remove in-game play tracking tables
DROP INDEX IF EXISTS idx_opponent_cards_match_id;
DROP INDEX IF EXISTS idx_opponent_cards_game_id;
DROP INDEX IF EXISTS idx_game_snapshots_match_id;
DROP INDEX IF EXISTS idx_game_snapshots_game_id;
DROP INDEX IF EXISTS idx_game_plays_turn;
DROP INDEX IF EXISTS idx_game_plays_match_id;
DROP INDEX IF EXISTS idx_game_plays_game_id;

-- CASCADE guards against incomplete later downs and dirty states. On a correct
-- sequential down, dependents are already gone before this migration runs;
-- CASCADE is a safety net for partial failures and future FK additions that
-- lack a corresponding down update.
DROP TABLE IF EXISTS opponent_cards_observed CASCADE;
DROP TABLE IF EXISTS game_state_snapshots CASCADE;
DROP TABLE IF EXISTS game_plays CASCADE;