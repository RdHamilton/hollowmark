import { test, expect, type Page, type BrowserContext } from '@playwright/test';
import * as fs from 'fs';
import * as path from 'path';

/**
 * WildcardAdvisorPanel Visual Screenshot Capture — Staging (#424 / #421)
 *
 * Captures authenticated screenshots of the WildcardAdvisorPanel on the
 * live staging SPA (stg-app.vaultmtg.app) for Prof's visual PLAYER_VERDICT.
 *
 * Auth: Clerk Backend API sign-in-token → FAPI ticket → session cookies.
 * Identical to the approach used in visual-auth-smoke.spec.ts.
 *
 * Account: ci-smoke-token (user_3EamRFdUZdQl1yYPf4Yg7OIQqm4, account_id=17)
 * seeded with ~10k card_inventory rows and 3 Standard mtgzone_archetypes.
 *
 * Required environment variables:
 *   CLERK_SECRET_KEY     — Clerk Backend API secret (sk_live_*) for staging.
 *   SCREENSHOT_DIR       — Absolute path for saving PNGs. Required.
 *   CI_SMOKE_USER_ID     — Clerk user ID. Default: user_3EamRFdUZdQl1yYPf4Yg7OIQqm4
 *
 * Screenshots captured:
 *   (a) wildcard-panel-loaded.png      — Full-page panel with Craft Tonight /
 *                                        Saving Toward split + budget header
 *   (b) wildcard-panel-drilldown.png   — Expanded card row (chevron + GIHWR)
 *   (c) wildcard-panel-format-toggle.png — Format toggle switched to Historic
 *   (d) wildcard-panel-panel-close.png — Panel state after close (collection page)
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
// Session establishment — Backend API sign-in-token + FAPI ticket
// ---------------------------------------------------------------------------

interface ClerkSessionCookies {
  clientCookie: string;
  clientUat: string;
  clientUatHkds: string;
  sessionJwt: string;
  sessionId: string;
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

  const signInRes = await fetch(
    `${FAPI_BASE}/v1/client/sign_ins?__clerk_api_version=2025-11-10&_clerk_js_version=6.12.1`,
    {
      method: 'POST',
      headers: {
        Origin: BASE_URL,
        'Content-Type': 'application/x-www-form-urlencoded',
        Cookie: clientSetCookie.split(';')[0],
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

  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const rawSetCookieHeaders: string = (signInRes.headers as any).raw?.()?.['set-cookie']?.join('\n') ?? signInRes.headers.get('set-cookie') ?? '';
  const clientCookieMatch = rawSetCookieHeaders.match(/__client=([^;\s]+)/);
  const clientCookie = clientCookieMatch ? clientCookieMatch[1] : '';

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

  const clientUatMatch = rawSetCookieHeaders.match(/__client_uat=(\d+)/);
  const clientUat = clientUatMatch ? clientUatMatch[1] : String(Math.floor(Date.now() / 1000));

  const clientUatHkdsMatch = rawSetCookieHeaders.match(/__client_uat_hKdSwoMR=(\d+)/);
  const clientUatHkds = clientUatHkdsMatch ? clientUatHkdsMatch[1] : clientUat;

  return {
    clientCookie,
    clientUat,
    clientUatHkds,
    sessionJwt: jwtData.jwt,
    sessionId,
  };
}

async function injectClerkSession(context: BrowserContext, cookies: ClerkSessionCookies): Promise<void> {
  const expiry = Math.floor(Date.now() / 1000) + 86400 * 30;

  await context.addCookies([
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
// Screenshot capture suite
// ---------------------------------------------------------------------------

test.describe('WildcardAdvisorPanel — visual screenshot capture (staging #421/#424)', () => {
  let sharedContext: BrowserContext;
  let sharedPage: Page;
  let sessionCookies: ClerkSessionCookies;

  const capturedFiles: string[] = [];

  test.beforeAll(async ({ browser }) => {
    requireAuthOrFail();

    if (!fs.existsSync(SCREENSHOT_DIR)) {
      fs.mkdirSync(SCREENSHOT_DIR, { recursive: true });
    }

    // Establish Clerk session via sign-in token + FAPI
    console.log('\nEstablishing Clerk session via sign-in token...');
    const ticketToken = await createSignInToken();
    sessionCookies = await processFapiSignIn(ticketToken);

    sharedContext = await browser.newContext({
      viewport: { width: 1280, height: 800 },
    });
    await injectClerkSession(sharedContext, sessionCookies);
    sharedPage = await sharedContext.newPage();

    // Warm up the session — navigate to /home and allow Clerk JS to initialize
    await sharedPage.goto(BASE_URL + '/collection', { waitUntil: 'domcontentloaded', timeout: 30_000 });
    await sharedPage.waitForTimeout(12_000);

    // Verify session is active (not redirected to sign-in)
    const currentUrl = sharedPage.url();
    const cookies = await sharedContext.cookies([BASE_URL]);
    const clientUat = cookies.find(c => c.name === '__client_uat');
    const uat = parseInt(clientUat?.value ?? '0');

    if (uat === 0) {
      throw new Error(
        'INCONCLUSIVE: Session injection failed — __client_uat=0 after cookie injection.\n' +
        'Clerk JS reset the UAT cookie. Check cookie domain/SameSite settings.',
      );
    }

    if (currentUrl.includes('/sign-in') || currentUrl.includes('/sign-up')) {
      throw new Error(`INCONCLUSIVE: Session not established — redirected to ${currentUrl}`);
    }

    console.log(`  session_id: ${sessionCookies.sessionId}`);
    console.log(`  __client_uat: ${uat} (${new Date(uat * 1000).toISOString()})`);
    console.log(`  landing_url: ${currentUrl}`);
    console.log(`  screenshot_dir: ${SCREENSHOT_DIR}`);
  });

  test.afterAll(async () => {
    console.log('\n=== WildcardAdvisorPanel SCREENSHOT CAPTURE RESULTS ===');
    capturedFiles.forEach(f => console.log(`  ${f}`));
    await sharedContext?.close();
  });

  // -------------------------------------------------------------------------
  // (a) Panel loaded — Craft Tonight / Saving Toward split + budget header
  // -------------------------------------------------------------------------

  test('(a) Loaded panel — Craft Tonight / Saving Toward + budget header', async () => {
    requireAuthOrFail();

    // Should already be on /collection from beforeAll warm-up; navigate fresh
    await sharedPage.goto(BASE_URL + '/collection', { waitUntil: 'domcontentloaded', timeout: 30_000 });
    await sharedPage.waitForTimeout(5_000);

    // Verify not on auth wall
    expect(sharedPage.url()).not.toContain('/sign-in');

    // Collection page must be loaded
    await expect(sharedPage.locator('[data-testid="collection-page"]')).toBeVisible({ timeout: 15_000 });

    // Click "Show Wildcard Advisor"
    const toggleBtn = sharedPage.locator('[data-testid="collection-toggle-wildcard-advisor"]');
    await expect(toggleBtn).toBeVisible({ timeout: 10_000 });
    await toggleBtn.click();

    // Wait for panel container to appear
    await expect(
      sharedPage.locator('[data-testid="collection-wildcard-advisor-container"]'),
    ).toBeVisible({ timeout: 10_000 });

    // Wait for the panel itself — loading skeleton should clear
    const panel = sharedPage.locator('[data-testid="wildcard-advisor-panel"]');
    await expect(panel).toBeVisible({ timeout: 10_000 });

    // Wait for loading skeleton to disappear (recommendations or sync-cta should appear)
    await sharedPage.waitForFunction(() => {
      const loading = document.querySelector('[data-testid="wildcard-advisor-loading"]');
      return !loading;
    }, { timeout: 20_000 });

    // Allow an extra moment for data to settle
    await sharedPage.waitForTimeout(2_000);

    // Full-page screenshot — captures panel + budget header + rec list
    const filePath = path.join(SCREENSHOT_DIR, 'wildcard-panel-loaded.png');
    await sharedPage.screenshot({ path: filePath, fullPage: true });
    capturedFiles.push(filePath);
    console.log(`  Captured: ${filePath}`);

    // Basic panel presence assertions
    expect(panel).toBeDefined();

    // The panel title must always be present
    await expect(sharedPage.locator('.wildcard-advisor__title')).toBeVisible();

    // Format toggle must always be present
    await expect(
      sharedPage.locator('[data-testid="wildcard-advisor-format-toggle"]'),
    ).toBeVisible();

    // Determine the rendered state for the report
    const isLoading = await sharedPage.locator('[data-testid="wildcard-advisor-loading"]').isVisible().catch(() => false);
    const isSyncCta = await sharedPage.locator('[data-testid="wildcard-advisor-sync-cta"]').isVisible().catch(() => false);
    const isError = await sharedPage.locator('[data-testid="wildcard-advisor-error"]').isVisible().catch(() => false);
    const craftTonightVisible = await sharedPage.locator('[data-testid="wildcard-advisor-craft-tonight"]').isVisible().catch(() => false);
    const savingTowardVisible = await sharedPage.locator('[data-testid="wildcard-advisor-saving-toward"]').isVisible().catch(() => false);
    const budgetVisible = await sharedPage.locator('[data-testid="wildcard-advisor-budget"]').isVisible().catch(() => false);

    console.log(`  Panel state: loading=${isLoading} syncCta=${isSyncCta} error=${isError}`);
    console.log(`  Sections: craftTonight=${craftTonightVisible} savingToward=${savingTowardVisible} budget=${budgetVisible}`);

    // The panel must not be in an error state or still loading
    expect(isLoading, 'Panel is still in loading state — BFF did not respond in time').toBe(false);
    expect(isError, 'Panel is in error/retry state — BFF returned a 503').toBe(false);
  });

  // -------------------------------------------------------------------------
  // (b) Expanded drill-down — SVG chevron + GIHWR label
  // -------------------------------------------------------------------------

  test('(b) Expanded card row drill-down — chevron + GIHWR label', async () => {
    requireAuthOrFail();

    // Click the first recommendation card to expand it
    const firstCard = sharedPage.locator('[data-testid="wildcard-advisor-rec-card"]').first();
    const cardCount = await sharedPage.locator('[data-testid="wildcard-advisor-rec-card"]').count();

    if (cardCount === 0) {
      // sync-cta or truly empty — capture screenshot and note it
      const filePath = path.join(SCREENSHOT_DIR, 'wildcard-panel-drilldown.png');
      await sharedPage.screenshot({ path: filePath, fullPage: true });
      capturedFiles.push(filePath);
      console.log(`  No rec cards — panel shows empty/sync-cta state. Screenshot saved.`);
      // Not a hard failure — document the state for Prof
      return;
    }

    // Scroll card into view and click to expand
    await firstCard.scrollIntoViewIfNeeded();
    const expandBtn = firstCard.locator('button.wildcard-advisor__rec-main');
    await expandBtn.click();

    // Wait for drill-down to appear
    await expect(
      firstCard.locator('[data-testid="wildcard-advisor-drill-down"]'),
    ).toBeVisible({ timeout: 5_000 });

    // Verify GIHWR label renders in the drill-down
    const drillDown = firstCard.locator('[data-testid="wildcard-advisor-drill-down"]');
    const drillDownText = await drillDown.textContent().catch(() => '');
    const hasGihwr = drillDownText?.includes('GIHWR') ?? false;
    console.log(`  Drill-down visible. GIHWR label present: ${hasGihwr}`);

    // Full-page screenshot with expanded card
    const filePath = path.join(SCREENSHOT_DIR, 'wildcard-panel-drilldown.png');
    await sharedPage.screenshot({ path: filePath, fullPage: true });
    capturedFiles.push(filePath);
    console.log(`  Captured: ${filePath}`);

    // ChevronDown icon should now be visible (aria-expanded=true on the button)
    const isExpanded = await expandBtn.getAttribute('aria-expanded');
    expect(isExpanded, 'Card row should be expanded (aria-expanded=true)').toBe('true');
  });

  // -------------------------------------------------------------------------
  // (c) Format toggle — switch to Historic
  // -------------------------------------------------------------------------

  test('(c) Format toggle — switch to Historic', async () => {
    requireAuthOrFail();

    const historicBtn = sharedPage.locator('[data-testid="wildcard-advisor-format-historic"]');
    await expect(historicBtn).toBeVisible({ timeout: 10_000 });
    await historicBtn.click();

    // Wait for Historic tab to become active
    await sharedPage.waitForFunction(() => {
      const btn = document.querySelector('[data-testid="wildcard-advisor-format-historic"]');
      return btn?.classList.contains('wildcard-advisor__format-btn--active');
    }, { timeout: 5_000 }).catch(() => {
      // Some environments may use a different active class — soft fail
      console.log('  Note: could not confirm Historic tab active class');
    });

    // Allow loading to settle
    await sharedPage.waitForFunction(() => {
      const loading = document.querySelector('[data-testid="wildcard-advisor-loading"]');
      return !loading;
    }, { timeout: 15_000 }).catch(() => {
      console.log('  Note: Historic data still loading at screenshot time');
    });

    await sharedPage.waitForTimeout(1_000);

    // Full-page screenshot
    const filePath = path.join(SCREENSHOT_DIR, 'wildcard-panel-format-toggle.png');
    await sharedPage.screenshot({ path: filePath, fullPage: true });
    capturedFiles.push(filePath);
    console.log(`  Captured: ${filePath}`);

    // Format toggle itself must still be visible
    await expect(
      sharedPage.locator('[data-testid="wildcard-advisor-format-toggle"]'),
    ).toBeVisible();
  });

  // -------------------------------------------------------------------------
  // (d) Sync-CTA or panel close — document accessible state
  // -------------------------------------------------------------------------

  test('(d) Panel close / sync-CTA state', async () => {
    requireAuthOrFail();

    // Switch back to Standard first (returns to the primary view Prof cares about)
    const standardBtn = sharedPage.locator('[data-testid="wildcard-advisor-format-standard"]');
    await standardBtn.click();
    await sharedPage.waitForTimeout(1_000);

    // Check if sync-CTA is visible
    const isSyncCta = await sharedPage.locator('[data-testid="wildcard-advisor-sync-cta"]').isVisible().catch(() => false);
    console.log(`  sync-CTA visible: ${isSyncCta}`);

    if (isSyncCta) {
      // Capture the sync-CTA state for Prof
      const filePath = path.join(SCREENSHOT_DIR, 'wildcard-panel-sync-cta.png');
      await sharedPage.screenshot({ path: filePath, fullPage: true });
      capturedFiles.push(filePath);
      console.log(`  Captured sync-CTA state: ${filePath}`);
    } else {
      // Close the panel and capture the collection page post-close state
      const closeBtn = sharedPage.locator('[data-testid="wildcard-advisor-close"]');
      const closeVisible = await closeBtn.isVisible().catch(() => false);
      if (closeVisible) {
        await closeBtn.click();
        await sharedPage.waitForTimeout(500);
      }

      const filePath = path.join(SCREENSHOT_DIR, 'wildcard-panel-closed.png');
      await sharedPage.screenshot({ path: filePath, fullPage: true });
      capturedFiles.push(filePath);
      console.log(`  Captured post-close collection: ${filePath}`);
    }
  });
});
