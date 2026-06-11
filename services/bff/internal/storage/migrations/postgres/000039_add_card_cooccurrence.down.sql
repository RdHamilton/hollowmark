DROP INDEX IF EXISTS idx_frequency_format;
DROP INDEX IF EXISTS idx_frequency_card;
-- CASCADE guards against incomplete later downs and dirty states. On a correct
-- sequential down, dependents are already gone before this migration runs;
-- CASCADE is a safety net for partial failures and future FK additions that
-- lack a corresponding down update.
DROP TABLE IF EXISTS card_frequency CASCADE;
DROP TABLE IF EXISTS cooccurrence_sources CASCADE;DROP INDEX IF EXISTS idx_cooccurrence_pmi;
DROP INDEX IF EXISTS idx_cooccurrence_format;
DROP INDEX IF EXISTS idx_cooccurrence_card_b;
DROP INDEX IF EXISTS idx_cooccurrence_card_a;
DROP TABLE IF EXISTS card_cooccurrence CASCADE;