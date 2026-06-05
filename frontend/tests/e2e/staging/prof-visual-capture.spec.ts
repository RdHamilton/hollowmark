import { test, expect, type Page, type BrowserContext } from '@playwright/test';
import * as fs from 'fs';
import * as path from 'path';

/**
 * Prof Visual Capture Harness — parameterized authenticated screenshot capture
 *
 * Driven by environment variables injected by prof-visual-capture.yml.
 * Prof triggers the workflow via `gh workflow run`; this spec runs in CI,
 * authenticates as ci-smoke-token, navigates to the requested surface,
 * performs the requested interaction, and uploads screenshots as an artifact.
 *
 * READ-ONLY: navigates + screenshots only — NO mutations, NO crafting,
 * NO settings changes, NO destructive actions.
 *
 * Auth: Clerk Backend API sign-in-token → FAPI ticket → session cookies.
 * Account: ci-smoke-token (user_3EamRFdUZdQl1yYPf4Yg7OIQqm4, account_id=17)
 *   — seeded with ~10k card_inventory rows and 3 Standard mtgzone_archetypes.
 *
 * Supported routes (PROF_ROUTE env var):
 *   collection              /collection — base collection page
 *   wildcard-advisor        /collection → open wildcard advisor panel
 *   match-history           /match-history
 *   match-details           /match-history → click first row → match detail view
 *   draft-analytics         /draft-analytics
 *   home                    /home — dashboard
 *   decks                   /decks
 *   meta                    /meta
 *   charts-win-rate         /charts/win-rate-trend
 *   charts-deck-perf        /charts/deck-performance
 *
 * Supported actions (PROF_ACTION env var):
 *   screenshot-only         Navigate and screenshot with no further interaction
 *   expand-rec              Expand first wildcard recommendation card (drill-down)
 *   switch-format-historic  Toggle wildcard advisor format to Historic
 *   switch-format-standard  Toggle wildcard advisor format to Standard (reset)
 *   grade-pill              Wait for and screenshot the draft grade pill
 *   open-first-row          Click the first row/item to open a detail view
 *
 * Required environment variables:
 *   CLERK_SECRET_KEY   — Clerk Backend API secret (sk_live_*) from SSM
 *   SCREENSHOT_DIR     — Absolute path for writing PNGs
 *   PROF_ROUTE         — Which surface to visit (see supported routes above)
 *   PROF_ACTION        — What to do before screenshotting (see above)
 *   PROF_LABEL         — Label prefix for screenshot filenames (e.g. "wildcard-v1")
 *   STAGING_SPA_URL    — Override staging SPA URL (optional, default: stg-app.vaultmtg.app)
 *   CI_SMOKE_USER_ID   — Override ci-smoke Clerk user ID (optional)
 */

// ---------------------------------------------------------------------------
// Config from environment
// ---------------------------------------------------------------------------

const CLERK_SECRET_KEY = process.env.CLERK_SECRET_KEY ?? '';
const BASE_URL = process.env.STAGING_SPA_URL || 'https://stg-app.vaultmtg.app';
const FAPI_BASE = 'https://clerk.stg-app.vaultmtg.app';
const CI_SMOKE_USER_ID = process.env.CI_SMOKE_USER_ID || 'user_3EamRFdUZdQl1yYPf4Yg7OIQqm4';
const SCREENSHOT_DIR = process.env.SCREENSHOT_DIR ?? '';
const PROF_ROUTE = process.env.PROF_ROUTE ?? 'collection';
const PROF_ACTION = process.env.PROF_ACTION ?? 'screenshot-only';
const PROF_LABEL = process.env.PROF_LABEL ?? 'capture';

// ---------------------------------------------------------------------------
// Route → URL path mapping
// ---------------------------------------------------------------------------

const ROUTE_PATHS: Record<string, string> = {
  'collection': '/collection',
  'wildcard-advisor': '/collection',
  'match-history': '/match-history',
  'match-details': '/match-history',
  'draft-analytics': '/draft-analytics',
  'home': '/home',
  'decks': '/decks',
  'meta': '/meta',
  'charts-win-rate': '/charts/win-rate-trend',
  'charts-deck-perf': '/charts/deck-performance',
};

// ---------------------------------------------------------------------------
// Auth enforcement
// ---------------------------------------------------------------------------

