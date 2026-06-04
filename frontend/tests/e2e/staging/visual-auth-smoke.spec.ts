import { test, expect, type Page, type BrowserContext } from '@playwright/test';
import * as fs from 'fs';
import * as path from 'path';

/**
 * Visual Authentication Smoke Test — v0.3.7 Surface Screenshots
 *
 * Establishes a real Clerk session on the production-type staging instance
 * (stg-app.vaultmtg.app) using the Backend API sign-in-token approach:
 *
 *   1. POST /v1/sign_in_tokens (Clerk Backend API) → one-time ticket token
 *   2. POST /v1/client/sign_ins?strategy=ticket (Clerk FAPI) → session + cookies
 *   3. Inject __client and __client_uat cookies into Playwright context
 *   4. Navigate to each surface and capture screenshots
 *
 * This approach works on production-type Clerk instances (pk_live_*).
 * Testing tokens are dev-instance only and fail on pk_live_* — this replaces
 * the testing-token approach from #2917 which was INCONCLUSIVE on staging.
 *
 * Required environment variables:
 *   CLERK_SECRET_KEY       — Clerk Backend API secret key (sk_live_*) for
 *                            staging. Used to create sign-in tokens for the
 *                            ci-smoke@vaultmtg.app account. NEVER exposed
 *                            in the browser or committed.
 *   SCREENSHOT_DIR         — Directory to save screenshots (required).
 *   CI_SMOKE_USER_ID       — Clerk user ID for the ci-smoke account.
 *                            Default: user_3EamRFdUZdQl1yYPf4Yg7OIQqm4
 *
 * The ci-smoke@vaultmtg.app account has no match/draft data on staging.
 * All data-driven surfaces will show empty states — this is expected and
 * valid. Empty-state rendering proves authenticated layout renders correctly.
 */

// ---------------------------------------------------------------------------
// Config
// ---------------------------------------------------------------------------

const CLERK_SECRET_KEY = process.env.CLERK_SECRET_KEY ?? '';
const BASE_URL = process.env.STAGING_SPA_URL || 'https://stg-app.vaultmtg.app';
const FAPI_BASE = 'https://clerk.stg-app.vaultmtg.app';
const CI_SMOKE_USER_ID = process.env.CI_SMOKE_USER_ID || 'user_3EamRFdUZdQl1yYPf4Yg7OIQqm4';
const SCREENSHOT_DIR = process.env.SCREENSHOT_DIR ?? '';

// ---------------------------------------------------------------------------
// Auth enforcement
// ---------------------------------------------------------------------------

function requireAuthOrFail(): void {
  if (!CLERK_SECRET_KEY) {
    throw new Error(
      'INCONCLUSIVE: CLERK_SECRET_KEY is not set.\n' +
      'This suite requires sk_live_* from SSM /vaultmtg/staging/CLERK_SECRET_KEY.\n' +
      'Verdict: INCONCLUSIVE — treat as FAIL.',
    );
  }
  if (!SCREENSHOT_DIR) {
    throw new Error('SCREENSHOT_DIR environment variable is required.');
  }
}

// ---------------------------------------------------------------------------
// Session establishment via sign-in token + FAPI
// ---------------------------------------------------------------------------

interface ClerkSessionCookies {
  clientCookie: string;       // __client JWT value (for clerk.stg-app.vaultmtg.app)
  clientUat: string;          // __client_uat value (unix timestamp, non-zero = active session)
  clientUatHkds: string;      // __client_uat_hKdSwoMR (Clerk instance-scoped UAT)
  sessionJwt: string;         // Session JWT for BFF API calls
  sessionId: string;          // Clerk session ID
}

async function createSignInToken(): Promise<string> {
  const res = await fetch('https://api.clerk.com/v1/sign_in_tokens', {
    method: 'POST',
    headers: {
      Authorization: `Bearer ${CLERK_SECRET_KEY}`,
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ user_id: CI_SMOKE_USER_ID, expires_in_seconds: 300 }),
  });
  if (!res.ok) {
    const body = await res.text();
    throw new Error(`Clerk sign_in_tokens failed: ${res.status} ${body}`);
  }
  const data = await res.json() as { token: string; id: string; status: string };
  return data.token;
}

