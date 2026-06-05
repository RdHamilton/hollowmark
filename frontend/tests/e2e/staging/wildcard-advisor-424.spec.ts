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
 * Required environment variables:
 *   STAGING_API_URL       — override the staging BFF base URL (optional)
 *   STAGING_SMOKE_TOKEN   — Clerk Development JWT. Required for auth tests.
 */

const STAGING_API = process.env.STAGING_API_URL ?? 'https://staging-api.vaultmtg.app';
const SMOKE_TOKEN = process.env.STAGING_SMOKE_TOKEN ?? '';

/**
 * Hard-fail with an INCONCLUSIVE verdict when the token is absent.
 * Matches the pattern established in staging-smoke.spec.ts (#678).
 */
function requireTokenOrFail(context: string): void {
  if (!SMOKE_TOKEN) {
    throw new Error(
      `INCONCLUSIVE: STAGING_SMOKE_TOKEN is not set.\n` +
      `Cannot exercise authenticated surface: ${context}\n` +
      'Verdict: INCONCLUSIVE — treat as FAIL.',
    );
  }
}

const authHeader = (): Record<string, string> =>
  SMOKE_TOKEN ? { Authorization: `Bearer ${SMOKE_TOKEN}` } : {};

// ---------------------------------------------------------------------------
// 1. Staging deploy confirmation — endpoint is reachable and registered
// ---------------------------------------------------------------------------

test.describe('Wildcard advisor (#424): deploy confirmation', () => {
  test('GET /api/v1/recommendations/wildcards is registered (not 404)', async ({ request }) => {
    requireTokenOrFail('GET /api/v1/recommendations/wildcards (deploy confirmation)');

    const res = await request.get(
      `${STAGING_API}/api/v1/recommendations/wildcards`,
      { headers: authHeader() },
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
  test('invalid ?format= value is not rejected with 422', async ({ request }) => {
    requireTokenOrFail('GET /api/v1/recommendations/wildcards?format=Pauper');

    const res = await request.get(
      `${STAGING_API}/api/v1/recommendations/wildcards?format=Pauper`,
      { headers: authHeader() },
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
    requireTokenOrFail('GET /api/v1/recommendations/wildcards?format=');

    const res = await request.get(
      `${STAGING_API}/api/v1/recommendations/wildcards?format=`,
      { headers: authHeader() },
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
  // eslint-disable-next-line playwright/no-skipped-test
  test.skip(
    true,
    'SKIPPED — no zero-inventory staging account available. ' +
    'The ci-smoke-token account has ~10k card_inventory rows. ' +
    'A dedicated zero-inventory account is required to exercise the 409 path. ' +
    'Bianca confirmed no such account is available (2026-06-05). ' +
    'File a follow-on ticket to provision a zero-inventory staging account.',
  );

  test('returns 409 with collection_not_synced when card_inventory is empty', async ({ request }) => {
    requireTokenOrFail('GET /api/v1/recommendations/wildcards (409 collection_not_synced)');

    const res = await request.get(
      `${STAGING_API}/api/v1/recommendations/wildcards`,
      { headers: authHeader() },
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
  test('200 response contains required ADR-045 §1 fields', async ({ request }) => {
    requireTokenOrFail('GET /api/v1/recommendations/wildcards (happy-path shape)');

    const res = await request.get(
      `${STAGING_API}/api/v1/recommendations/wildcards`,
      { headers: authHeader() },
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
    requireTokenOrFail('GET /api/v1/recommendations/wildcards (max 10 cap)');

    const res = await request.get(
      `${STAGING_API}/api/v1/recommendations/wildcards`,
      { headers: authHeader() },
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
    requireTokenOrFail('GET /api/v1/recommendations/wildcards (latency)');

    const start = Date.now();
    const res = await request.get(
      `${STAGING_API}/api/v1/recommendations/wildcards`,
      { headers: authHeader() },
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
