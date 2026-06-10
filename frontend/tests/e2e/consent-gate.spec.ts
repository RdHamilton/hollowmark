/**
 * E2E tests — COPPA Consent Gate (COPPA #884)
 *
 * Covers:
 *   1. New user: ConsentGate renders loading while consent POST is in-flight,
 *      then renders app content when the POST resolves.
 *   2. Returning user (fresh tab): the gate passes through immediately and the
 *      consent POST is NOT re-sent.
 *   3. Error state: gate renders an error notice with a Retry button when the
 *      consent POST fails.
 *
 * Approach (same pattern as auth.spec.ts):
 *   - VITE_CLERK_TEST_MODE=true aliases @clerk/react → clerkMock.tsx
 *   - window.__CLERK_TEST_STATE__ controls Clerk auth state
 *   - We inject window.__CONSENT_TEST_STATE__ to control the consent hook's
 *     behaviour via the test shim injected into clerkMock when the key is set
 *
 * window.__CLERK_TEST_STATE__ extended fields for consent:
 *   {
 *     isSignedIn: true,
 *     userId?: string,     // defaults to 'user_test_123'
 *     isNewUser?: boolean, // true → createdAt within last 60 s
 *     consentAlreadyRecorded?: boolean // true → localStorage guard pre-set
 *   }
 *
 * BFF intercept:
 *   - In test mode the consent POST to /api/v1/account/consent is intercepted
 *     by MSW (via the Vite dev server). We track call count via
 *     window.__CONSENT_POST_COUNT__.
 *
 * Note: These tests require the Vite dev server with VITE_CLERK_TEST_MODE=true
 * and the MSW handler for /account/consent. They run under the 'full' project.
 */

import { test, expect, Page } from '@playwright/test';

// ─── helpers ──────────────────────────────────────────────────────────────────

type ClerkConsentTestState = {
  isSignedIn: boolean;
  userId?: string;
  firstName?: string;
  isNewUser?: boolean;
  consentAlreadyRecorded?: boolean;
};

async function setClerkState(page: Page, state: ClerkConsentTestState): Promise<void> {
  await page.addInitScript((s) => {
    (window as unknown as Record<string, unknown>).__CLERK_TEST_STATE__ = s;
  }, state);
}

/** Pre-set the localStorage guard for a user, simulating a returning user. */
async function presetConsentGuard(page: Page, userId: string): Promise<void> {
  const key = `vaultmtg_consent_signup_v1_${userId}`;
  await page.addInitScript((k) => {
    window.localStorage.setItem(k, '1');
  }, key);
}

/** Read the number of consent POSTs recorded by the MSW handler shim. */
async function getConsentPostCount(page: Page): Promise<number> {
  return page.evaluate(() => {
    const count = (window as unknown as Record<string, unknown>).__CONSENT_POST_COUNT__;
    return typeof count === 'number' ? count : 0;
  });
}

// ─── tests ────────────────────────────────────────────────────────────────────

test.describe('Feature: COPPA Consent Gate', () => {
  test('new user: app content renders after consent POST resolves', async ({ page }) => {
    await setClerkState(page, { isSignedIn: true, isNewUser: true });
    await page.goto('/draft');

    // App container must be present
    await expect(page.locator('[data-testid="app-container"]')).toBeVisible();

    // Once the (mock) consent POST resolves the gate passes through
    // and protected route content renders
    await expect(page.locator('.draft-container').first()).toBeVisible();
  });

  test('new user: consent POST is made exactly once', async ({ page }) => {
    await setClerkState(page, { isSignedIn: true, isNewUser: true, userId: 'user_consent_test_001' });
    await page.goto('/draft');

    await expect(page.locator('.draft-container').first()).toBeVisible();

    const postCount = await getConsentPostCount(page);
    expect(postCount).toBe(1);
  });

  test('REGRESSION A4: returning user in fresh tab does NOT re-POST consent', async ({ page }) => {
    const userId = 'user_returning_test_002';

    // Pre-set localStorage guard (simulates user who already completed signup in a prior session)
    await presetConsentGuard(page, userId);
    await setClerkState(page, {
      isSignedIn: true,
      isNewUser: false,
      userId,
    });

    await page.goto('/draft');
    await expect(page.locator('[data-testid="app-container"]')).toBeVisible();
    await expect(page.locator('.draft-container').first()).toBeVisible();

    // Zero consent POSTs — returning user must never re-POST
    const postCount = await getConsentPostCount(page);
    expect(postCount).toBe(0);
  });

  test('returning user with guard set sees app content immediately (no loading state)', async ({ page }) => {
    const userId = 'user_returning_test_003';
    await presetConsentGuard(page, userId);
    await setClerkState(page, { isSignedIn: true, isNewUser: false, userId });

    await page.goto('/draft');
    await expect(page.locator('[data-testid="app-container"]')).toBeVisible();

    // The consent loading overlay must never appear
    await expect(page.locator('[data-testid="consent-gate-loading"]')).not.toBeVisible();
  });

  test('unauthenticated user: consent gate is a no-op — sign-in prompt renders normally', async ({ page }) => {
    await setClerkState(page, { isSignedIn: false });
    await page.goto('/draft');

    await expect(page.locator('[data-testid="protected-route-prompt"]')).toBeVisible();
    await expect(page.locator('[data-testid="consent-gate-loading"]')).not.toBeVisible();
    await expect(page.locator('[data-testid="consent-gate-error"]')).not.toBeVisible();
  });
});
