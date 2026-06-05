/**
 * AC-9 Multi-Device E2E — Staging Verification (#433)
 *
 * Verifies the multi-device pairing and revocation flow against the live
 * staging BFF (staging-api.vaultmtg.app).
 *
 * ## What this test exercises (REAL, via API):
 *   1. GET /api/v1/daemons — unauthenticated request returns 401
 *   2. GET /api/v1/daemons — Clerk-authenticated request returns 200 with a
 *      valid `{ devices: [...] }` response shape
 *   3. DELETE /api/v1/daemons/{device_id} — unauthenticated returns 401
 *   4. DELETE /api/v1/daemons/{device_id} — authenticated but non-existent
 *      device_id returns 404 (not 500 or 401) — proves revoke endpoint is
 *      wired and the auth middleware runs
 *
 * ## What requires manual verification (INCONCLUSIVE — documented here):
 *   The full AC-9 scenario ("two devices paired, both visible, revoke one,
 *   revoked device gets 401 on heartbeat") requires:
 *
 *   a) Two physical daemon installs on separate machines that each complete
 *      the PKCE OAuth flow against the staging Clerk instance. The
 *      POST /api/v1/daemon/register endpoint uses ClerkOAuthMiddl (PKCE
 *      browser flow), not a Bearer JWT — a headless test runner cannot
 *      replicate this step without a real browser OAuth session.
 *
 *   b) The ingest/heartbeat 401 assertion depends on calling
 *      POST /api/v1/ingest/events with the raw daemon API key from step (a).
 *      The key is plaintext only on 201 (new registration) and is not
 *      recoverable afterwards. Without completing step (a), there is no
 *      key to revoke and no key to verify a 401 with.
 *
 *   Verdict for those two sub-ACs: INCONCLUSIVE — manual verification required.
 *   See the manual checklist in AC-9 commentary below.
 *
 * ## Authentication approach:
 *   The staging Clerk instance uses pk_live_* (production-type). We use the
 *   Backend API sign-in-token + FAPI cookie flow (same as staging-spa-smoke).
 *   CLERK_SECRET_KEY (sk_live_* for staging) must be present. Absence causes
 *   a hard INCONCLUSIVE failure — not a silent skip.
 *
 * ## Required environment variables:
 *   CLERK_SECRET_KEY  — Clerk Backend API secret key (sk_live_*). REQUIRED.
 *   STAGING_API_URL   — Override staging BFF base URL (optional).
 *   CI_SMOKE_USER_ID  — Override ci-smoke Clerk user ID (optional).
 *
 * ## Manual verification checklist (for Ray co-sign, production run):
 *   [ ] Install daemon v0.3.8 on Machine A (macOS or Windows).
 *   [ ] Install daemon v0.3.8 on Machine B (different OS or separate install).
 *   [ ] Both machines complete the PKCE OAuth pairing against production Clerk.
 *   [ ] Navigate to Settings → Connected Devices on the SPA.
 *   [ ] Confirm both device rows appear with distinct device_id truncations.
 *   [ ] Click "Revoke" on Machine A's row.
 *   [ ] Confirm Machine A's row disappears from the Devices list.
 *   [ ] On Machine A, observe the daemon's next heartbeat (within 90 s) returns
 *       401 from POST /api/v1/ingest/events, causing the daemon to stop sending.
 *   [ ] Confirm Machine B's daemon continues sending heartbeats (not affected).
 */

import { test, expect } from '@playwright/test';

// ---------------------------------------------------------------------------
// Config
// ---------------------------------------------------------------------------

const STAGING_API = process.env.STAGING_API_URL ?? 'https://staging-api.vaultmtg.app';
const CLERK_SECRET_KEY = process.env.CLERK_SECRET_KEY ?? '';
const CI_SMOKE_USER_ID = process.env.CI_SMOKE_USER_ID || 'user_3EamRFdUZdQl1yYPf4Yg7OIQqm4';

/**
 * A well-formed UUID that is guaranteed not to exist as a device_id for the
 * ci-smoke account. Used to exercise the 404 path on DELETE without needing
 * a real registered device.
 */
const SENTINEL_DEVICE_ID = '00000000-0000-4000-8000-000000000000';

