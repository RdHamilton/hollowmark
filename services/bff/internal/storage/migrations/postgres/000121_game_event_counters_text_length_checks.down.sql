-- Rollback migration 000121: remove CHECK length constraints from game_event_counters.
-- Ticket: #621

ALTER TABLE game_event_counters
    DROP CONSTRAINT IF EXISTS chk_game_event_counters_counter_type_len;

ALTER TABLE game_event_counters
    DROP CONSTRAINT IF EXISTS chk_game_event_counters_controller_len;
