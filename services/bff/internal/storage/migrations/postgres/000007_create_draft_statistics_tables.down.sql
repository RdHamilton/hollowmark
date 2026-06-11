-- Drop draft statistics tables
DROP INDEX IF EXISTS idx_draft_colors_staleness;
DROP INDEX IF EXISTS idx_draft_colors_event;
DROP INDEX IF EXISTS idx_draft_colors_expansion;
-- CASCADE guards against incomplete later downs and dirty states. On a correct
-- sequential down, dependents are already gone before this migration runs;
-- CASCADE is a safety net for partial failures and future FK additions that
-- lack a corresponding down update.
DROP TABLE IF EXISTS draft_color_ratings CASCADE;
DROP INDEX IF EXISTS idx_draft_ratings_staleness;
DROP INDEX IF EXISTS idx_draft_ratings_format;
DROP INDEX IF EXISTS idx_draft_ratings_expansion;
DROP INDEX IF EXISTS idx_draft_ratings_arena_id;
DROP TABLE IF EXISTS draft_card_ratings CASCADE;