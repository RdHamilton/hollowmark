-- Rollback migration 000103.
ALTER TABLE match_game_results
    DROP COLUMN IF EXISTS player_on_play;
