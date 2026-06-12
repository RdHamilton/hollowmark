import { test, expect, type Page, type BrowserContext } from '@playwright/test';

/**
 * Staging SPA Smoke Suite (#1933, updated #678, real-credential auth 2026-06-02)
 *
 * Authenticates via Backend API sign-in-token + FAPI and navigates every SPA
 * route at stg-app.vaultmtg.app, asserting no blank screen and no React error
 * boundary.
 *
 * Authentication approach (FAPI sign-in-token):
 *   The staging Clerk instance is environment_type=production (pk_live_*).
 *   Testing tokens (POST /v1/testing_tokens) are dev-instance only and fail on
 *   pk_live_* instances. The correct headless auth chain for production instances:
 *
 *   1. POST /v1/sign_in_tokens (Backend API) → one-time ticket for ci-smoke user
 *   2. POST /v1/client/sign_ins?strategy=ticket (FAPI) → session cookies
 *   3. Inject __client + __client_uat cookies into Playwright browser context
 *   4. Navigate — Clerk JS reads the cookies and establishes the session
 *
 *   The ci-smoke@vaultmtg.app account is the dedicated headless smoke account.
 *   It has no match/draft data on staging; data-driven surfaces show empty states
 *   (authenticated rendering still verified).
 *
 * Auth-enforcement policy (#678):
 *   If CLERK_SECRET_KEY is absent, the suite reports INCONCLUSIVE and FAILS —
 *   it does NOT silently skip and report PASS. The CI workflow always supplies
 *   CLERK_SECRET_KEY from secrets; absence indicates a secrets misconfiguration.
 *
 * waitUntil strategy (#1949):
 *   All page.goto() calls use 'domcontentloaded' instead of 'networkidle'.
 *   Background analytics/CDN requests can keep the network busy indefinitely
 *   on GitHub-hosted runners, causing intermittent 30 s timeouts.
 *
 * Verdict reporting (#678):
 *   The final test in the protected-routes describe emits a structured verdict
 *   banner naming surfaces exercised (AUTHENTICATED) vs not, so CI log readers
 *   can tell at a glance what was covered.
 *
 * Required environment variables:
 *   CLERK_SECRET_KEY  — Clerk Backend API secret key (sk_live_* for staging).
 *                       REQUIRED. Absence causes INCONCLUSIVE hard failure.
 *   STAGING_SPA_URL   — Override staging SPA base URL (optional).
 *   CLERK_FAPI_URL    — Clerk FAPI base URL for the staging instance (optional).
 *                       Default: https://clerk.stg-app.hollowmark.app
 *   CI_SMOKE_USER_ID  — Override ci-smoke Clerk user ID (optional).
 */

// ---------------------------------------------------------------------------
// Config
// ---------------------------------------------------------------------------

