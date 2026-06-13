-- Rollback 000125: remove arena_id_source from set_cards.
ALTER TABLE set_cards
    DROP COLUMN IF EXISTS arena_id_source;
