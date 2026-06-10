-- Migration 000118 down: drop FK daemon_events.user_id → users(id)
-- The user_id column itself (BIGINT NOT NULL, from migration 000061) is retained.
ALTER TABLE daemon_events DROP CONSTRAINT IF EXISTS fk_daemon_events_user_id;
