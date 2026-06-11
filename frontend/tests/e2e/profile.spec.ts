import { test, expect, type Page } from '@playwright/test';

/**
 * Profile Page E2E Tests (#2025)
 *
 * Covers the dedicated /profile route: content rendering (name, avatar
 * placeholder, email) and the display-name edit flow.
 *
 * Auth approach: same pattern as auth.spec.ts — inject
 * window.__CLERK_TEST_STATE__ via page.addInitScript() so the Clerk mock
 * (src/test/mocks/clerkMock.tsx) returns a controlled signed-in user without
 * needing a real Clerk session or network call.
 *
 * The mock's useUser() returns:
 *   { isLoaded: true, isSignedIn: true, user: { firstName, lastName, fullName,
 *     primaryEmailAddress: { emailAddress }, imageUrl: '', id, update, setProfileImage } }
 *
 * Note: Profile.tsx accepts a `useUserHook` prop for DI, but in E2E the real
 * component is mounted (no prop injection). Auth state flows through the Clerk
 * mock, which is identical to the pattern used by settings.spec.ts and
 * auth.spec.ts.
 */

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

type ClerkTestState = {
  isSignedIn: boolean;
  firstName?: string;
  lastName?: string;
  email?: string;
};

/** Inject signed-in state before page load. Must be called before page.goto(). */
async function setClerkSignedIn(page: Page, user?: Partial<ClerkTestState>): Promise<void> {
  const state: ClerkTestState = {
    isSignedIn: true,
    firstName: user?.firstName ?? 'Planeswalker',
    lastName: user?.lastName ?? 'Mock',
    email: user?.email ?? 'planeswalker@vaultmtg.test',
  };
  await page.addInitScript((s) => {
    (window as unknown as Record<string, unknown>).__CLERK_TEST_STATE__ = s;
  }, state);
}

/** Inject signed-out state before page load. Must be called before page.goto(). */
async function setClerkSignedOut(page: Page): Promise<void> {
  const state: ClerkTestState = { isSignedIn: false };
  await page.addInitScript((s) => {
    (window as unknown as Record<string, unknown>).__CLERK_TEST_STATE__ = s;
  }, state);
}

// ---------------------------------------------------------------------------
// Navigation and content rendering
// ---------------------------------------------------------------------------

test.describe('@smoke Profile page — navigation and content', () => {
  test('authenticated user navigating to /profile sees the profile page @smoke', async ({ page }) => {
    await setClerkSignedIn(page, { firstName: 'Planeswalker', lastName: 'Mock', email: 'planeswalker@vaultmtg.test' });

    await page.goto('/profile');
    await expect(page.locator('[data-testid="app-container"]')).toBeVisible();

    // The profile page container must render
    await expect(page.locator('[data-testid="profile-page"]')).toBeVisible();

    // Page title
    await expect(page.locator('[data-testid="profile-title"]')).toBeVisible();
    await expect(page.locator('[data-testid="profile-title"]')).toContainText('Profile');
  });

  test('profile page renders display name @smoke', async ({ page }) => {
    await setClerkSignedIn(page, { firstName: 'Planeswalker', lastName: 'Mock', email: 'planeswalker@vaultmtg.test' });

    await page.goto('/profile');
    await expect(page.locator('[data-testid="profile-page"]')).toBeVisible();

    // Display name section and value
    await expect(page.locator('[data-testid="profile-name-section"]')).toBeVisible();
    await expect(page.locator('[data-testid="profile-name-value"]')).toBeVisible();
    await expect(page.locator('[data-testid="profile-name-value"]')).toContainText('Planeswalker Mock');
  });

  test('profile page renders email address @smoke', async ({ page }) => {
    await setClerkSignedIn(page, { firstName: 'Planeswalker', lastName: 'Mock', email: 'planeswalker@vaultmtg.test' });

    await page.goto('/profile');
    await expect(page.locator('[data-testid="profile-page"]')).toBeVisible();

    // Email section and value
    await expect(page.locator('[data-testid="profile-email-section"]')).toBeVisible();
    await expect(page.locator('[data-testid="profile-email-value"]')).toBeVisible();
    await expect(page.locator('[data-testid="profile-email-value"]')).toHaveText('planeswalker@vaultmtg.test');
  });

  test('profile page renders avatar placeholder when no imageUrl is set @smoke', async ({ page }) => {
    await setClerkSignedIn(page, { firstName: 'Planeswalker', lastName: 'Mock' });

    await page.goto('/profile');
    await expect(page.locator('[data-testid="profile-page"]')).toBeVisible();

    // Avatar section is present
    await expect(page.locator('[data-testid="profile-avatar-section"]')).toBeVisible();

    // No imageUrl → VaultMTG initial-letter avatar
    await expect(page.locator('[data-testid="profile-avatar-vault-initial"]')).toBeVisible();
    await expect(page.locator('[data-testid="profile-avatar-vault-initial"]')).toContainText('P');
  });
});

