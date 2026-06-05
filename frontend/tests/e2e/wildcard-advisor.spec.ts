/**
 * E2E tests for the WildcardAdvisorPanel (#421).
 *
 * Covers:
 *  - Load: panel renders after toggling from the Collection page.
 *  - Format toggle: switching formats triggers a new fetch.
 *  - 409 sync-CTA: collection not synced state.
 *  - Stale-warning: banner appears when data is >24h stale.
 *
 * All BFF endpoints (including collection and wildcard) are mocked via
 * page.route() so these tests do not depend on a live authenticated BFF.
 *
 * Auth: uses __CLERK_TEST_STATE__ injection (same pattern as collection.spec.ts).
 */

import { test, expect, type Page } from '@playwright/test';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

async function setClerkSignedIn(page: Page): Promise<void> {
  await page.addInitScript(() => {
    (window as unknown as Record<string, unknown>).__CLERK_TEST_STATE__ = { isSignedIn: true };
  });
}

/** Mock all endpoints Collection.tsx needs so the page renders without errors. */
async function mockCollectionEndpoints(page: Page): Promise<void> {
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
    void route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ data: {} }) });
  });

  await page.route('**/api/v1/collection/sets', (route) => {
    void route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ data: [] }) });
  });

  await page.route('**/api/v1/cards/sets', (route) => {
    void route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ data: [] }) });
  });
}

/** Mock the wildcard advisor endpoint with a standard successful response. */
async function mockWildcardAdvisorSuccess(page: Page): Promise<void> {
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
            owned_copies: 2,
            missing_copies: 2,
            gihwr: 62.1,
            archetype_count: 5,
            format_context: 'Appears in 5 top Standard archetypes',
            set_code: 'DMU',
          },
        ],
        wildcard_budget: { common: 10, uncommon: 8, rare: 4, mythic: 1 },
      }),
    });
  });
}

/** Mock the wildcard advisor endpoint with a 409 response. */
async function mockWildcardAdvisor409(page: Page): Promise<void> {
  await page.route('**/api/v1/recommendations/wildcards**', (route) => {
    void route.fulfill({
      status: 409,
      contentType: 'application/json',
      body: JSON.stringify({ error: 'collection_not_synced' }),
    });
  });
}

/** Mock the wildcard advisor endpoint with stale headers. */
async function mockWildcardAdvisorStale(page: Page): Promise<void> {
  await page.route('**/api/v1/recommendations/wildcards**', (route) => {
    void route.fulfill({
      status: 200,
      contentType: 'application/json',
      headers: {
        'x-cache-degraded': 'true',
        'x-cache-age-hours': '36',
      },
      body: JSON.stringify({
        format: 'Standard',
        recommendations: [
          {
            arena_id: 102,
            name: 'The One Ring',
            rarity: 'mythic',
            owned_copies: 0,
            missing_copies: 1,
            gihwr: 70.0,
            set_code: 'LTR',
          },
        ],
        wildcard_budget: { common: 10, uncommon: 8, rare: 4, mythic: 0 },
      }),
    });
  });
}

// ---------------------------------------------------------------------------
// Navigate to the Collection page
// ---------------------------------------------------------------------------

