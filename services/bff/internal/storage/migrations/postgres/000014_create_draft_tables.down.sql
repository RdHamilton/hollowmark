-- Drop indexes first
DROP INDEX IF EXISTS idx_draft_sessions_status;
DROP INDEX IF EXISTS idx_draft_sessions_set_code;
DROP INDEX IF EXISTS idx_draft_sessions_start_time;
DROP INDEX IF EXISTS idx_draft_picks_session;
DROP INDEX IF EXISTS idx_draft_picks_timestamp;
DROP INDEX IF EXISTS idx_draft_packs_session;
DROP INDEX IF EXISTS idx_set_cards_arena_id;
DROP INDEX IF EXISTS idx_set_cards_set_code;
DROP INDEX IF EXISTS idx_set_cards_scryfall_id;

-- Drop tables
-- CASCADE guards against incomplete later downs and dirty states. On a correct
-- sequential down, dependents are already gone before this migration runs;
-- CASCADE is a safety net for partial failures and future FK additions that
-- lack a corresponding down update.
DROP TABLE IF EXISTS draft_picks CASCADE;
DROP TABLE IF EXISTS draft_packs CASCADE;
DROP TABLE IF EXISTS draft_sessions CASCADE;
DROP TABLE IF EXISTS set_cards CASCADE;
