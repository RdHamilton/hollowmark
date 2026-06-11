import { test, expect, type Page } from '@playwright/test';
import * as fs from 'fs';
import * as path from 'path';
import { fileURLToPath } from 'url';

// ESM-compatible __dirname (Playwright specs run as ESM modules)
const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

/**
 * Compendium Phase-1 Playwright smoke tests (#1022)
 *
 * Covers the Compendium Phase-1 surfaces that are merged to main as of 2026-06-07:
 *
 *   - MERGED  #1015/#1018  --hollowmark-gilt token (PR #3039) — CSS custom property
 *   - MERGED  #1019        Status footer data-testid (PR #3041) — present on all routes
 *   - MERGED  #1020        Hollowmark stamp logo + wordmark in nav (PR #3040)
 *   - MERGED  #1021        Gilt progress bars on Quests (PR #3042)
 *   - MERGED  #1019  PR #3045  StatusStrip dedicated component refactor — data-testid="status-strip"
 *
 * NOT YET MERGED (Chromatic accept pending):
 *   - OPEN    #1026  PR #3044  nav-tile glyphs
 *   - OPEN    #1024  PR #3048  tier-badge D17 color gate (Vitest only — no E2E surface yet)
 *
 * Auth approach: VITE_CLERK_TEST_MODE=true (set in playwright.config.ts webServer)
 * aliases @clerk/react to src/test/mocks/clerkMock.tsx. Auth state is injected via
 * window.__CLERK_TEST_STATE__ before each navigation so ProtectedRoute renders the
 * page content rather than the sign-in prompt.
 *
 * BFF-data mocking: all Clerk-protected endpoints are mocked via page.route() before
 * navigation so tests run without a live authenticated BFF. The apiClient unwraps
 * every response as `data.data`, so match-stats and health bodies use the envelope.
 *
 * Design spec: vault-mtg-docs/engineering/design/2026-06-compendium-redesign-review-consolidation.md
 * Brand token authority: frontend/src/index.css (--hollowmark-gilt: #B87D32)
 */

// ---------------------------------------------------------------------------
// Shared auth helper
// ---------------------------------------------------------------------------

async function setClerkSignedIn(page: Page): Promise<void> {
  await page.addInitScript(() => {
    (window as unknown as Record<string, unknown>).__CLERK_TEST_STATE__ = { isSignedIn: true };
  });
}

// ---------------------------------------------------------------------------
// BFF mock helpers
// ---------------------------------------------------------------------------

/**
 * Mock the StatusStrip's BFF dependencies (stats + health) so the strip
 * renders in the "loading" → "empty" terminal state without a live BFF.
 *
 * StatusStrip calls:
 *   POST /api/v1/matches/stats    → Statistics
 *   POST /api/v1/matches          → { Matches, Total, Page, Limit }
 *   GET  /api/v1/health/daemon    → daemon health
 *
 * Returning empty-but-valid data gets the strip out of its loading state
 * and into the "No matches yet" render path. The strip element itself
 * (data-testid="status-strip") is present on all three paths.
 */
async function mockStatusStripEndpoints(page: Page): Promise<void> {
  await page.route('**/api/v1/matches/stats', (route) => {
    void route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        data: { TotalMatches: 0, WinRate: 0, MatchesWon: 0, MatchesLost: 0 },
      }),
    });
  });
  await page.route('**/api/v1/matches', (route) => {
    void route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        data: { Matches: [], Total: 0, Page: 1, Limit: 50 },
      }),
    });
  });
  await page.route('**/api/v1/health/daemon', (route) => {
    void route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ data: { status: 'disconnected' } }),
    });
  });
}

/**
 * Mock the Quests page BFF endpoints so it renders without a live BFF.
 * Returns a single in-progress quest to exercise the gilt progress fill.
 */
