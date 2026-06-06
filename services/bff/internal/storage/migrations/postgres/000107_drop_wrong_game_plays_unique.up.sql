-- Migration 000107: drop the semantically-wrong per-game uniqueness left on the
-- per-turn game_plays table by 000073. game_plays is PER-TURN (real uniqueness =
-- idx_game_plays_unique (game_id, sequence_number), 000047/000101). Per-game
-- uniqueness correctly lives on match_game_results (000100). 000073 created
-- uq_game_plays_account_match_game and no migration dropped it; the 000106
-- account_id backfill collides on it on staging-lineage DBs.
-- PROD-SAFETY: prod came up the incremental path; 000073's CREATE TABLE IF NOT
-- EXISTS was a no-op, so prod never acquired this constraint. DROP ... IF EXISTS
-- is a NO-OP on prod and effective on staging-lineage DBs. Forward-only.
ALTER TABLE game_plays DROP CONSTRAINT IF EXISTS uq_game_plays_account_match_game;
