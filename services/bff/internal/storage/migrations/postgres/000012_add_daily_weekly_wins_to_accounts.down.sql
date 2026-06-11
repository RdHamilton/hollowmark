-- Remove daily and weekly wins columns from accounts table
ALTER TABLE IF EXISTS accounts DROP COLUMN IF EXISTS daily_wins;
ALTER TABLE IF EXISTS accounts DROP COLUMN IF EXISTS weekly_wins;