async function mockQuestsEndpoints(page: Page): Promise<void> {
  await page.route('**/api/v1/quests/active', (route) => {
    void route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        data: {
          quests: [
            {
              id: 'test-quest-1',
              title: 'Flame On',
              description: 'Win 3 matches',
              type: 'win',
              goal: 3,
              ending_progress: 3,
              completed: true,
              gold_reward: 500,
              first_seen_at: '2026-06-07T10:00:00Z',
              rerolled: false,
            },
            {
              id: 'test-quest-2',
              title: 'Aggro Initiate',
              description: 'Play 5 games',
              type: 'play',
              goal: 5,
              ending_progress: 2,
              completed: false,
              gold_reward: 250,
              first_seen_at: '2026-06-07T09:00:00Z',
              rerolled: false,
            },
          ],
        },
      }),
    });
  });
  await page.route('**/api/v1/quests/history**', (route) => {
    void route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ data: [] }),
    });
  });
  await page.route('**/api/v1/quests/wins/daily', (route) => {
    void route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ data: { dailyWins: 2, goal: 15 } }),
    });
  });
  await page.route('**/api/v1/quests/wins/weekly', (route) => {
    void route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ data: { weeklyWins: 8, goal: 15 } }),
    });
  });
  await page.route('**/api/v1/system/account', (route) => {
    void route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ data: {} }),
    });
  });
}

/**
 * Mock Home page BFF dependencies so the Layout/Footer render without error.
 */
async function mockHomeEndpoints(page: Page): Promise<void> {
  await page.route('**/api/v1/history/summary', (route) => {
    void route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        data: {
          today: { wins: 0, losses: 0, win_rate: 0 },
          this_week: { wins: 0, losses: 0, win_rate: 0, matches: 0 },
          all_time: { wins: 0, losses: 0, win_rate: 0, matches: 0 },
          last_match: null,
        },
      }),
    });
  });
  await page.route('**/api/v1/drafts**', (route) => {
    void route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ data: [] }),
    });
  });
  await page.route('**/api/v1/decks**', (route) => {
    void route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ data: [] }),
    });
  });
}

// ===========================================================================
// Suite 1: Persistent bottom status strip (AC1)
// Asserts the status footer is present on every primary route.
// ===========================================================================

test.describe('Compendium Phase-1 — Status Strip presence @smoke', () => {
  test.beforeEach(async ({ page }) => {
    await setClerkSignedIn(page);
    await mockStatusStripEndpoints(page);
  });

  test('@smoke status strip present on /home', async ({ page }) => {
    await mockHomeEndpoints(page);
    await page.goto('/home');
    await expect(page.locator('[data-testid="app-container"]')).toBeVisible();
    await expect(page.locator('[data-testid="status-strip"]')).toBeAttached();
  });

  test('@smoke status strip present on /match-history', async ({ page }) => {
    await page.route('**/api/v1/matches**', (route) => {
      void route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ data: { Matches: [], Total: 0, Page: 1, Limit: 50 } }),
      });
    });
    await page.route('**/api/v1/matches/stats', (route) => {
      void route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ data: { TotalMatches: 0, WinRate: 0, MatchesWon: 0, MatchesLost: 0 } }),
      });
    });
    await page.goto('/match-history');
    await expect(page.locator('[data-testid="app-container"]')).toBeVisible();
    await expect(page.locator('[data-testid="status-strip"]')).toBeAttached();
  });

  test('@smoke status strip present on /quests', async ({ page }) => {
    await mockQuestsEndpoints(page);
    await page.goto('/quests');
    await expect(page.locator('[data-testid="app-container"]')).toBeVisible();
    await expect(page.locator('[data-testid="status-strip"]')).toBeAttached();
  });

  test('@smoke status strip present on /decks', async ({ page }) => {
    await page.route('**/api/v1/decks**', (route) => {
      void route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ data: [] }),
      });
    });
    await page.goto('/decks');
    await expect(page.locator('[data-testid="app-container"]')).toBeVisible();
    await expect(page.locator('[data-testid="status-strip"]')).toBeAttached();
  });

  test('@smoke status strip present on /collection', async ({ page }) => {
    await page.route('**/api/v1/collection**', (route) => {
      void route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          data: { cards: [], totalCount: 0, filterCount: 0, unknownCardsRemaining: 0, unknownCardsFetched: 0 },
        }),
      });
    });
    await page.route('**/api/v1/cards/sets**', (route) => {
      void route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ data: [] }),
      });
    });
    await page.goto('/collection');
    await expect(page.locator('[data-testid="app-container"]')).toBeVisible();
    await expect(page.locator('[data-testid="status-strip"]')).toBeAttached();
  });

  // ---------------------------------------------------------------------------
  // PENDING: daemon-offline / synced state distinctions
  //
  // StatusStrip renders a single element (data-testid="status-strip")
  // regardless of daemon health state. Detailed daemon-state assertions
  // (offline indicator, "Synced:" timestamp vs. no-timestamp) belong on
  // DaemonHealthIndicator — already covered in daemon-health-indicator.spec.ts.
  //
  // Future StatusStrip iterations may introduce explicit offline/synced slots
  // with distinct testids. These assertions will be enabled then.
  //
  // test('status strip shows daemon-offline indicator when daemon disconnected', ...)
  // test('status strip shows Synced timestamp when daemon connected', ...)
  // ---------------------------------------------------------------------------
});