async function processFapiSignIn(ticketToken: string): Promise<ClerkSessionCookies> {
  // Step 1: Create a FAPI client to get initial cookies
  const clientRes = await fetch(
    `${FAPI_BASE}/v1/client?__clerk_api_version=2025-11-10&_clerk_js_version=6.12.1`,
    {
      headers: {
        Origin: BASE_URL,
        Referer: `${BASE_URL}/`,
      },
    },
  );
  const clientSetCookie = clientRes.headers.get('set-cookie') ?? '';

  // Step 2: Use the ticket strategy to create a sign-in via FAPI
  const signInRes = await fetch(
    `${FAPI_BASE}/v1/client/sign_ins?__clerk_api_version=2025-11-10&_clerk_js_version=6.12.1`,
    {
      method: 'POST',
      headers: {
        Origin: BASE_URL,
        'Content-Type': 'application/x-www-form-urlencoded',
        Cookie: clientSetCookie.split(';')[0], // pass the __client cookie from step 1
      },
      body: `strategy=ticket&ticket=${ticketToken}`,
    },
  );

  if (!signInRes.ok) {
    const body = await signInRes.text();
    throw new Error(`FAPI sign_in failed: ${signInRes.status} ${body}`);
  }

  const signInData = await signInRes.json() as {
    response: { status: string; created_session_id: string };
    client: { sessions: Array<{ id: string; status: string }> } | null;
  };

  if (signInData.response.status !== 'complete') {
    throw new Error(`FAPI sign_in status: ${signInData.response.status} (expected complete)`);
  }

  const sessionId = signInData.response.created_session_id;

  // Extract the new __client cookie from the sign-in response
  // Clerk returns multiple Set-Cookie headers; node-fetch exposes them as a single
  // comma-joined string. Join all set-cookie headers to search all cookies.
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const rawSetCookieHeaders: string = (signInRes.headers as any).raw?.()?.['set-cookie']?.join('\n') ?? signInRes.headers.get('set-cookie') ?? '';
  const clientCookieMatch = rawSetCookieHeaders.match(/__client=([^;\s]+)/);
  const clientCookie = clientCookieMatch ? clientCookieMatch[1] : '';

  // Fetch the session JWT via Backend API
  const jwtRes = await fetch(`https://api.clerk.com/v1/sessions/${sessionId}/tokens`, {
    method: 'POST',
    headers: {
      Authorization: `Bearer ${CLERK_SECRET_KEY}`,
      'Content-Type': 'application/json',
    },
  });
  if (!jwtRes.ok) {
    throw new Error(`Session JWT fetch failed: ${jwtRes.status}`);
  }
  const jwtData = await jwtRes.json() as { jwt: string };

  // Parse __client_uat from sign-in response cookies
  const clientUatMatch = rawSetCookieHeaders.match(/__client_uat=(\d+)/);
  const clientUat = clientUatMatch ? clientUatMatch[1] : String(Math.floor(Date.now() / 1000));

  // The hKdSwoMR suffix is the Clerk instance key identifier
  const clientUatHkdsMatch = rawSetCookieHeaders.match(/__client_uat_hKdSwoMR=(\d+)/);
  const clientUatHkds = clientUatHkdsMatch ? clientUatHkdsMatch[1] : clientUat;

  return {
    clientCookie,
    clientUat,
    clientUatHkds: clientUatHkds,
    sessionJwt: jwtData.jwt,
    sessionId,
  };
}

async function injectClerkSession(context: BrowserContext, cookies: ClerkSessionCookies): Promise<void> {
  const expiry = Math.floor(Date.now() / 1000) + 86400 * 30; // 30 days

  await context.addCookies([
    // __client_uat on .vaultmtg.app — tells Clerk JS that there is an active session
    {
      name: '__client_uat',
      value: cookies.clientUat,
      domain: '.vaultmtg.app',
      path: '/',
      httpOnly: false,
      secure: true,
      sameSite: 'None',
      expires: expiry,
    },
    // __client_uat_hKdSwoMR — instance-scoped UAT cookie
    {
      name: '__client_uat_hKdSwoMR',
      value: cookies.clientUatHkds,
      domain: '.vaultmtg.app',
      path: '/',
      httpOnly: false,
      secure: true,
      sameSite: 'None',
      expires: expiry,
    },
    // __client on clerk.stg-app.vaultmtg.app — the Clerk client JWT
    ...(cookies.clientCookie ? [{
      name: '__client',
      value: cookies.clientCookie,
      domain: '.clerk.stg-app.vaultmtg.app',
      path: '/',
      httpOnly: true,
      secure: true,
      sameSite: 'None' as const,
      expires: expiry,
    }] : []),
  ]);
}

// ---------------------------------------------------------------------------
// Screenshot helper
// ---------------------------------------------------------------------------

