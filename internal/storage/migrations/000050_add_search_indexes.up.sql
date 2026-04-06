-- Add index on set_cards.name to improve ORDER BY performance for card search queries.
CREATE INDEX IF NOT EXISTS idx_set_cards_name ON set_cards(name);
