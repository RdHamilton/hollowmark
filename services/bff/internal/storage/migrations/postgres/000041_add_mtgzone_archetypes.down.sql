DROP INDEX IF EXISTS idx_mtgzone_articles_format;
DROP INDEX IF EXISTS idx_mtgzone_articles_type;
-- CASCADE guards against incomplete later downs and dirty states. On a correct
-- sequential down, dependents are already gone before this migration runs;
-- CASCADE is a safety net for partial failures and future FK additions that
-- lack a corresponding down update.
DROP TABLE IF EXISTS mtgzone_articles CASCADE;

DROP INDEX IF EXISTS idx_mtgzone_synergies_card_b;
DROP INDEX IF EXISTS idx_mtgzone_synergies_card_a;
DROP TABLE IF EXISTS mtgzone_synergies CASCADE;

DROP INDEX IF EXISTS idx_mtgzone_archetype_cards_role;
DROP INDEX IF EXISTS idx_mtgzone_archetype_cards_card;
DROP INDEX IF EXISTS idx_mtgzone_archetype_cards_archetype;
DROP TABLE IF EXISTS mtgzone_archetype_cards CASCADE;

DROP INDEX IF EXISTS idx_mtgzone_archetypes_tier;
DROP INDEX IF EXISTS idx_mtgzone_archetypes_format;
DROP TABLE IF EXISTS mtgzone_archetypes CASCADE;
