-- Drop indexes first
DROP INDEX IF EXISTS idx_draft_card_ratings_set;
DROP INDEX IF EXISTS idx_draft_card_ratings_arena_id;
DROP INDEX IF EXISTS idx_draft_card_ratings_gihwr;
DROP INDEX IF EXISTS idx_draft_color_ratings_set;
DROP INDEX IF EXISTS idx_draft_color_ratings_win_rate;

-- Drop tables
-- CASCADE guards against incomplete later downs and dirty states. On a correct
-- sequential down, dependents are already gone before this migration runs;
-- CASCADE is a safety net for partial failures and future FK additions that
-- lack a corresponding down update.
DROP TABLE IF EXISTS draft_card_ratings CASCADE;
DROP TABLE IF EXISTS draft_color_ratings CASCADE;