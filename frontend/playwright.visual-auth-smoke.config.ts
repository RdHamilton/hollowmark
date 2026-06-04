import { defineConfig, devices } from '@playwright/test';

/**
 * Playwright configuration for visual authenticated screenshot capture.
 *
 * Targets the live staging SPA at stg-app.vaultmtg.app using a real browser.
 * Establishes a genuine Clerk session via Backend API sign-in-token + FAPI —
 * the only approach that works on production-type (pk_live_*) Clerk instances.
 *
 * Run via:
 *   CLERK_SECRET_KEY=<sk_live_*> SCREENSHOT_DIR=<dir> \
 *     npx playwright test --config=playwright.visual-auth-smoke.config.ts
 *
 * Required environment variables:
 *   CLERK_SECRET_KEY   — Clerk Backend API secret for staging (sk_live_*)
 *   SCREENSHOT_DIR     — Directory to save screenshots
 *
 * Optional:
 *   STAGING_SPA_URL    — Override staging SPA base URL
 *   CI_SMOKE_USER_ID   — Override ci-smoke Clerk user ID
 */
export default defineConfig({
  testDir: './tests/e2e/staging',
  testMatch: /visual-auth-smoke\.spec\.ts/,

  // Long timeout — each surface waits 10 s for data fetch + screenshot
  timeout: 120 * 1000,

  // Sequential — one browser context per surface, against shared staging env
  fullyParallel: false,
  workers: 1,

  // No retries — flakiness should surface, not be masked
  retries: 0,

  forbidOnly: !!process.env.CI,

  reporter: [
    ['html', { open: 'never', outputFolder: 'playwright-report-visual-auth-smoke' }],
    ['list'],
    ...(process.env.CI ? [['github'] as const] : []),
  ],

  use: {
    baseURL: process.env.STAGING_SPA_URL || 'https://stg-app.vaultmtg.app',
    trace: 'on-first-retry',
    screenshot: 'only-on-failure',
  },

  projects: [
    {
      name: 'visual-auth-smoke',
      use: {
        ...devices['Desktop Chrome'],
        viewport: { width: 1280, height: 800 },
      },
    },
  ],
});
