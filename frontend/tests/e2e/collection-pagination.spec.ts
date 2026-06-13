import { test, expect, type Page } from '@playwright/test';

/**
 * Collection Pagination E2E Tests (#1339)
 *
 * Proves that the offset-pagination fix shipped in PR #3240 (ticket #1325)
 * works end-to-end in the browser: a collection larger than the old LIMIT-5000
 * cap is fully reachable through the UI paging controls, header counts reflect
 * the TRUE fixture totals, and server-side sort/filter span the whole dataset.
 *
 * Fixture approach:
 *   All /api/v1/collection requests are intercepted via page.route() and
 *   answered by a stateful mock that implements the same offset-pagination
 *   contract the real BFF exposes. The mock honours the `page` + `limit` +
 *   `sort_by` + `sort_desc` fields sent in the POST body, so the tests
 *   verify the exact request parameters the SPA sends, not just the rendered
 *   output.
 *
 *   Total fixture size: FIXTURE_TOTAL = 10_140 cards — one more than double
 *   the old 5,000-card LIMIT, so any regression to that limit would be
 *   detectable by the count assertions alone.
 *
 * Auth approach:
 *   Identical to collection.spec.ts — window.__CLERK_TEST_STATE__ injection
 *   before navigation, combined with page.route() mocks so no live BFF is
 *   required. This keeps the test independent of Clerk staging credentials.
 */

// ---------------------------------------------------------------------------
// Fixture constants
// ---------------------------------------------------------------------------

/** True total for this test's simulated collection — exceeds old 5 k cap. */
const FIXTURE_TOTAL = 10_140;
const ITEMS_PER_PAGE = 50;
const TOTAL_PAGES = Math.ceil(FIXTURE_TOTAL / ITEMS_PER_PAGE); // 203

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/** Inject signed-in Clerk state before page load. Must be called before page.goto(). */
async function setClerkSignedIn(page: Page): Promise<void> {
  await page.addInitScript(() => {
    (window as unknown as Record<string, unknown>).__CLERK_TEST_STATE__ = { isSignedIn: true };
  });
}

/**
 * Build a page of minimal card stubs for the mock.
 * Cards are named "Card 00001" through "Card 10140" so name-sort assertions
 * are deterministic: ascending = Card 00001 first, descending = Card 10140 first.
 */
function buildPage(
  pageNumber: number,
  limit: number,
  total: number,
  sortDesc: boolean,
): {
  cards: Record<string, unknown>[];
  totalCount: number;
  filterCount: number;
  totalPages: number;
  page: number;
  unknownCardsRemaining: number;
  unknownCardsFetched: number;
} {
  const offset = (pageNumber - 1) * limit;
  const count = Math.min(limit, total - offset);
  const cards = Array.from({ length: count }, (_, i) => {
    // For name-desc: reverse global indices so the first card on page 1 is
    // Card 10140 and the last card on the last page is Card 00001 — matching
    // what a real BFF ORDER BY name DESC would return.
    const globalIndex = sortDesc
      ? FIXTURE_TOTAL - offset - i
      : offset + i + 1;
    return {
      cardId: globalIndex,
      arenaId: globalIndex,
      quantity: 1,
      name: `Card ${String(globalIndex).padStart(5, '0')}`,
      setCode: 'tst',
      setName: 'Test Set',
      rarity: 'common',
      manaCost: '{1}',
      cmc: 1,
      typeLine: 'Creature',
      colors: [],
      colorIdentity: [],
      imageUri: '',
    };
  });

  return {
    cards,
    totalCount: FIXTURE_TOTAL,
    filterCount: total,
    totalPages: Math.ceil(total / limit),
    page: pageNumber,
    unknownCardsRemaining: 0,
    unknownCardsFetched: 0,
  };
}

/**
 * Mount the collection mock. Intercepts POST /api/v1/collection and responds
 * with pages from the 10,140-card fixture, honouring `page`, `limit`, and
 * `sort_desc` from the request body.
 *
 * Also mounts the necessary auxiliary endpoints so the page renders correctly,
 * including a /config.json stub that satisfies the ADR-077 boot sequence so
 * the React tree mounts even when VITE_CLERK_PUBLISHABLE_KEY is not set in the
 * local environment.
 */
