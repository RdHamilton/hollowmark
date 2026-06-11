-- Rollback v1.3 Deck Builder support

DROP INDEX IF EXISTS idx_deck_tags_tag;
DROP INDEX IF EXISTS idx_deck_tags_deck_id;
DROP TABLE IF EXISTS deck_tags;

DROP INDEX IF EXISTS idx_decks_draft_event_id;
DROP INDEX IF EXISTS idx_decks_source;

ALTER TABLE IF EXISTS deck_cards DROP COLUMN IF EXISTS from_draft_pick;
ALTER TABLE IF EXISTS decks DROP COLUMN IF EXISTS games_won;
ALTER TABLE IF EXISTS decks DROP COLUMN IF EXISTS games_played;
ALTER TABLE IF EXISTS decks DROP COLUMN IF EXISTS matches_won;
ALTER TABLE IF EXISTS decks DROP COLUMN IF EXISTS matches_played;
ALTER TABLE IF EXISTS decks DROP COLUMN IF EXISTS draft_event_id;
ALTER TABLE IF EXISTS decks DROP COLUMN IF EXISTS source;