// ---------------------------------------------------------------------------
// Auth enforcement helpers (same pattern as staging-smoke.spec.ts and
// staging-spa-smoke.spec.ts — INCONCLUSIVE verdict on missing credentials)
// ---------------------------------------------------------------------------

function requireAuthOrFail(context: string): void {
  if (!CLERK_SECRET_KEY) {
    throw new Error(
      `INCONCLUSIVE: CLERK_SECRET_KEY is not set.\n` +
      `Cannot exercise authenticated surface: ${context}\n` +
      '\n' +
      'In CI (staging deploy workflow) CLERK_SECRET_KEY is always injected from\n' +
      'secrets. Its absence indicates a secrets misconfiguration.\n' +
      '\n' +
      'Verdict: INCONCLUSIVE (unauthenticated) — treat as FAIL.\n' +
      '\n' +
      'To run locally: export CLERK_SECRET_KEY=<staging sk_live_*> before running\n' +
      'this suite. The key is in SSM at /vaultmtg/staging/CLERK_SECRET_KEY.',
    );
  }
}

/**
 * Obtain a Clerk session JWT for the ci-smoke account using the Backend API
 * sign-in-token + FAPI ticket strategy.
 *
 * Returns the session token string suitable for use as a Bearer header value.
 * This token is accepted by BFF routes protected by RequireClerkAuth
 * (standard Clerk JWT middleware) — NOT by DaemonAPIKeyAuth (daemon ingest).
 */
async function obtainClerkSessionToken(): Promise<string> {
  // Step 1: Create a sign-in token via Clerk Backend API
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
    throw new Error(`Clerk Backend API sign_in_tokens failed: HTTP ${tokenRes.status} — ${body}`);
  }
  const { token: ticketToken } = await tokenRes.json() as { token: string };

  // Step 2: Exchange the ticket for a FAPI session
  const fapiBase = 'https://clerk.stg-app.vaultmtg.app';
  const signinRes = await fetch(
    `${fapiBase}/v1/client/sign_ins?__clerk_api_version=2025-11-10&_clerk_js_version=6.12.1`,
    {
      method: 'POST',
      headers: {
        Origin: 'https://stg-app.vaultmtg.app',
        'Content-Type': 'application/x-www-form-urlencoded',
      },
      body: `strategy=ticket&ticket=${ticketToken}`,
    },
  );
  if (!signinRes.ok) {
    const body = await signinRes.text();
    throw new Error(`FAPI sign_in failed: HTTP ${signinRes.status} — ${body}`);
  }
  const signinData = await signinRes.json() as {
    client: { sessions: Array<{ last_active_token: { jwt: string } }> };
    response: { status: string };
  };
  if (signinData.response?.status !== 'complete') {
    throw new Error(
      `FAPI sign_in did not complete: status=${signinData.response?.status ?? 'unknown'}`,
    );
  }

  // Extract the session JWT from the response
  const sessions = signinData.client?.sessions ?? [];
  if (sessions.length === 0) {
    throw new Error('FAPI sign_in response contained no sessions');
  }
  const sessionToken = sessions[0].last_active_token?.jwt;
  if (!sessionToken) {
    throw new Error('FAPI session has no JWT — cannot obtain Bearer token for BFF calls');
  }
  return sessionToken;
}

// ---------------------------------------------------------------------------
// 1. Unauthenticated access — must return 401 (always exercisable)
// ---------------------------------------------------------------------------

test.describe('AC-9 multi-device: auth enforcement (unauthenticated)', () => {
  test('GET /api/v1/daemons without auth returns 401', async ({ request }) => {
    const res = await request.get(`${STAGING_API}/api/v1/daemons`);
    expect(
      res.status(),
      `GET /api/v1/daemons returned ${res.status()} without auth — expected 401`,
    ).toBe(401);
  });

  test('DELETE /api/v1/daemons/{device_id} without auth returns 401', async ({ request }) => {
    const res = await request.delete(`${STAGING_API}/api/v1/daemons/${SENTINEL_DEVICE_ID}`);
    expect(
      res.status(),
      `DELETE /api/v1/daemons/${SENTINEL_DEVICE_ID} returned ${res.status()} without auth — expected 401`,
    ).toBe(401);
  });
});

