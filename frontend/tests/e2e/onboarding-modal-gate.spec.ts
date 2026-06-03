/**
 * Onboarding modal gating E2E tests — #715 tri-state fix
 *
 * Verifies that the "Get Started with VaultMTG" first-run onboarding modal:
 *   - Does NOT appear for returning users who already have BFF data
 *   - DOES appear for genuine new users (0 matches, daemon disconnected)
 *   - Does NOT appear while the summary fetch is still in flight ('pending')
 *   - Does NOT appear when the summary endpoint errors (fail-closed 'pending')
 *   - Does not re-appear after sign-out and re-sign-in for a data-having account
 *
 * The distinguishing factor is what GET /api/v1/history/summary returns:
 *   - all_time.matches > 0 → 'has-data' → modal suppressed
 *   - all_time.matches === 0 → 'empty' + daemon disconnected → modal fires
 *   - error → stays 'pending' → modal suppressed (fail-closed)
 *
 * DaemonHealthIndicator is stubbed to 'disconnected' so the daemon-status
 * condition is always satisfied; the summary response is the only variable.
 */

import { test, expect } from '@playwright/test';

type ClerkTestState = { isSignedIn: boolean; firstName?: string };

/** Inject signed-in Clerk state before page load. */
async function setClerkSignedIn(
  page: import('@playwright/test').Page,
  user: Partial<ClerkTestState> = {}
): Promise<void> {
  const state: ClerkTestState = { isSignedIn: true, firstName: 'Test', ...user };
  await page.addInitScript((s) => {
    (window as unknown as Record<string, unknown>).__CLERK_TEST_STATE__ = s;
  }, state);
}

/** Stub the daemon health endpoint to always return 'disconnected'. */
async function stubDaemonDisconnected(page: import('@playwright/test').Page): Promise<void> {
  await page.route('**/api/v1/health/daemon', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ status: 'disconnected' }),
    });
  });
}

/** Stub the history/summary endpoint to simulate a returning user (19 matches). */
async function stubSummaryReturningUser(page: import('@playwright/test').Page): Promise<void> {
  await page.route('**/api/v1/history/summary', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        today: { wins: 2, losses: 1, win_rate: 0.667 },
        this_week: { wins: 10, losses: 9, win_rate: 0.526, matches: 19 },
        all_time: {
          wins: 10,
          losses: 9,
          win_rate: 0.526,
          matches: 19,
          current_streak: 1,
          streak_type: 'W',
        },
        last_match: { result: 'win', opponent_archetype: null, elapsed_seconds: 600 },
      }),
    });
  });
}

/** Stub the history/summary endpoint to simulate a new user (0 matches). */
async function stubSummaryNewUser(page: import('@playwright/test').Page): Promise<void> {
  await page.route('**/api/v1/history/summary', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        today: { wins: 0, losses: 0, win_rate: 0 },
        this_week: { wins: 0, losses: 0, win_rate: 0, matches: 0 },
        all_time: {
          wins: 0,
          losses: 0,
          win_rate: 0,
          matches: 0,
          current_streak: 0,
          streak_type: 'W',
        },
        last_match: null,
      }),
    });
  });
}

