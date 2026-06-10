/**
 * Account Deletion E2E (#887 GDPR Right to Erasure)
 *
 * Covers the full Settings → Danger Zone → "Delete my account" flow:
 *   - Happy path: modal opens with required copy, confirm triggers DELETE +
 *     status polling → terminal-success message
 *   - Cancel flow: modal opens, cancel closes it
 *   - DELETE transport error (500): terminal-error message visible
 *   - Status GET transport error mid-poll (503): terminal-error message visible
 *   - Modal ARIA: dialog role present, backdrop click does NOT close
 *   - Copy assertions: legally required retention wording present
 *
 * BFF endpoints are mocked via page.route() so no live BFF is needed.
 * Auth is injected via window.__CLERK_TEST_STATE__ (same pattern as other
 * settings specs).
 *
 * Poll-cap-timeout is tested at the Vitest component level (vi.useFakeTimers)
 * rather than E2E because driving 10 real minutes in a browser test is not
 * practical; the behaviour is fully covered by DangerZoneSection.test.tsx.
 */

import { test, expect, type Page } from '@playwright/test';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

type ClerkTestState = {
  isSignedIn: boolean;
  firstName?: string;
  lastName?: string;
  email?: string;
};

async function setClerkSignedIn(page: Page, user?: Partial<ClerkTestState>): Promise<void> {
  const state: ClerkTestState = {
    isSignedIn: true,
    firstName: user?.firstName ?? 'Test',
    lastName: user?.lastName ?? 'User',
    email: user?.email ?? 'test@example.com',
  };
  await page.addInitScript((s) => {
    (window as unknown as Record<string, unknown>).__CLERK_TEST_STATE__ = s;
  }, state);
}

/** Mock GET /api/v1/settings so the Settings page renders without error. */
async function mockSettingsEndpoint(page: Page): Promise<void> {
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
}

/**
 * Mock the daemon status endpoint to look "connected" — needed so the
 * DangerZoneSection renders.
 */
async function mockDaemonConnected(page: Page): Promise<void> {
  await page.route('**/127.0.0.1:9001/api/v1/system/status', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        data: {
          status: 'connected',
          connected: true,
          mode: 'live',
          url: 'http://localhost:8080',
          port: 9001,
        },
      }),
    });
  });
}

interface MockDeleteOpts {
  status: number;
  jobId?: string;
}

/** Mock DELETE /api/v1/account */
async function mockDeleteAccount(page: Page, opts: MockDeleteOpts): Promise<void> {
  await page.route('**/api/v1/account', async (route) => {
    if (route.request().method() !== 'DELETE') {
      await route.continue();
      return;
    }
    if (opts.status >= 400) {
      await route.fulfill({
        status: opts.status,
        contentType: 'application/json',
        body: JSON.stringify({ error: 'Server error', message: 'Internal server error' }),
      });
    } else {
      await route.fulfill({
        status: 202,
        contentType: 'application/json',
        body: JSON.stringify({
          data: { job_id: opts.jobId ?? 'test-job-123', message: 'Deletion scheduled.' },
        }),
      });
    }
  });
}

interface StatusSequence {
  responses: Array<{ status: number; jobStatus?: 'pending' | 'completed' }>;
}

/** Mock GET /api/v1/account/deletion-status/* with a response sequence. */
async function mockDeletionStatus(page: Page, seq: StatusSequence): Promise<void> {
  let callIndex = 0;
  await page.route('**/api/v1/account/deletion-status/**', async (route) => {
    const resp = seq.responses[callIndex] ?? seq.responses[seq.responses.length - 1];
    callIndex++;
    if (resp.status >= 400) {
      await route.fulfill({
        status: resp.status,
        contentType: 'application/json',
        body: JSON.stringify({ error: 'Service unavailable' }),
      });
    } else {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          data: {
            job_id: 'test-job-123',
            status: resp.jobStatus ?? 'pending',
            requested_at: '2026-06-10T00:00:00Z',
          },
        }),
      });
    }
  });
}