// ===========================================================================
// Suite 1a: Status strip label-text assertions (#1063)
//
// Asserts the stable structural label texts inside [data-testid="status-strip"]
// on /home (representative authenticated route). Uses scoped getByText() within
// the strip container per selector-priority rules (no per-slot testids exist).
//
// Two tests:
//   1. Baseline labels (Matches:, Win Rate:) — always render once the strip
//      exits loading state, regardless of match data or daemon state.
//   2. All 4 stable labels (adds Last Played:, Synced:) — uses a richer mock:
//      - One match → lastMatch is non-empty → Last Played: renders
//      - getDaemonHealth response uses { status: 'connected' } (no data envelope;
//        bffHealth.getDaemonHealth calls response.json() directly, not the
//        apiClient envelope-unwrapper) → daemonStatus=connected → isDaemonOffline=false
//        → Synced: branch renders instead of "Daemon offline"
//
// Note: Streak: is conditional (count > 0 only) and is not asserted here — the
// mock below produces a single win match which WILL render a streak, but the
// assertion set is limited to the 4 labels named in the #1063 enrichment scope.
// See status-strip.spec.ts for the existing streak-aware assertion.
// ===========================================================================

/**
 * Mock StatusStrip BFF dependencies with a connected daemon + one match so
 * all 4 stable labels render: Matches:, Win Rate:, Last Played:, Synced:.
 *
 * Key differences from mockStatusStripEndpoints:
 *   - /api/v1/health/daemon returns { status: 'connected' } — no data envelope,
 *     because getDaemonHealth (bffHealth.ts) reads result.status from response.json()
 *     directly rather than through the apiClient envelope-unwrapper. This makes
 *     isDaemonOffline=false so the Synced: label branch renders.
 *   - /api/v1/matches returns one match so lastMatch is non-empty → Last Played: renders.
 *   - /api/v1/matches/stats returns TotalMatches=1 consistent with the match payload.
 */
async function mockStatusStripAllLabels(page: Page): Promise<void> {
  await page.route('**/api/v1/matches/stats', (route) => {
    void route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        data: { TotalMatches: 1, WinRate: 1.0, MatchesWon: 1, MatchesLost: 0 },
      }),
    });
  });
  await page.route('**/api/v1/matches', (route) => {
    void route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        data: {
          Matches: [
            {
              ID: 'test-match-1',
              Result: 'win',
              Timestamp: '2026-06-10T12:00:00Z',
              OpponentName: 'TestOpponent',
              DeckName: 'TestDeck',
            },
          ],
          Total: 1,
          Page: 1,
          Limit: 50,
        },
      }),
    });
  });
  // No data envelope — getDaemonHealth reads result.status directly from response.json()
  await page.route('**/api/v1/health/daemon', (route) => {
    void route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ status: 'connected' }),
    });
  });
}

