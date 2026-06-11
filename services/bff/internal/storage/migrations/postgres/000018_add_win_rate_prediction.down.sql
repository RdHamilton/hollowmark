-- Remove win rate prediction fields from draft_sessions table
DROP INDEX IF EXISTS idx_draft_sessions_predicted_win_rate;
ALTER TABLE IF EXISTS draft_sessions DROP COLUMN IF EXISTS predicted_at;
ALTER TABLE IF EXISTS draft_sessions DROP COLUMN IF EXISTS prediction_factors;
ALTER TABLE IF EXISTS draft_sessions DROP COLUMN IF EXISTS predicted_win_rate_max;
ALTER TABLE IF EXISTS draft_sessions DROP COLUMN IF EXISTS predicted_win_rate_min;
ALTER TABLE IF EXISTS draft_sessions DROP COLUMN IF EXISTS predicted_win_rate;
