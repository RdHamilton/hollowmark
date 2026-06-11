-- Rollback opponent tracking and game-level result reasons

DROP INDEX IF EXISTS idx_games_result_reason;
ALTER TABLE IF EXISTS games DROP COLUMN IF EXISTS result_reason;

DROP INDEX IF EXISTS idx_matches_opponent_id;
DROP INDEX IF EXISTS idx_matches_opponent_name;
ALTER TABLE IF EXISTS matches DROP COLUMN IF EXISTS opponent_id;
ALTER TABLE IF EXISTS matches DROP COLUMN IF EXISTS opponent_name;