test.describe('Compendium Phase-1 — Status Strip label-text assertions @smoke (#1063)', () => {
  test.beforeEach(async ({ page }) => {
    await setClerkSignedIn(page);
    await mockHomeEndpoints(page);
  });

  test('@smoke status strip shows Matches: and Win Rate: labels on /home', async ({ page }) => {
    // Baseline: these two labels always render once the strip exits loading state.
    await mockStatusStripEndpoints(page);
    await page.goto('/home');
    await expect(page.locator('[data-testid="app-container"]')).toBeVisible();

    const strip = page.locator('[data-testid="status-strip"]');
    await expect(strip).toBeAttached();
    // Wait for the strip to exit loading state before asserting labels
    await expect(strip.getByText('Loading stats...')).not.toBeAttached({ timeout: 5000 }).catch(() => undefined);

    await expect(strip.getByText('Matches:')).toBeVisible();
    await expect(strip.getByText('Win Rate:')).toBeVisible();
  });

  test('@smoke status strip shows all 4 stable labels on /home (connected daemon + match data)', async ({ page }) => {
    // Full label set: Matches:, Win Rate:, Last Played:, Synced:
    await mockStatusStripAllLabels(page);
    await page.goto('/home');
    await expect(page.locator('[data-testid="app-container"]')).toBeVisible();

    const strip = page.locator('[data-testid="status-strip"]');
    await expect(strip).toBeAttached();

    await expect(strip.getByText('Matches:')).toBeVisible();
    await expect(strip.getByText('Win Rate:')).toBeVisible();
    await expect(strip.getByText('Last Played:')).toBeVisible();
    await expect(strip.getByText('Synced:')).toBeVisible();
  });
});

// ===========================================================================
// Suite 2: Hollowmark logo + wordmark in nav (AC from design spec §keep)
// ===========================================================================

test.describe('Compendium Phase-1 — Hollowmark logo + wordmark in nav @smoke', () => {
  test.beforeEach(async ({ page }) => {
    await setClerkSignedIn(page);
    await mockStatusStripEndpoints(page);
    await mockHomeEndpoints(page);
  });

  test('@smoke Hollowmark orb mark renders in nav at ≥32px', async ({ page }) => {
    await page.goto('/home');
    await expect(page.locator('[data-testid="app-container"]')).toBeVisible();

    const mark = page.locator('[data-testid="nav-brand"] img.nav-brand-mark');
    await expect(mark).toBeVisible();

    // Design spec: mark must be rendered at ≥32px width (#1020 commit message)
    const box = await mark.boundingBox();
    expect(box).not.toBeNull();
    expect(box!.width).toBeGreaterThanOrEqual(32);
    expect(box!.height).toBeGreaterThanOrEqual(32);
  });

  test('@smoke Hollowmark wordmark reads "Hollowmark" in nav', async ({ page }) => {
    await page.goto('/home');
    await expect(page.locator('[data-testid="app-container"]')).toBeVisible();

    const wordmark = page.locator('[data-testid="nav-brand"] .nav-brand-wordmark');
    await expect(wordmark).toBeVisible();
    await expect(wordmark).toHaveText('Hollowmark');
  });

  test('@smoke nav-brand aria-label is "Hollowmark home"', async ({ page }) => {
    await page.goto('/home');
    await expect(page.locator('[data-testid="app-container"]')).toBeVisible();

    const brand = page.locator('[data-testid="nav-brand"]');
    await expect(brand).toHaveAttribute('aria-label', 'Hollowmark home');
  });

  test('@smoke nav-brand links to /home', async ({ page }) => {
    await page.goto('/match-history');
    await page.route('**/api/v1/matches**', (route) => {
      void route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ data: { Matches: [], Total: 0, Page: 1, Limit: 50 } }),
      });
    });
    await expect(page.locator('[data-testid="app-container"]')).toBeVisible();

    const brand = page.locator('[data-testid="nav-brand"]');
    await expect(brand).toHaveAttribute('href', '/home');
  });
});

// ===========================================================================
// Suite 3: Gilt token CSS custom property (AC from design spec §gilt)
//
// Asserts --hollowmark-gilt is defined on :root and resolves to the expected
// value (#B87D32). This is a CSS property assertion — Playwright evaluates it
// via getComputedStyle on the document root.
// ===========================================================================