function requireAuthOrFail(): void {
  if (!CLERK_SECRET_KEY) {
    throw new Error(
      'INCONCLUSIVE: CLERK_SECRET_KEY is not set.\n' +
      'This suite requires sk_live_* from SSM /vaultmtg/staging/CLERK_SECRET_KEY.\n' +
      'The CI workflow (prof-visual-capture.yml) injects it automatically.\n' +
      'Verdict: INCONCLUSIVE — treat as FAIL.',
    );
  }
  if (!SCREENSHOT_DIR) {
    throw new Error('SCREENSHOT_DIR environment variable is required.');
  }
}

// ---------------------------------------------------------------------------
// Session establishment — Backend API sign-in-token + FAPI ticket
// (same pattern as staging-spa-smoke.spec.ts and wildcard-panel-visual-424.spec.ts)
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
// Shared helpers
// ---------------------------------------------------------------------------

async function dismissOnboardingModal(page: Page): Promise<void> {
  const modal = page.locator('[data-testid="onboarding-modal"]');
  const visible = await modal.isVisible().catch(() => false);
  if (!visible) return;
  const closeBtn = page.locator('[data-testid="onboarding-modal-close"]');
  if (await closeBtn.isVisible().catch(() => false)) {
    await closeBtn.click();
    await page.waitForTimeout(400);
  } else {
    await page.keyboard.press('Escape');
    await page.waitForTimeout(400);
  }
  await modal.waitFor({ state: 'hidden', timeout: 5_000 }).catch(() => {});
}

function screenshot(label: string, suffix: string): string {
  return path.join(SCREENSHOT_DIR, `${label}-${suffix}.png`);
}

// ---------------------------------------------------------------------------
// Actions
// ---------------------------------------------------------------------------

async function actionScreenshotOnly(page: Page, label: string, capturedFiles: string[]): Promise<void> {
  await page.waitForTimeout(3_000);
  const filePath = screenshot(label, 'page');
  await page.screenshot({ path: filePath, fullPage: true });
  capturedFiles.push(filePath);
  console.log(`  Captured: ${filePath}`);
}

async function actionOpenWildcardAdvisor(page: Page, label: string, capturedFiles: string[]): Promise<void> {
  // Must be on /collection already
  await expect(page.locator('[data-testid="collection-page"]')).toBeVisible({ timeout: 15_000 });
  const toggleBtn = page.locator('[data-testid="collection-toggle-wildcard-advisor"]');
  await expect(toggleBtn).toBeVisible({ timeout: 10_000 });
  await toggleBtn.click();
  await expect(page.locator('[data-testid="wildcard-advisor-panel"]')).toBeVisible({ timeout: 10_000 });
  // Wait for loading to clear
  await page.waitForFunction(
    () => !document.querySelector('[data-testid="wildcard-advisor-loading"]'),
    { timeout: 20_000 },
  ).catch(() => console.log('  Note: loading indicator still present at screenshot time'));
  await page.waitForTimeout(2_000);
  const filePath = screenshot(label, 'wildcard-advisor-open');
  await page.screenshot({ path: filePath, fullPage: true });
  capturedFiles.push(filePath);
  console.log(`  Captured: ${filePath}`);
}

async function actionExpandRec(page: Page, label: string, capturedFiles: string[]): Promise<void> {
  // First open the advisor if not yet open
  const panelVisible = await page.locator('[data-testid="wildcard-advisor-panel"]').isVisible().catch(() => false);
  if (!panelVisible) {
    await actionOpenWildcardAdvisor(page, label, capturedFiles);
  }
  const cards = page.locator('[data-testid="wildcard-advisor-rec-card"]');
  const count = await cards.count();
  if (count === 0) {
    const filePath = screenshot(label, 'wildcard-advisor-no-recs');
    await page.screenshot({ path: filePath, fullPage: true });
    capturedFiles.push(filePath);
    console.log(`  No rec cards visible — empty/sync-cta state. Captured: ${filePath}`);
    return;
  }
  const firstCard = cards.first();
  await firstCard.scrollIntoViewIfNeeded();
  const expandBtn = firstCard.locator('button.wildcard-advisor__rec-main');
  await expandBtn.click();
  await expect(firstCard.locator('[data-testid="wildcard-advisor-drill-down"]')).toBeVisible({ timeout: 5_000 });
  await page.waitForTimeout(500);
  const filePath = screenshot(label, 'wildcard-advisor-rec-expanded');
  await page.screenshot({ path: filePath, fullPage: true });
  capturedFiles.push(filePath);
  console.log(`  Captured: ${filePath}`);
}

