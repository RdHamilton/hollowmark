import { test, expect } from '@playwright/test';

/**
 * Wildcard Advisor Staging E2E (#424)
 *
 * Targets GET /api/v1/recommendations/wildcards on the staging BFF.
 * Verifies the endpoint contract per ADR-045 §1: auth guard, response shape,
 * 409 on empty collection, and format-param handling.
 *
 * Staging state as of 2026-06-05 (Bianca seed applied):
 *   - mtgzone_archetypes: 3 Standard archetypes seeded
 *   - mtgzone_archetype_cards: card rows for those 3 archetypes seeded
 *   - ci-smoke-token account: ~10k card_inventory rows (Bianca confirmed)
 *
 * The 503 tests that previously documented "meta absent" staging state have
 * been removed — they are no longer valid now that meta has been seeded.
 *
 * NOTE: if the happy-path tests fail with 503, it indicates the seeded
 * mtgzone_archetypes.last_updated timestamps are older than 7 days (the
 * metaStalenessThreshold in wildcard_recommendations.go). Bob/Bianca must
 * re-seed with current timestamps.
 *
 * Authentication (tickets#1190):
 *   Rewired from the stale STAGING_SMOKE_TOKEN path to the Backend-API
 *   sign-in-token + FAPI ticket chain. This is the authoritative headless auth
 *   pattern for prod-type Clerk instances (same as staging-smoke.spec.ts,
 *   multi-device-433.spec.ts, and the staging-replay-gate.yml corpus-replay
 *   gate). CLERK_SECRET_KEY absence causes a hard INCONCLUSIVE failure — not a
 *   silent skip.
 *
 * Required environment variables:
 *   STAGING_API_URL     — override the staging BFF base URL (optional)
 *   CLERK_SECRET_KEY    — Clerk Backend API secret key (sk_live_*). REQUIRED
 *                         for authenticated tests. Injected from SSM
 *                         /vaultmtg/app/staging/CLERK_SECRET_KEY by CI.
 *   CI_SMOKE_USER_ID    — override the ci-smoke Clerk user ID (optional)
 */

// ---------------------------------------------------------------------------
// Config
// ---------------------------------------------------------------------------

const STAGING_API = process.env.STAGING_API_URL ?? 'https://staging-api.hollowmark.app';

const CLERK_SECRET_KEY = process.env.CLERK_SECRET_KEY ?? '';

/**
 * Staging FAPI base URL — the per-application Clerk Frontend API subdomain for
 * the staging Hollowmark Clerk instance.
 */
const STAGING_FAPI_HOST = 'https://clerk.stg-app.hollowmark.app';

/**
 * ci-smoke Clerk user ID. Dedicated synthetic account in the staging Clerk
 * instance with card_inventory rows seeded.
 */
const CI_SMOKE_USER_ID =
  process.env.CI_SMOKE_USER_ID ?? 'user_3EmtmrSgZrtd0yRRdisTIIFYnnF';

// ---------------------------------------------------------------------------
// Auth enforcement
// ---------------------------------------------------------------------------

/**
 * Fail hard with an INCONCLUSIVE verdict when CLERK_SECRET_KEY is absent.
 *
 * Replaces the former requireTokenOrFail / STAGING_SMOKE_TOKEN check.
 * A suite that skips all authenticated surfaces and reports PASS provides false
 * confidence (#678 auth-enforcement contract, tickets#1190).
 */
function requireAuthOrFail(context: string): void {
  if (!CLERK_SECRET_KEY) {
    throw new Error(
      `INCONCLUSIVE: CLERK_SECRET_KEY is not set.\n` +
      `Cannot exercise authenticated surface: ${context}\n` +
      '\n' +
      'In CI (staging deploy workflow) CLERK_SECRET_KEY is always injected from\n' +
      'SSM /vaultmtg/app/staging/CLERK_SECRET_KEY. Its absence indicates a secrets\n' +
      'misconfiguration or a missing OIDC role SSM grant.\n' +
      '\n' +
      'Verdict: INCONCLUSIVE (unauthenticated) — treat as FAIL.\n' +
      '\n' +
      'To run locally:\n' +
      '  export CLERK_SECRET_KEY=$(aws ssm get-parameter \\\n' +
      '    --name /vaultmtg/app/staging/CLERK_SECRET_KEY \\\n' +
      '    --with-decryption --query Parameter.Value --output text \\\n' +
      '    --profile personal --region us-east-1)\n' +
      '  export CI_SMOKE_USER_ID=user_3EmtmrSgZrtd0yRRdisTIIFYnnF\n' +
      '  npx playwright test --config=playwright.staging.config.ts',
    );
  }
}

// ---------------------------------------------------------------------------
// Headless Clerk auth helper (Backend-API sign-in-token chain)
// ---------------------------------------------------------------------------

