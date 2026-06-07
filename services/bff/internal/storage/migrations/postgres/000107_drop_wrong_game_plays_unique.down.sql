-- Irreversible by design (re-adding re-breaks the per-turn backfill); present
-- only for migrate-down symmetry on non-prod. Never expected to run on prod.
ALTER TABLE game_plays
    ADD CONSTRAINT uq_game_plays_account_match_game
        UNIQUE (account_id, match_id, game_number);