async function mountCollectionMock(page: Page): Promise<void> {
  // ADR-077: provide a valid runtime config so the app boots without a real
  // VITE_CLERK_PUBLISHABLE_KEY in the local environment. VITE_CLERK_TEST_MODE=true
  // (set by the Playwright webServer command) aliases @clerk/react to clerkMock.tsx,
  // so only a format-valid key is needed — it is never sent to Clerk's servers.
  await page.route('**/config.json', (route) => {
    void route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        clerkPublishableKey: 'pk_test_placeholder00000000000000000000000000000',
        bffUrl: 'http://localhost:8080/api/v1',
        sentryDsn: '',
        sentryEnv: 'test',
        posthogKey: '',
        posthogHost: 'https://app.posthog.com',
        envLabel: 'test',
        daemonUrl: 'http://localhost:9001/api/v1',
      }),
    });
  });

  await page.route('**/api/v1/collection', async (route, request) => {
    let body: Record<string, unknown> = {};
    try {
      body = (await request.postDataJSON()) as Record<string, unknown>;
    } catch {
      // Empty body — use defaults
    }

    const pageNum = typeof body.page === 'number' ? body.page : 1;
    const limit = typeof body.limit === 'number' ? body.limit : ITEMS_PER_PAGE;
    const sortDesc = body.sort_desc === true;

    const payload = buildPage(pageNum, limit, FIXTURE_TOTAL, sortDesc);

    void route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ data: payload }),
    });
  });

  // Auxiliary endpoints required for page render
  await page.route('**/api/v1/collection/value', (route) => {
    void route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        data: {
          totalValueUsd: 0,
          totalValueEur: 0,
          uniqueCardsWithPrice: 0,
          cardCount: 0,
          valueByRarity: {},
          topCards: [],
        },
      }),
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
 * Navigate to the collection page and wait for it to settle.
 * Assumes setClerkSignedIn + mountCollectionMock were called beforehand.
 */
