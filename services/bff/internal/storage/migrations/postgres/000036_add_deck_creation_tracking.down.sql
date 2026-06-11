-- Remove deck creation tracking fields
DROP INDEX IF EXISTS idx_decks_app_created;
DROP INDEX IF EXISTS idx_decks_created_method;
ALTER TABLE IF EXISTS decks DROP COLUMN IF EXISTS seed_card_id;
ALTER TABLE IF EXISTS decks DROP COLUMN IF EXISTS created_method;
ALTER TABLE IF EXISTS decks DROP COLUMN IF EXISTS is_app_created;
