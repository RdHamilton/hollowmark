import { test, expect, type Page } from '@playwright/test';

/**
 * Daemon Onboarding Flow E2E tests (#1398)
 *
 * Verifies that the OnboardingModal appears for a new user whose daemon
 * is not connected, and that the 3-step flow works correctly.
 *
 * The BFF's /api/v1/health/daemon endpoint is mocked per-test to return
 * disconnected so the onboarding modal triggers deterministically.
 *
 * Note: Onboarding modal visibility is gated on (useDaemonOnboarding) by SIX gates:
 * 1. User is signed in (Clerk test mode provides mock auth)
 * 2. accountDataState === 'empty' (getHomeSummary returns 0 matches)
 * 3. Daemon is disconnected (BFF health check returns disconnected)
 * 4. User has not previously dismissed/completed onboarding (localStorage is clean)
 * 5. Not manually closed this session
 * 6. collectionMode === 'enhanced' (#895 D3: manual-mode new users see ManualImportModal
 *    instead; the daemon modal is ONLY for enhanced-mode users)
 *
 * Fix (#2178): added setClerkSignedIn() injection in beforeEach. Without it the
 * Clerk mock (src/test/mocks/clerkMock.tsx) defaults to isSignedIn: false, so
 * useDaemonOnboarding's autoShow gate (which requires isSignedIn) never fires
 * and the onboarding modal never appears — every assertion timed out in CI.
 * The mock reads window.__CLERK_TEST_STATE__ injected via addInitScript, and
 * addInitScript persists across every navigation in the page's context, so a
 * single injection in beforeEach covers all the page.goto() calls below.
 *
 * Fix (#715): accountDataState is now a tri-state. The modal fires ONLY when
 * getHomeSummary resolves with 0 matches ('empty'). Every test that expects the
 * modal to appear must stub history/summary to return 0 matches.
 *
 * Fix (#895 D3 — v0.4.2 smoke regression): beforeEach must set
 * vaultmtg_collection_mode='enhanced' so gate 6 is satisfied. Without it,
 * readMode() defaults to 'manual' and the daemon modal never fires, causing
 * the two @smoke assertions below to time-out deterministically (not flake).
 */

/** Inject signed-in Clerk state before page load. addInitScript persists across navigations. */
async function setClerkSignedIn(page: Page): Promise<void> {
  await page.addInitScript((s) => {
    (window as unknown as Record<string, unknown>).__CLERK_TEST_STATE__ = s;
  }, { isSignedIn: true, firstName: 'Test', lastName: 'User' });
}