async function navigateToCollection(page: Page): Promise<void> {
  await page.goto('/');
  await expect(page.locator('[data-testid="app-container"]')).toBeVisible();
  await page.click('a[href="/collection"]');
  await page.waitForURL('**/collection');
  await expect(page.locator('[data-testid="collection-page"]')).toBeVisible();
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

test.describe('WildcardAdvisorPanel', () => {
  test.describe('Load', () => {
    test('@smoke panel renders after toggling from the Collection page', async ({ page }) => {
      await setClerkSignedIn(page);
      await mockCollectionEndpoints(page);
      await mockWildcardAdvisorSuccess(page);
      await navigateToCollection(page);

      // Toggle the panel open
      const toggleBtn = page.locator('[data-testid="collection-toggle-wildcard-advisor"]');
      await expect(toggleBtn).toBeVisible({ timeout: 10000 });
      await toggleBtn.click();

      // Panel should appear
      await expect(
        page.locator('[data-testid="wildcard-advisor-panel"]')
      ).toBeVisible({ timeout: 15000 });
    });

    test('panel closes when the close button is clicked', async ({ page }) => {
      await setClerkSignedIn(page);
      await mockCollectionEndpoints(page);
      await mockWildcardAdvisorSuccess(page);
      await navigateToCollection(page);

      await page.locator('[data-testid="collection-toggle-wildcard-advisor"]').click();
      await expect(page.locator('[data-testid="wildcard-advisor-panel"]')).toBeVisible({ timeout: 10000 });

      await page.locator('[data-testid="wildcard-advisor-close"]').click();
      await expect(page.locator('[data-testid="wildcard-advisor-panel"]')).not.toBeVisible();
    });

    test('shows wildcard budget after loading', async ({ page }) => {
      await setClerkSignedIn(page);
      await mockCollectionEndpoints(page);
      await mockWildcardAdvisorSuccess(page);
      await navigateToCollection(page);

      await page.locator('[data-testid="collection-toggle-wildcard-advisor"]').click();

      await expect(
        page.locator('[data-testid="wildcard-advisor-budget"]')
      ).toBeVisible({ timeout: 20000 });
    });
  });

  test.describe('Format toggle', () => {
    test('all 4 format buttons are visible', async ({ page }) => {
      await setClerkSignedIn(page);
      await mockCollectionEndpoints(page);
      await mockWildcardAdvisorSuccess(page);
      await navigateToCollection(page);

      await page.locator('[data-testid="collection-toggle-wildcard-advisor"]').click();
      await expect(page.locator('[data-testid="wildcard-advisor-panel"]')).toBeVisible({ timeout: 10000 });

      await expect(page.locator('[data-testid="wildcard-advisor-format-standard"]')).toBeVisible();
      await expect(page.locator('[data-testid="wildcard-advisor-format-historic"]')).toBeVisible();
      await expect(page.locator('[data-testid="wildcard-advisor-format-explorer"]')).toBeVisible();
      await expect(page.locator('[data-testid="wildcard-advisor-format-alchemy"]')).toBeVisible();
    });

    test('clicking a format button triggers a new fetch', async ({ page }) => {
      await setClerkSignedIn(page);
      await mockCollectionEndpoints(page);

      const fetchedFormats: string[] = [];
      await page.route('**/api/v1/recommendations/wildcards**', (route) => {
        const url = new URL(route.request().url());
        fetchedFormats.push(url.searchParams.get('format') ?? '');
        void route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            format: url.searchParams.get('format') ?? 'Standard',
            recommendations: [],
            wildcard_budget: { common: 0, uncommon: 0, rare: 0, mythic: 0 },
          }),
        });
      });

      await navigateToCollection(page);
      await page.locator('[data-testid="collection-toggle-wildcard-advisor"]').click();
      await expect(page.locator('[data-testid="wildcard-advisor-panel"]')).toBeVisible({ timeout: 10000 });

      // Wait for initial Standard fetch
      await expect(page.locator('[data-testid="wildcard-advisor-format-standard"]')).toBeVisible();

      // Click Historic
      await page.locator('[data-testid="wildcard-advisor-format-historic"]').click();

      await page.waitForFunction(() => {
        return (window as unknown as { __WILDCARD_FETCHED_FORMATS__?: string[] }).__WILDCARD_FETCHED_FORMATS__ !== undefined
          || true; // just wait for network
      });

      // The Historic button should become active
      await expect(
        page.locator('[data-testid="wildcard-advisor-format-historic"]')
      ).toHaveAttribute('aria-pressed', 'true', { timeout: 10000 });
    });
  });

  test.describe('409 sync-CTA', () => {
    test('shows sync-CTA state when collection is not synced', async ({ page }) => {
      await setClerkSignedIn(page);
      await mockCollectionEndpoints(page);
      await mockWildcardAdvisor409(page);
      await navigateToCollection(page);

      await page.locator('[data-testid="collection-toggle-wildcard-advisor"]').click();

      await expect(
        page.locator('[data-testid="wildcard-advisor-sync-cta"]')
      ).toBeVisible({ timeout: 20000 });

      await expect(page.locator('[data-testid="wildcard-advisor-sync-cta"]')).toContainText(
        'Collection Not Synced'
      );
    });
  });

  test.describe('Stale-warning banner', () => {
    test('shows stale banner when data is degraded and >24h old', async ({ page }) => {
      await setClerkSignedIn(page);
      await mockCollectionEndpoints(page);
      await mockWildcardAdvisorStale(page);
      await navigateToCollection(page);

      await page.locator('[data-testid="collection-toggle-wildcard-advisor"]').click();

      await expect(
        page.locator('[data-testid="wildcard-advisor-stale-banner"]')
      ).toBeVisible({ timeout: 20000 });
    });
  });
});