async function actionSwitchFormatHistoric(page: Page, label: string, capturedFiles: string[]): Promise<void> {
  const panelVisible = await page.locator('[data-testid="wildcard-advisor-panel"]').isVisible().catch(() => false);
  if (!panelVisible) {
    await actionOpenWildcardAdvisor(page, label, capturedFiles);
  }
  const historicBtn = page.locator('[data-testid="wildcard-advisor-format-historic"]');
  await expect(historicBtn).toBeVisible({ timeout: 10_000 });
  await historicBtn.click();
  await page.waitForFunction(
    () => !document.querySelector('[data-testid="wildcard-advisor-loading"]'),
    { timeout: 15_000 },
  ).catch(() => console.log('  Note: Historic data still loading at screenshot time'));
  await page.waitForTimeout(1_000);
  const filePath = screenshot(label, 'wildcard-advisor-historic');
  await page.screenshot({ path: filePath, fullPage: true });
  capturedFiles.push(filePath);
  console.log(`  Captured: ${filePath}`);
}

async function actionSwitchFormatStandard(page: Page, label: string, capturedFiles: string[]): Promise<void> {
  const panelVisible = await page.locator('[data-testid="wildcard-advisor-panel"]').isVisible().catch(() => false);
  if (!panelVisible) {
    await actionOpenWildcardAdvisor(page, label, capturedFiles);
  }
  const standardBtn = page.locator('[data-testid="wildcard-advisor-format-standard"]');
  await expect(standardBtn).toBeVisible({ timeout: 10_000 });
  await standardBtn.click();
  await page.waitForFunction(
    () => !document.querySelector('[data-testid="wildcard-advisor-loading"]'),
    { timeout: 15_000 },
  ).catch(() => console.log('  Note: Standard data still loading at screenshot time'));
  await page.waitForTimeout(1_000);
  const filePath = screenshot(label, 'wildcard-advisor-standard');
  await page.screenshot({ path: filePath, fullPage: true });
  capturedFiles.push(filePath);
  console.log(`  Captured: ${filePath}`);
}

async function actionGradePill(page: Page, label: string, capturedFiles: string[]): Promise<void> {
  // Wait for the draft grade pill on /draft-analytics
  await page.waitForFunction(
    () => document.querySelector('[data-testid="session-overall-grade"]') !== null,
    { timeout: 20_000 },
  ).catch(() => console.log('  Note: grade pill not found within timeout'));
  await page.waitForTimeout(1_000);
  const filePath = screenshot(label, 'draft-analytics-grade-pill');
  await page.screenshot({ path: filePath, fullPage: true });
  capturedFiles.push(filePath);
  console.log(`  Captured: ${filePath}`);
}

async function actionOpenFirstRow(page: Page, label: string, capturedFiles: string[]): Promise<void> {
  await page.waitForTimeout(3_000);
  // Capture landing state first
  const landingPath = screenshot(label, 'list-view');
  await page.screenshot({ path: landingPath, fullPage: true });
  capturedFiles.push(landingPath);
  console.log(`  Captured list view: ${landingPath}`);

  // Try to click the first clickable row — match-history rows or similar
  const firstRow = page.locator('[data-testid="match-row"], [data-testid="deck-row"], tr[role="row"]').first();
  const rowCount = await page.locator('[data-testid="match-row"], [data-testid="deck-row"], tr[role="row"]').count();
  if (rowCount === 0) {
    console.log('  No clickable rows found — empty state. Landing screenshot already captured.');
    return;
  }
  await firstRow.scrollIntoViewIfNeeded();
  await firstRow.click();
  await page.waitForTimeout(2_000);
  const detailPath = screenshot(label, 'detail-view');
  await page.screenshot({ path: detailPath, fullPage: true });
  capturedFiles.push(detailPath);
  console.log(`  Captured detail view: ${detailPath}`);
}

// ---------------------------------------------------------------------------
// Main capture suite
// ---------------------------------------------------------------------------

