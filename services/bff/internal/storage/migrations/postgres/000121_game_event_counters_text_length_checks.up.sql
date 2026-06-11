-- Migration 000121: add CHECK length constraints on game_event_counters TEXT columns.
-- Ticket: #621 (Sarah S-07 follow-up from PR #2861)
--
-- counter_type and controller are daemon-supplied TEXT values.  Without an upper
-- bound a misbehaving daemon build or crafted payload could insert arbitrarily
-- long strings.  These CHECK constraints cap them at 64 characters — generous
-- for all known GRE values (e.g. "loyalty", "+1/+1", "poison", "player",
-- "opponent") while bounding storage and rejecting obviously invalid payloads.
--
-- ADD CONSTRAINT NOT VALID: avoids a full-table scan under a heavy lock.
-- VALIDATE CONSTRAINT:      runs under SHARE UPDATE EXCLUSIVE — reads and writes
--                           proceed concurrently.  Guarded by an information_schema
--                           existence check so re-runs are safe.
--
-- golang-migrate wraps this in a transaction.

-- counter_type: cap at 64 characters.
DO $$ BEGIN
    ALTER TABLE game_event_counters
        ADD CONSTRAINT chk_game_event_counters_counter_type_len
        CHECK (char_length(counter_type) <= 64) NOT VALID;
EXCEPTION WHEN duplicate_object THEN NULL; END $$;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.table_constraints
               WHERE constraint_name = 'chk_game_event_counters_counter_type_len'
                 AND table_name      = 'game_event_counters') THEN
        ALTER TABLE game_event_counters
            VALIDATE CONSTRAINT chk_game_event_counters_counter_type_len;
    END IF;
END $$;

-- controller: cap at 64 characters.
DO $$ BEGIN
    ALTER TABLE game_event_counters
        ADD CONSTRAINT chk_game_event_counters_controller_len
        CHECK (char_length(controller) <= 64) NOT VALID;
EXCEPTION WHEN duplicate_object THEN NULL; END $$;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.table_constraints
               WHERE constraint_name = 'chk_game_event_counters_controller_len'
                 AND table_name      = 'game_event_counters') THEN
        ALTER TABLE game_event_counters
            VALIDATE CONSTRAINT chk_game_event_counters_controller_len;
    END IF;
END $$;
