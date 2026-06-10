-- Migration 000111 rollback: remove COPPA columns from users table.
DROP INDEX IF EXISTS users_coppa_restricted_idx;

ALTER TABLE users
    DROP COLUMN IF EXISTS coppa_restricted,
    DROP COLUMN IF EXISTS date_of_birth_year;