interface SurfaceResult {
  surface: string;
  file: string;
  status: 'rendered' | 'empty_state' | 'broken' | 'auth_wall';
  notes: string;
}

async function screenshotSurface(
  page: Page,
  surface: string,
  route: string,
  outputDir: string,
): Promise<SurfaceResult> {
  const filename = surface.toLowerCase().replace(/[^a-z0-9]+/g, '-') + '.png';
  const filePath = path.join(outputDir, filename);

  await page.goto(BASE_URL + route, { waitUntil: 'domcontentloaded', timeout: 30_000 });
  // 10 s: allow Clerk JS to refresh the session token on the new route and for
  // all data-fetch hooks to complete. Empty-state surfaces resolve in ~2 s;
  // chart/collection surfaces with multiple API calls need up to 8-10 s.
  await page.waitForTimeout(10_000);

  // Check auth wall (redirected to sign-in)
  const currentUrl = page.url();
  if (currentUrl.includes('/sign-in') || currentUrl.includes('/sign-up')) {
    await page.screenshot({ path: filePath, fullPage: true });
    return { surface, file: filename, status: 'auth_wall', notes: `Redirected to ${currentUrl}` };
  }

  // Check for React error boundary
  const hasErrorBoundary = await page.locator('.react-error-boundary, [data-testid="error-boundary"]').isVisible().catch(() => false);
  if (hasErrorBoundary) {
    await page.screenshot({ path: filePath, fullPage: true });
    return { surface, file: filename, status: 'broken', notes: 'React error boundary visible' };
  }

  // Detect empty state
  const bodyText = await page.locator('body').textContent().catch(() => '');
  const emptyStateKeywords = [
    'no matches', 'no data', 'no results', 'play some games',
    'empty', 'nothing here', 'no quests', 'no decks',
    'connect the desktop app', 'no drafts',
  ];
  const isEmptyState = emptyStateKeywords.some(kw => bodyText?.toLowerCase().includes(kw));

  await page.screenshot({ path: filePath, fullPage: true });

  const notes = isEmptyState ? 'Empty state — ci-smoke account has no data on staging' : 'Rendered with content';
  return {
    surface,
    file: filename,
    status: isEmptyState ? 'empty_state' : 'rendered',
    notes,
  };
}

// ---------------------------------------------------------------------------
// v0.3.7 Surfaces
// ---------------------------------------------------------------------------

const V037_SURFACES = [
  { surface: 'Home (command-strip)', route: '/home' },
  { surface: 'Match History', route: '/match-history' },
  { surface: 'Quests', route: '/quests' },
  { surface: 'Draft Pack Grid', route: '/draft' },
  { surface: 'Draft Live', route: '/draft/live' },
  { surface: 'Draft Analytics', route: '/draft-analytics' },
  { surface: 'Decks', route: '/decks' },
  { surface: 'Collection', route: '/collection' },
  { surface: 'Meta', route: '/meta' },
  { surface: 'Win-Rate Trend (set annotations)', route: '/charts/win-rate-trend' },
  { surface: 'Rank Progression', route: '/charts/rank-progression' },
  { surface: 'Deck Performance', route: '/charts/deck-performance' },
  { surface: 'Format Distribution', route: '/charts/format-distribution' },
  { surface: 'Result Breakdown', route: '/charts/result-breakdown' },
  { surface: 'Settings', route: '/settings' },
  { surface: 'API Keys', route: '/api-keys' },
  { surface: 'History (Drafts)', route: '/history/drafts' },
] as const;

// ---------------------------------------------------------------------------
// Test suite
// ---------------------------------------------------------------------------