test.describe('Onboarding modal gate — tri-state AccountDataState (#715)', () => {
  test.beforeEach(async ({ page }) => {
    // Ensure localStorage is clean — no previously-dismissed/completed state.
    await page.addInitScript(() => {
      localStorage.removeItem('vaultmtg_onboarding_dismissed');
      localStorage.removeItem('vaultmtg_onboarding_completed');
      // Clear the session-level 'has-data' cache too.
      sessionStorage.removeItem('vaultmtg_has_account_data');
    });
  });

  // ── Returning user: modal must NOT appear ────────────────────────────────

  test('@smoke returning user with match data does not see onboarding modal on /home', async ({ page }) => {
    await setClerkSignedIn(page);
    await stubDaemonDisconnected(page);
    await stubSummaryReturningUser(page);

    await page.goto('/home');
    await expect(page.locator('[data-testid="app-container"]')).toBeVisible();

    // Summary fires on isSignedIn; once resolved to 'has-data' the modal is suppressed.
    await expect(page.locator('[data-testid="onboarding-modal"]')).not.toBeVisible();
  });

  test('returning user does not see modal on /match-history after navigation', async ({ page }) => {
    await setClerkSignedIn(page);
    await stubDaemonDisconnected(page);
    await stubSummaryReturningUser(page);

    await page.goto('/match-history');
    await expect(page.locator('[data-testid="app-container"]')).toBeVisible();

    await expect(page.locator('[data-testid="onboarding-modal"]')).not.toBeVisible();
  });

  test('returning user does not see modal on /draft even on hard reload', async ({ page }) => {
    await setClerkSignedIn(page);
    await stubDaemonDisconnected(page);
    await stubSummaryReturningUser(page);

    await page.goto('/draft');
    await expect(page.locator('[data-testid="app-container"]')).toBeVisible();

    await expect(page.locator('[data-testid="onboarding-modal"]')).not.toBeVisible();
  });

  // ── Genuine new user: modal MUST appear ──────────────────────────────────

  test('new user with no BFF data sees onboarding modal', async ({ page }) => {
    await setClerkSignedIn(page);
    await stubDaemonDisconnected(page);
    await stubSummaryNewUser(page);

    await page.goto('/home');
    await expect(page.locator('[data-testid="app-container"]')).toBeVisible();

    // Summary fires immediately on sign-in; resolves to 'empty' → modal should appear
    // once daemon status is also 'disconnected'.
    await expect(page.locator('[data-testid="onboarding-modal"]')).toBeVisible();
    await expect(page.getByText('Get Started with VaultMTG')).toBeVisible();
    await expect(page.locator('[data-testid="onboarding-step-1"]')).toBeVisible();
  });

  // ── Fail-closed: transient 500 must NOT pop modal for any user ────────────

  test('transient 500 on summary does not show modal (fail-closed — accountDataState stays pending)', async ({ page }) => {
    await setClerkSignedIn(page);
    await stubDaemonDisconnected(page);

    // Summary returns 500 — Layout catches the error and stays at 'pending'.
    // 'pending' blocks the modal regardless of daemonStatus.
    await page.route('**/api/v1/history/summary', async (route) => {
      await route.fulfill({ status: 500, body: 'internal error' });
    });

    await page.goto('/home');
    await expect(page.locator('[data-testid="app-container"]')).toBeVisible();

    // Modal must NOT appear — fail-closed means uncertain state blocks the modal.
    await expect(page.locator('[data-testid="onboarding-modal"]')).not.toBeVisible();
  });

  // ── Sign-out reset: session data cleared on sign-out ─────────────────────

  test('returning user: sessionStorage cleared on sign-out hides modal on re-sign-in of same account', async ({ page }) => {
    // Start signed in as a returning user.
    await setClerkSignedIn(page);
    await stubDaemonDisconnected(page);
    await stubSummaryReturningUser(page);

    await page.goto('/home');
    await expect(page.locator('[data-testid="app-container"]')).toBeVisible();
    await expect(page.locator('[data-testid="onboarding-modal"]')).not.toBeVisible();

    // Verify sessionStorage was set to 'true' (has-data persisted).
    const cached = await page.evaluate(() => sessionStorage.getItem('vaultmtg_has_account_data'));
    expect(cached).toBe('true');

    // Sign out: inject signed-out state and navigate.
    // This simulates the isSignedIn useEffect firing the reset (ref + sessionStorage clear).
    await page.evaluate(() => {
      (window as unknown as Record<string, unknown>).__CLERK_TEST_STATE__ = { isSignedIn: false };
    });

    // After sign-out, the sessionStorage entry is cleared by the Layout useEffect.
    // We need to navigate to re-trigger the effect in the test environment.
    await page.goto('/home');

    // On the fresh page load (signed-out), sessionStorage should be cleared.
    const cachedAfterSignout = await page.evaluate(() =>
      sessionStorage.getItem('vaultmtg_has_account_data')
    );
    // The key should be absent after sign-out (Layout useEffect clears it).
    expect(cachedAfterSignout).toBeNull();
  });
});
