-- Rollback migration 000099: drop game_event_counters table (and its indexes,
-- which are dropped automatically when the table is dropped).
DROP TABLE IF EXISTS game_event_counters CASCADE;