const CLERK_SECRET_KEY = process.env.CLERK_SECRET_KEY ?? '';
// Use `||` (not `??`) so that an empty-string CI secret falls back to the
// default. `??` only falls back on `undefined`/`null`, which left
// BASE_URL = '' when STAGING_SPA_URL was set-but-empty in CI (#1933).
const BASE_URL = process.env.STAGING_SPA_URL || 'https://stg-app.hollowmark.app';
// CLERK_FAPI_URL must match the Clerk instance the staging CLERK_SECRET_KEY belongs to.
// Updated for Hollowmark staging cutover — old instance was clerk.stg-app.vaultmtg.app.
const FAPI_BASE = process.env.CLERK_FAPI_URL || 'https://clerk.stg-app.hollowmark.app';
// Derive the __client cookie domain from FAPI_BASE: strip the scheme and prepend '.'.
const CLERK_COOKIE_DOMAIN = '.' + FAPI_BASE.replace(/^https?:\/\//, '');
// Derive the __client_uat cookie domain from BASE_URL: the top two domain segments.
// e.g. https://stg-app.hollowmark.app → .hollowmark.app
const UAT_COOKIE_DOMAIN = '.' + BASE_URL.replace(/^https?:\/\/[^.]+\./, '');
const CI_SMOKE_USER_ID = process.env.CI_SMOKE_USER_ID || 'user_3EmtmrSgZrtd0yRRdisTIIFYnnF';
const API_BASE_URL = 'staging-api.vaultmtg.app';

// ---------------------------------------------------------------------------
// Routes from App.tsx
// ---------------------------------------------------------------------------

/** Public routes — accessible without authentication. */
const PUBLIC_ROUTES = ['/download', '/setup'] as const;

/**
 * Protected routes — require Clerk sign-in.
 * `/` redirects to `/home` so it is covered by the protected list.
 */
const PROTECTED_ROUTES = [
  '/home',
  '/match-history',
  '/quests',
  '/draft',
  '/draft-analytics',
  '/decks',
  '/collection',
  '/meta',
  '/charts/win-rate-trend',
  '/charts/deck-performance',
  '/charts/rank-progression',
  '/charts/format-distribution',
  '/charts/result-breakdown',
  '/settings',
  '/history/drafts',
  '/draft/live',
  '/api-keys',
] as const;

// ---------------------------------------------------------------------------
// Auth enforcement helpers (#678)
// ---------------------------------------------------------------------------

/**
 * Fail hard with an INCONCLUSIVE verdict when CLERK_SECRET_KEY is absent.
 *
 * This replaces the former `test.skip` pattern. A suite that skips all
 * authenticated surfaces and reports PASS is worse than no test: it provides
 * false confidence and was the direct cause of the 2026-06-02 staging miss.
 *
 * INCONCLUSIVE = "we cannot determine health because auth was unavailable."
 * This must be treated as a failure by the CI gate.
 */
function requireAuthOrFail(): void {
  if (!CLERK_SECRET_KEY) {
    throw new Error(
      'INCONCLUSIVE: CLERK_SECRET_KEY is not set.\n' +
      '\n' +
      'This suite cannot exercise any authenticated surface without it.\n' +
      'In CI (deploy-spa-staging.yml) CLERK_SECRET_KEY is always injected from\n' +
      'secrets. Its absence here indicates a secrets misconfiguration.\n' +
      '\n' +
      'Surfaces that WERE NOT exercised (all authenticated):\n' +
      PROTECTED_ROUTES.map((r) => `  - ${r}`).join('\n') +
      '\n' +
      'Verdict: INCONCLUSIVE (unauthenticated) — treat as FAIL.\n' +
      '\n' +
      'To run locally: export CLERK_SECRET_KEY=<staging sk_test_*> before running\n' +
      'this suite. The key is in SSM at /vaultmtg/staging/CLERK_SECRET_KEY.',
    );
  }
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/**
 * Assert that the page is in a genuinely healthy state — not merely that it
 * rendered *something*, but that it did NOT render an error or failure UI.
 *
 * ## Why the old checks were insufficient (#722)
 *
 * The original three checks all passed trivially on error-state pages:
 *   1. `document.body.children.length > 0` — error UI is still DOM content.
 *   2. `.react-error-boundary` / `[data-testid="error-boundary"]` — none of the
 *      three problem routes (/decks, /collection, /charts/result-breakdown) use
 *      React's error boundary mechanism; they handle errors in local state and
 *      render custom error UI with CSS classes like `.error-state`.
 *   3. ARIA landmark / `#root` present — `#root` is always present. The former
 *      landmark check also matched `[data-testid]`, which is present even on
 *      error pages (e.g. `data-testid="collection-error"`).
 *
 * ## What this function now checks
 *
 * Universal checks (every route):
 *   - No blank screen (body children > 0 — unchanged)
 *   - No React error boundary (unchanged — belt-and-suspenders)
 *   - At least one ARIA landmark present (unchanged — basic mount verification)
 *   - `[data-testid="config-error-screen"]` NOT visible — ADR-077 boot-level
 *     ConfigErrorScreen; its presence means /config.json failed to load before
 *     the SPA mounted (no page content loaded successfully).
 *   - `.error-state` NOT visible — shared CSS class used by the ErrorState
 *     component (ResultBreakdown) and the inline error branches of Decks and
 *     Collection.
 *   - `[data-testid="collection-error"]` NOT visible — Collection's error branch
 *     testid, present when the Collection API call fails.
 *   - Known error-message text patterns NOT present — catches error UI that does
 *     not use the standard CSS class, or future pages with different patterns.
 *
 * Route-specific positive checks:
 *   Assert that the expected healthy-state container IS present so that a page
 *   stuck on a spinner (rendering neither error nor healthy content) also fails.
 *   - /decks          → `.decks-page` must be visible
 *   - /collection     → `[data-testid="collection-page"]` must be visible
 *                       (only rendered on the healthy path; error path renders
 *                       `[data-testid="collection-error"]` instead)
 *   - /charts/result-breakdown → `.page-container` must be visible (outermost
 *                       wrapper always rendered regardless of data/error/empty)
 *
 * ## ci-smoke account context
 *
 * The ci-smoke account has no match/draft data on staging. Data-bearing surfaces
 * (/decks, /collection, /charts/result-breakdown) may legitimately render their
 * empty-state variant ("No Decks Yet", "No Cards Found", "No performance data").
 * An empty-state IS a healthy outcome. These assertions do NOT require data rows
 * to be present; they only require that the error UI is absent.
 */
async function assertPageIsHealthy(page: Page, route: string): Promise<void> {
  // -------------------------------------------------------------------------
  // 1. Page must have some content (unchanged — basic mount check)
  // -------------------------------------------------------------------------
  const bodyChildren = await page.evaluate(() => document.body.children.length);
  expect(
    bodyChildren,
    `Route ${route}: blank screen — document.body has no children`,
  ).toBeGreaterThan(0);

  // -------------------------------------------------------------------------
  // 2. No visible React error boundary (unchanged — belt-and-suspenders)
  // -------------------------------------------------------------------------
  const errorBoundary = page.locator('.react-error-boundary, [data-testid="error-boundary"]');
  await expect(
    errorBoundary,
    `Route ${route}: React error boundary is visible`,
  ).not.toBeVisible();

  // -------------------------------------------------------------------------
  // 3. At least one ARIA landmark present (updated — dropped [data-testid] from
  //    the landmark list; it matched error-state testids and made this a no-op)
  // -------------------------------------------------------------------------
  const hasLandmark = await page.evaluate(() => {
    const landmarks = [
      'main', 'nav', 'header', 'footer', 'aside',
      '[role="main"]', '[role="navigation"]', '[role="banner"]',
      '#root',
    ];
    return landmarks.some((selector) => document.querySelector(selector) !== null);
  });
  expect(
    hasLandmark,
    `Route ${route}: no ARIA landmark found — page may not have mounted`,
  ).toBe(true);

  // -------------------------------------------------------------------------
  // 4. ConfigErrorScreen (ADR-077) must NOT be visible (#722)
  //
  // Rendered before the SPA mounts when /config.json fails to load.
  // If this is visible, no page rendered at all — it is a boot-level failure.
  // -------------------------------------------------------------------------
  await expect(
    page.locator('[data-testid="config-error-screen"]'),
    `Route ${route}: ConfigErrorScreen is visible — /config.json failed to load before SPA mount`,
  ).not.toBeVisible();

  // -------------------------------------------------------------------------
  // 5. Generic error-state UI must NOT be visible (#722)
  //
  // .error-state — shared CSS class used by:
  //   ErrorState component (ResultBreakdown, others)
  //   Inline error branch of Decks ("decks-page error-state")
  //   Inline error branch of Collection ("collection-page error-state")
  //
  // [data-testid="collection-error"] — Collection's error branch; present when
  //   the GET /api/v1/collection call fails or returns a non-OK status.
  // -------------------------------------------------------------------------
  await expect(
    page.locator('.error-state').first(),
    `Route ${route}: .error-state is visible — page rendered an error branch (API failure or CORS error)`,
  ).not.toBeVisible();

  await expect(
    page.locator('[data-testid="collection-error"]'),
    `Route ${route}: [data-testid="collection-error"] is visible — Collection API call failed`,
  ).not.toBeVisible();

  // -------------------------------------------------------------------------
  // 6. Known error-message text must NOT be present (#722)
  //
  // Belt-and-suspenders: catches error UI that does not use .error-state (e.g.
  // a component that renders error copy in a plain div, or a future page with a
  // different error pattern). Visibility-checked so hidden retry toasts don't
  // fail the assertion.
  // -------------------------------------------------------------------------
  const errorTextPatterns = [
    'Error Loading Decks',
    'Error Loading Collection',
    'Failed to load performance metrics',
    'Failed to fetch',
    'Something went wrong',
  ];
  for (const pattern of errorTextPatterns) {
    await expect(
      page.getByText(pattern, { exact: false }),
      `Route ${route}: error text "${pattern}" is visible`,
    ).not.toBeVisible();
  }

  // -------------------------------------------------------------------------
  // 7. Route-specific positive health checks (#722)
  //
  // Assert that the healthy-state container for the route IS present.
  // A page stuck on a loading spinner (rendering neither error nor content)
  // also fails here.
  // -------------------------------------------------------------------------
  if (route === '/decks') {
    // .decks-page is always the root wrapper (healthy, error, and loading states
    // all use it). Its presence proves the Decks component mounted beyond a
    // blank screen. The .error-state check above already asserts the co-class
    // "decks-page error-state" is absent.
    await expect(
      page.locator('.decks-page'),
      `Route ${route}: .decks-page container not found — Decks component did not render`,
    ).toBeVisible();
  }

  if (route === '/collection') {
    // [data-testid="collection-page"] is ONLY present on the healthy render path.
    // The error branch renders [data-testid="collection-error"] instead; the
    // loading branch renders [data-testid="collection-loading"]. Neither has
    // "collection-page". Asserting its presence proves a successful data load
    // (or an authenticated empty state — both are healthy).
    await expect(
      page.locator('[data-testid="collection-page"]'),
      `Route ${route}: [data-testid="collection-page"] not found — Collection rendered its error or loading branch`,
    ).toBeVisible();
  }

  if (route === '/charts/result-breakdown') {
    // .page-container is the outermost wrapper of ResultBreakdown, present on
    // ALL render paths (loading, error, empty, data). Its absence means the
    // component did not mount at all (e.g. routing error, blank shell).
    await expect(
      page.locator('.page-container'),
      `Route ${route}: .page-container not found — ResultBreakdown component did not render`,
    ).toBeVisible();
  }
}

/**
 * Establish a real Clerk session via Backend API sign-in-token + FAPI.
 *
 * This is the correct headless auth approach for production-type Clerk instances
 * (pk_live_*). Testing tokens (POST /v1/testing_tokens) work on dev instances
 * only and return 422 on production instances.
 *
 * Flow:
 *   1. Create a sign-in token for ci-smoke@vaultmtg.app via Backend API
 *   2. Process the token via FAPI (strategy=ticket) to get session cookies
 *   3. Inject __client and __client_uat cookies into the browser context
 *   4. Navigate to /home — Clerk JS reads cookies and activates the session
 *
 * Requires CLERK_SECRET_KEY (sk_live_*) to be set in the environment.
 * Callers must call requireAuthOrFail() before this function.
 */
async function signIn(page: Page): Promise<void> {
  // Step 1: Create a sign-in token for the ci-smoke account
  const tokenRes = await fetch('https://api.clerk.com/v1/sign_in_tokens', {
    method: 'POST',
    headers: {
      Authorization: `Bearer ${CLERK_SECRET_KEY}`,
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({ user_id: CI_SMOKE_USER_ID, expires_in_seconds: 300 }),
  });
  if (!tokenRes.ok) {
    throw new Error(`Clerk sign_in_tokens failed: ${tokenRes.status} ${await tokenRes.text()}`);
  }
  const { token: ticketToken } = await tokenRes.json() as { token: string };

  // Step 2: Create a FAPI client (get initial cookies)
  const clientRes = await fetch(
    `${FAPI_BASE}/v1/client?__clerk_api_version=2025-11-10&_clerk_js_version=6.12.1`,
    { headers: { Origin: BASE_URL } },
  );
  const clientSetCookieRaw = clientRes.headers.get('set-cookie') ?? '';
  const clientCookieHeader = clientSetCookieRaw.split(';')[0];

  // Step 3: Sign in with the ticket strategy via FAPI
  const signInRes = await fetch(
    `${FAPI_BASE}/v1/client/sign_ins?__clerk_api_version=2025-11-10&_clerk_js_version=6.12.1`,
    {
      method: 'POST',
      headers: {
        Origin: BASE_URL,
        'Content-Type': 'application/x-www-form-urlencoded',
        Cookie: clientCookieHeader,
      },
      body: `strategy=ticket&ticket=${ticketToken}`,
    },
  );
  if (!signInRes.ok) {
    throw new Error(`FAPI sign_in failed: ${signInRes.status} ${await signInRes.text()}`);
  }
  const signInData = await signInRes.json() as {
    response: { status: string; created_session_id: string };
  };
  if (signInData.response.status !== 'complete') {
    throw new Error(`FAPI sign_in status: ${signInData.response.status} (expected complete)`);
  }

  // Extract session cookies from the sign-in response
  // Clerk returns multiple Set-Cookie headers; raw() gives us all of them
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const rawCookies: string = (signInRes.headers as any).raw?.()?.['set-cookie']?.join('\n') ?? signInRes.headers.get('set-cookie') ?? '';
  const clientCookieMatch = rawCookies.match(/__client=([^;\s]+)/);
  const clientUatMatch = rawCookies.match(/__client_uat=(\d+)/);
  const clientUatHkdsMatch = rawCookies.match(/__client_uat_hKdSwoMR=(\d+)/);

  const clientCookie = clientCookieMatch ? clientCookieMatch[1] : '';
  const clientUat = clientUatMatch ? clientUatMatch[1] : String(Math.floor(Date.now() / 1000));
  const clientUatHkds = clientUatHkdsMatch ? clientUatHkdsMatch[1] : clientUat;

  // Step 4: Inject session cookies into the browser context
  const expiry = Math.floor(Date.now() / 1000) + 86400 * 30;
  const context: BrowserContext = page.context();

  const cookiesToAdd = [
    {
      name: '__client_uat',
      value: clientUat,
      domain: UAT_COOKIE_DOMAIN,
      path: '/',
      httpOnly: false,
      secure: true,
      sameSite: 'None' as const,
      expires: expiry,
    },
    {
      name: '__client_uat_hKdSwoMR',
      value: clientUatHkds,
      domain: UAT_COOKIE_DOMAIN,
      path: '/',
      httpOnly: false,
      secure: true,
      sameSite: 'None' as const,
      expires: expiry,
    },
  ];

  if (clientCookie) {
    cookiesToAdd.push({
      name: '__client',
      value: clientCookie,
      domain: CLERK_COOKIE_DOMAIN,
      path: '/',
      httpOnly: true,
      secure: true,
      sameSite: 'None' as const,
      expires: expiry,
    });
  }

  await context.addCookies(cookiesToAdd);

  // Step 5: Navigate to the app — Clerk JS reads the injected cookies and
  // activates the session. Wait 12 s for Clerk JS to fully initialize and
  // fire the first authenticated API requests.
  await page.goto(`${BASE_URL}/home`, { waitUntil: 'domcontentloaded' });
  await page.waitForSelector('#root > *', { timeout: 30_000 });
  await page.waitForTimeout(12_000);

  // Verify the session is active: __client_uat must be non-zero
  const cookies = await context.cookies([BASE_URL]);
  const uat = cookies.find(c => c.name === '__client_uat');
  if (!uat || parseInt(uat.value) === 0) {
    throw new Error(
      'FAPI sign-in: __client_uat is 0 after cookie injection — session not established.\n' +
      'The Clerk JS SDK may have reset the cookie. Check domain/SameSite configuration.',
    );
  }

  // Verify we are NOT on the sign-in page
  const currentPath = new URL(page.url()).pathname;
  if (currentPath.startsWith('/sign-in')) {
    throw new Error(`FAPI sign-in: page redirected to ${currentPath} — session rejected by Clerk JS`);
  }
}

// ---------------------------------------------------------------------------
// Public routes — no auth required
// ---------------------------------------------------------------------------

test.describe('Staging SPA smoke: public routes', () => {
  for (const route of PUBLIC_ROUTES) {
    test(`${route} — no blank screen, no error boundary`, async ({ page }) => {
      await page.goto(BASE_URL + route, { waitUntil: 'domcontentloaded' });
      await assertPageIsHealthy(page, route);
    });
  }
});

// ---------------------------------------------------------------------------
// Root redirect
// ---------------------------------------------------------------------------

test.describe('Staging SPA smoke: root redirect', () => {
  test('/ redirects to /home or /sign-in (not blank)', async ({ page }) => {
    await page.goto(BASE_URL + '/', { waitUntil: 'domcontentloaded' });

    // Should land on /home (authenticated), /sign-in, or still / while loading
    // App.tsx: <Route path="/" element={<Navigate to="/home" replace />} />
    const currentPath = new URL(page.url()).pathname;
    const isExpectedPath =
      currentPath === '/home' ||
      currentPath === '/match-history' || // allow legacy redirect in case of stale deploy
      currentPath.startsWith('/sign-in') ||
      currentPath === '/';

    expect(
      isExpectedPath,
      `/ redirected to unexpected path: ${currentPath}`,
    ).toBe(true);

    // Either way — no blank screen
    const bodyChildren = await page.evaluate(() => document.body.children.length);
    expect(bodyChildren, '/ redirect resulted in blank screen').toBeGreaterThan(0);
  });
});

// ---------------------------------------------------------------------------
// Protected routes — require Clerk sign-in
//
// Auth-enforcement (#678): if CLERK_SECRET_KEY is absent, every test in this
// block FAILS with an INCONCLUSIVE error rather than silently skipping. This
// is the remediation for the 2026-06-02 false-PASS staging incident.
// ---------------------------------------------------------------------------

test.describe('Staging SPA smoke: protected routes (authenticated)', () => {
  // Use one browser context across all protected route tests to avoid re-signing
  // in for every route. Playwright `test.use` applies per-file, so we manage
  // the shared page manually via a beforeAll/afterAll block.
  let sharedPage: Page;

  // Collects any 401/403 responses from the staging BFF during authenticated
  // route navigation. Any entry here indicates a Clerk instance mismatch or
  // auth regression — the suite fails with a descriptive message.
  const apiAuthErrors: string[] = [];

  // Tracks which surfaces were successfully reached for the verdict banner.
  const authenticatedSurfaces: string[] = [];

  test.beforeAll(async ({ browser }) => {
    // Hard fail if auth is unavailable — do not allow PASS on unauthenticated run.
    requireAuthOrFail();

    const context = await browser.newContext();
    sharedPage = await context.newPage();

    // Attach a response listener BEFORE sign-in so we catch any auth errors
    // during the initial session establishment as well as subsequent navigation.
    sharedPage.on('response', (response) => {
      if (
        response.url().includes(API_BASE_URL) &&
        (response.status() === 401 || response.status() === 403)
      ) {
        apiAuthErrors.push(`${response.status()} ${response.url()}`);
      }
    });

    await signIn(sharedPage);
  });

  test.afterAll(async () => {
    // Emit structured verdict banner so CI log readers can tell what was covered.
    const allRoutes = [...PROTECTED_ROUTES];
    const unauthenticated = allRoutes.filter((r) => !authenticatedSurfaces.includes(r));

    if (unauthenticated.length === 0) {
      console.log(
        '\n' +
        '╔══════════════════════════════════════════════════════════════════════╗\n' +
        '║  STAGING SMOKE VERDICT: PASS (authenticated)                        ║\n' +
        '║  All authenticated surfaces were exercised.                         ║\n' +
        `║  Surfaces reached (${String(authenticatedSurfaces.length).padEnd(2)}): ${authenticatedSurfaces.join(', ').substring(0, 40).padEnd(40)} ║\n` +
        '╚══════════════════════════════════════════════════════════════════════╝',
      );
    } else {
      console.error(
        '\n' +
        '╔══════════════════════════════════════════════════════════════════════╗\n' +
        '║  STAGING SMOKE VERDICT: PARTIAL                                     ║\n' +
        `║  Surfaces authenticated (${String(authenticatedSurfaces.length).padEnd(2)}/${String(allRoutes.length).padEnd(2)}):                              ║\n` +
        authenticatedSurfaces.map((r) => `║    PASS  ${r.padEnd(60)}║`).join('\n') + '\n' +
        `║  Surfaces NOT reached (${String(unauthenticated.length).padEnd(2)}):                                    ║\n` +
        unauthenticated.map((r) => `║    SKIP  ${r.padEnd(60)}║`).join('\n') + '\n' +
        '╚══════════════════════════════════════════════════════════════════════╝',
      );
    }

    // Assert no authenticated API calls returned 401/403 across the entire suite.
    // A non-empty list here means the staging BFF rejected the Clerk session —
    // most likely a Clerk instance mismatch (staging key vs prod key) or an
    // auth regression on the BFF.
    expect(
      apiAuthErrors,
      `Authenticated API calls returned 401/403 — Clerk instance mismatch or auth regression: ${apiAuthErrors.join(', ')}`,
    ).toHaveLength(0);

    if (sharedPage) {
      await sharedPage.context().close();
    }
  });

  for (const route of PROTECTED_ROUTES) {
    test(`${route} — no blank screen, no error boundary`, async () => {
      // Hard fail if auth is unavailable (belt-and-suspenders: beforeAll also throws).
      requireAuthOrFail();

      // Clear per-route errors so failures are attributed to the right route.
      apiAuthErrors.length = 0;

      await sharedPage.goto(BASE_URL + route, { waitUntil: 'domcontentloaded' });

      // If Clerk redirected us to sign-in, the session expired — fail loudly
      const currentPath = new URL(sharedPage.url()).pathname;
      if (currentPath.startsWith('/sign-in')) {
        throw new Error(
          `Route ${route}: Clerk session expired mid-suite — page redirected to ${currentPath}`,
        );
      }

      await assertPageIsHealthy(sharedPage, route);

      // Assert no authenticated API calls returned 401/403 for this route.
      expect(
        [...apiAuthErrors],
        `Route ${route}: authenticated API calls returned 401/403 — Clerk instance mismatch or auth regression: ${apiAuthErrors.join(', ')}`,
      ).toHaveLength(0);

      // Record this surface as successfully authenticated for the verdict banner.
      authenticatedSurfaces.push(route);
    });
  }

  // Verdict test — always the last test in this describe.
  // Verifies the count of authenticated surfaces matches the full list.
  // This is the authoritative PASS/INCONCLUSIVE signal consumed by CI.
  test('Verdict: all protected surfaces were exercised (authenticated)', () => {
    requireAuthOrFail();

    const allRoutes = [...PROTECTED_ROUTES];
    expect(
      authenticatedSurfaces,
      `STAGING SMOKE INCONCLUSIVE: only ${authenticatedSurfaces.length}/${allRoutes.length} surfaces were exercised.\n` +
      `Reached: ${authenticatedSurfaces.join(', ')}\n` +
      `Missing: ${allRoutes.filter((r) => !authenticatedSurfaces.includes(r)).join(', ')}`,
    ).toHaveLength(allRoutes.length);
  });
});
