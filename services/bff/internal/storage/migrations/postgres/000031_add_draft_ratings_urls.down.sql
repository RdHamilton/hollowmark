-- Remove URL columns from draft_card_ratings
ALTER TABLE IF EXISTS draft_card_ratings DROP COLUMN IF EXISTS url_back;
ALTER TABLE IF EXISTS draft_card_ratings DROP COLUMN IF EXISTS url;