test.describe('Visual auth smoke — v0.3.7 screenshot capture', () => {
  let sharedContext: BrowserContext;
  let sharedPage: Page;
  let sessionCookies: ClerkSessionCookies;
  const results: SurfaceResult[] = [];

  test.beforeAll(async ({ browser }) => {
    requireAuthOrFail();

    // Ensure output directory exists
    if (!fs.existsSync(SCREENSHOT_DIR)) {
      fs.mkdirSync(SCREENSHOT_DIR, { recursive: true });
    }

    // Establish Clerk session via sign-in token + FAPI
    const ticketToken = await createSignInToken();
    sessionCookies = await processFapiSignIn(ticketToken);

    // Create browser context and inject session cookies
    sharedContext = await browser.newContext({
      viewport: { width: 1280, height: 800 },
    });
    await injectClerkSession(sharedContext, sessionCookies);
    sharedPage = await sharedContext.newPage();

    // Verify session: navigate to home and check __client_uat.
    // 12 s warm-up: Clerk JS needs ~5-8 s to initialize, rotate the session token,
    // and fire the first API requests in a headless browser with cookie injection.
    // Subsequent navigations (screenshotSurface) reuse the warmed-up session.
    await sharedPage.goto(BASE_URL + '/home', { waitUntil: 'domcontentloaded', timeout: 30_000 });
    await sharedPage.waitForTimeout(12_000);

    const cookies = await sharedContext.cookies([BASE_URL]);
    const clientUat = cookies.find(c => c.name === '__client_uat');
    const uat = parseInt(clientUat?.value ?? '0');

    if (uat === 0) {
      // Cookie injection didn't take — Clerk JS may have reset it
      // The SPA Clerk JS re-reads from the clerk. subdomain; cookies may need
      // to be read by Clerk JS first. Navigate with __clerk_ticket as fallback.
      throw new Error(
        'Session injection: __client_uat=0 after cookie injection.\n' +
        'Clerk JS may have reset the UAT cookie. Check cookie domain/SameSite settings.',
      );
    }

    const currentUrl = sharedPage.url();
    if (currentUrl.includes('/sign-in')) {
      throw new Error(`Session not established — page redirected to ${currentUrl} after cookie injection`);
    }

    console.log(`\nAuth session established:`);
    console.log(`  session_id: ${sessionCookies.sessionId}`);
    console.log(`  __client_uat: ${uat} (${new Date(uat * 1000).toISOString()})`);
    console.log(`  jwt_length: ${sessionCookies.sessionJwt.length}`);
    console.log(`  landing_url: ${currentUrl}`);
    console.log(`  screenshot_dir: ${SCREENSHOT_DIR}`);
  });

  test.afterAll(async () => {
    // Print results table
    console.log('\n=== v0.3.7 VISUAL VERIFICATION RESULTS ===');
    console.log('Surface'.padEnd(35) + 'Status'.padEnd(15) + 'File'.padEnd(45) + 'Notes');
    console.log('-'.repeat(120));
    for (const r of results) {
      console.log(
        r.surface.padEnd(35) +
        r.status.padEnd(15) +
        r.file.padEnd(45) +
        r.notes,
      );
    }
    console.log(`\nScreenshots saved to: ${SCREENSHOT_DIR}`);

    const broken = results.filter(r => r.status === 'broken' || r.status === 'auth_wall');
    if (broken.length > 0) {
      console.error(`\nFAILURES:`);
      broken.forEach(r => console.error(`  ${r.surface}: ${r.status} — ${r.notes}`));
    }

    await sharedContext?.close();
  });

  for (const { surface, route } of V037_SURFACES) {
    test(`${surface} — screenshot at desktop viewport`, async () => {
      requireAuthOrFail();
      const result = await screenshotSurface(sharedPage, surface, route, SCREENSHOT_DIR);
      results.push(result);

      // Auth walls are hard failures
      expect(
        result.status,
        `${surface}: auth wall — session was not established correctly`,
      ).not.toBe('auth_wall');

      // React error boundaries are hard failures
      expect(
        result.status,
        `${surface}: React error boundary visible — ${result.notes}`,
      ).not.toBe('broken');

      // Empty states are acceptable (ci-smoke account has no data)
      // We only assert the page rendered (not auth_wall, not broken)
      console.log(`  ${surface}: ${result.status} — ${result.notes}`);
    });
  }

  test('Verdict: all v0.3.7 surfaces rendered (authenticated)', () => {
    requireAuthOrFail();

    const authWalls = results.filter(r => r.status === 'auth_wall');
    const broken = results.filter(r => r.status === 'broken');
    const emptyStates = results.filter(r => r.status === 'empty_state');
    const rendered = results.filter(r => r.status === 'rendered');

    console.log(`\nFinal tally:`);
    console.log(`  rendered (with data): ${rendered.length}`);
    console.log(`  empty_state: ${emptyStates.length}`);
    console.log(`  broken: ${broken.length}`);
    console.log(`  auth_wall: ${authWalls.length}`);

    expect(
      authWalls.length,
      `Auth walls detected — session not established on: ${authWalls.map(r => r.surface).join(', ')}`,
    ).toBe(0);

    expect(
      broken.length,
      `React error boundaries on: ${broken.map(r => r.surface).join(', ')}`,
    ).toBe(0);
  });
});
