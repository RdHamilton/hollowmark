import path from 'path';
import { fileURLToPath } from 'url';
import { test, expect, type Page } from '@playwright/test';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

/**
 * Manual-Import CI Gate (#901)
 *
 * Purpose: ensure the manual-import path cannot silently rot. Per D3 and D4,
 * manual-import is the DEFAULT collection method and must be genuinely ready
 * and testable at all times — the D4 counsel directive requires this gate to
 * be live on every CI run.
 *
 * Structure:
 *   1. @smoke tests — run today. They exercise the collection-browse surface
 *      that manual-import feeds into. A regression there means the import
 *      destination is broken even before the upload step.
 *   2. Upload happy-path tests (Suite 3) — live as of #895 / PR #3105
 *      (merged 2026-06-10). These exercise the full import flow: file select,
 *      submit, success/error state, and post-import collection browsability.
 *      All four stable data-testid selectors introduced in #895 are used.
 *
 * Auth: tests inject the Clerk test state via window.__CLERK_TEST_STATE__ so
 * ProtectedRoute passes without a live Clerk session. All BFF data endpoints
 * are mocked via page.route() so tests are independent of a live BFF.
 *
 * Golden fixture: frontend/tests/fixtures/manual-import-collection.csv
 *   — committed as AC3 of #901. Do not delete it. Update only when the MTGA
 *     export format changes.
 */

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const FIXTURE_PATH = path.resolve(
  __dirname,
  '../fixtures/manual-import-collection.csv',
);

async function setClerkSignedIn(page: Page): Promise<void> {
  await page.addInitScript(() => {
    (window as unknown as Record<string, unknown>).__CLERK_TEST_STATE__ = {
      isSignedIn: true,
    };
  });
}

/** Mock BFF collection endpoints with a minimal set of cards matching the
 *  golden fixture. Keeps tests independent of a live authenticated BFF. */
async function mockCollectionEndpointsWithData(page: Page): Promise<void> {
  // Three cards from the golden fixture, enough to verify browsability.
  const mockCards = [
    {
      cardId: 1,
      arenaId: 67518,
      quantity: 4,
      name: 'Lightning Bolt',
      setCode: 'ONS',
      setName: 'Onslaught',
      rarity: 'common',
      manaCost: '{R}',
      cmc: 1,
      typeLine: 'Instant',
      colors: ['R'],
      colorIdentity: ['R'],
      imageUri: 'https://example.com/lightning-bolt.jpg',
    },
    {
      cardId: 2,
      arenaId: 10002,
      quantity: 4,
      name: 'Counterspell',
      setCode: 'ME2',
      setName: 'Masters Edition II',
      rarity: 'common',
      manaCost: '{U}{U}',
      cmc: 2,
      typeLine: 'Instant',
      colors: ['U'],
      colorIdentity: ['U'],
      imageUri: 'https://example.com/counterspell.jpg',
    },
    {
      cardId: 3,
      arenaId: 10003,
      quantity: 2,
      name: 'Thoughtseize',
      setCode: 'THS',
      setName: 'Theros',
      rarity: 'rare',
      manaCost: '{B}',
      cmc: 1,
      typeLine: 'Sorcery',
      colors: ['B'],
      colorIdentity: ['B'],
      imageUri: 'https://example.com/thoughtseize.jpg',
    },
  ];

  await page.route('**/api/v1/collection', (route) => {
    void route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        data: {
          cards: mockCards,
          totalCount: mockCards.length,
          filterCount: mockCards.length,
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
      body: JSON.stringify({
        data: {
          totalValueUsd: 12.5,
          totalValueEur: 11.0,
          uniqueCardsWithPrice: 3,
          cardCount: 10,
          valueByRarity: { common: 2.5, rare: 10.0 },
          topCards: [],
        },
      }),
    });
  });

  await page.route('**/api/v1/collection/stats', (route) => {
    void route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ data: { totalUniqueCards: 3, totalCards: 10 } }),
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

/** Mock the collection/import endpoint for upload tests.
 *  Returns a 200 with the accepted card count. */
async function mockImportEndpoint(
  page: Page,
  opts: { status?: number; body?: object } = {},
): Promise<void> {
  const status = opts.status ?? 200;
  const body = opts.body ?? { data: { accepted: 10, rejected: 0 } };
  await page.route('**/api/v1/collection/import', (route) => {
    void route.fulfill({
      status,
      contentType: 'application/json',
      body: JSON.stringify(body),
    });
  });
}