/**
 * Obtain a Clerk session JWT for the ci-smoke account using the Backend-API
 * sign-in-token + FAPI ticket strategy.
 *
 * Boundary 1 — Clerk Backend API: POST api.clerk.com/v1/sign_in_tokens
 *   Authorization: Bearer CLERK_SECRET_KEY (sk_live_* from SSM)
 *   Body: { user_id: CI_SMOKE_USER_ID }
 *   → sign-in ticket (sit_xxx, one-use)
 *
 * Boundary 2 — Staging FAPI: POST clerk.stg-app.hollowmark.app/v1/client/sign_ins
 *   strategy=ticket&ticket=sit_xxx
 *   → { client: { sessions: [{ last_active_token: { jwt } }] }, response: { status: "complete" } }
 *
 * Returns the session JWT string for use as a Bearer header value. Accepted by
 * BFF routes protected by ClerkAuthMiddleware.
 *
 * This is the authoritative headless auth pattern for prod-type Clerk instances
 * — identical to the pattern in staging-smoke.spec.ts and multi-device-433.spec.ts.
 */
async function obtainClerkSessionToken(): Promise<string> {
  // Boundary 1: Clerk Backend API — mint sign-in ticket (one-use)
  const tokenRes = await fetch('https://api.clerk.com/v1/sign_in_tokens', {
    method: 'POST',
    headers: {
      Authorization: `Bearer ${CLERK_SECRET_KEY}`,
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ user_id: CI_SMOKE_USER_ID, expires_in_seconds: 300 }),
  });
  if (!tokenRes.ok) {
    const body = await tokenRes.text();
    throw new Error(
      `Clerk Backend API /v1/sign_in_tokens failed: HTTP ${tokenRes.status} — ${body}\n` +
      'Check: CLERK_SECRET_KEY is sk_live_* for staging? CI_SMOKE_USER_ID exists in staging Clerk?',
    );
  }
  const { token: ticketToken } = await tokenRes.json() as { token: string };

  // Boundary 2: Staging FAPI — exchange ticket for session JWT
  const signinRes = await fetch(
    `${STAGING_FAPI_HOST}/v1/client/sign_ins?__clerk_api_version=2025-11-10&_clerk_js_version=6.12.1`,
    {
      method: 'POST',
      headers: {
        Origin: 'https://stg-app.hollowmark.app',
        'Content-Type': 'application/x-www-form-urlencoded',
      },
      body: `strategy=ticket&ticket=${ticketToken}`,
    },
  );
  if (!signinRes.ok) {
    const body = await signinRes.text();
    throw new Error(
      `Staging FAPI /v1/client/sign_ins failed: HTTP ${signinRes.status} — ${body}\n` +
      `Check: STAGING_FAPI_HOST correct (${STAGING_FAPI_HOST})? Ticket not expired?`,
    );
  }
  const signinData = await signinRes.json() as {
    client: { sessions: Array<{ last_active_token: { jwt: string } }> };
    response: { status: string };
  };
  if (signinData.response?.status !== 'complete') {
    throw new Error(
      `Staging FAPI sign_in did not complete: status=${signinData.response?.status ?? 'unknown'}`,
    );
  }
  const sessions = signinData.client?.sessions ?? [];
  if (sessions.length === 0) {
    throw new Error('Staging FAPI sign_in response contained no sessions');
  }
  const sessionToken = sessions[0].last_active_token?.jwt;
  if (!sessionToken) {
    throw new Error(
      'Staging FAPI session has no JWT — cannot obtain Bearer token for BFF calls',
    );
  }
  return sessionToken;
}

// ---------------------------------------------------------------------------
// 1. Staging deploy confirmation — endpoint is reachable and registered
// ---------------------------------------------------------------------------

test.describe('Wildcard advisor (#424): deploy confirmation', () => {
  let sessionToken: string;

  test.beforeAll(async () => {
    requireAuthOrFail('GET /api/v1/recommendations/wildcards (deploy confirmation)');
    sessionToken = await obtainClerkSessionToken();
  });

  test('GET /api/v1/recommendations/wildcards is registered (not 404)', async ({ request }) => {
    requireAuthOrFail('GET /api/v1/recommendations/wildcards (deploy confirmation)');

    const res = await request.get(
      `${STAGING_API}/api/v1/recommendations/wildcards`,
      { headers: { Authorization: `Bearer ${sessionToken}` } },
    );

    // The endpoint must be registered — any response other than 404 confirms
    // the route mounted. 401/409/503/200 are all acceptable here; 404 means
    // the route was never registered (deploy failed or old binary still running).
    expect(
      res.status(),
      `Route not registered — staging BFF may still be running the v0.3.7 scaffold (expected non-404, got ${res.status()})`,
    ).not.toBe(404);

    // Must not be the v0.3.7 501 scaffold.
    expect(
      res.status(),
      'Endpoint still returning 501 — v0.3.7 scaffold is deployed, not v0.3.8 full impl',
    ).not.toBe(501);
  });

  test('GET /healthz migration_version is 105 (wildcard advisor indexes applied)', async ({ request }) => {
    const res = await request.get(`${STAGING_API}/healthz`);
    expect(res.status()).toBe(200);
    const body = await res.json() as Record<string, unknown>;
    expect(
      Number(body['migration_version']),
      `migration_version should be >= 105 (got ${body['migration_version']}) — wildcard advisor indexes migration has not run`,
    ).toBeGreaterThanOrEqual(105);
  });
});

