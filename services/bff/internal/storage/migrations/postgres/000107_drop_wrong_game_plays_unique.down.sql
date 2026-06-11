-- Irreversible by design (re-adding re-breaks the per-turn backfill); present
-- only for migrate-down symmetry on non-prod. Never expected to run on prod.
--
-- The constraint references game_number, which only exists on the fresh-init
-- (000054/000073) path.  On the incremental path, 000030 created game_plays
-- without game_number so the column does not exist; attempting to ADD the
-- constraint would error.  Guard the statement so the down is a no-op on
-- incremental-path DBs, matching the up migration's own IF EXISTS guard.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = 'public'
          AND table_name   = 'game_plays'
          AND column_name  = 'game_number'
    ) THEN
        ALTER TABLE game_plays
            ADD CONSTRAINT uq_game_plays_account_match_game
                UNIQUE (account_id, match_id, game_number);
    END IF;
END
$$;
