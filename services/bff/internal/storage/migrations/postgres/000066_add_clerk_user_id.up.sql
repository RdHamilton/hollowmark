-- Add Clerk identity fields to the users table.
-- clerk_user_id is Clerk's canonical user identifier (e.g. "user_2abc...").
-- subscription_tier captures the app-level billing tier; kept distinct from
-- subscription_status to allow independent evolution of each concept.

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS clerk_user_id   TEXT,
    ADD COLUMN IF NOT EXISTS subscription_tier TEXT NOT NULL DEFAULT 'free'
        CHECK (subscription_tier IN ('free', 'pro'));

-- Unique index on clerk_user_id (partial: only for rows that have one set,
-- supporting legacy rows that pre-date Clerk integration).
CREATE UNIQUE INDEX idx_users_clerk_user_id
    ON users(clerk_user_id)
    WHERE clerk_user_id IS NOT NULL;
