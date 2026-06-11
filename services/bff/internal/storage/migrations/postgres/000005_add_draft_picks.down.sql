-- Rollback draft_picks table
DROP INDEX IF EXISTS idx_draft_picks_selected_card;
DROP INDEX IF EXISTS idx_draft_picks_timestamp;
DROP INDEX IF EXISTS idx_draft_picks_event;
-- CASCADE guards against incomplete later downs and dirty states. On a correct
-- sequential down, dependents are already gone before this migration runs;
-- CASCADE is a safety net for partial failures and future FK additions that
-- lack a corresponding down update.
DROP TABLE IF EXISTS draft_picks CASCADE;
