-- Revert arena source and NOT NULL constraint on account_id (PostgreSQL)
ALTER TABLE IF EXISTS decks DROP CONSTRAINT IF EXISTS fk_decks_account_id;
ALTER TABLE IF EXISTS decks DROP CONSTRAINT IF EXISTS decks_source_check;
ALTER TABLE IF EXISTS decks ALTER COLUMN account_id DROP NOT NULL;
ALTER TABLE IF EXISTS decks ALTER COLUMN account_id DROP DEFAULT;
ALTER TABLE IF EXISTS decks ADD CONSTRAINT decks_source_check
    CHECK(source IN ('draft', 'constructed', 'imported'));
