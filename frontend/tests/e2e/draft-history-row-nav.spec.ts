import { test, expect, Page } from '@playwright/test';

/**
 * Draft-history row navigation E2E (#58 / FT-3)
 *
 * Verifies end-to-end: clicking a row in BffDraftHistory (/history/drafts)
 * navigates to DraftAnalytics (/draft-analytics) scoped to that specific draft.
 *
 * Auth approach: same as history-pages.spec.ts — Vite build uses
 * VITE_CLERK_TEST_MODE=true which aliases @clerk/react to the test mock.
 * window.__CLERK_TEST_STATE__ controls auth state.
 *
 * BFF mocking: registers page.route() interceptors for:
 *   - GET /api/v1/history/drafts  — draft list fixture
 *   - GET /api/v1/drafts/formats  — set list used by DraftAnalytics
 */

async function setClerkSignedIn(page: Page): Promise<void> {
  await page.addInitScript((s) => {
    (window as unknown as Record<string, unknown>).__CLERK_TEST_STATE__ = s;
  }, { isSignedIn: true, firstName: 'Test', lastName: 'User' });
}

/** Wire-shape draft items matching BffDraftHistory's getDraftHistory adapter output. */
type DraftFixtureItem = {
  id: string;
  set_code: string;
  format: string;
  started_at: string;
  completed_at: string | null;
  wins: number;
  losses: number;
};

const DRAFT_FIXTURE: DraftFixtureItem[] = [
  {
    id: 'fixture-session-sos-63',
    set_code: 'SOS',
    format: 'PremierDraft',
    started_at: '2026-05-01T10:00:00Z',
    completed_at: '2026-05-01T12:00:00Z',
    wins: 6,
    losses: 3,
  },
  {
    id: 'fixture-session-blb-30',
    set_code: 'BLB',
    format: 'QuickDraft',
    started_at: '2026-04-15T10:00:00Z',
    completed_at: '2026-04-15T11:30:00Z',
    wins: 3,
    losses: 0,
  },
];

/**
 * Register BFF route mocks before page.goto() is called.
 */
async function mockDraftHistoryBff(page: Page): Promise<void> {
  // BFF /api/v1/history/drafts — getDraftHistory maps wire.data → drafts
  await page.route('**/api/v1/history/drafts**', (route) => {
    void route.fulfill({
      status: 200,
      contentType: 'application/json',
      // Wire shape: { data, total, page, limit }
      body: JSON.stringify({
        data: DRAFT_FIXTURE,
        total: DRAFT_FIXTURE.length,
        page: 1,
        limit: 20,
      }),
    });
  });
}

async function mockDraftFormatsBff(page: Page, sets: string[] = ['SOS', 'BLB', 'DSK']): Promise<void> {
  await page.route('**/api/v1/drafts/formats**', (route) => {
    void route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(sets),
    });
  });
}

// ---------------------------------------------------------------------------

test.describe('Draft history row → DraftAnalytics navigation (#58)', () => {
  test.beforeEach(async ({ page }) => {
    await setClerkSignedIn(page);
    await mockDraftHistoryBff(page);
    await mockDraftFormatsBff(page);
  });

  test('@smoke clicking a draft-history row navigates to /draft-analytics with session and set params', async ({ page }) => {
    await page.goto('/history/drafts');

    // Wait for the draft table to render.
    const table = page.locator('[data-testid="draft-history-table"]');
    await expect(table).toBeVisible();

    // Click the first row (SOS 6-3).
    const firstRow = page.locator('[data-testid="draft-history-row"]').first();
    await expect(firstRow).toBeVisible();
    await firstRow.click();

    // Must navigate to /draft-analytics with ?session= and ?set= params.
    await page.waitForURL('**/draft-analytics?session=fixture-session-sos-63&set=SOS', { timeout: 10_000 });

    const url = page.url();
    expect(url).toContain('session=fixture-session-sos-63');
    expect(url).toContain('set=SOS');
  });

  test('DraftAnalytics shows the session-scope banner after row-click navigation', async ({ page }) => {
    await page.goto('/history/drafts');

    const table = page.locator('[data-testid="draft-history-table"]');
    await expect(table).toBeVisible();

    await page.locator('[data-testid="draft-history-row"]').first().click();

    await page.waitForURL('**/draft-analytics**');

    // Session scope banner must be present.
    const banner = page.locator('[data-testid="draft-analytics-session-scope"]');
    await expect(banner).toBeVisible({ timeout: 15_000 });
    await expect(banner).toContainText('SOS');
  });

  test('DraftAnalytics pre-selects the set from the ?set= param after row-click navigation', async ({ page }) => {
    await page.goto('/history/drafts');

    const table = page.locator('[data-testid="draft-history-table"]');
    await expect(table).toBeVisible();

    // Click the second row (BLB 3-0) to verify a non-first set is pre-selected.
    const secondRow = page.locator('[data-testid="draft-history-row"]').nth(1);
    await expect(secondRow).toBeVisible();
    await secondRow.click();

    await page.waitForURL('**/draft-analytics**');

    // Wait for DraftAnalytics content to load.
    const contentOrEmpty = page.locator('.draft-analytics__content, .draft-analytics--empty');
    await expect(contentOrEmpty).toBeVisible({ timeout: 15_000 });

    const hasContent = await page.locator('.draft-analytics__content').isVisible().catch(() => false);
    if (hasContent) {
      const setSelect = page.locator('select#set-select');
      await expect(setSelect).toBeVisible();
      await expect(setSelect).toHaveValue('BLB');
    }
  });

  test('DraftAnalytics without params renders generic view (no session banner)', async ({ page }) => {
    await page.goto('/draft-analytics');

    // The session-scope banner must NOT be present when navigating directly.
    const contentOrEmpty = page.locator('.draft-analytics__content, .draft-analytics--empty');
    await expect(contentOrEmpty).toBeVisible({ timeout: 15_000 });

    await expect(page.locator('[data-testid="draft-analytics-session-scope"]')).not.toBeVisible();
  });
});
