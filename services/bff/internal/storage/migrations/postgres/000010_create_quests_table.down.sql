-- Drop quest indexes
DROP INDEX IF EXISTS idx_quests_completed_at;
DROP INDEX IF EXISTS idx_quests_assigned_at;
DROP INDEX IF EXISTS idx_quests_completed;

-- Drop quests table
-- CASCADE guards against incomplete later downs and dirty states. On a correct
-- sequential down, dependents are already gone before this migration runs;
-- CASCADE is a safety net for partial failures and future FK additions that
-- lack a corresponding down update.
DROP TABLE IF EXISTS quests CASCADE;