/** Navigate to /settings and expand the Danger Zone accordion. */
async function openDangerZone(page: Page): Promise<void> {
  await mockSettingsEndpoint(page);
  await mockDaemonConnected(page);
  await page.goto('/settings');
  await expect(page.locator('[data-testid="app-container"]')).toBeVisible();
  await page.waitForURL('**/settings');
  const dangerZoneHeader = page.locator('button').filter({ hasText: /danger zone/i });
  await expect(dangerZoneHeader).toBeVisible();
  await dangerZoneHeader.click();
  // Wait for the section to be visible
  await expect(page.locator('[data-testid="danger-zone-section"]')).toBeVisible();
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

test.describe('Account Deletion — Danger Zone', () => {
  test.beforeEach(async ({ page }) => {
    await setClerkSignedIn(page);
  });

  // ---- Happy path ----

  test('happy path: modal shows required copy, confirm triggers deletion, terminal-success appears', async ({
    page,
  }) => {
    await mockDeleteAccount(page, { status: 202, jobId: 'job-happy' });
    await mockDeletionStatus(page, {
      responses: [
        { status: 200, jobStatus: 'pending' },
        { status: 200, jobStatus: 'completed' },
      ],
    });
    await openDangerZone(page);

    // Click "Delete my account" to open the modal
    await page.getByTestId('danger-zone-delete-account-button').click();

    // Modal is visible with dialog role
    const dialog = page.getByRole('dialog');
    await expect(dialog).toBeVisible();

    // AC3 copy assertions — legally required
    await expect(dialog).toContainText(/account and login credentials/i);
    await expect(dialog).toContainText(/gameplay history/i);
    await expect(dialog).toContainText(/analytics data/i);
    await expect(dialog).toContainText(/match outcomes, draft picks, play patterns/i);
    await expect(dialog).toContainText(/cannot be linked back to you/i);
    await expect(dialog).toContainText(/permanent and cannot be undone/i);

    // Confirm inside the modal
    await dialog.getByRole('button', { name: /Delete my account/i }).click();

    // Terminal-success message should eventually appear
    await expect(page.getByTestId('account-deletion-success')).toBeVisible({ timeout: 15000 });
  });

  // ---- Cancel flow ----

  test('cancel flow: modal opens, cancel closes it, entry button is visible again', async ({
    page,
  }) => {
    await openDangerZone(page);

    await page.getByTestId('danger-zone-delete-account-button').click();
    const dialog = page.getByRole('dialog');
    await expect(dialog).toBeVisible();

    await dialog.getByRole('button', { name: /Cancel/i }).click();

    // Dialog is gone
    await expect(page.getByRole('dialog')).toHaveCount(0);
    // Entry button is back
    await expect(page.getByTestId('danger-zone-delete-account-button')).toBeVisible();
  });

  // ---- DELETE transport error ----

  test('DELETE transport error: 500 response shows terminal-error message', async ({ page }) => {
    await mockDeleteAccount(page, { status: 500 });
    await openDangerZone(page);

    await page.getByTestId('danger-zone-delete-account-button').click();
    const dialog = page.getByRole('dialog');
    await expect(dialog).toBeVisible();

    await dialog.getByRole('button', { name: /Delete my account/i }).click();

    await expect(page.getByTestId('account-deletion-error')).toBeVisible({ timeout: 10000 });
  });

  // ---- Status GET transport error mid-poll ----

  test('status GET transport error mid-poll: terminal-error message appears', async ({ page }) => {
    await mockDeleteAccount(page, { status: 202, jobId: 'job-poll-error' });
    await mockDeletionStatus(page, {
      responses: [
        { status: 200, jobStatus: 'pending' },
        { status: 503 },
      ],
    });
    await openDangerZone(page);

    await page.getByTestId('danger-zone-delete-account-button').click();
    const dialog = page.getByRole('dialog');
    await expect(dialog).toBeVisible();

    await dialog.getByRole('button', { name: /Delete my account/i }).click();

    // After the 503, terminal-error should show
    await expect(page.getByTestId('account-deletion-error')).toBeVisible({ timeout: 15000 });
  });

  // ---- Modal ARIA ----

  test('modal ARIA: dialog role present, backdrop click does not close', async ({ page }) => {
    await openDangerZone(page);
    await page.getByTestId('danger-zone-delete-account-button').click();

    const dialog = page.getByRole('dialog');
    await expect(dialog).toBeVisible();
    await expect(dialog).toHaveAttribute('aria-modal', 'true');

    // Click the overlay backdrop (outside the modal box)
    // Account deletion is too consequential for accidental dismiss
    await page.locator('.account-deletion-modal-overlay').click({ position: { x: 5, y: 5 } });

    // Dialog must still be visible — backdrop click must not close it
    await expect(dialog).toBeVisible();
  });

  // ---- Copy assertions (standalone) ----

  test('modal copy: retained-data paragraph contains legally required text', async ({ page }) => {
    await openDangerZone(page);
    await page.getByTestId('danger-zone-delete-account-button').click();

    const dialog = page.getByRole('dialog');
    await expect(dialog).toBeVisible();

    await expect(dialog.getByText(/match outcomes, draft picks, play patterns/i)).toBeVisible();
    await expect(dialog.getByText(/cannot be linked back to you/i)).toBeVisible();
  });
});
