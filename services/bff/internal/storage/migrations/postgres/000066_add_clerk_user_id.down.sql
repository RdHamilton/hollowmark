-- Reverse 000066: remove Clerk identity fields from users table.

DROP INDEX IF EXISTS idx_users_clerk_user_id;

ALTER TABLE users
    DROP COLUMN IF EXISTS subscription_tier,
    DROP COLUMN IF EXISTS clerk_user_id;