// ---------------------------------------------------------------------------
// 2. Auth guard — unauthenticated request must return 401
// ---------------------------------------------------------------------------

test.describe('Wildcard advisor (#424): auth guard', () => {
  test('GET /api/v1/recommendations/wildcards returns 401 without token', async ({ request }) => {
    const res = await request.get(`${STAGING_API}/api/v1/recommendations/wildcards`);
    expect(res.status()).toBe(401);
  });

  test('401 response body is valid JSON', async ({ request }) => {
    const res = await request.get(`${STAGING_API}/api/v1/recommendations/wildcards`);
    expect(res.status()).toBe(401);
    const body = await res.json() as Record<string, unknown>;
    expect(body).toHaveProperty('error');
  });
});

// ---------------------------------------------------------------------------
// 3. Format param: invalid value defaults to Standard (not 422)
//    Per handler comment: "Invalid values default to Standard rather than 422
//    — the SPA may omit the param or send a stale value."
// ---------------------------------------------------------------------------

test.describe('Wildcard advisor (#424): format param handling', () => {
  let sessionToken: string;

  test.beforeAll(async () => {
    requireAuthOrFail('GET /api/v1/recommendations/wildcards (format param tests)');
    sessionToken = await obtainClerkSessionToken();
  });

  test('invalid ?format= value is not rejected with 422', async ({ request }) => {
    requireAuthOrFail('GET /api/v1/recommendations/wildcards?format=Pauper');

    const res = await request.get(
      `${STAGING_API}/api/v1/recommendations/wildcards?format=Pauper`,
      { headers: { Authorization: `Bearer ${sessionToken}` } },
    );

    // The handler silently defaults to Standard — must never 422.
    expect(
      res.status(),
      `Invalid format should default to Standard, not return 422 (got ${res.status()})`,
    ).not.toBe(422);

    // Must also not be 401 (token still valid).
    expect(res.status()).not.toBe(401);
  });

  test('empty ?format= param is accepted (defaults to Standard)', async ({ request }) => {
    requireAuthOrFail('GET /api/v1/recommendations/wildcards?format=');

    const res = await request.get(
      `${STAGING_API}/api/v1/recommendations/wildcards?format=`,
      { headers: { Authorization: `Bearer ${sessionToken}` } },
    );

    expect(res.status()).not.toBe(422);
    expect(res.status()).not.toBe(401);
  });
});

// ---------------------------------------------------------------------------
// 4. 409 on empty collection (SKIPPED — no zero-inventory staging account)
//    The 409 path requires a CI token account with zero card_inventory rows.
//    The ci-smoke-token account has ~10k rows (Bianca confirmed 2026-06-05),
//    so this path is unreachable with the current staging account.
//    A dedicated zero-inventory staging account is needed to exercise 409.
//    Bianca confirmed no such account is available — skip is permanent for now.
// ---------------------------------------------------------------------------

test.describe('Wildcard advisor (#424): 409 on empty collection [SKIPPED — no zero-inventory account]', () => {
  test.skip(
    true,
    'SKIPPED — no zero-inventory staging account available. ' +
    'The ci-smoke-token account has ~10k card_inventory rows. ' +
    'A dedicated zero-inventory account is required to exercise the 409 path. ' +
    'Bianca confirmed no such account is available (2026-06-05). ' +
    'File a follow-on ticket to provision a zero-inventory staging account.',
  );

  test('returns 409 with collection_not_synced when card_inventory is empty', async ({ request }) => {
    requireAuthOrFail('GET /api/v1/recommendations/wildcards (409 collection_not_synced)');

    const sessionToken = await obtainClerkSessionToken();

    const res = await request.get(
      `${STAGING_API}/api/v1/recommendations/wildcards`,
      { headers: { Authorization: `Bearer ${sessionToken}` } },
    );

    expect(
      res.status(),
      `Expected 409 (empty collection), got ${res.status()}`,
    ).toBe(409);

    const body = await res.json() as Record<string, unknown>;
    expect(body).toHaveProperty('error');
    expect(body['error']).toBe('collection_not_synced');
  });
});

