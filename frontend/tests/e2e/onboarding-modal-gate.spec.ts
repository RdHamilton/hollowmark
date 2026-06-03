/**
 * Onboarding modal gating E2E tests — Bug 1 fix
 *
 * Verifies that the "Get Started with VaultMTG" first-run onboarding modal:
 *   - Does NOT appear for returning users who already have BFF data
 *   - DOES appear for genuine new users (no BFF data, daemon disconnected)
 *
 * Both scenarios require daemon disconnected status (modal auto-show condition).
 * The distinguishing factor is whether GET /api/v1/history/summary returns
 * all_time.matches > 0.
 *
 * The DaemonHealthIndicator polls GET /api/v1/health/daemon; we stub it to
 * return 'disconnected' to satisfy the daemon-status condition.
 */

import { test, expect } from '@playwright/test';

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

test.describe('Onboarding modal gate', () => {
  test.beforeEach(async ({ page }) => {
    // Inject signed-in Clerk state so ProtectedRoute passes through.
    await page.addInitScript(() => {
      (window as unknown as Record<string, unknown>).__CLERK_TEST_STATE__ = {
        isSignedIn: true,
      };
    });

    // Ensure localStorage is clean — no previously-dismissed/completed state.
    await page.addInitScript(() => {
      localStorage.removeItem('vaultmtg_onboarding_dismissed');
      localStorage.removeItem('vaultmtg_onboarding_completed');
    });
  });

  // ── Bug 1 regression: returning user with data must NOT see the modal ──────

  test('@smoke returning user with match data does not see onboarding modal', async ({ page }) => {
    await stubDaemonDisconnected(page);

    // Stub home summary: returning user has 19 matches.
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

    await page.goto('/home');

    // The onboarding modal must NOT be present for a returning user.
    await expect(page.locator('[data-testid="onboarding-modal"]')).not.toBeVisible();
  });

  // ── Genuine new user with no data DOES see the modal ─────────────────────

  test('new user with no BFF data sees onboarding modal', async ({ page }) => {
    await stubDaemonDisconnected(page);

    // Stub home summary: new user has 0 matches.
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

    await page.goto('/home');

    // The onboarding modal IS shown for a new user.
    await expect(page.locator('[data-testid="onboarding-modal"]')).toBeVisible();
    await expect(page.getByText('Get Started with VaultMTG')).toBeVisible();
    await expect(page.locator('[data-testid="onboarding-step-1"]')).toBeVisible();
  });

  // ── Fallback: summary endpoint failure must not block the modal for new users

  test('onboarding modal shows when summary endpoint fails (new user fallback)', async ({ page }) => {
    await stubDaemonDisconnected(page);

    // Summary returns error — conservative fallback: treat as no data.
    await page.route('**/api/v1/history/summary', async (route) => {
      await route.fulfill({ status: 500, body: 'internal error' });
    });

    await page.goto('/home');

    // The modal should still appear — a failing summary endpoint must not
    // silently hide the onboarding flow from a new user.
    await expect(page.locator('[data-testid="onboarding-modal"]')).toBeVisible();
  });
});
