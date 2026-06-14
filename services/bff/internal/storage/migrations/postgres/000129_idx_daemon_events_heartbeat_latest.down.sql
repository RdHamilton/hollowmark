-- Reverse migration 000129: drop the heartbeat-latest partial index.
DROP INDEX IF EXISTS idx_daemon_events_heartbeat_latest;
