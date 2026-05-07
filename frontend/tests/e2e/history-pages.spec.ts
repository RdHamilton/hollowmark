import { test, expect, Page } from '@playwright/test'

/**
 * History Pages Smoke Tests
 *
 * Verifies that the cloud BFF match history and draft history pages load
 * and show either a data table or an empty state.
 *
 * Auth: uses the same window.__CLERK_TEST_STATE__ injection pattern as auth.spec.ts.
 * The Vite dev server is started with VITE_CLERK_TEST_MODE=true which aliases
 * @clerk/react to the test mock that reads this state before every navigation.
 */

/** Inject signed-in Clerk state before page load. Must be called before page.goto(). */
async function setClerkSignedIn(page: Page): Promise<void> {
  await page.addInitScript((s) => {
    (window as unknown as Record<string, unknown>).__CLERK_TEST_STATE__ = s;
  }, { isSignedIn: true, firstName: 'Test', lastName: 'User' });
}

test.describe('History pages', () => {
  test.beforeEach(async ({ page }) => {
    await setClerkSignedIn(page);
  });

  test('match history page shows table or empty state @smoke', async ({ page }) => {
    await page.goto('/history/matches')

    // After loading, must show table OR empty state
    const table = page.locator('[data-testid="match-history-table"]')
    const empty = page.locator('[data-testid="match-history-empty"]')

    await expect(table.or(empty)).toBeVisible({ timeout: 15000 })
  })

  test('draft history page shows table or empty state @smoke', async ({ page }) => {
    await page.goto('/history/drafts')

    // After loading, must show table OR empty state
    const table = page.locator('[data-testid="draft-history-table"]')
    const empty = page.locator('[data-testid="draft-history-empty"]')

    await expect(table.or(empty)).toBeVisible({ timeout: 15000 })
  })
})