test.describe('Prof Visual Capture Harness', () => {
  let sharedContext: BrowserContext;
  let sharedPage: Page;
  let sessionCookies: ClerkSessionCookies;

  const capturedFiles: string[] = [];

  test.beforeAll(async ({ browser }) => {
    requireAuthOrFail();

    if (!fs.existsSync(SCREENSHOT_DIR)) {
      fs.mkdirSync(SCREENSHOT_DIR, { recursive: true });
    }

    const routePath = ROUTE_PATHS[PROF_ROUTE];
    if (!routePath) {
      throw new Error(
        `Unknown PROF_ROUTE: "${PROF_ROUTE}". ` +
        `Supported values: ${Object.keys(ROUTE_PATHS).join(', ')}`,
      );
    }

    console.log(`\n=== Prof Visual Capture ===`);
    console.log(`  route:  ${PROF_ROUTE} (${routePath})`);
    console.log(`  action: ${PROF_ACTION}`);
    console.log(`  label:  ${PROF_LABEL}`);
    console.log(`  dir:    ${SCREENSHOT_DIR}`);

    // Establish Clerk session
    console.log('\nEstablishing Clerk session via sign-in token...');
    const ticketToken = await createSignInToken();
    sessionCookies = await processFapiSignIn(ticketToken);

    sharedContext = await browser.newContext({
      viewport: { width: 1280, height: 800 },
    });
    await injectClerkSession(sharedContext, sessionCookies);
    sharedPage = await sharedContext.newPage();

    // Warm up — navigate to the target route and allow Clerk JS to initialize
    await sharedPage.goto(BASE_URL + routePath, { waitUntil: 'domcontentloaded', timeout: 30_000 });
    await sharedPage.waitForTimeout(12_000);

    await dismissOnboardingModal(sharedPage);

    // Verify session is active
    const pageCookies = await sharedContext.cookies([BASE_URL]);
    const clientUat = pageCookies.find(c => c.name === '__client_uat');
    const uat = parseInt(clientUat?.value ?? '0');

    if (uat === 0) {
      throw new Error(
        'INCONCLUSIVE: Session injection failed — __client_uat=0 after cookie injection.\n' +
        'Check cookie domain/SameSite settings.',
      );
    }

    const currentUrl = sharedPage.url();
    if (currentUrl.includes('/sign-in') || currentUrl.includes('/sign-up')) {
      throw new Error(`INCONCLUSIVE: Session not established — redirected to ${currentUrl}`);
    }

    console.log(`  session_id: ${sessionCookies.sessionId}`);
    console.log(`  __client_uat: ${uat} (${new Date(uat * 1000).toISOString()})`);
    console.log(`  landing_url: ${currentUrl}`);
  });

  test.afterAll(async () => {
    console.log('\n=== PROF CAPTURE RESULTS ===');
    if (capturedFiles.length === 0) {
      console.log('  (no screenshots captured)');
    } else {
      capturedFiles.forEach(f => console.log(`  ${path.basename(f)}`));
    }
    await sharedContext?.close();
  });

  // Single test — the action dispatch happens here
  test('capture: navigate route + perform action', async () => {
    requireAuthOrFail();

    const routePath = ROUTE_PATHS[PROF_ROUTE];
    expect(routePath, `Unknown PROF_ROUTE: "${PROF_ROUTE}"`).toBeDefined();

    // Ensure we are on the target route (beforeAll may have navigated already,
    // but be explicit so each test is self-contained)
    const currentPath = new URL(sharedPage.url()).pathname;
    if (currentPath !== routePath) {
      await sharedPage.goto(BASE_URL + routePath, { waitUntil: 'domcontentloaded', timeout: 30_000 });
      await sharedPage.waitForTimeout(5_000);
      await dismissOnboardingModal(sharedPage);
    }

    // Verify not on auth wall
    expect(sharedPage.url(), 'Session redirected to sign-in').not.toContain('/sign-in');

    // Dispatch action
    switch (PROF_ACTION) {
      case 'screenshot-only':
        await actionScreenshotOnly(sharedPage, PROF_LABEL, capturedFiles);
        break;
      case 'open-wildcard-advisor':
        await actionOpenWildcardAdvisor(sharedPage, PROF_LABEL, capturedFiles);
        break;
      case 'expand-rec':
        await actionExpandRec(sharedPage, PROF_LABEL, capturedFiles);
        break;
      case 'switch-format-historic':
        await actionSwitchFormatHistoric(sharedPage, PROF_LABEL, capturedFiles);
        break;
      case 'switch-format-standard':
        await actionSwitchFormatStandard(sharedPage, PROF_LABEL, capturedFiles);
        break;
      case 'grade-pill':
        await actionGradePill(sharedPage, PROF_LABEL, capturedFiles);
        break;
      case 'open-first-row':
        await actionOpenFirstRow(sharedPage, PROF_LABEL, capturedFiles);
        break;
      default:
        throw new Error(
          `Unknown PROF_ACTION: "${PROF_ACTION}". ` +
          `Supported values: screenshot-only, open-wildcard-advisor, expand-rec, ` +
          `switch-format-historic, switch-format-standard, grade-pill, open-first-row`,
        );
    }

    expect(capturedFiles.length, 'No screenshots were captured').toBeGreaterThan(0);
    console.log(`\n  Total screenshots: ${capturedFiles.length}`);
    capturedFiles.forEach(f => console.log(`    ${path.basename(f)}`));
  });
});
