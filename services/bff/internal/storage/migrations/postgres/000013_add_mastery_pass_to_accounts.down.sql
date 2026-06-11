-- Remove mastery pass columns from accounts table
ALTER TABLE IF EXISTS accounts DROP COLUMN IF EXISTS mastery_level;
ALTER TABLE IF EXISTS accounts DROP COLUMN IF EXISTS mastery_pass;
ALTER TABLE IF EXISTS accounts DROP COLUMN IF EXISTS mastery_max;