// ---------------------------------------------------------------------------
// Unauthenticated access
// ---------------------------------------------------------------------------

test.describe('Profile page — unauthenticated', () => {
  test('unauthenticated user visiting /profile sees the ProtectedRoute sign-in prompt', async ({ page }) => {
    await setClerkSignedOut(page);

    await page.goto('/profile');
    await expect(page.locator('[data-testid="app-container"]')).toBeVisible();

    // ProtectedRoute must intercept and show the sign-in prompt
    await expect(page.locator('[data-testid="protected-route-prompt"]')).toBeVisible();

    // Profile page content must NOT render
    await expect(page.locator('[data-testid="profile-page"]')).not.toBeVisible();
  });
});

// ---------------------------------------------------------------------------
// Name-edit flow
// ---------------------------------------------------------------------------

test.describe('@smoke Profile page — name-edit flow', () => {
  test('Edit button opens the name-edit form with pre-filled first and last name inputs @smoke', async ({ page }) => {
    await setClerkSignedIn(page, { firstName: 'Planeswalker', lastName: 'Mock', email: 'planeswalker@vaultmtg.test' });

    await page.goto('/profile');
    await expect(page.locator('[data-testid="profile-page"]')).toBeVisible();

    // Click Edit in the Display Name section
    await page.click('[data-testid="profile-edit-name-button"]');

    // The name form must appear
    await expect(page.locator('[data-testid="profile-name-form"]')).toBeVisible();

    // Inputs are pre-filled with the current name
    await expect(page.locator('[data-testid="profile-first-name-input"]')).toHaveValue('Planeswalker');
    await expect(page.locator('[data-testid="profile-last-name-input"]')).toHaveValue('Mock');
  });

  test('Cancel button dismisses the name-edit form without changes @smoke', async ({ page }) => {
    await setClerkSignedIn(page, { firstName: 'Planeswalker', lastName: 'Mock', email: 'planeswalker@vaultmtg.test' });

    await page.goto('/profile');
    await expect(page.locator('[data-testid="profile-page"]')).toBeVisible();

    await page.click('[data-testid="profile-edit-name-button"]');
    await expect(page.locator('[data-testid="profile-name-form"]')).toBeVisible();

    // Cancel — form must disappear and display reverts
    await page.click('[data-testid="profile-cancel-name-button"]');
    await expect(page.locator('[data-testid="profile-name-form"]')).not.toBeVisible();
    await expect(page.locator('[data-testid="profile-name-display"]')).toBeVisible();
    await expect(page.locator('[data-testid="profile-name-value"]')).toContainText('Planeswalker Mock');
  });

  test('Save button submits the updated name and shows success feedback @smoke', async ({ page }) => {
    // The Clerk mock's useUser() returns an update() function that resolves immediately,
    // so the save flow runs through its happy path without a real API call.
    await setClerkSignedIn(page, { firstName: 'Planeswalker', lastName: 'Mock', email: 'planeswalker@vaultmtg.test' });

    await page.goto('/profile');
    await expect(page.locator('[data-testid="profile-page"]')).toBeVisible();

    // Open the edit form
    await page.click('[data-testid="profile-edit-name-button"]');
    await expect(page.locator('[data-testid="profile-name-form"]')).toBeVisible();

    // Clear and type a new first name
    await page.fill('[data-testid="profile-first-name-input"]', 'Teferi');
    await page.fill('[data-testid="profile-last-name-input"]', 'Hero');

    // Save
    await page.click('[data-testid="profile-save-name-button"]');

    // After save the form closes and the success message appears
    await expect(page.locator('[data-testid="profile-name-form"]')).not.toBeVisible();
    await expect(page.locator('[data-testid="profile-name-success"]')).toBeVisible();
    await expect(page.locator('[data-testid="profile-name-success"]')).toContainText('Display name updated successfully');
  });
});

