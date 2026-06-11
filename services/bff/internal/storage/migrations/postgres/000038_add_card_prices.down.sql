-- Remove price fields from set_cards table
DROP INDEX IF EXISTS idx_set_cards_prices;
ALTER TABLE IF EXISTS set_cards DROP COLUMN IF EXISTS prices_updated_at;
ALTER TABLE IF EXISTS set_cards DROP COLUMN IF EXISTS price_tix;
ALTER TABLE IF EXISTS set_cards DROP COLUMN IF EXISTS price_eur_foil;
ALTER TABLE IF EXISTS set_cards DROP COLUMN IF EXISTS price_eur;
ALTER TABLE IF EXISTS set_cards DROP COLUMN IF EXISTS price_usd_foil;
ALTER TABLE IF EXISTS set_cards DROP COLUMN IF EXISTS price_usd;