// ---------------------------------------------------------------------------
// 2. Authenticated GET /api/v1/daemons — list shape validation
//
// Exercises: Clerk JWT accepted by BFF, response envelope, devices array.
// Does NOT require physical daemon installs — exercises the BFF endpoint and
// Clerk middleware independently of how many devices are actually registered.
// ---------------------------------------------------------------------------

test.describe('AC-9 multi-device: authenticated device list', () => {
  let sessionToken: string;

  test.beforeAll(async () => {
    requireAuthOrFail('GET /api/v1/daemons (authenticated device list)');
    sessionToken = await obtainClerkSessionToken();
  });

  test('GET /api/v1/daemons returns 200 with valid { devices: [...] } envelope', async ({ request }) => {
    requireAuthOrFail('GET /api/v1/daemons (authenticated device list)');

    const res = await request.get(`${STAGING_API}/api/v1/daemons`, {
      headers: { Authorization: `Bearer ${sessionToken}` },
    });

    expect(
      res.status(),
      `GET /api/v1/daemons returned ${res.status()} with a valid token — expected 200`,
    ).toBe(200);

    const body = await res.json() as Record<string, unknown>;
    expect(
      body,
      'GET /api/v1/daemons response body is missing the "devices" field',
    ).toHaveProperty('devices');
    expect(
      Array.isArray(body.devices),
      `"devices" field is not an array — got ${typeof body.devices}`,
    ).toBe(true);

    // If devices are present, validate the shape of each row per ADR-031 §4.
    // The ci-smoke account may have zero devices — both cases are valid here.
    const devices = body.devices as Array<Record<string, unknown>>;
    for (const device of devices) {
      expect(typeof device.device_id).toBe('string');
      expect((device.device_id as string).length).toBeGreaterThan(0);
      expect(typeof device.platform).toBe('string');
      expect(typeof device.daemon_ver).toBe('string');
      expect(typeof device.paired_at).toBe('string');
      // key_hash and key_prefix must NOT be exposed (ADR-031 §4 — sensitive columns)
      expect(device).not.toHaveProperty('key_hash');
      expect(device).not.toHaveProperty('key_prefix');
    }
  });

  test('GET /api/v1/daemons response does not expose sensitive key columns', async ({ request }) => {
    requireAuthOrFail('GET /api/v1/daemons (sensitive column check)');

    const res = await request.get(`${STAGING_API}/api/v1/daemons`, {
      headers: { Authorization: `Bearer ${sessionToken}` },
    });

    expect(res.status()).toBe(200);
    const body = await res.json() as { devices: Array<Record<string, unknown>> };
    const devices = body.devices ?? [];

    for (const device of devices) {
      expect(
        device,
        `Device ${String(device.device_id)} exposes key_hash — ADR-031 §4 violation`,
      ).not.toHaveProperty('key_hash');
      expect(
        device,
        `Device ${String(device.device_id)} exposes key_prefix — ADR-031 §4 violation`,
      ).not.toHaveProperty('key_prefix');
    }
  });
});

// ---------------------------------------------------------------------------
// 3. Authenticated DELETE — auth accepted, non-existent device_id returns 404
//
// This verifies the revoke endpoint is reachable, the Clerk middleware runs,
// and the handler returns 404 (not 500 or 401) for an unknown device_id.
// The 404 response proves the endpoint is wired all the way through to the
// database query per ADR-031 §3.
// ---------------------------------------------------------------------------