/** Stub history/summary to return 0 matches (new user / 'empty' state). */
async function stubSummaryNewUser(page: Page): Promise<void> {
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

test.describe('Daemon Onboarding Flow', () => {
  test.beforeEach(async ({ page }) => {
    // Inject signed-in Clerk state so useDaemonOnboarding's autoShow gate
    // (isSignedIn && accountDataState === 'empty' && daemonStatus === 'disconnected')
    // can fire. Without this the modal never appears (#2178).
    await setClerkSignedIn(page);

    // Clear localStorage so onboarding state is fresh for each test.
    // Also clear sessionStorage 'has-data' cache so tri-state starts at 'pending'.
    //
    // D3 precondition (#895): set collectionMode to 'enhanced' so gate 6 of
    // useDaemonOnboarding's autoShow is satisfied. Without this the daemon modal
    // never fires — manual-mode new users see ManualImportModal instead, which is
    // the correct shipped default. These tests exercise the enhanced-mode path.
    await page.addInitScript(() => {
      localStorage.removeItem('vaultmtg_onboarding_dismissed');
      localStorage.removeItem('vaultmtg_onboarding_completed');
      sessionStorage.removeItem('vaultmtg_has_account_data');
      localStorage.setItem('vaultmtg_collection_mode', 'enhanced');
    });
  });

  test('@smoke onboarding modal appears for new user with no daemon', async ({ page }) => {
    // Mock the daemon health endpoint to return disconnected
    await page.route('**/api/v1/health/daemon', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ status: 'disconnected' }),
      });
    });
    // Stub summary: new user (0 matches) so accountDataState resolves to 'empty'
    await stubSummaryNewUser(page);

    await page.goto('/');

    await expect(page.locator('[data-testid="app-container"]')).toBeVisible();

    // Onboarding modal should appear once the daemon health check returns disconnected
    // AND accountDataState resolves to 'empty'.
    await expect(page.locator('[data-testid="onboarding-modal"]')).toBeVisible();
  });

  test('@smoke step 1 shows download link to vaultmtg.app/download', async ({ page }) => {
    await page.route('**/api/v1/health/daemon', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ status: 'disconnected' }),
      });
    });
    await stubSummaryNewUser(page);

    await page.goto('/');

    await expect(page.locator('[data-testid="onboarding-modal"]')).toBeVisible();

    const downloadLink = page.locator('[data-testid="onboarding-download-link"]');
    await expect(downloadLink).toBeVisible();
    await expect(downloadLink).toHaveAttribute('href', 'https://vaultmtg.app/download');
  });

  test('step 1 to step 2 navigation works', async ({ page }) => {
    await page.route('**/api/v1/health/daemon', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ status: 'disconnected' }),
      });
    });
    await stubSummaryNewUser(page);

    await page.goto('/');

    await expect(page.locator('[data-testid="onboarding-modal"]')).toBeVisible();

    await page.locator('[data-testid="onboarding-step-1-next"]').click();
    await expect(page.locator('[data-testid="onboarding-step-2"]')).toBeVisible();
  });

  test('step 2 shows Mac and Windows install instructions', async ({ page }) => {
    await page.route('**/api/v1/health/daemon', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ status: 'disconnected' }),
      });
    });
    await stubSummaryNewUser(page);

    await page.goto('/');

    await expect(page.locator('[data-testid="onboarding-modal"]')).toBeVisible();

    await page.locator('[data-testid="onboarding-step-1-next"]').click();
    await expect(page.locator('[data-testid="onboarding-platform-mac"]')).toBeVisible();
    await expect(page.locator('[data-testid="onboarding-platform-windows"]')).toBeVisible();
  });

  test('step 2 to step 3 navigation works', async ({ page }) => {
    await page.route('**/api/v1/health/daemon', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ status: 'disconnected' }),
      });
    });
    await stubSummaryNewUser(page);

    await page.goto('/');

    await expect(page.locator('[data-testid="onboarding-modal"]')).toBeVisible();

    await page.locator('[data-testid="onboarding-step-1-next"]').click();
    await page.locator('[data-testid="onboarding-step-2-next"]').click();
    await expect(page.locator('[data-testid="onboarding-step-3"]')).toBeVisible();
  });

  test('step 3 shows spinner while waiting for daemon', async ({ page }) => {
    await page.route('**/api/v1/health/daemon', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ status: 'disconnected' }),
      });
    });
    await stubSummaryNewUser(page);

    await page.goto('/');

    await expect(page.locator('[data-testid="onboarding-modal"]')).toBeVisible();

    await page.locator('[data-testid="onboarding-step-1-next"]').click();
    await page.locator('[data-testid="onboarding-step-2-next"]').click();
    await expect(page.locator('[data-testid="onboarding-spinner"]')).toBeVisible();
  });

  test('step 3 shows success state when daemon connects', async ({ page }) => {
    // First return disconnected to trigger modal, then return connected
    let callCount = 0;
    await page.route('**/api/v1/health/daemon', async (route) => {
      callCount++;
      if (callCount <= 1) {
        // Initial nav check — disconnected so modal appears
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ status: 'disconnected' }),
        });
      } else {
        // Step 3 poll — daemon connected
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ status: 'connected' }),
        });
      }
    });
    await stubSummaryNewUser(page);

    await page.goto('/');

    await expect(page.locator('[data-testid="onboarding-modal"]')).toBeVisible();

    await page.locator('[data-testid="onboarding-step-1-next"]').click();
    await page.locator('[data-testid="onboarding-step-2-next"]').click();

    // Wait for the step 3 poll to succeed and show the success state
    await expect(page.locator('[data-testid="onboarding-success-heading"]')).toBeVisible();
  });

  test('dismiss button closes modal and does not re-show', async ({ page }) => {
    await page.route('**/api/v1/health/daemon', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ status: 'disconnected' }),
      });
    });
    await stubSummaryNewUser(page);

    await page.goto('/');

    await expect(page.locator('[data-testid="onboarding-modal"]')).toBeVisible();

    await page.locator('[data-testid="onboarding-modal-close"]').click();
    await expect(page.locator('[data-testid="onboarding-modal"]')).not.toBeVisible();

    // Verify localStorage was updated
    const dismissed = await page.evaluate(() =>
      localStorage.getItem('vaultmtg_onboarding_dismissed')
    );
    expect(dismissed).toBe('true');
  });

  test('clicking the disconnected daemon indicator re-opens onboarding', async ({ page }) => {
    await page.route('**/api/v1/health/daemon', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ status: 'disconnected' }),
      });
    });
    await stubSummaryNewUser(page);

    await page.goto('/');

    await expect(page.locator('[data-testid="onboarding-modal"]')).toBeVisible();

    // Dismiss
    await page.locator('[data-testid="onboarding-modal-close"]').click();
    await expect(page.locator('[data-testid="onboarding-modal"]')).not.toBeVisible();

    // Click the daemon health indicator to re-open
    await page.locator('[data-testid="daemon-health-indicator"]').click();
    await expect(page.locator('[data-testid="onboarding-modal"]')).toBeVisible();
  });
});

