-- Rollback initial schema
-- Drops all tables in reverse order of dependencies

-- CASCADE guards against incomplete later downs and dirty states. On a correct
-- sequential down, dependents are already gone before this migration runs;
-- CASCADE is a safety net for partial failures and future FK additions that
-- lack a corresponding down update.
DROP TABLE IF EXISTS draft_events CASCADE;
DROP TABLE IF EXISTS rank_history CASCADE;
DROP TABLE IF EXISTS collection_history CASCADE;
DROP TABLE IF EXISTS collection CASCADE;
DROP TABLE IF EXISTS deck_cards CASCADE;
DROP TABLE IF EXISTS decks CASCADE;
DROP TABLE IF EXISTS player_stats CASCADE;
DROP TABLE IF EXISTS games CASCADE;
DROP TABLE IF EXISTS matches CASCADE;