// ---------------------------------------------------------------------------
// Back button
// ---------------------------------------------------------------------------

test.describe('Profile page — back button', () => {
  test('Back button is visible on the profile page', async ({ page }) => {
    await setClerkSignedIn(page);

    await page.goto('/profile');
    await expect(page.locator('[data-testid="profile-page"]')).toBeVisible();

    await expect(page.locator('[data-testid="profile-back-button"]')).toBeVisible();
  });
});

// ---------------------------------------------------------------------------
// AC4 — PostHog analytics disclosure — always-visible @smoke
// ---------------------------------------------------------------------------

test.describe('@smoke Profile page — PostHog analytics disclosure (AC4)', () => {
  test('disclosure text is visible on the profile page @smoke', async ({ page }) => {
    await setClerkSignedIn(page);

    await page.goto('/profile');
    await expect(page.locator('[data-testid="profile-page"]')).toBeVisible();

    const disclosure = page.locator('[data-testid="profile-posthog-disclosure"]');
    await expect(disclosure).toBeVisible();
    await expect(disclosure).toContainText('Historical analytics events cannot be retroactively corrected');
    await expect(disclosure).toContainText('no new events will be recorded with the old value after rectification');
  });

  test('disclosure is visible in idle state (not gated on edit mode) @smoke', async ({ page }) => {
    await setClerkSignedIn(page);

    await page.goto('/profile');
    await expect(page.locator('[data-testid="profile-page"]')).toBeVisible();

    // Not in edit mode — disclosure still present
    await expect(page.locator('[data-testid="profile-email-input"]')).not.toBeVisible();
    await expect(page.locator('[data-testid="profile-posthog-disclosure"]')).toBeVisible();
  });
});

// ---------------------------------------------------------------------------
// AC6 — Email-change E2E flow @smoke
// ---------------------------------------------------------------------------