/**
 * D3 default path — ManualImportModal auto-fires for manual-mode new users.
 *
 * Per #895 D3: the DEFAULT for new users is collectionMode='manual'. In that
 * mode, the daemon OnboardingModal MUST NOT auto-show; instead ManualImportModal
 * auto-shows (controlled by useCollectionMode.isImportModalOpen).
 *
 * This test verifies the default path that the majority of new users encounter.
 * It is a @smoke gate — a regression here means new users get a blank first-run
 * experience (neither modal shows) or the wrong modal entirely.
 *
 * Preconditions: no vaultmtg_collection_mode key in localStorage (readMode()
 * defaults to 'manual'), no import_completed flag, daemon disconnected. The
 * onboarding-modal must NOT appear; manual-import-modal MUST appear.
 */
test.describe('D3 default path — manual-mode new user sees ManualImportModal', () => {
  test('@smoke manual-mode new user auto-sees ManualImportModal, not OnboardingModal', async ({ page }) => {
    // Inject signed-in Clerk state.
    await page.addInitScript((s) => {
      (window as unknown as Record<string, unknown>).__CLERK_TEST_STATE__ = s;
    }, { isSignedIn: true, firstName: 'Test', lastName: 'User' });

    // Ensure localStorage is clean — no collection mode key (defaults to 'manual'),
    // no import_completed flag, no previous onboarding state.
    await page.addInitScript(() => {
      localStorage.removeItem('vaultmtg_collection_mode');
      localStorage.removeItem('vaultmtg_import_completed');
      localStorage.removeItem('vaultmtg_onboarding_dismissed');
      localStorage.removeItem('vaultmtg_onboarding_completed');
      sessionStorage.removeItem('vaultmtg_has_account_data');
    });

    // Stub daemon as disconnected — ensures the daemon modal gate is not blocked
    // by daemon status, so we can confirm it stays hidden for a different reason.
    await page.route('**/api/v1/health/daemon', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ status: 'disconnected' }),
      });
    });

    // Stub summary: new user (0 matches) so accountDataState resolves to 'empty',
    // which is required for both modal auto-show conditions.
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

    await page.goto('/');
    await expect(page.locator('[data-testid="app-container"]')).toBeVisible();

    // ManualImportModal MUST auto-show for the default manual-mode new user.
    await expect(page.locator('[data-testid="manual-import-modal"]')).toBeVisible({ timeout: 15_000 });

    // Daemon OnboardingModal must NOT show — gate 6 (collectionMode === 'enhanced')
    // is not satisfied when collectionMode is 'manual' (the default).
    await expect(page.locator('[data-testid="onboarding-modal"]')).not.toBeVisible();
  });
});