// ---------------------------------------------------------------------------
// Suite 1 — Collection browsability (@smoke)
//
// These run today and protect the surface that the manual-import result feeds
// into.  A broken collection page means a broken import result even before
// the upload step is implemented.
// ---------------------------------------------------------------------------

test.describe('Manual-Import: collection-browse gate', () => {
  test.beforeEach(async ({ page }) => {
    await setClerkSignedIn(page);
    await mockCollectionEndpointsWithData(page);
    await page.goto('/');
    await expect(page.locator('[data-testid="app-container"]')).toBeVisible();
    await page.click('a[href="/collection"]');
    await page.waitForURL('**/collection');
  });

  test('@smoke collection page loads and is browsable', async ({ page }) => {
    // The collection page must render without an error state so that an
    // imported collection has somewhere to land (#901 AC1 destination).
    await expect(page.locator('[data-testid="collection-page"]')).toBeVisible();
    await expect(page).toHaveURL(/.*\/collection/);

    const errorState = page.locator('[data-testid="collection-error"]');
    await expect(errorState).not.toBeVisible();
  });

  test('@smoke collection header renders with imported-card data', async ({
    page,
  }) => {
    // The header must show once BFF data arrives — this is what a user sees
    // immediately after a successful manual import.
    const header = page.locator('[data-testid="collection-header"]');
    await expect(header).toBeVisible({ timeout: 15_000 });
  });

  test('@smoke collection stats panel renders', async ({ page }) => {
    // Stats panel shows owned-card counts — the primary feedback a user gets
    // after importing their collection.
    const stats = page.locator('[data-testid="collection-stats"]');
    await expect(stats).toBeVisible({ timeout: 15_000 });
  });

  test('@smoke collection shows cards or empty state — no silent failure',
    async ({ page }) => {
      // This guards against a silent regression where the collection endpoint
      // returns 200 but the SPA renders neither cards nor an empty state —
      // the user would see a blank page after importing.
      await expect(
        page.locator('[data-testid="collection-page"]'),
      ).toBeVisible();

      const cardGrid = page.locator('[data-testid="collection-card-grid"]');
      const emptyState = page.locator('[data-testid="collection-empty"]');

      const hasCards = await cardGrid.isVisible().catch(() => false);
      const hasEmpty = await emptyState.isVisible().catch(() => false);

      expect(
        hasCards || hasEmpty,
        'collection page shows neither cards nor empty state — silent failure',
      ).toBeTruthy();
    });
});

// ---------------------------------------------------------------------------
// Suite 2 — Settings / Import-Export section (@smoke)
//
// The Settings page hosts the import entry-point (ImportExportSection).
// These tests confirm the section is reachable and renders without error —
// guarding against the import UI being accidentally removed or hidden.
// ---------------------------------------------------------------------------

test.describe('Manual-Import: settings import-export section gate', () => {
  test.beforeEach(async ({ page }) => {
    await setClerkSignedIn(page);
    // Mock the settings endpoint so useSettings() doesn't call a real BFF.
    // Must be registered before page.goto() so it intercepts the initial load.
    await page.route('**/api/v1/settings', (route) => {
      if (route.request().method() === 'GET') {
        void route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ data: {} }),
        });
        return;
      }
      void route.continue();
    });
    // Navigate directly to settings so the URL transition is synchronous
    // and we avoid the click+waitForURL race that occurs when Clerk test-state
    // injection causes the page to briefly redirect before settling.
    await page.goto('/settings');
    await expect(page.locator('h1')).toContainText('Settings', {
      timeout: 15_000,
    });
  });

  test('@smoke settings page loads without error', async ({ page }) => {
    // If settings crashes, the import entry point is unreachable.
    await expect(page.locator('h1')).toContainText('Settings', {
      timeout: 15_000,
    });
    // No global error boundary should have fired.
    await expect(
      page.locator('[data-testid="route-error-fallback"]'),
    ).not.toBeAttached();
  });

  test('@smoke export accordion section is present in settings', async ({
    page,
  }) => {
    // The Export accordion item must exist — it is the current home for the
    // ImportExportSection component that will gain the import controls in #895.
    // If this disappears the import UI will have no home.
    // Use the accordion item id "export" from Settings.tsx (id: 'export').
    await expect(page.locator('#accordion-export')).toBeVisible({
      timeout: 15_000,
    });
  });
});