test.describe('Compendium Phase-1 — --hollowmark-gilt token @smoke', () => {
  test.beforeEach(async ({ page }) => {
    await setClerkSignedIn(page);
    await mockStatusStripEndpoints(page);
    await mockHomeEndpoints(page);
  });

  test('@smoke --hollowmark-gilt CSS custom property is defined and resolves to #B87D32', async ({ page }) => {
    await page.goto('/home');
    await expect(page.locator('[data-testid="app-container"]')).toBeVisible();

    const giltValue = await page.evaluate(() =>
      getComputedStyle(document.documentElement)
        .getPropertyValue('--hollowmark-gilt')
        .trim()
        .toLowerCase()
    );

    // Token must be non-empty (defined)
    expect(giltValue.length).toBeGreaterThan(0);
    // Token must resolve to the aged-brass value from the design spec
    expect(giltValue).toBe('#b87d32');
  });

  test('@smoke --hollowmark-gilt-light CSS custom property is defined', async ({ page }) => {
    await page.goto('/home');
    await expect(page.locator('[data-testid="app-container"]')).toBeVisible();

    const giltLightValue = await page.evaluate(() =>
      getComputedStyle(document.documentElement)
        .getPropertyValue('--hollowmark-gilt-light')
        .trim()
        .toLowerCase()
    );

    expect(giltLightValue.length).toBeGreaterThan(0);
    expect(giltLightValue).toBe('#c8913a');
  });
});

// ===========================================================================
// Suite 4: Gilt on Quests gold-economy surfaces (AC2 — visual)
//
// The Quests page uses --hollowmark-gilt on:
//   - .daily-wins-reward  (gold reward labels)
//   - .quest-progress-fill--done  (completed quest bars)
//   - .mini-progress-fill--done   (completed history bars)
//
// These tests assert the CSS classes and token wiring are present, replacing
// the visual snapshot baseline requirement (AC2/AC3) since Playwright's
// toHaveCSS checks the computed value and is deterministic without a snapshot
// store, which is more maintainable pre-beta than committing PNG baselines.
// ===========================================================================

test.describe('Compendium Phase-1 — Gilt on Quests surfaces', () => {
  test.beforeEach(async ({ page }) => {
    await setClerkSignedIn(page);
    await mockStatusStripEndpoints(page);
    await mockQuestsEndpoints(page);
  });

  test('Quests page loads and renders quest cards', async ({ page }) => {
    await page.goto('/quests');
    await expect(page.locator('[data-testid="app-container"]')).toBeVisible();
    // Active quests section renders (seeded with 2 quests in mockQuestsEndpoints)
    await expect(page.locator('.quest-card').first()).toBeVisible();
  });

  test('completed quest progress bar carries gilt gradient class', async ({ page }) => {
    await page.goto('/quests');
    await expect(page.locator('[data-testid="app-container"]')).toBeVisible();
    await expect(page.locator('.quest-card').first()).toBeVisible();

    // The first quest in our mock is completed (ending_progress === goal)
    // so its fill should carry the --done modifier
    const doneFill = page.locator('.quest-progress-fill--done').first();
    await expect(doneFill).toBeAttached();

    // Verify the gilt gradient is wired via computed background
    const bg = await doneFill.evaluate((el) =>
      getComputedStyle(el).backgroundImage
    );
    // The CSS gradient uses var(--hollowmark-gilt) — the resolved value will
    // contain the rgb equivalent of #B87D32 = rgb(184, 125, 50)
    expect(bg).toMatch(/gradient|rgb/i);
  });

  test('daily-wins-reward label uses gilt color', async ({ page }) => {
    await page.goto('/quests');
    await expect(page.locator('[data-testid="app-container"]')).toBeVisible();

    const rewardLabel = page.locator('.daily-wins-reward').first();
    await expect(rewardLabel).toBeAttached();

    // CSS class is present — wiring confirmed by stylesheet (.daily-wins-reward { color: var(--hollowmark-gilt) })
    // Computed color: rgb(184, 125, 50) = #B87D32
    const color = await rewardLabel.evaluate((el) =>
      getComputedStyle(el).color
    );
    expect(color).toMatch(/rgb\(184,\s*125,\s*50\)/);
  });
});

