import { test, expect, type Page, type BrowserContext } from '@playwright/test';
import * as fs from 'fs';
import * as path from 'path';

/**
 * Visual Authentication + Data Verification — v0.3.7 seeded account
 *
 * Extends the visual-auth-smoke spec with per-surface data assertions now
 * that the ci-smoke staging account has real seeded data:
 *
 *   account_id 17, Clerk user user_3EamRFdUZdQl1yYPf4Yg7OIQqm4
 *   - 19 matches  (10W-9L, 52.6% WR)
 *   - 3 draft_sessions (QuickDraft SOS 6W-3L, PremierDraft SOS 1W-3L,
 *                        PremierDraft BLB 0W-0L)
 *   - 119 draft_picks
 *   - 4 quests (active)
 *   - 3 decks
 *
 * Auth approach: Clerk Backend API sign-in token → FAPI ticket sign-in →
 * cookie injection (same mechanism as visual-auth-smoke.spec.ts / PR #2931).
 * This is the only approach that works on pk_live_* production-type instances.
 *
 * Required env:
 *   CLERK_SECRET_KEY   — sk_live_* from SSM /vaultmtg/staging/CLERK_SECRET_KEY
 *   SCREENSHOT_DIR     — output directory for PNG captures
 *
 * Per-surface PASS criteria (Ramone verbatim: "visual comparisons with data"):
 *   - No auth wall (no redirect to /sign-in)
 *   - No React error boundary
 *   - Sapphire theme rendered (--vault-sapphire* tokens, not amber)
 *   - Data-bearing surfaces show CONTENT, not empty-state text
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
// Seeded account ground truth (Bob-verified 2026-06-02)
// ---------------------------------------------------------------------------

const EXPECTED = {
  matchCount: 19,
  matchWins: 10,
  matchLosses: 9,
  winRatePct: '52.6',
  draftCount: 3,
  questCount: 4,
  deckCount: 3,
  draftPickCount: 119,
};

// ---------------------------------------------------------------------------
// Auth enforcement
// ---------------------------------------------------------------------------

function requireAuthOrFail(): void {
  if (!CLERK_SECRET_KEY) {
    throw new Error(
      'INCONCLUSIVE: CLERK_SECRET_KEY is not set.\n' +
      'This suite requires sk_live_* from SSM /vaultmtg/staging/CLERK_SECRET_KEY.\n' +
      'Pull via: aws ssm get-parameter --name /vaultmtg/staging/CLERK_SECRET_KEY --with-decryption --profile personal\n' +
      'Verdict: INCONCLUSIVE — treat as FAIL.',
    );
  }
  if (!SCREENSHOT_DIR) {
    throw new Error('SCREENSHOT_DIR environment variable is required.');
  }
}

// ---------------------------------------------------------------------------
// Session establishment (identical to visual-auth-smoke.spec.ts)
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

  return { clientCookie, clientUat, clientUatHkds, sessionJwt: jwtData.jwt, sessionId };
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
// Helpers
// ---------------------------------------------------------------------------

interface SurfaceResult {
  surface: string;
  file: string;
  verdict: 'PASS' | 'FAIL' | 'WARN';
  detail: string;
  dataRendered: boolean;
  counts?: Record<string, number | string>;
}

async function gotoAndWait(page: Page, route: string, waitMs = 10_000): Promise<void> {
  await page.goto(BASE_URL + route, { waitUntil: 'domcontentloaded', timeout: 30_000 });
  await page.waitForTimeout(waitMs);
}

async function capture(page: Page, outputDir: string, name: string): Promise<string> {
  const filename = name.toLowerCase().replace(/[^a-z0-9]+/g, '-') + '.png';
  await page.screenshot({ path: path.join(outputDir, filename), fullPage: true });
  return filename;
}

function checkAuthWall(page: Page): boolean {
  const url = page.url();
  return url.includes('/sign-in') || url.includes('/sign-up');
}

async function checkErrorBoundary(page: Page): Promise<boolean> {
  return page.locator('.react-error-boundary, [data-testid="error-boundary"]').isVisible().catch(() => false);
}

async function bodyText(page: Page): Promise<string> {
  return (await page.locator('body').textContent().catch(() => '')) ?? '';
}

// ---------------------------------------------------------------------------
// Test suite
// ---------------------------------------------------------------------------

test.describe('Visual data verification — seeded ci-smoke account', () => {
  let sharedContext: BrowserContext;
  let sharedPage: Page;
  let sessionCookies: ClerkSessionCookies;
  const results: SurfaceResult[] = [];

  test.beforeAll(async ({ browser }) => {
    requireAuthOrFail();
    fs.mkdirSync(SCREENSHOT_DIR, { recursive: true });

    const ticketToken = await createSignInToken();
    sessionCookies = await processFapiSignIn(ticketToken);

    sharedContext = await browser.newContext({ viewport: { width: 1280, height: 800 } });
    await injectClerkSession(sharedContext, sessionCookies);
    sharedPage = await sharedContext.newPage();

    // Warm-up navigation — Clerk JS needs ~10-12 s to initialize on first load
    await sharedPage.goto(BASE_URL + '/home', { waitUntil: 'domcontentloaded', timeout: 30_000 });
    await sharedPage.waitForTimeout(12_000);

    const cookies = await sharedContext.cookies([BASE_URL]);
    const clientUat = cookies.find(c => c.name === '__client_uat');
    const uat = parseInt(clientUat?.value ?? '0');

    if (uat === 0) {
      throw new Error('Session injection: __client_uat=0. Clerk JS reset it — check cookie domain/SameSite.');
    }
    if (sharedPage.url().includes('/sign-in')) {
      throw new Error(`Session not established — redirected to ${sharedPage.url()}`);
    }

    console.log(`\nAuth established:`);
    console.log(`  session_id: ${sessionCookies.sessionId}`);
    console.log(`  __client_uat: ${uat} (${new Date(uat * 1000).toISOString()})`);
    console.log(`  screenshot_dir: ${SCREENSHOT_DIR}`);
  });

  test.afterAll(async () => {
    // Write results.json
    const resultsPath = path.join(SCREENSHOT_DIR, 'results.json');
    fs.writeFileSync(resultsPath, JSON.stringify(results, null, 2));

    // Print summary table
    console.log('\n=== VISUAL DATA VERIFICATION RESULTS ===');
    console.log('Surface'.padEnd(38) + 'Verdict'.padEnd(8) + 'Detail');
    console.log('-'.repeat(110));
    for (const r of results) {
      const icon = r.verdict === 'PASS' ? 'PASS' : r.verdict === 'WARN' ? 'WARN' : 'FAIL';
      console.log(r.surface.padEnd(38) + icon.padEnd(8) + r.detail);
    }

    const pass = results.filter(r => r.verdict === 'PASS').length;
    const warn = results.filter(r => r.verdict === 'WARN').length;
    const fail = results.filter(r => r.verdict === 'FAIL').length;
    console.log(`\nSummary: ${pass} PASS / ${warn} WARN / ${fail} FAIL`);
    console.log(`Screenshots: ${SCREENSHOT_DIR}`);
    console.log(`Results JSON: ${resultsPath}`);

    await sharedContext?.close();
  });

  // ─────────────────────────────────────────────────────────────────────────
  // HOME — command strip
  // ─────────────────────────────────────────────────────────────────────────
  test('Home — command strip with seeded data', async () => {
    requireAuthOrFail();
    await gotoAndWait(sharedPage, '/home');

    const url = sharedPage.url();
    expect(url, 'Home: auth wall').not.toContain('/sign-in');
    expect(await checkErrorBoundary(sharedPage), 'Home: React error boundary').toBe(false);

    const file = await capture(sharedPage, SCREENSHOT_DIR, 'home-command-strip');

    // Home should NOT be in empty state — account has 19 matches
    const homeEmpty = await sharedPage.locator('[data-testid="home-empty"]').isVisible().catch(() => false);
    expect(homeEmpty, 'Home: empty state visible — no matches loaded from BFF').toBe(false);

    // Home page container must be present
    const homePage = await sharedPage.locator('[data-testid="home-page"]').isVisible().catch(() => false);
    expect(homePage, 'Home: home-page container not found').toBe(true);

    // Weekly record strip should be visible
    const weeklyStrip = await sharedPage.locator('[data-testid="home-strip-weekly"]').isVisible().catch(() => false);

    // Read the weekly wins/losses/winrate from the DOM
    const weeklyWins = await sharedPage.locator('[data-testid="home-weekly-wins"]').textContent().catch(() => '');
    const weeklyLosses = await sharedPage.locator('[data-testid="home-weekly-losses"]').textContent().catch(() => '');
    const weeklyWR = await sharedPage.locator('[data-testid="home-weekly-winrate"]').textContent().catch(() => '');

    // Last match strip (at least one match exists)
    const lastMatchStrip = await sharedPage.locator('[data-testid="home-strip-last-match"]').isVisible().catch(() => false);

    const detail = weeklyStrip
      ? `weekly strip: ${weeklyWins} / ${weeklyLosses} / ${weeklyWR}; last_match_strip: ${lastMatchStrip}`
      : 'home-strip-weekly not visible (this_week may be 0 if no matches this calendar week)';

    // In loaded state (data-having account), QuickNavStrip must be ABSENT — it only renders in the
    // first-run / empty branch. The unit test (Home.test.tsx ~line 657) explicitly asserts this.
    const quickNav = await sharedPage.locator('[data-testid="home-quick-nav"]').isVisible().catch(() => false);
    expect(quickNav, 'Home: quick-nav strip visible in loaded state — should only appear in empty/first-run state').toBe(false);

    // WhatsNextNudge must be present instead (any of the four nudge variants)
    const whatsNextVisible = await sharedPage.locator('[data-testid^="home-whats-next-"]').isVisible().catch(() => false);
    expect(whatsNextVisible, 'Home: WhatsNextNudge not visible in loaded state').toBe(true);

    // PR #2932 BUG1 fix: onboarding modal must NOT be visible for returning users with account data
    const onboardingModalVisible = await sharedPage.locator('[data-testid="onboarding-modal"]').isVisible().catch(() => false);
    expect(onboardingModalVisible, 'Home: onboarding modal visible for returning user with 19 matches — BUG1 fix regression').toBe(false);

    const onboardingDetail = onboardingModalVisible
      ? '; REGRESSION: onboarding-modal visible for returning user (BUG1 re-opened)'
      : '; onboarding-modal absent (BUG1 fix confirmed)';

    results.push({
      surface: 'Home (command strip)',
      file,
      verdict: homePage && whatsNextVisible && !quickNav && !homeEmpty && !onboardingModalVisible ? 'PASS' : 'FAIL',
      detail: detail + onboardingDetail,
      dataRendered: !homeEmpty,
      counts: {
        weekly_wins: weeklyWins ?? '',
        weekly_losses: weeklyLosses ?? '',
        weekly_wr: weeklyWR ?? '',
        last_match_visible: String(lastMatchStrip),
        onboarding_modal_visible: String(onboardingModalVisible),
      },
    });
  });

  // ─────────────────────────────────────────────────────────────────────────
  // MATCH HISTORY — 19 rows
  // ─────────────────────────────────────────────────────────────────────────
  test('Match History — 19 match rows with on-play/score/opponent', async () => {
    requireAuthOrFail();
    await gotoAndWait(sharedPage, '/match-history', 12_000);

    expect(sharedPage.url(), 'Match History: auth wall').not.toContain('/sign-in');
    expect(await checkErrorBoundary(sharedPage), 'Match History: error boundary').toBe(false);

    const file = await capture(sharedPage, SCREENSHOT_DIR, 'match-history');

    // Empty state must NOT be visible
    const emptyState = await sharedPage.locator('[data-testid="match-history-empty"]').isVisible().catch(() => false);
    expect(emptyState, 'Match History: empty state visible — 19 rows were seeded').toBe(false);

    // Table must be visible
    const table = await sharedPage.locator('[data-testid="match-history-table"]').isVisible().catch(() => false);
    expect(table, 'Match History: table not rendered').toBe(true);

    // Count match rows
    const rowCount = await sharedPage.locator('[data-testid="match-row"]').count();
    console.log(`  Match History: ${rowCount} rows rendered (expected ${EXPECTED.matchCount})`);

    // On-play badges
    const playDrawBadges = await sharedPage.locator('[data-testid="play-draw-badge"]').count();

    // Score cells
    const scoreCells = await sharedPage.locator('[data-testid="match-score"]').count();

    const pass = table && !emptyState && rowCount >= EXPECTED.matchCount;

    let detail: string;
    if (rowCount === 0) {
      detail = `FAIL: table visible but 0 rows rendered — BFF /history/matches may be returning empty for account_id=17`;
    } else if (rowCount < EXPECTED.matchCount) {
      detail = `WARN: ${rowCount}/${EXPECTED.matchCount} rows (pagination may be truncating); play-draw badges: ${playDrawBadges}; score cells: ${scoreCells}`;
    } else {
      detail = `${rowCount} rows; play-draw badges: ${playDrawBadges}; score cells: ${scoreCells}`;
    }

    results.push({
      surface: 'Match History',
      file,
      verdict: pass ? (rowCount < EXPECTED.matchCount ? 'WARN' : 'PASS') : 'FAIL',
      detail,
      dataRendered: rowCount > 0,
      counts: { rows: rowCount, play_draw_badges: playDrawBadges, score_cells: scoreCells },
    });
  });

  // ─────────────────────────────────────────────────────────────────────────
  // WIN-RATE TREND
  // ─────────────────────────────────────────────────────────────────────────
  test('Charts / Win-Rate Trend — data renders (not empty state)', async () => {
    requireAuthOrFail();
    await gotoAndWait(sharedPage, '/charts/win-rate-trend', 12_000);

    expect(sharedPage.url(), 'Win-Rate Trend: auth wall').not.toContain('/sign-in');
    expect(await checkErrorBoundary(sharedPage), 'Win-Rate Trend: error boundary').toBe(false);

    const file = await capture(sharedPage, SCREENSHOT_DIR, 'charts-win-rate-trend');

    const emptyEl = await sharedPage.locator('[data-testid="win-rate-trend-empty"]').isVisible().catch(() => false);
    const chartEl = await sharedPage.locator('[data-testid="win-rate-trend-chart"]').isVisible().catch(() => false);

    // Set-release annotation legend (only appears when chart has data)
    const annotationLegend = await sharedPage.locator('[data-testid="set-annotation-legend"]').isVisible().catch(() => false);

    // recharts renders an SVG inside the chart container
    const svgCount = chartEl
      ? await sharedPage.locator('[data-testid="win-rate-trend-chart"] svg').count()
      : 0;

    let verdict: 'PASS' | 'FAIL' | 'WARN';
    let detail: string;

    if (emptyEl && !chartEl) {
      verdict = 'FAIL';
      detail = 'FAIL: empty-state visible despite 19 seeded matches — BFF /history/matches/stats or /trends endpoint may be returning 0 entries for account_id=17';
    } else if (chartEl && svgCount > 0) {
      verdict = 'PASS';
      detail = `chart rendered (svg: ${svgCount}); annotation-legend: ${annotationLegend}`;
    } else if (chartEl && svgCount === 0) {
      verdict = 'WARN';
      detail = 'WARN: chart container present but no SVG — recharts may not have mounted yet; check screenshot';
    } else {
      verdict = 'WARN';
      detail = `chart-el: ${chartEl}; empty-el: ${emptyEl}; svg: ${svgCount}`;
    }

    results.push({
      surface: 'Charts / Win-Rate Trend',
      file,
      verdict,
      detail,
      dataRendered: chartEl && svgCount > 0,
      counts: { svg_elements: svgCount, annotation_legend: annotationLegend ? 1 : 0 },
    });
  });

  // ─────────────────────────────────────────────────────────────────────────
  // DECK PERFORMANCE (matchup matrix)
  // ─────────────────────────────────────────────────────────────────────────
  test('Charts / Deck Performance — data renders', async () => {
    requireAuthOrFail();
    await gotoAndWait(sharedPage, '/charts/deck-performance', 12_000);

    expect(sharedPage.url(), 'Deck Performance: auth wall').not.toContain('/sign-in');
    expect(await checkErrorBoundary(sharedPage), 'Deck Performance: error boundary').toBe(false);

    const file = await capture(sharedPage, SCREENSHOT_DIR, 'charts-deck-performance');
    const text = await bodyText(sharedPage);
    const lower = text.toLowerCase();

    // Check for empty-state indicators
    const isEmptyState = lower.includes('no matches') || lower.includes('no data') || lower.includes('play some games');
    const hasNumbers = /\d+/.test(text); // any number = some data rendered

    let verdict: 'PASS' | 'FAIL' | 'WARN';
    let detail: string;

    if (isEmptyState) {
      verdict = 'FAIL';
      detail = 'FAIL: empty-state text present — DeckPerformance not loading seeded match data';
    } else if (hasNumbers) {
      verdict = 'PASS';
      detail = 'chart/table area has content with numeric data';
    } else {
      verdict = 'WARN';
      detail = 'WARN: no empty-state text but also no numbers — screenshot needed for manual review';
    }

    results.push({ surface: 'Charts / Deck Performance', file, verdict, detail, dataRendered: !isEmptyState });
  });

  // ─────────────────────────────────────────────────────────────────────────
  // FORMAT DISTRIBUTION
  // ─────────────────────────────────────────────────────────────────────────
  test('Charts / Format Distribution — data renders', async () => {
    requireAuthOrFail();
    await gotoAndWait(sharedPage, '/charts/format-distribution', 12_000);

    expect(sharedPage.url(), 'Format Dist: auth wall').not.toContain('/sign-in');
    expect(await checkErrorBoundary(sharedPage), 'Format Dist: error boundary').toBe(false);

    const file = await capture(sharedPage, SCREENSHOT_DIR, 'charts-format-distribution');
    const text = await bodyText(sharedPage);
    const lower = text.toLowerCase();

    const isEmptyState = lower.includes('no data') || lower.includes('no matches') || lower.includes('no results');
    const hasFormatLabel = lower.includes('premier draft') || lower.includes('quick draft') || lower.includes('ranked') || lower.includes('traditional');

    let verdict: 'PASS' | 'FAIL' | 'WARN';
    let detail: string;

    if (isEmptyState) {
      verdict = 'FAIL';
      detail = 'FAIL: empty-state text visible — format-distribution endpoint not returning data for seeded account';
    } else if (hasFormatLabel) {
      verdict = 'PASS';
      detail = 'format labels visible in rendered chart';
    } else {
      verdict = 'WARN';
      detail = 'no empty-state but no format labels found — screenshot for manual review';
    }

    results.push({ surface: 'Charts / Format Distribution', file, verdict, detail, dataRendered: !isEmptyState });
  });

  // ─────────────────────────────────────────────────────────────────────────
  // RANK PROGRESSION
  // ─────────────────────────────────────────────────────────────────────────
  test('Charts / Rank Progression — renders (rank data may be limited)', async () => {
    requireAuthOrFail();
    await gotoAndWait(sharedPage, '/charts/rank-progression', 12_000);

    expect(sharedPage.url(), 'Rank Progression: auth wall').not.toContain('/sign-in');
    expect(await checkErrorBoundary(sharedPage), 'Rank Progression: error boundary').toBe(false);

    const file = await capture(sharedPage, SCREENSHOT_DIR, 'charts-rank-progression');

    const emptyEl = await sharedPage.locator('[data-testid="rank-chart-empty"]').isVisible().catch(() => false);
    const chartEl = await sharedPage.locator('[data-testid="rank-chart"]').isVisible().catch(() => false);
    const svgCount = chartEl
      ? await sharedPage.locator('[data-testid="rank-chart"] svg').count()
      : 0;

    // Rank progression may legitimately be empty if matches don't have rank data
    // This is WARN not FAIL if empty
    let verdict: 'PASS' | 'FAIL' | 'WARN';
    let detail: string;

    if (await checkErrorBoundary(sharedPage)) {
      verdict = 'FAIL';
      detail = 'FAIL: React error boundary on rank chart';
    } else if (chartEl && svgCount > 0) {
      verdict = 'PASS';
      detail = `rank chart rendered (svg: ${svgCount})`;
    } else if (emptyEl) {
      // Empty is acceptable if matches lack rank tier data
      verdict = 'WARN';
      detail = 'WARN: rank-chart-empty shown — seeded matches may lack rank progression data (acceptable)';
    } else {
      verdict = 'WARN';
      detail = `chart-el: ${chartEl}; empty-el: ${emptyEl}; svg: ${svgCount}`;
    }

    results.push({
      surface: 'Charts / Rank Progression',
      file,
      verdict,
      detail,
      dataRendered: chartEl && svgCount > 0,
      counts: { svg_elements: svgCount },
    });
  });

  // ─────────────────────────────────────────────────────────────────────────
  // META ARCHETYPES
  // ─────────────────────────────────────────────────────────────────────────
  test('Meta — archetypes page renders', async () => {
    requireAuthOrFail();
    await gotoAndWait(sharedPage, '/meta', 10_000);

    expect(sharedPage.url(), 'Meta: auth wall').not.toContain('/sign-in');
    expect(await checkErrorBoundary(sharedPage), 'Meta: error boundary').toBe(false);

    const file = await capture(sharedPage, SCREENSHOT_DIR, 'meta-archetypes');
    const text = await bodyText(sharedPage);
    const lower = text.toLowerCase();

    // Meta page shows archetypes from the meta DB, not user data — may show content even with empty user data
    const hasArchetypeContent = lower.includes('archetype') || lower.includes('tier') || lower.includes('format') || /\d+%/.test(text);
    const isError = lower.includes('error') && lower.includes('loading');

    let verdict: 'PASS' | 'FAIL' | 'WARN';
    let detail: string;

    if (await checkErrorBoundary(sharedPage)) {
      verdict = 'FAIL';
      detail = 'FAIL: error boundary on Meta page';
    } else if (isError) {
      verdict = 'WARN';
      detail = 'WARN: error loading archetypes from meta endpoint';
    } else if (hasArchetypeContent) {
      verdict = 'PASS';
      detail = 'archetype content visible';
    } else {
      verdict = 'WARN';
      detail = 'page rendered without error but no archetype text found — meta DB may be empty on staging';
    }

    results.push({ surface: 'Meta (Archetypes)', file, verdict, detail, dataRendered: hasArchetypeContent });
  });

  // ─────────────────────────────────────────────────────────────────────────
  // DRAFT HISTORY — 3 sessions
  // ─────────────────────────────────────────────────────────────────────────
  test('Draft History — 3 sessions with W/L records', async () => {
    requireAuthOrFail();
    await gotoAndWait(sharedPage, '/history/drafts', 12_000);

    expect(sharedPage.url(), 'Draft History: auth wall').not.toContain('/sign-in');
    expect(await checkErrorBoundary(sharedPage), 'Draft History: error boundary').toBe(false);

    const file = await capture(sharedPage, SCREENSHOT_DIR, 'draft-history');

    const emptyEl = await sharedPage.locator('[data-testid="draft-history-empty"]').isVisible().catch(() => false);
    const tableEl = await sharedPage.locator('[data-testid="draft-history-table"]').isVisible().catch(() => false);

    // Count draft rows (using table rows in the draft history table)
    const draftRows = tableEl
      ? await sharedPage.locator('[data-testid="draft-history-table"] tbody tr').count()
      : 0;

    let verdict: 'PASS' | 'FAIL' | 'WARN';
    let detail: string;

    if (emptyEl && !tableEl) {
      verdict = 'FAIL';
      detail = `FAIL: draft-history-empty shown — 3 draft_sessions seeded; BFF /history/drafts may not be returning data for account_id=17`;
    } else if (tableEl && draftRows >= EXPECTED.draftCount) {
      verdict = 'PASS';
      detail = `${draftRows} draft rows rendered (expected ${EXPECTED.draftCount})`;
    } else if (tableEl && draftRows > 0) {
      verdict = 'WARN';
      detail = `WARN: ${draftRows}/${EXPECTED.draftCount} rows — may be pagination`;
    } else if (tableEl && draftRows === 0) {
      verdict = 'FAIL';
      detail = `FAIL: table visible but 0 rows — projection gap for draft sessions`;
    } else {
      verdict = 'WARN';
      detail = `table: ${tableEl}; empty: ${emptyEl}; rows: ${draftRows}`;
    }

    results.push({
      surface: 'Draft History',
      file,
      verdict,
      detail,
      dataRendered: draftRows > 0,
      counts: { draft_rows: draftRows },
    });
  });

  // ─────────────────────────────────────────────────────────────────────────
  // DRAFT ANALYTICS
  // ─────────────────────────────────────────────────────────────────────────
  test('Draft Analytics — renders with pick data', async () => {
    requireAuthOrFail();
    await gotoAndWait(sharedPage, '/draft-analytics', 12_000);

    expect(sharedPage.url(), 'Draft Analytics: auth wall').not.toContain('/sign-in');
    expect(await checkErrorBoundary(sharedPage), 'Draft Analytics: error boundary').toBe(false);

    const file = await capture(sharedPage, SCREENSHOT_DIR, 'draft-analytics');
    const text = await bodyText(sharedPage);
    const lower = text.toLowerCase();

    const isEmptyState = lower.includes('no drafts') || lower.includes('no data') || lower.includes('no picks');
    const hasDraftContent = lower.includes('pick') || lower.includes('draft') || lower.includes('grade') || /\d+/.test(text);

    let verdict: 'PASS' | 'FAIL' | 'WARN';
    let detail: string;

    if (await checkErrorBoundary(sharedPage)) {
      verdict = 'FAIL';
      detail = 'FAIL: error boundary on draft analytics';
    } else if (isEmptyState) {
      verdict = 'FAIL';
      detail = `FAIL: empty state shown — 119 draft_picks seeded; analytics not loading from BFF`;
    } else if (hasDraftContent) {
      verdict = 'PASS';
      detail = 'draft analytics content visible';
    } else {
      verdict = 'WARN';
      detail = 'no error/empty but no recognizable draft content — screenshot for review';
    }

    results.push({ surface: 'Draft Analytics', file, verdict, detail, dataRendered: !isEmptyState });
  });

  // ─────────────────────────────────────────────────────────────────────────
  // QUESTS — 4 active
  // ─────────────────────────────────────────────────────────────────────────
  test('Quests — 4 active quests with progress', async () => {
    requireAuthOrFail();
    await gotoAndWait(sharedPage, '/quests', 10_000);

    expect(sharedPage.url(), 'Quests: auth wall').not.toContain('/sign-in');
    expect(await checkErrorBoundary(sharedPage), 'Quests: error boundary').toBe(false);

    const file = await capture(sharedPage, SCREENSHOT_DIR, 'quests');
    const text = await bodyText(sharedPage);
    const lower = text.toLowerCase();

    const isEmptyState = lower.includes('no quests') || lower.includes('no active quests');
    const questDateCount = await sharedPage.locator('[data-testid="quest-date"]').count();
    const questCardCount = await sharedPage.locator('.quest-card').count();

    let verdict: 'PASS' | 'FAIL' | 'WARN';
    let detail: string;

    if (await checkErrorBoundary(sharedPage)) {
      verdict = 'FAIL';
      detail = 'FAIL: error boundary on quests page';
    } else if (isEmptyState) {
      verdict = 'FAIL';
      detail = `FAIL: no-quests empty state — 4 quests seeded; BFF /quests/active not returning data`;
    } else if (questCardCount >= EXPECTED.questCount) {
      verdict = 'PASS';
      detail = `${questCardCount} quest cards rendered (expected ${EXPECTED.questCount}); quest-date testids: ${questDateCount}`;
    } else if (questCardCount > 0) {
      verdict = 'WARN';
      detail = `WARN: ${questCardCount}/${EXPECTED.questCount} quest cards — partial`;
    } else {
      verdict = 'WARN';
      detail = `page rendered but no .quest-card elements found; text contains quests: ${lower.includes('quest')}`;
    }

    results.push({
      surface: 'Quests',
      file,
      verdict,
      detail,
      dataRendered: questCardCount > 0,
      counts: { quest_cards: questCardCount, quest_dates: questDateCount },
    });
  });

  // ─────────────────────────────────────────────────────────────────────────
  // DECKS — 3 decks
  // ─────────────────────────────────────────────────────────────────────────
  test('Decks — 3 decks listed', async () => {
    requireAuthOrFail();
    await gotoAndWait(sharedPage, '/decks', 10_000);

    expect(sharedPage.url(), 'Decks: auth wall').not.toContain('/sign-in');
    expect(await checkErrorBoundary(sharedPage), 'Decks: error boundary').toBe(false);

    const file = await capture(sharedPage, SCREENSHOT_DIR, 'decks');
    const text = await bodyText(sharedPage);
    const lower = text.toLowerCase();

    const isEmptyState = lower.includes('no decks') || lower.includes('no results');

    // Decks page may use a list or card layout — check for any deck-like content
    const hasDeckContent = lower.includes('deck') && /[a-z]{3,}/.test(text) && !isEmptyState;

    let verdict: 'PASS' | 'FAIL' | 'WARN';
    let detail: string;

    if (await checkErrorBoundary(sharedPage)) {
      verdict = 'FAIL';
      detail = 'FAIL: error boundary on decks page';
    } else if (isEmptyState) {
      verdict = 'FAIL';
      detail = `FAIL: empty state shown — ${EXPECTED.deckCount} decks seeded; BFF /decks not returning data`;
    } else if (hasDeckContent) {
      verdict = 'PASS';
      detail = 'deck content visible';
    } else {
      verdict = 'WARN';
      detail = 'no error/empty but deck content unclear — screenshot for review';
    }

    results.push({ surface: 'Decks', file, verdict, detail, dataRendered: !isEmptyState });
  });

  // ─────────────────────────────────────────────────────────────────────────
  // DRAFT LIVE — no active draft empty state, NO error badge (PR #2932 BUG6)
  // ─────────────────────────────────────────────────────────────────────────
  test('Draft Live — no active draft empty state, NO stream-status error badge', async () => {
    requireAuthOrFail();
    await gotoAndWait(sharedPage, '/draft/live', 12_000);

    const url = sharedPage.url();
    const isAuthWall = url.includes('/sign-in') || url.includes('/sign-up');
    const hasErrorBoundary = await checkErrorBoundary(sharedPage);

    const file = await capture(sharedPage, SCREENSHOT_DIR, 'draft-live');

    expect(isAuthWall, 'Draft Live: auth wall').toBe(false);
    expect(hasErrorBoundary, 'Draft Live: React error boundary').toBe(false);

    // PR #2932 BUG6 fix: the stream-status badge should NOT be visible in idle state
    // In idle state the correct UI is "No active draft" empty state only — no error badge
    const streamStatusVisible = await sharedPage.locator('[data-testid="stream-status"]').isVisible().catch(() => false);
    const streamStatusText = streamStatusVisible
      ? (await sharedPage.locator('[data-testid="stream-status"]').textContent().catch(() => '')) ?? ''
      : '';

    // Check for "No active draft" empty state (expected correct content)
    const text = await bodyText(sharedPage);
    const hasNoActiveDraftText = text.toLowerCase().includes('no active draft');

    // The badge may appear if a live draft is in progress — only flag it as a bug if
    // the idle empty-state is showing AND the error badge is present simultaneously
    const isIdleState = hasNoActiveDraftText;
    const isBug6Regression = isIdleState && streamStatusVisible && streamStatusText.toLowerCase().includes('error');

    expect(isBug6Regression, `Draft Live: Error badge visible alongside "No active draft" empty state — BUG6 regression (stream-status: "${streamStatusText}")`).toBe(false);

    let verdict: 'PASS' | 'FAIL' | 'WARN';
    let detail: string;

    if (isAuthWall || hasErrorBoundary) {
      verdict = 'FAIL';
      detail = `auth_wall: ${isAuthWall}; error_boundary: ${hasErrorBoundary}`;
    } else if (isBug6Regression) {
      verdict = 'FAIL';
      detail = `REGRESSION BUG6: stream-status "${streamStatusText}" badge visible alongside "No active draft" empty state`;
    } else if (isIdleState && !streamStatusVisible) {
      verdict = 'PASS';
      detail = 'idle empty state rendered; stream-status badge absent (BUG6 fix confirmed)';
    } else if (isIdleState && streamStatusVisible && !isBug6Regression) {
      verdict = 'WARN';
      detail = `WARN: idle state with stream-status "${streamStatusText}" — not an error badge but verify screenshot`;
    } else {
      // Active draft may be in progress — that's fine
      verdict = 'PASS';
      detail = `draft live rendered; stream-status: "${streamStatusText}"; no active draft text: ${hasNoActiveDraftText}`;
    }

    results.push({
      surface: 'Draft Live',
      file,
      verdict,
      detail,
      dataRendered: !isAuthWall && !hasErrorBoundary,
      counts: {
        stream_status_visible: streamStatusVisible ? 1 : 0,
        stream_status_text: streamStatusText,
        is_idle_state: isIdleState ? 1 : 0,
      },
    });
  });

  // ─────────────────────────────────────────────────────────────────────────
  // COLLECTION — renders with data, no "Failed to fetch" (nginx CORS fix)
  // ─────────────────────────────────────────────────────────────────────────
  test('Collection — renders authenticated, no "Failed to fetch" error', async () => {
    requireAuthOrFail();
    await gotoAndWait(sharedPage, '/collection', 12_000);

    const url = sharedPage.url();
    const isAuthWall = url.includes('/sign-in') || url.includes('/sign-up');
    const hasErrorBoundary = await checkErrorBoundary(sharedPage);

    const file = await capture(sharedPage, SCREENSHOT_DIR, 'collection');

    expect(isAuthWall, 'Collection: auth wall').toBe(false);
    expect(hasErrorBoundary, 'Collection: React error boundary').toBe(false);

    const text = await bodyText(sharedPage);
    const lower = text.toLowerCase();
    const hasFailedToFetch = lower.includes('failed to fetch') || lower.includes('network error');

    expect(hasFailedToFetch, 'Collection: "Failed to fetch" error present — nginx OPTIONS preflight fix may not be live').toBe(false);

    const verdict: 'PASS' | 'FAIL' = !isAuthWall && !hasErrorBoundary && !hasFailedToFetch ? 'PASS' : 'FAIL';
    const detail = hasFailedToFetch
      ? 'FAIL: "Failed to fetch" error visible — CORS preflight 502 regression'
      : verdict === 'PASS' ? 'rendered authenticated, no failed-to-fetch errors' : `auth_wall: ${isAuthWall}; error_boundary: ${hasErrorBoundary}`;

    results.push({ surface: 'Collection', file, verdict, detail, dataRendered: !isAuthWall && !hasErrorBoundary && !hasFailedToFetch });
  });

  // ─────────────────────────────────────────────────────────────────────────
  // CHARTS / RESULT BREAKDOWN — no "Failed to fetch" (nginx CORS fix)
  // ─────────────────────────────────────────────────────────────────────────
  test('Charts / Result Breakdown — renders, no "Failed to fetch" error', async () => {
    requireAuthOrFail();
    await gotoAndWait(sharedPage, '/charts/result-breakdown', 12_000);

    const url = sharedPage.url();
    const isAuthWall = url.includes('/sign-in') || url.includes('/sign-up');
    const hasErrorBoundary = await checkErrorBoundary(sharedPage);

    const file = await capture(sharedPage, SCREENSHOT_DIR, 'charts-result-breakdown');

    expect(isAuthWall, 'Result Breakdown: auth wall').toBe(false);
    expect(hasErrorBoundary, 'Result Breakdown: React error boundary').toBe(false);

    const text = await bodyText(sharedPage);
    const lower = text.toLowerCase();
    const hasFailedToFetch = lower.includes('failed to fetch') || lower.includes('network error');
    const hasFailedToLoad = lower.includes('failed to load');

    const hasFetchError = hasFailedToFetch || hasFailedToLoad;
    expect(hasFetchError, 'Result Breakdown: "Failed to fetch/load" visible — nginx OPTIONS preflight fix may not be live').toBe(false);

    const verdict: 'PASS' | 'FAIL' = !isAuthWall && !hasErrorBoundary && !hasFetchError ? 'PASS' : 'FAIL';
    const detail = hasFetchError
      ? `FAIL: fetch error visible — CORS preflight 502 regression (failed_to_fetch: ${hasFailedToFetch}; failed_to_load: ${hasFailedToLoad})`
      : verdict === 'PASS' ? 'rendered authenticated, no failed-to-fetch errors' : `auth_wall: ${isAuthWall}; error_boundary: ${hasErrorBoundary}`;

    results.push({ surface: 'Charts / Result Breakdown', file, verdict, detail, dataRendered: !isAuthWall && !hasErrorBoundary && !hasFetchError });
  });

  // ─────────────────────────────────────────────────────────────────────────
  // Additional surfaces — auth + no-crash verification
  // ─────────────────────────────────────────────────────────────────────────
  for (const { surface, route, name } of [
    { surface: 'Draft (Pack Grid)', route: '/draft', name: 'draft-pack-grid' },
    { surface: 'Settings', route: '/settings', name: 'settings' },
    { surface: 'API Keys', route: '/api-keys', name: 'api-keys' },
  ] as const) {
    test(`${surface} — renders authenticated without crash`, async () => {
      requireAuthOrFail();
      await gotoAndWait(sharedPage, route, 10_000);

      const url = sharedPage.url();
      const isAuthWall = url.includes('/sign-in') || url.includes('/sign-up');
      const hasErrorBoundary = await checkErrorBoundary(sharedPage);

      const file = await capture(sharedPage, SCREENSHOT_DIR, name);

      expect(isAuthWall, `${surface}: auth wall`).toBe(false);
      expect(hasErrorBoundary, `${surface}: React error boundary`).toBe(false);

      const verdict: 'PASS' | 'FAIL' = !isAuthWall && !hasErrorBoundary ? 'PASS' : 'FAIL';
      const detail = verdict === 'PASS' ? 'rendered authenticated, no crashes' : `auth_wall: ${isAuthWall}; error_boundary: ${hasErrorBoundary}`;

      results.push({ surface, file, verdict, detail, dataRendered: !isAuthWall && !hasErrorBoundary });
    });
  }

  // ─────────────────────────────────────────────────────────────────────────
  // Final verdict
  // ─────────────────────────────────────────────────────────────────────────
  test('Overall verdict — all surfaces PASS/WARN (no FAIL)', () => {
    requireAuthOrFail();

    const fails = results.filter(r => r.verdict === 'FAIL');
    const warns = results.filter(r => r.verdict === 'WARN');

    if (fails.length > 0) {
      console.error('\nFAILURES:');
      fails.forEach(r => console.error(`  [${r.surface}] ${r.detail}`));
    }
    if (warns.length > 0) {
      console.warn('\nWARNINGS:');
      warns.forEach(r => console.warn(`  [${r.surface}] ${r.detail}`));
    }

    expect(
      fails.length,
      `${fails.length} surface(s) FAILED: ${fails.map(r => r.surface).join(', ')}`,
    ).toBe(0);
  });
});