// ---------------------------------------------------------------------------
// Suite 3 — Upload happy-path (live — activated by #895 / PR #3105)
//
// These were shape-holders (test.fixme) until #895 landed. The test.fixme()
// calls were removed in PR #3105 (merged 2026-06-10) once Frank added the
// stable data-testid selectors to CollectionImportForm.tsx:
//   manual-import-file-input, manual-import-submit,
//   manual-import-success, manual-import-error
//
// Ticket: #1181 — confirmed live by Frida (2026-06-11).
// ---------------------------------------------------------------------------

test.describe('Manual-Import: upload happy path', () => {
  test.beforeEach(async ({ page }) => {
    await setClerkSignedIn(page);
    await mockCollectionEndpointsWithData(page);
    await mockImportEndpoint(page);
    // Mock settings endpoint so the Settings page doesn't call a real BFF.
    await page.route('**/api/v1/settings', (route) => {
      if (route.request().method() === 'GET') {
        void route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ data: {} }),
        });
        return;
      }
      void route.continue();
    });
  });

  test('user uploads golden-fixture CSV and collection becomes browsable @smoke',
    async ({ page }) => {
      // AC1 of #901/#895: user uploads → import succeeds → collection is browsable.

      // Navigate to the import entry point.
      await page.goto('/settings');
      await page.waitForURL('**/settings');
      // Open the export/import accordion section (button id: accordion-header-export).
      await page.click('#accordion-header-export');

      // The import file input should be visible.
      const fileInput = page.locator('[data-testid="manual-import-file-input"]');
      await expect(fileInput).toBeVisible();

      // Upload the golden fixture.
      await fileInput.setInputFiles(FIXTURE_PATH);

      // Confirm button / submit.
      const submitBtn = page.locator('[data-testid="manual-import-submit"]');
      await submitBtn.click();

      // Success state must render.
      await expect(
        page.locator('[data-testid="manual-import-success"]'),
      ).toBeVisible({ timeout: 15_000 });

      // Navigate to collection and verify it is browsable.
      await page.click('a[href="/collection"]');
      await page.waitForURL('**/collection');
      await expect(
        page.locator('[data-testid="collection-page"]'),
      ).toBeVisible();
      const errorState = page.locator('[data-testid="collection-error"]');
      await expect(errorState).not.toBeVisible();
    });

  test('import with invalid file shows validation error', async ({ page }) => {
    // AC1 failure mode: uploading a non-MTGA file must show a validation
    // error rather than silently producing a corrupt collection.

    await page.goto('/settings');
    await page.waitForURL('**/settings');
    await page.click('#accordion-header-export');

    const fileInput = page.locator('[data-testid="manual-import-file-input"]');
    await expect(fileInput).toBeVisible();

    // Set a nonsense file.
    await fileInput.setInputFiles({
      name: 'not-a-collection.txt',
      mimeType: 'text/plain',
      buffer: Buffer.from('this is not MTGA format'),
    });

    const submitBtn = page.locator('[data-testid="manual-import-submit"]');
    await submitBtn.click();

    await expect(
      page.locator('[data-testid="manual-import-error"]'),
    ).toBeVisible({ timeout: 10_000 });
  });

  test('import endpoint error shows user-facing error message', async ({
    page,
  }) => {
    // Regression guard: if the BFF import endpoint returns 500, the SPA must
    // surface a user-facing error rather than silently leaving the collection
    // unchanged.

    await mockImportEndpoint(page, {
      status: 500,
      body: { error: 'internal server error' },
    });

    await page.goto('/settings');
    await page.waitForURL('**/settings');
    await page.click('#accordion-header-export');

    const fileInput = page.locator('[data-testid="manual-import-file-input"]');
    await fileInput.setInputFiles(FIXTURE_PATH);

    await page.locator('[data-testid="manual-import-submit"]').click();

    await expect(
      page.locator('[data-testid="manual-import-error"]'),
    ).toBeVisible({ timeout: 10_000 });
  });
});
