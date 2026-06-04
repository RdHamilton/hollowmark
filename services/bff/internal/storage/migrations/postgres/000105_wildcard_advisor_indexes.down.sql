-- Down migration for 000105_wildcard_advisor_indexes
DROP INDEX IF EXISTS idx_draft_card_ratings_arena_format;
DROP INDEX IF EXISTS idx_set_cards_name_lower;
