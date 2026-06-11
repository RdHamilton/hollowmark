-- Rollback card metadata tables

DROP INDEX IF EXISTS idx_sets_released_at;
DROP INDEX IF EXISTS idx_cards_last_updated;
DROP INDEX IF EXISTS idx_cards_set;
DROP INDEX IF EXISTS idx_cards_name;
DROP INDEX IF EXISTS idx_cards_arena_id;

-- CASCADE guards against incomplete later downs and dirty states. On a correct
-- sequential down, dependents are already gone before this migration runs;
-- CASCADE is a safety net for partial failures and future FK additions that
-- lack a corresponding down update.
DROP TABLE IF EXISTS sets CASCADE;
DROP TABLE IF EXISTS cards CASCADE;
