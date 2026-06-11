-- Rollback v1.3 Deck Builder support

DROP INDEX IF EXISTS idx_deck_tags_tag;
DROP INDEX IF EXISTS idx_deck_tags_deck_id;
-- CASCADE guards against incomplete later downs and dirty states. On a correct
-- sequential down, dependents are already gone before this migration runs;
-- CASCADE is a safety net for partial failures and future FK additions that
-- lack a corresponding down update.
DROP TABLE IF EXISTS deck_tags CASCADE;
DROP INDEX IF EXISTS idx_decks_draft_event_id;
DROP INDEX IF EXISTS idx_decks_source;

ALTER TABLE IF EXISTS deck_cards DROP COLUMN IF EXISTS from_draft_pick;
ALTER TABLE IF EXISTS decks DROP COLUMN IF EXISTS games_won;
ALTER TABLE IF EXISTS decks DROP COLUMN IF EXISTS games_played;
ALTER TABLE IF EXISTS decks DROP COLUMN IF EXISTS matches_won;
ALTER TABLE IF EXISTS decks DROP COLUMN IF EXISTS matches_played;
ALTER TABLE IF EXISTS decks DROP COLUMN IF EXISTS draft_event_id;
ALTER TABLE IF EXISTS decks DROP COLUMN IF EXISTS source;