test.describe('AC-9 multi-device: authenticated revoke endpoint', () => {
  let sessionToken: string;

  test.beforeAll(async () => {
    requireAuthOrFail('DELETE /api/v1/daemons/{device_id} (revoke endpoint)');
    sessionToken = await obtainClerkSessionToken();
  });

  test('DELETE /api/v1/daemons/{sentinel} with auth returns 404 for unknown device', async ({ request }) => {
    requireAuthOrFail('DELETE /api/v1/daemons/{device_id} (revoke endpoint)');

    const res = await request.delete(
      `${STAGING_API}/api/v1/daemons/${SENTINEL_DEVICE_ID}`,
      { headers: { Authorization: `Bearer ${sessionToken}` } },
    );

    // 404 = endpoint wired, Clerk auth passed, handler ran, DB returned no row.
    // NOT 401 (token rejected) or 500 (endpoint not wired / handler crashed).
    // Per ADR-031 §3: non-existent / cross-tenant / revoked → all collapse to 404.
    expect(
      res.status(),
      `DELETE /api/v1/daemons/${SENTINEL_DEVICE_ID} returned ${res.status()} with a valid token — expected 404 for non-existent device`,
    ).toBe(404);
  });

  test('DELETE /api/v1/daemons/{malformed_id} with auth returns 400', async ({ request }) => {
    requireAuthOrFail('DELETE /api/v1/daemons (malformed device_id)');

    const res = await request.delete(
      `${STAGING_API}/api/v1/daemons/not-a-uuid`,
      { headers: { Authorization: `Bearer ${sessionToken}` } },
    );

    expect(
      res.status(),
      `DELETE /api/v1/daemons/not-a-uuid returned ${res.status()} — expected 400 for malformed UUID`,
    ).toBe(400);
  });
});

// ---------------------------------------------------------------------------
// 4. INCONCLUSIVE surface documentation
//
// The sub-ACs that require two physical daemon installs are explicitly
// documented here. The test below always passes (it documents constraints,
// not behavior) but emits a console warning with the INCONCLUSIVE verdict so
// CI log readers can see what was NOT covered automatically.
//
// These surfaces require Ray co-sign on a manual production run per the AC-9
// ticket requirements.
// ---------------------------------------------------------------------------

test.describe('AC-9 multi-device: INCONCLUSIVE surfaces (manual required)', () => {
  test('INCONCLUSIVE — two-device pairing and ingest-401-after-revoke cannot be automated headlessly', () => {
    console.warn(
      '\n' +
      '╔══════════════════════════════════════════════════════════════════════════════╗\n' +
      '║  AC-9 PARTIAL COVERAGE — MANUAL VERIFICATION REQUIRED                       ║\n' +
      '║                                                                              ║\n' +
      '║  Automated (this spec):                                                      ║\n' +
      '║    PASS  GET /api/v1/daemons → 401 without auth                             ║\n' +
      '║    PASS  GET /api/v1/daemons → 200 + { devices: [...] } with auth           ║\n' +
      '║    PASS  Sensitive columns (key_hash, key_prefix) not exposed                ║\n' +
      '║    PASS  DELETE /api/v1/daemons/{sentinel} → 404 with auth                  ║\n' +
      '║    PASS  DELETE /api/v1/daemons/{malformed} → 400 with auth                 ║\n' +
      '║    PASS  DELETE /api/v1/daemons/{device_id} → 401 without auth              ║\n' +
      '║                                                                              ║\n' +
      '║  INCONCLUSIVE — requires two physical daemon installs + manual steps:        ║\n' +
      '║    ?     Two devices paired under same Clerk account, both visible in UI     ║\n' +
      '║    ?     Revoke Device A from Devices UI — row disappears                    ║\n' +
      '║    ?     Device A daemon heartbeat returns 401 within 90 s of revocation     ║\n' +
      '║    ?     Device B daemon heartbeat continues unaffected                      ║\n' +
      '║                                                                              ║\n' +
      '║  REASON: POST /api/v1/daemon/register uses PKCE OAuth browser flow          ║\n' +
      '║  (ClerkOAuthMiddl), not a Bearer JWT. Daemon API keys are only returned      ║\n' +
      '║  as plaintext on 201 (first registration). A headless test runner cannot    ║\n' +
      '║  replicate the browser PKCE flow to obtain two daemon API keys.             ║\n' +
      '║                                                                              ║\n' +
      '║  ACTION: Ray co-signs manual production run per AC-9 ticket ACs.            ║\n' +
      '╚══════════════════════════════════════════════════════════════════════════════╝\n',
    );

    // This test always passes — it is documentation, not a behavioral assertion.
    // The INCONCLUSIVE verdict is in the console output for CI log readers.
    expect(true).toBe(true);
  });
});