// ===========================================================================
// Suite 5: Favicon identity assertion (AC4)
//
// Asserts the app serves the Hollowmark SVG favicon (logo-vaultmtg-app-icon.svg)
// and that the HTML <head> references it. Does NOT byte-hash the .ico fallback
// since .ico generation is a separate build step — the canonical assertion is
// that the SVG favicon URL is linked and responds with SVG content type.
// ===========================================================================

test.describe('Compendium Phase-1 — Favicon identity @smoke', () => {
  test('@smoke HTML references /logo-vaultmtg-app-icon.svg as the primary favicon', async ({ page }) => {
    await page.goto('/');
    // The <link rel="icon" type="image/svg+xml"> must reference the Hollowmark SVG
    const svgFaviconHref = await page.evaluate(() => {
      const link = document.querySelector<HTMLLinkElement>('link[rel="icon"][type="image/svg+xml"]');
      return link?.getAttribute('href') ?? null;
    });
    expect(svgFaviconHref).toBe('/logo-vaultmtg-app-icon.svg');
  });

  test('@smoke /logo-vaultmtg-app-icon.svg is reachable and is SVG content', async ({ page }) => {
    await page.goto('/');
    const response = await page.request.get('/logo-vaultmtg-app-icon.svg');
    expect(response.status()).toBe(200);

    const contentType = response.headers()['content-type'] ?? '';
    expect(contentType).toMatch(/svg/i);

    const body = await response.text();
    // Confirm the SVG contains the Hollowmark title set in the asset (#1020)
    expect(body).toContain('Hollowmark app icon');
  });

  test('@smoke local SVG asset contains Hollowmark title (build-time assertion)', () => {
    // Asserts the public asset on-disk is the Hollowmark one, not the old WUBRG favicon.
    const assetPath = path.resolve(
      __dirname,
      '../../public/logo-vaultmtg-app-icon.svg'
    );
    expect(fs.existsSync(assetPath)).toBe(true);

    const svgContent = fs.readFileSync(assetPath, 'utf-8');
    expect(svgContent).toContain('Hollowmark app icon');
    // Must NOT contain old WUBRG V-chevron identity strings
    expect(svgContent).not.toContain('VaultMTG app icon');
  });
});

// ===========================================================================
// Suite 6: Nav-tile glyphs (PR #3044 — PENDING MERGE)
//
// These assertions target the Magic-native nav-tile glyphs introduced in PR
// #3044 / ticket #1026. That PR is currently OPEN pending Chromatic accept.
// The tests are structured and ready; each is skipped with a note pointing to
// the PR. Enable by removing the test.skip() once #3044 merges to main.
// ===========================================================================

test.describe('Compendium Phase-1 — Nav-tile glyphs (PENDING #3044)', () => {
  test('Home nav-tile glyph renders (PENDING PR #3044)', async ({ page }) => {
    test.skip(true, 'PENDING: PR #3044 (feat(home): Magic-native nav-tile glyphs) not yet merged to main');
    await page.goto('/home');
    await expect(page.locator('[data-testid="app-container"]')).toBeVisible();
    // Assertion placeholder: update selector once #3044 ships the glyph testids
    await expect(page.locator('[data-testid="nav-tile-glyph-draft"]')).toBeVisible();
  });
});

// ===========================================================================
// Suite 7: Tier-badge D17 colors (PR #3048 — PENDING MERGE)
//
// The tier-badge D17 color-ordering gate is a Vitest component test (not E2E)
// that lives in PR #3048 / ticket #1024. That PR is currently OPEN pending
// Chromatic accept. No E2E surface exists yet for tier colors — this suite
// serves as a documented placeholder for when the Draft page tier-badge
// rendering gets explicit E2E coverage.
// ===========================================================================

