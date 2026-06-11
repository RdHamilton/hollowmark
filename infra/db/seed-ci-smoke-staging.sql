-- seed-ci-smoke-staging.sql
--
-- Durable, idempotent seed for the ci-smoke service account on staging.
--
-- PURPOSE
-- -------
-- The E2E staging smoke suite (staging-smoke.spec.ts AC2) authenticates as the
-- ci-smoke Clerk user and asserts that POST /api/v1/matches returns ≥1 match row.
-- Without this seed the assertion is non-deterministic: it passes only if a
-- previous test run happened to ingest matches for this account.
--
-- This script guarantees at least one match row exists for the ci-smoke account
-- at all times, making the AC2 assertion deterministic regardless of prior runs.
--
-- SAFETY
-- ------
-- STAGING ONLY — run-staging-migrations.sh executes this via the master
-- credential after migrations. It is NOT run against production (the
-- run-prod-sql-oninstance.sh / run-prod-sql.yml guard blocks DML on prod).
--
-- IDEMPOTENCY
-- -----------
-- Every INSERT uses ON CONFLICT DO NOTHING keyed on a deterministic fixture ID
-- (ci_smoke_fixture_match_001). Re-running this script is a safe no-op.
--
-- ACCOUNT RESOLUTION
-- ------------------
-- The ci-smoke Clerk user ID is user_3EmtmrSgZrtd0yRRdisTIIFYnnF.
-- The BFF resolves: Clerk JWT sub → users.clerk_user_id → users.id → accounts.id.
-- This script walks the same chain via subqueries so no environment-specific
-- integer IDs are hardcoded. If the users/accounts rows don't exist yet
-- (first-run staging bootstrap) the INSERT is skipped silently — subsequent
-- smoke runs will JIT-provision them via Clerk auth and the seed can be
-- re-applied by re-running the workflow.
--
-- SCHEMA REFERENCE
-- ----------------
-- matches: id TEXT PK, account_id BIGINT FK→accounts(id),
--          event_id TEXT NOT NULL, event_name TEXT NOT NULL,
--          timestamp TIMESTAMPTZ NOT NULL, duration_seconds INT,
--          player_wins INT NOT NULL, opponent_wins INT NOT NULL,
--          player_team_id INT NOT NULL, deck_id TEXT,
--          rank_before TEXT, rank_after TEXT,
--          format TEXT NOT NULL, result TEXT NOT NULL CHECK(win|loss),
--          result_reason TEXT, opponent_name TEXT, opponent_id TEXT,
--          notes TEXT NOT NULL DEFAULT '', rating INT NOT NULL DEFAULT 0,
--          processed_for_ml BOOLEAN NOT NULL DEFAULT FALSE,
--          created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
--
-- The ci-smoke Clerk user ID (hardcoded below) matches the CI_SMOKE_USER_ID
-- constant in staging-smoke.spec.ts and e2e-staging-auth-smoke.yml.

DO $$
DECLARE
    v_user_id    BIGINT;
    v_account_id BIGINT;
BEGIN
    -- Resolve users.id from the ci-smoke Clerk user ID.
    SELECT id INTO v_user_id
    FROM   users
    WHERE  clerk_user_id = 'user_3EmtmrSgZrtd0yRRdisTIIFYnnF'
    LIMIT  1;

    IF v_user_id IS NULL THEN
        RAISE NOTICE 'seed-ci-smoke: users row for clerk_user_id=user_3EmtmrSgZrtd0yRRdisTIIFYnnF not found. '
                     'Run the staging smoke once to JIT-provision, then re-apply this seed.';
        RETURN;
    END IF;

    -- Resolve accounts.id (first account for this user).
    SELECT id INTO v_account_id
    FROM   accounts
    WHERE  user_id = v_user_id
    LIMIT  1;

    IF v_account_id IS NULL THEN
        RAISE NOTICE 'seed-ci-smoke: accounts row for user_id=% not found. '
                     'Run the staging smoke once to JIT-provision, then re-apply this seed.', v_user_id;
        RETURN;
    END IF;

    -- Insert the durable fixture match row. ON CONFLICT DO NOTHING ensures
    -- re-runs are safe (the PK ci_smoke_fixture_match_001 is unique per staging).
    INSERT INTO matches (
        id,
        account_id,
        event_id,
        event_name,
        timestamp,
        duration_seconds,
        player_wins,
        opponent_wins,
        player_team_id,
        deck_id,
        rank_before,
        rank_after,
        format,
        result,
        result_reason,
        opponent_name,
        opponent_id,
        notes,
        rating,
        processed_for_ml
    ) VALUES (
        'ci_smoke_fixture_match_001',   -- deterministic PK for idempotency
        v_account_id,
        'ci_smoke_event_001',
        'Ranked Standard',
        '2026-01-01 00:00:00+00',       -- fixed past timestamp — never clashes with real data
        900,
        2,
        1,
        1,
        NULL,                           -- no deck association needed for AC2
        'Gold-4',
        'Gold-3',
        'Standard',
        'win',
        'OpponentConceded',
        'ci_smoke_opponent',
        'ci_smoke_opp_001',
        '',
        0,
        FALSE
    )
    ON CONFLICT (id) DO NOTHING;

    RAISE NOTICE 'seed-ci-smoke: fixture match ci_smoke_fixture_match_001 seeded for account_id=%', v_account_id;
END
$$;