// ---------------------------------------------------------------------------
// 5. Happy-path response shape
//    Validates ADR-045 §1 response shape:
//      recommendations[], wildcard_budget, tier as string, data_freshness
//      with optional stale_reason.
//    Requires: fresh meta in mtgzone_archetypes + synced collection for the
//    CI token account. Both confirmed by Bianca (2026-06-05):
//      - 3 Standard archetypes seeded in mtgzone_archetypes
//      - card rows seeded in mtgzone_archetype_cards
//      - ci-smoke-token account has ~10k card_inventory rows
//
//    NOTE: if these tests fail with 503, the seeded last_updated timestamps
//    are older than the 7-day metaStalenessThreshold. Bob/Bianca must re-seed
//    with current timestamps (NOW()) rather than historical values.
// ---------------------------------------------------------------------------

test.describe('Wildcard advisor (#424): happy-path response shape', () => {
  let sessionToken: string;

  test.beforeAll(async () => {
    requireAuthOrFail('GET /api/v1/recommendations/wildcards (happy-path)');
    sessionToken = await obtainClerkSessionToken();
  });

  test('200 response contains required ADR-045 §1 fields', async ({ request }) => {
    requireAuthOrFail('GET /api/v1/recommendations/wildcards (happy-path shape)');

    const res = await request.get(
      `${STAGING_API}/api/v1/recommendations/wildcards`,
      { headers: { Authorization: `Bearer ${sessionToken}` } },
    );

    expect(res.status()).toBe(200);

    const body = await res.json() as Record<string, unknown>;

    // Top-level required fields.
    expect(body).toHaveProperty('wildcard_budget');
    expect(body).toHaveProperty('data_freshness');
    expect(body).toHaveProperty('recommendations');
    expect(Array.isArray(body['recommendations'])).toBe(true);

    // wildcard_budget shape.
    const budget = body['wildcard_budget'] as Record<string, unknown>;
    expect(budget).toHaveProperty('common');
    expect(budget).toHaveProperty('uncommon');
    expect(budget).toHaveProperty('rare');
    expect(budget).toHaveProperty('mythic');

    // data_freshness shape — stale_reason is optional (omitempty).
    const freshness = body['data_freshness'] as Record<string, unknown>;
    expect(freshness).toHaveProperty('stale');
    expect(typeof freshness['stale']).toBe('boolean');
    // stale_reason must only appear when stale=true.
    if (freshness['stale'] === true) {
      expect(freshness).toHaveProperty('stale_reason');
      expect(typeof freshness['stale_reason']).toBe('string');
    }

    // If recommendations array is non-empty, validate each item.
    const recs = body['recommendations'] as Array<Record<string, unknown>>;
    for (const rec of recs) {
      // ADR-045 §1: tier is a string (S / 1 / 2 / 3 / 4 / "").
      expect(typeof rec['tier']).toBe(
        'string',
        `tier must be a string per ADR-045 §1 (Ray's ruling). Got: ${typeof rec['tier']}`,
      );
      expect(rec).toHaveProperty('archetype_name');
      expect(rec).toHaveProperty('format');
      expect(rec).toHaveProperty('completion_score');
      expect(rec).toHaveProperty('cards_needed');
      expect(rec).toHaveProperty('wildcards_required');
      expect(rec).toHaveProperty('affordable');
      expect(Array.isArray(rec['missing_cards'])).toBe(true);
    }
  });

  test('recommendations list is capped at 10 items (ADR-045 §3)', async ({ request }) => {
    requireAuthOrFail('GET /api/v1/recommendations/wildcards (max 10 cap)');

    const res = await request.get(
      `${STAGING_API}/api/v1/recommendations/wildcards`,
      { headers: { Authorization: `Bearer ${sessionToken}` } },
    );

    if (res.status() !== 200) {
      test.skip();
      return;
    }

    const body = await res.json() as Record<string, unknown>;
    const recs = body['recommendations'] as unknown[];
    expect(
      recs.length,
      `ADR-045 §3 caps recommendations at 10. Got ${recs.length}.`,
    ).toBeLessThanOrEqual(10);
  });

  test('response time is under 500ms (ADR-045 fitness function)', async ({ request }) => {
    requireAuthOrFail('GET /api/v1/recommendations/wildcards (latency)');

    const start = Date.now();
    const res = await request.get(
      `${STAGING_API}/api/v1/recommendations/wildcards`,
      { headers: { Authorization: `Bearer ${sessionToken}` } },
    );
    const elapsed = Date.now() - start;

    if (res.status() !== 200) {
      test.skip();
      return;
    }

    expect(
      elapsed,
      `ADR-045 fitness function: p99 must be <500ms. Took ${elapsed}ms.`,
    ).toBeLessThan(500);
  });
});