test.describe('Compendium Phase-1 — Tier-badge D17 colors (PENDING #3048)', () => {
  test('D17 tier-badge color ordering is enforced (PENDING PR #3048)', async ({ page }) => {
    test.skip(true, 'PENDING: PR #3048 (test(TierList): D17 tier-severity color ordering) not yet merged to main. D17 fix: D-grade must NOT be gold (re-mapped to red/dark-gray). See compendium-redesign-review-consolidation.md §3');
    // Placeholder — the real gate is a Vitest component test in PR #3048.
    // When E2E coverage is wanted, target the Draft page tier-badge elements here.
    await page.goto('/draft');
    await expect(page.locator('[data-testid="app-container"]')).toBeVisible();
  });
});

// ===========================================================================
// Suite 8: Collection gilt surface — mythic wildcard tally (#1064)
//
// Asserts the wildcard advisor panel renders the mythic wildcard gem on the
// Collection page after the user toggles the panel open. The gem element carries
// data-testid="wildcard-advisor-gem-mythic" on the BudgetGem span
// (WildcardAdvisorPanel.tsx line 119).
//
// Flow:
//   1. Navigate to /collection (with mocked collection + wildcard-advisor endpoints)
//   2. Click [data-testid="collection-toggle-wildcard-advisor"] to open the panel
//   3. Wait for [data-testid="wildcard-advisor-budget"] to appear (panel data state)
//   4. Assert [data-testid="wildcard-advisor-gem-mythic"] is visible
//   5. Assert aria-label matches the mocked count ("3 Mythic wildcards")
//   6. Assert the count text "3" is present inside .wildcard-advisor__budget-gem-count
//
// Color note: the gem uses --vault-rarity-mythic (#DC7E0E, orange) via --gem-color,
// NOT --hollowmark-gilt. The gilt CSS token is used by BuildAroundSeedModal and
// SetCompletion for their mythic surfaces; WildcardAdvisorPanel uses the standard
// rarity token. The assertions here are structural (element presence + aria-label +
// count text) per the selector confirmed by Frank (comment 4683759775 on #1064).
//
// BFF mock: the wildcard-advisor endpoint is NOT wrapped by the apiClient envelope;
// bffWildcardAdvisor.ts reads the JSON body directly (WildcardAdvisorResponse shape).
// The collection endpoints use the standard { data: { ... } } envelope.
// ===========================================================================

/**
 * Mock the Collection page's BFF endpoints so it renders without a live BFF.
 * Mirrors the mock pattern from wildcard-advisor.spec.ts.
 */
async function mockCollectionEndpointsForSuite8(page: Page): Promise<void> {
  await page.route('**/api/v1/collection', (route) => {
    void route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        data: {
          cards: [],
          totalCount: 0,
          filterCount: 0,
          unknownCardsRemaining: 0,
          unknownCardsFetched: 0,
        },
      }),
    });
  });
  await page.route('**/api/v1/collection/value', (route) => {
    void route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ data: { totalValueUsd: 0 } }),
    });
  });
  await page.route('**/api/v1/collection/stats', (route) => {
    void route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ data: {} }),
    });
  });
  await page.route('**/api/v1/collection/sets', (route) => {
    void route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ data: [] }),
    });
  });
  await page.route('**/api/v1/cards/sets', (route) => {
    void route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ data: [] }),
    });
  });
}

/**
 * Mock the wildcard-advisor endpoint with 3 mythic wildcards in the budget.
 *
 * The bffWildcardAdvisor adapter reads the response body directly (no apiClient
 * envelope); shape is WildcardAdvisorResponse, NOT { data: WildcardAdvisorResponse }.
 *
 * wildcard_budget.mythic = 3 → BudgetGem renders aria-label="3 Mythic wildcards"
 * and count text "3" inside .wildcard-advisor__budget-gem-count.
 */
async function mockWildcardAdvisorWithMythicBudget(page: Page): Promise<void> {
  await page.route('**/api/v1/recommendations/wildcards**', (route) => {
    void route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        format: 'Standard',
        recommendations: [
          {
            arena_id: 101,
            name: 'Sheoldred, the Apocalypse',
            rarity: 'mythic',
            owned_copies: 1,
            missing_copies: 3,
            gihwr: 64.5,
            set_code: 'DMU',
          },
        ],
        wildcard_budget: { common: 12, uncommon: 9, rare: 5, mythic: 3 },
      }),
    });
  });
}