test.describe('@smoke Profile page — email-change flow (AC6 — #888)', () => {
  test('Edit Email button is visible in idle state @smoke', async ({ page }) => {
    await setClerkSignedIn(page, { firstName: 'Planeswalker', lastName: 'Mock', email: 'planeswalker@vaultmtg.test' });

    await page.goto('/profile');
    await expect(page.locator('[data-testid="profile-page"]')).toBeVisible();

    await expect(page.locator('[data-testid="profile-edit-email-button"]')).toBeVisible();
    // Old read-only note must NOT be present
    await expect(page.locator('text=Email is managed by your Clerk account')).not.toBeVisible();
  });

  test('clicking Edit Email shows the new-email input @smoke', async ({ page }) => {
    await setClerkSignedIn(page, { firstName: 'Planeswalker', lastName: 'Mock', email: 'planeswalker@vaultmtg.test' });

    await page.goto('/profile');
    await expect(page.locator('[data-testid="profile-page"]')).toBeVisible();

    await page.click('[data-testid="profile-edit-email-button"]');

    await expect(page.locator('[data-testid="profile-email-input"]')).toBeVisible();
    await expect(page.locator('[data-testid="profile-email-submit-button"]')).toBeVisible();
    await expect(page.locator('[data-testid="profile-email-cancel-button"]')).toBeVisible();
  });

  test('Cancel in pending state returns to idle @smoke', async ({ page }) => {
    await setClerkSignedIn(page, { firstName: 'Planeswalker', lastName: 'Mock', email: 'planeswalker@vaultmtg.test' });

    await page.goto('/profile');
    await expect(page.locator('[data-testid="profile-page"]')).toBeVisible();

    await page.click('[data-testid="profile-edit-email-button"]');
    await expect(page.locator('[data-testid="profile-email-input"]')).toBeVisible();

    await page.click('[data-testid="profile-email-cancel-button"]');
    await expect(page.locator('[data-testid="profile-email-input"]')).not.toBeVisible();
    await expect(page.locator('[data-testid="profile-edit-email-button"]')).toBeVisible();
  });

  test('entering a new email and submitting advances to verification step @smoke', async ({ page }) => {
    // The Clerk mock's createEmailAddress() + prepareVerification() resolve
    // immediately so the flow advances without a real email being sent.
    await setClerkSignedIn(page, { firstName: 'Planeswalker', lastName: 'Mock', email: 'planeswalker@vaultmtg.test' });

    await page.goto('/profile');
    await expect(page.locator('[data-testid="profile-page"]')).toBeVisible();

    await page.click('[data-testid="profile-edit-email-button"]');
    await page.fill('[data-testid="profile-email-input"]', 'newemail@e2e.test');
    await page.click('[data-testid="profile-email-submit-button"]');

    // Advances to the OTP code step
    await expect(page.locator('[data-testid="profile-email-code-input"]')).toBeVisible();
    await expect(page.locator('[data-testid="profile-email-verify-button"]')).toBeVisible();
  });

  test('Cancel in verifying state returns to idle @smoke', async ({ page }) => {
    await setClerkSignedIn(page, { firstName: 'Planeswalker', lastName: 'Mock', email: 'planeswalker@vaultmtg.test' });

    await page.goto('/profile');
    await expect(page.locator('[data-testid="profile-page"]')).toBeVisible();

    await page.click('[data-testid="profile-edit-email-button"]');
    await page.fill('[data-testid="profile-email-input"]', 'newemail@e2e.test');
    await page.click('[data-testid="profile-email-submit-button"]');
    await expect(page.locator('[data-testid="profile-email-code-input"]')).toBeVisible();

    await page.click('[data-testid="profile-email-cancel-button"]');
    await expect(page.locator('[data-testid="profile-email-code-input"]')).not.toBeVisible();
    await expect(page.locator('[data-testid="profile-edit-email-button"]')).toBeVisible();
  });

  test('full email-change flow: enter email → enter code → success @smoke', async ({ page }) => {
    // The Clerk mock's attemptVerification() resolves immediately, so the full
    // happy path runs without a real Clerk session or OTP.
    await setClerkSignedIn(page, { firstName: 'Planeswalker', lastName: 'Mock', email: 'planeswalker@vaultmtg.test' });

    // Intercept the BFF PATCH call so it doesn't fail in the dev server (no real BFF)
    await page.route('**/api/v1/account/profile', (route) => route.fulfill({
      status: 204,
      body: '',
    }));

    await page.goto('/profile');
    await expect(page.locator('[data-testid="profile-page"]')).toBeVisible();

    // Step 1: open edit form
    await page.click('[data-testid="profile-edit-email-button"]');
    await page.fill('[data-testid="profile-email-input"]', 'newemail@e2e.test');
    await page.click('[data-testid="profile-email-submit-button"]');

    // Step 2: verification code input appears
    await expect(page.locator('[data-testid="profile-email-code-input"]')).toBeVisible();
    await page.fill('[data-testid="profile-email-code-input"]', '123456');
    await page.click('[data-testid="profile-email-verify-button"]');

    // Step 3: success and return to idle
    await expect(page.locator('[data-testid="profile-email-success"]')).toBeVisible();
    await expect(page.locator('[data-testid="profile-email-code-input"]')).not.toBeVisible();
    await expect(page.locator('[data-testid="profile-edit-email-button"]')).toBeVisible();
  });
});

// ---------------------------------------------------------------------------
// BFF PATCH error handling — tickets#1183 (AC2 — local mocked BFF)
//
// The email-change flow ends with a non-blocking BFF sync call:
//   patchAccountProfile({ email }).catch(() => undefined)
//
// Per AC5 in PR #3100, Clerk is authoritative — BFF failures are silently
// swallowed and the success banner still displays. These tests verify that
// contract in E2E via Playwright route interception: even when the BFF returns
// 4xx or 5xx, the user sees the success state and no error message is surfaced.
//
// The complete 3-step Clerk mock flow (createEmailAddress → prepareVerification
// → attemptVerification → user.update) runs through the Clerk test mock which
// resolves immediately; only the final non-blocking BFF sync call is intercepted.
// ---------------------------------------------------------------------------

