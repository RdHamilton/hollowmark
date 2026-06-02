-- Migration 000103: add player_on_play to match_game_results (ticket #687).
--
-- player_on_play stores whether the local player was on the play (true) or
-- draw (false) for a given game. NULL means the daemon did not capture the
-- signal — either the GRE buffer was flushed before the first-turn
-- GameStateMessage arrived, or the match predates this feature.
--
-- The column is added with ADD COLUMN IF NOT EXISTS so the migration is
-- safe to replay on DBs that already have the column.

ALTER TABLE match_game_results
    ADD COLUMN IF NOT EXISTS player_on_play BOOLEAN;