async function gotoCollectionPage(page: Page): Promise<void> {
  await page.goto('/');
  await expect(page.locator('[data-testid="app-container"]')).toBeVisible();
  await page.click('a[href="/collection"]');
  await page.waitForURL('**/collection');
  // Wait for the card grid to appear (initial load complete)
  await expect(page.locator('[data-testid="collection-card-grid"]')).toBeVisible({ timeout: 15_000 });
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

test.describe('Collection pagination >5k', () => {
  test.beforeEach(async ({ page }) => {
    await setClerkSignedIn(page);
    await mountCollectionMock(page);
  });

  /**
   * AC1 + AC2 — Total card count is not capped at 5,000.
   * The "Total Cards:" header stat must display the true fixture total (10,140),
   * not the old 5,000-row limit. The "Showing X of Y" banner is also sourced
   * from the server-returned filterCount/totalCount, not array.length.
   */
  test('@smoke header shows TRUE total count — not capped at 5,000 (AC1 + AC2)', async ({ page }) => {
    await gotoCollectionPage(page);

    const stats = page.locator('[data-testid="collection-stats"]');
    await expect(stats).toBeVisible();

    // Total Cards: must be 10,140 (fixture total)
    await expect(stats).toContainText('10,140');

    // filterCount also = FIXTURE_TOTAL (no filter applied) — "Showing X of Y"
    const resultsBanner = page.locator('.filter-results');
    await expect(resultsBanner).toContainText('Showing 10,140 of 10,140 cards');
  });

  /**
   * AC1 — Pagination controls render and First/Last navigate to the correct
   * page boundaries for a 10,140-card collection.
   */
  test('@smoke paging controls reach page 1 and last page (AC1)', async ({ page }) => {
    await gotoCollectionPage(page);

    const firstBtn = page.locator('[data-testid="collection-pagination-first"]');
    const lastBtn = page.locator('[data-testid="collection-pagination-last"]');
    const nextBtn = page.locator('[data-testid="collection-pagination-next"]');
    await expect(firstBtn).toBeVisible();
    await expect(lastBtn).toBeVisible();
    await expect(nextBtn).toBeVisible();

    // First button disabled on page 1
    await expect(firstBtn).toBeDisabled();

    // Navigate to the last page
    await lastBtn.click();

    // Page-jump input should reflect the last page number (203)
    const jumpInput = page.locator('[data-testid="collection-page-jump"]');
    await expect(jumpInput).toHaveValue(String(TOTAL_PAGES), { timeout: 10_000 });

    // Last button now disabled, First enabled
    await expect(lastBtn).toBeDisabled();
    await expect(firstBtn).toBeEnabled();
  });

  /**
   * AC1 — Page-jump past the old 5,000-card boundary (page 101) loads cards
   * with indices beyond 5,000, proving the SPA sends page=101 rather than
   * stopping at the old cap.
   */
  test('page-jump past old 5k boundary — page 101 renders cards beyond index 5000 (AC1)', async ({ page }) => {
    await gotoCollectionPage(page);

    const jumpInput = page.locator('[data-testid="collection-page-jump"]');
    await expect(jumpInput).toBeVisible();

    // Jump to page 101 — first card on that page is Card 05001 (offset 5000)
    await jumpInput.fill('101');
    await jumpInput.press('Enter');

    await expect(jumpInput).toHaveValue('101', { timeout: 10_000 });

    // The card grid must contain cards beyond index 5000
    const grid = page.locator('[data-testid="collection-card-grid"]');
    await expect(grid).toBeVisible();
    await expect(grid).toContainText('Card 05001');
  });

  /**
   * AC2 — Header counters sourced from count endpoints, not page-length inference.
   * The last page of this fixture has 40 cards (10,140 mod 50 = 40). If the
   * SPA were computing totals from array.length it would show 40, not 10,140.
   */
  test('header counts unchanged on last (partial) page — sourced from count endpoints (AC2)', async ({ page }) => {
    await gotoCollectionPage(page);

    const lastBtn = page.locator('[data-testid="collection-pagination-last"]');
    await lastBtn.click();

    // Even on the partial last page the header still shows 10,140
    const stats = page.locator('[data-testid="collection-stats"]');
    await expect(stats).toContainText('10,140', { timeout: 10_000 });

    const resultsBanner = page.locator('.filter-results');
    await expect(resultsBanner).toContainText('Showing 10,140 of 10,140 cards');
  });

  /**
   * AC3 — Sort spans the full dataset.
   * Switching to name-desc and navigating to the last page must show the
   * globally-last card in descending order ("Card 00001"), not the last
   * card in the first page's local slice.
   */
  test('sort desc spans full dataset — last page shows globally-last card (AC3)', async ({ page }) => {
    await gotoCollectionPage(page);

    // Switch sort to Name (Z-A) — sort_desc: true
    const sortSelect = page.locator('[data-testid="collection-sort-select"]');
    await expect(sortSelect).toBeVisible();
    await sortSelect.selectOption('name-desc');

    const grid = page.locator('[data-testid="collection-card-grid"]');
    await expect(grid).toBeVisible();

    // With sort_desc, page 1 should start with the highest card name: Card 10140
    await expect(grid).toContainText('Card 10140', { timeout: 10_000 });

    // Navigate to last page — last card in desc order should be Card 00001
    const lastBtn = page.locator('[data-testid="collection-pagination-last"]');
    await lastBtn.click();
    await expect(grid).toContainText('Card 00001', { timeout: 10_000 });
  });

  /**
   * AC1 — Next navigation moves one page at a time, and the page-jump input
   * stays in sync with the current page number.
   */
  test('Next button advances page-by-page and jump input stays in sync (AC1)', async ({ page }) => {
    await gotoCollectionPage(page);

    const nextBtn = page.locator('[data-testid="collection-pagination-next"]');
    const jumpInput = page.locator('[data-testid="collection-page-jump"]');

    await expect(jumpInput).toHaveValue('1');

    await nextBtn.click();
    await expect(jumpInput).toHaveValue('2', { timeout: 10_000 });

    await nextBtn.click();
    await expect(jumpInput).toHaveValue('3', { timeout: 10_000 });
  });

  /**
   * AC1 — totalPages reflects the full 10,140-card dataset.
   * Must show 203 total pages (ceil(10140/50)), not the 100 pages that the
   * old LIMIT-5000 would have implied (ceil(5000/50) = 100).
   */
  test('totalPages reflects full dataset — 203 pages, not capped at 100 (AC1)', async ({ page }) => {
    await gotoCollectionPage(page);

    const lastBtn = page.locator('[data-testid="collection-pagination-last"]');
    await lastBtn.click();

    const jumpInput = page.locator('[data-testid="collection-page-jump"]');
    await expect(jumpInput).toHaveValue(String(TOTAL_PAGES), { timeout: 10_000 });

    // Sanity: TOTAL_PAGES must exceed the old 100-page ceiling
    expect(TOTAL_PAGES).toBeGreaterThan(100);
  });
});