test.describe('@smoke Profile page — email-change BFF PATCH error handling (#1183)', () => {
  /**
   * Drives the component through the full 3-step Clerk mock email-change flow:
   *   pending → verifying → complete (OTP submit).
   * The BFF PATCH route must be set up by the caller BEFORE page.goto().
   */
  async function runFullEmailChangeFlow(page: Page): Promise<void> {
    await page.click('[data-testid="profile-edit-email-button"]');
    await page.fill('[data-testid="profile-email-input"]', 'newemail@e2e.test');
    await page.click('[data-testid="profile-email-submit-button"]');
    await expect(page.locator('[data-testid="profile-email-code-input"]')).toBeVisible();
    await page.fill('[data-testid="profile-email-code-input"]', '123456');
    await page.click('[data-testid="profile-email-verify-button"]');
  }

  test('BFF PATCH 422 does NOT surface an error — success banner shown (non-blocking AC5) @smoke', async ({ page }) => {
    // AC2 (tickets#1183) — BFF returns validation/conflict error; Clerk succeeded,
    // so the component swallows the BFF failure and shows success to the user.
    await setClerkSignedIn(page, {
      firstName: 'Planeswalker', lastName: 'Mock', email: 'planeswalker@vaultmtg.test',
    });

    // Simulate BFF returning 422 (e.g. email_taken or validation failure)
    await page.route('**/api/v1/account/profile', (route) => route.fulfill({
      status: 422,
      contentType: 'application/json',
      body: JSON.stringify({ error: 'email_taken' }),
    }));

    await page.goto('/profile');
    await expect(page.locator('[data-testid="profile-page"]')).toBeVisible();
    await runFullEmailChangeFlow(page);

    // BFF error must be swallowed — success banner visible (Clerk is authoritative)
    await expect(page.locator('[data-testid="profile-email-success"]')).toBeVisible();
    // No error alert rendered to the user
    await expect(page.locator('[data-testid="profile-email-error"]')).not.toBeVisible();
    // Flow returned to idle
    await expect(page.locator('[data-testid="profile-edit-email-button"]')).toBeVisible();
  });

  test('BFF PATCH 500 does NOT surface an error — success banner shown (non-blocking AC5) @smoke', async ({ page }) => {
    // AC2 (tickets#1183) — BFF is offline / internal error; the non-blocking
    // catch swallows this too. User experience is unchanged from the happy path.
    await setClerkSignedIn(page, {
      firstName: 'Planeswalker', lastName: 'Mock', email: 'planeswalker@vaultmtg.test',
    });

    // Simulate BFF returning 500 (e.g. BFF service unavailable)
    await page.route('**/api/v1/account/profile', (route) => route.fulfill({
      status: 500,
      contentType: 'application/json',
      body: JSON.stringify({ error: 'internal_server_error' }),
    }));

    await page.goto('/profile');
    await expect(page.locator('[data-testid="profile-page"]')).toBeVisible();
    await runFullEmailChangeFlow(page);

    // BFF 500 silently swallowed — success still shown
    await expect(page.locator('[data-testid="profile-email-success"]')).toBeVisible();
    await expect(page.locator('[data-testid="profile-email-error"]')).not.toBeVisible();
    await expect(page.locator('[data-testid="profile-edit-email-button"]')).toBeVisible();
  });

  test('BFF PATCH is NOT called when user cancels before completing the Clerk flow @smoke', async ({ page }) => {
    // Regression guard: cancelling in the pending step must not trigger the BFF
    // sync at all — the route handler records whether it was hit.
    await setClerkSignedIn(page, {
      firstName: 'Planeswalker', lastName: 'Mock', email: 'planeswalker@vaultmtg.test',
    });

    let bffPatchHit = false;
    await page.route('**/api/v1/account/profile', (route) => {
      bffPatchHit = true;
      return route.fulfill({ status: 204, body: '' });
    });

    await page.goto('/profile');
    await expect(page.locator('[data-testid="profile-page"]')).toBeVisible();

    // Open edit form, fill email, then cancel — never reach the verification step
    await page.click('[data-testid="profile-edit-email-button"]');
    await page.fill('[data-testid="profile-email-input"]', 'newemail@e2e.test');
    await page.click('[data-testid="profile-email-cancel-button"]');

    // Flow cancelled — returns to idle, BFF not called
    await expect(page.locator('[data-testid="profile-edit-email-button"]')).toBeVisible();
    await expect(page.locator('[data-testid="profile-email-input"]')).not.toBeVisible();
    expect(bffPatchHit).toBe(false);
  });
});
