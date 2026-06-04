import { test, expect, type Page } from '@playwright/test';

/**
 * Report Bug Button E2E Tests (#425 — SH-5)
 *
 * Verifies that the "Report a bug" button in the nav header:
 *   1. Is visible when the user is signed in
 *   2. Is hidden when the user is signed out
 *
 * The Sentry feedback dialog itself is a third-party widget injected at
 * runtime by the @sentry/react feedbackIntegration. In E2E test mode Sentry
 * is not initialised (VITE_SENTRY_DSN is absent), so getFeedback() returns
 * undefined and clicking the button is a no-op — we only assert render and
 * visibility here. The pre-fill logic (buildContextMessage) is fully covered
 * by the companion component unit tests in ReportBugButton.test.tsx.
 *
 * Auth approach: VITE_CLERK_TEST_MODE=true aliases @clerk/react to
 * clerkMock.tsx. Auth state is injected via window.__CLERK_TEST_STATE__
 * before each navigation (same pattern as settings.spec.ts / home.spec.ts).
 *
 * BFF routes used by Layout (HomeSummary) are mocked so the page renders
 * without a live authenticated BFF.
 */

// ---------------------------------------------------------------------------
// Clerk test-state helpers
// ---------------------------------------------------------------------------

type ClerkTestState = {
  isSignedIn: boolean;
  userId?: string;
  firstName?: string;
  lastName?: string;
  email?: string;
};

async function setClerkSignedIn(page: Page, user?: Partial<ClerkTestState>): Promise<void> {
  const state: ClerkTestState = {
    isSignedIn: true,
    userId: user?.userId ?? 'user_test_123',
    firstName: user?.firstName ?? 'Test',
    lastName: user?.lastName ?? 'User',
    email: user?.email ?? 'test@example.com',
  };
  await page.addInitScript((s) => {
    (window as unknown as Record<string, unknown>).__CLERK_TEST_STATE__ = s;
  }, state);
}

async function setClerkSignedOut(page: Page): Promise<void> {
  await page.addInitScript(() => {
    (window as unknown as Record<string, unknown>).__CLERK_TEST_STATE__ = { isSignedIn: false };
  });
}

// ---------------------------------------------------------------------------
// BFF mocks
// ---------------------------------------------------------------------------

/**
 * Mock BFF endpoints that Layout fetches on mount so the page can render
 * without a live authenticated BFF.
 */
async function mockLayoutEndpoints(page: Page): Promise<void> {
  // HomeSummary — Layout fires this on sign-in to decide onboarding modal
  await page.route('**/api/v1/history/summary', (route) => {
    void route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        data: {
          all_time: { matches: 5, wins: 3, losses: 2, win_rate: 0.6 },
          this_week: { matches: 2, wins: 1, losses: 1, win_rate: 0.5 },
        },
      }),
    });
  });

  // Daemon health — Layout's DaemonHealthIndicator polls this
  await page.route('**/api/v1/daemon/health', (route) => {
    void route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ data: { status: 'connected' } }),
    });
  });
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

test.describe('ReportBugButton', () => {
  test('@smoke button is visible in the nav header when signed in', async ({ page }) => {
    await setClerkSignedIn(page);
    await mockLayoutEndpoints(page);

    await page.goto('/home');
    await expect(page.locator('[data-testid="app-container"]')).toBeVisible();

    const btn = page.locator('[data-testid="report-bug-button"]');
    await expect(btn).toBeVisible();
    await expect(btn).toHaveText('Report a bug');
  });

  test('button is not rendered when signed out', async ({ page }) => {
    await setClerkSignedOut(page);
    await mockLayoutEndpoints(page);

    await page.goto('/');

    // Signed-out users see the sign-in prompt, not the Layout chrome
    const btn = page.locator('[data-testid="report-bug-button"]');
    await expect(btn).not.toBeVisible();
  });

  test('clicking the button does not throw when Sentry is uninitialised', async ({ page }) => {
    await setClerkSignedIn(page);
    await mockLayoutEndpoints(page);

    // Capture any uncaught errors thrown during the click
    const errors: string[] = [];
    page.on('pageerror', (err) => errors.push(err.message));

    await page.goto('/home');
    await expect(page.locator('[data-testid="report-bug-button"]')).toBeVisible();

    await page.locator('[data-testid="report-bug-button"]').click();

    // Allow any async handlers to settle
    await page.waitForTimeout(200);

    // No uncaught errors should have been thrown
    expect(errors).toHaveLength(0);
  });
});
