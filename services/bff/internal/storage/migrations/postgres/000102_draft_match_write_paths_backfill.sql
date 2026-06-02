-- Backfill script for migration 000102 (ADR-051).
-- One-time manual run via SSM Session Manager against staging (and production
-- when applicable). Idempotent: safe to run multiple times.
--
-- How to run on staging:
--   aws ssm start-session --target <instance-id> --profile personal
--   psql "$DATABASE_URL" -f /path/to/000102_draft_match_write_paths_backfill.sql
--
-- Step 1: Link existing matches to draft sessions via event_name + 48h window.
-- Only touches rows where draft_session_id IS NULL and event_name contains 'Draft'.
-- Only links a match when exactly one candidate session exists in the window.
WITH candidate AS (
    SELECT
        m.id                        AS match_id,
        m.account_id,
        m.timestamp                 AS match_ts,
        m.event_name,
        m.player_wins,
        m.opponent_wins,
        CASE
            WHEN m.result IN ('win', 'loss') THEN m.result
            ELSE 'loss'
        END                         AS match_result,
        ds.id                       AS session_id,
        ROW_NUMBER() OVER (
            PARTITION BY m.id
            ORDER BY ds.start_time DESC
        )                           AS rn,
        COUNT(*) OVER (PARTITION BY m.id) AS candidate_count
    FROM matches m
    JOIN draft_sessions ds
        ON  ds.account_id = m.account_id
        AND ds.event_name = m.event_name
        AND ds.start_time >= m.timestamp - INTERVAL '48 hours'
        AND ds.start_time <= m.timestamp
    WHERE m.draft_session_id IS NULL
      AND m.event_name LIKE '%Draft%'
)
UPDATE matches
SET draft_session_id = candidate.session_id
FROM candidate
WHERE matches.id           = candidate.match_id
  AND candidate.rn         = 1
  AND candidate.candidate_count = 1;

-- Step 2: Populate draft_match_results from now-linked matches.
-- Idempotent: ON CONFLICT DO NOTHING.
INSERT INTO draft_match_results (session_id, match_id, result, game_wins, game_losses, match_timestamp)
SELECT
    m.draft_session_id,
    m.id,
    CASE
        WHEN m.result IN ('win', 'loss') THEN m.result
        ELSE 'loss'
    END,
    m.player_wins,
    m.opponent_wins,
    m.timestamp
FROM matches m
WHERE m.draft_session_id IS NOT NULL
ON CONFLICT (session_id, match_id) DO NOTHING;

-- Step 3: Update draft_sessions.status = 'completed' for sessions that
-- have an end_time set but are still 'in_progress' (stuck due to the
-- draft.ended → draft.completed rename — Finding 3 in ADR-051).
UPDATE draft_sessions
SET status = 'completed'
WHERE end_time IS NOT NULL
  AND status = 'in_progress';