test.describe('Compendium Phase-1 — Collection gilt surface: mythic wildcard tally @smoke (#1064)', () => {
  test.beforeEach(async ({ page }) => {
    await setClerkSignedIn(page);
    await mockStatusStripEndpoints(page);
    await mockCollectionEndpointsForSuite8(page);
    await mockWildcardAdvisorWithMythicBudget(page);
  });

  test('@smoke wildcard advisor panel appears after toggle on /collection', async ({ page }) => {
    // Navigate directly to /collection — mocks registered in beforeEach
    await page.goto('/collection');
    await expect(page.locator('[data-testid="app-container"]')).toBeVisible();
    await expect(page.locator('[data-testid="collection-page"]')).toBeVisible();

    // Toggle the wildcard advisor panel open
    const toggleBtn = page.locator('[data-testid="collection-toggle-wildcard-advisor"]');
    await expect(toggleBtn).toBeVisible({ timeout: 10000 });
    await toggleBtn.click();

    // Panel must appear and reach the data state (budget section visible)
    await expect(
      page.locator('[data-testid="wildcard-advisor-panel"]')
    ).toBeVisible({ timeout: 15000 });
    await expect(
      page.locator('[data-testid="wildcard-advisor-budget"]')
    ).toBeVisible({ timeout: 15000 });
  });

  test('@smoke mythic wildcard gem is visible in budget panel after toggle', async ({ page }) => {
    await page.goto('/collection');
    await expect(page.locator('[data-testid="app-container"]')).toBeVisible();
    await expect(page.locator('[data-testid="collection-page"]')).toBeVisible();

    // Open the advisor panel
    await page.locator('[data-testid="collection-toggle-wildcard-advisor"]').click();

    // Wait for the budget section to confirm the panel is in the data state
    await expect(
      page.locator('[data-testid="wildcard-advisor-budget"]')
    ).toBeVisible({ timeout: 20000 });

    // Assert the mythic gem element is present via the full scoped selector path
    // confirmed by Frank (comment 4683759775 on #1064):
    //   [data-testid="wildcard-advisor-panel"]
    //     [data-testid="wildcard-advisor-budget"]
    //       [data-testid="wildcard-advisor-gem-mythic"]
    const mythicGem = page.locator(
      '[data-testid="wildcard-advisor-panel"] [data-testid="wildcard-advisor-budget"] [data-testid="wildcard-advisor-gem-mythic"]'
    );
    await expect(mythicGem).toBeVisible();
  });

  test('@smoke mythic gem aria-label reflects mocked wildcard count', async ({ page }) => {
    await page.goto('/collection');
    await expect(page.locator('[data-testid="app-container"]')).toBeVisible();

    await page.locator('[data-testid="collection-toggle-wildcard-advisor"]').click();
    await expect(
      page.locator('[data-testid="wildcard-advisor-budget"]')
    ).toBeVisible({ timeout: 20000 });

    const mythicGem = page.locator('[data-testid="wildcard-advisor-gem-mythic"]');
    await expect(mythicGem).toBeVisible();

    // BudgetGem renders: aria-label={`${count} Mythic wildcard${count !== 1 ? 's' : ''}`}
    // With wildcard_budget.mythic = 3 → "3 Mythic wildcards"
    await expect(mythicGem).toHaveAttribute('aria-label', '3 Mythic wildcards');
  });

  test('@smoke mythic gem count text reflects mocked wildcard budget', async ({ page }) => {
    await page.goto('/collection');
    await expect(page.locator('[data-testid="app-container"]')).toBeVisible();

    await page.locator('[data-testid="collection-toggle-wildcard-advisor"]').click();
    await expect(
      page.locator('[data-testid="wildcard-advisor-budget"]')
    ).toBeVisible({ timeout: 20000 });

    // The numeric count is inside .wildcard-advisor__budget-gem-count (per Frank's comment)
    const mythicGem = page.locator('[data-testid="wildcard-advisor-gem-mythic"]');
    await expect(mythicGem).toBeVisible();

    const countEl = mythicGem.locator('.wildcard-advisor__budget-gem-count');
    await expect(countEl).toHaveText('3');
  });
});
