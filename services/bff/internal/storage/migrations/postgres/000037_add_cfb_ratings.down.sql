-- Remove ChannelFireball ratings table
DROP INDEX IF EXISTS idx_cfb_ratings_set_code;
DROP INDEX IF EXISTS idx_cfb_ratings_arena_id;
DROP INDEX IF EXISTS idx_cfb_ratings_card_name;
-- CASCADE guards against incomplete later downs and dirty states. On a correct
-- sequential down, dependents are already gone before this migration runs;
-- CASCADE is a safety net for partial failures and future FK additions that
-- lack a corresponding down update.
DROP TABLE IF EXISTS cfb_ratings CASCADE;