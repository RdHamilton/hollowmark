/**
 * Playwright E2E spec — ADR-077 config.json boot failure screens.
 *
 * Tests the three error branches and retry affordance that the boot sequence
 * exposes when /config.json is unavailable, malformed, or has missing/invalid fields.
 *
 * Approach:
 * - Route intercepts override the /config.json response for each scenario.
 * - All tests assert on data-testid selectors from Tim's spec §7.
 * - The beacon (sendBeacon) is not asserted in E2E — AC11 is covered by
 *   unit tests in runtimeConfig.test.ts and the collection.integration.test.ts
 *   mapErrorToBranches seam test.
 */

import { test, expect, type Page } from '@playwright/test';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/** Intercept /config.json to return a network error (fetch throws). */
async function routeConfigNetworkFailure(page: Page): Promise<void> {
  await page.route('**/config.json', (route) => route.abort('failed'));
}

/** Intercept /config.json to return HTTP 200 with HTML body (CloudFront trap). */
async function routeConfigHtmlBody(page: Page): Promise<void> {
  await page.route('**/config.json', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'text/html',
      body: '<!doctype html><html><head><title>VaultMTG</title></head><body></body></html>',
    }),
  );
}

/** Intercept /config.json to return valid JSON but missing required fields. */
async function routeConfigMissingFields(page: Page): Promise<void> {
  await page.route('**/config.json', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ bffUrl: 'http://localhost:8080/api/v1' }), // missing clerkPublishableKey etc.
    }),
  );
}

/** Intercept /config.json to return valid JSON with an invalid clerkPublishableKey format. */
async function routeConfigInvalidClerkKey(page: Page): Promise<void> {
  await page.route('**/config.json', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        clerkPublishableKey: 'not-a-valid-key',  // fails /^pk_(live|test)_[A-Za-z0-9]+$/
        bffUrl: 'http://localhost:8080/api/v1',
        sentryEnv: 'test',
        envLabel: 'test',
        daemonUrl: 'http://localhost:9001/api/v1',
        posthogHost: 'https://app.posthog.com',
      }),
    }),
  );
}

/** Intercept /config.json to return HTTP 500. */
async function routeConfigNon2xx(page: Page): Promise<void> {
  await page.route('**/config.json', (route) =>
    route.fulfill({
      status: 500,
      contentType: 'application/json',
      body: JSON.stringify({ error: 'Internal Server Error' }),
    }),
  );
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

test.describe('config.json boot failure — ADR-077', () => {
  // ── Branch A: network failure ──────────────────────────────────────────────

  test('Branch A: shows network error screen when /config.json fetch throws', async ({ page }) => {
    await routeConfigNetworkFailure(page);
    await page.goto('/');

    await expect(page.getByTestId('config-error-screen')).toBeVisible();
    await expect(page.getByTestId('config-error-screen-headline')).toContainText(
      'Could not reach VaultMTG',
    );
    await expect(page.getByTestId('config-error-screen-body')).toContainText(
      'Check your network connection',
    );
  });

  test('Branch A: shows retry button on network error screen', async ({ page }) => {
    await routeConfigNetworkFailure(page);
    await page.goto('/');

    await expect(page.getByTestId('config-error-screen-retry')).toBeVisible();
    await expect(page.getByTestId('config-error-screen-retry')).toContainText('Try Again');
  });

  test('Branch A: non-2xx HTTP response shows network error screen (not parse error)', async ({
    page,
  }) => {
    await routeConfigNon2xx(page);
    await page.goto('/');

    await expect(page.getByTestId('config-error-screen')).toBeVisible();
    await expect(page.getByTestId('config-error-screen-headline')).toContainText(
      'Could not reach VaultMTG',
    );
  });

  // ── Branch B: parse failure (CloudFront 200+HTML trap) ────────────────────

  test('Branch B: shows parse error screen when /config.json returns HTTP 200 with HTML body', async ({
    page,
  }) => {
    await routeConfigHtmlBody(page);
    await page.goto('/');

    await expect(page.getByTestId('config-error-screen')).toBeVisible();
    await expect(page.getByTestId('config-error-screen-headline')).toContainText(
      'VaultMTG has a setup problem',
    );
    await expect(page.getByTestId('config-error-screen-body')).toContainText(
      'This is not your fault',
    );
  });

  test('Branch B: parse error screen does NOT show retry button', async ({ page }) => {
    await routeConfigHtmlBody(page);
    await page.goto('/');

    await expect(page.getByTestId('config-error-screen-retry')).not.toBeVisible();
  });

  // ── Branch C: missing fields (including AC7 format-invalid clerkPublishableKey) ──

  test('Branch C: shows missing-fields error when required fields are absent', async ({ page }) => {
    await routeConfigMissingFields(page);
    await page.goto('/');

    await expect(page.getByTestId('config-error-screen')).toBeVisible();
    await expect(page.getByTestId('config-error-screen-headline')).toContainText(
      'VaultMTG has a setup problem',
    );
  });

  test('Branch C (AC7): shows missing-fields error when clerkPublishableKey has invalid format', async ({
    page,
  }) => {
    await routeConfigInvalidClerkKey(page);
    await page.goto('/');

    // An invalid key format is treated as ConfigMissingFieldsError → branch C.
    await expect(page.getByTestId('config-error-screen')).toBeVisible();
    await expect(page.getByTestId('config-error-screen-headline')).toContainText(
      'VaultMTG has a setup problem',
    );
  });
});
