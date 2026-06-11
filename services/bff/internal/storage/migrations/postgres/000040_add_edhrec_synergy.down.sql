DROP INDEX IF EXISTS idx_edhrec_theme_cards_card;
DROP INDEX IF EXISTS idx_edhrec_theme_cards_theme;
-- CASCADE guards against incomplete later downs and dirty states. On a correct
-- sequential down, dependents are already gone before this migration runs;
-- CASCADE is a safety net for partial failures and future FK additions that
-- lack a corresponding down update.
DROP TABLE IF EXISTS edhrec_theme_cards CASCADE;DROP INDEX IF EXISTS idx_edhrec_metadata_name;
DROP TABLE IF EXISTS edhrec_card_metadata CASCADE;DROP INDEX IF EXISTS idx_edhrec_synergy_score;
DROP INDEX IF EXISTS idx_edhrec_synergy_card;
DROP TABLE IF EXISTS edhrec_synergy CASCADE;