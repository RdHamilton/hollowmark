import { defineConfig, devices } from '@playwright/test';

/**
 * Playwright configuration for authenticated visual-capture runs (#1080).
 *
 * Used by:
 *   - wildcard-panel-visual-capture.yml  (wildcard-panel-visual-424.spec.ts)
 *   - prof-visual-capture.yml            (prof-visual-capture.spec.ts)
 *
 * Why a separate config from playwright.staging-spa.config.ts:
 *   PR #3022 narrowed playwright.staging-spa.config.ts to
 *   testMatch: /staging-spa-smoke\.spec\.ts/ so that the SPA smoke workflow
 *   does not accidentally pick up screenshot specs that require SCREENSHOT_DIR.
 *   Playwright INTERSECTS an explicit file argument with testMatch — it does NOT
 *   replace it — so passing e.g. wildcard-panel-visual-424.spec.ts to the smoke
 *   config produced "No tests found" (#1080).
 *
 *   This config deliberately omits testMatch so Playwright's default glob
 *   (**\/*.spec.ts) applies. The two capture workflows pass their spec file
 *   explicitly via the CLI, so the default glob never over-selects; the smoke
 *   config remains narrowed and unaffected.
 *
 * Run via:
 *   npx playwright test tests/e2e/staging/wildcard-panel-visual-424.spec.ts \
 *     --config=playwright.visual-capture.config.ts
 *   npx playwright test tests/e2e/staging/prof-visual-capture.spec.ts \
 *     --config=playwright.visual-capture.config.ts
 *
 * Required environment variables (same as staging-spa config):
 *   STAGING_SPA_URL   — override the staging SPA base URL (optional)
 *   CLERK_SECRET_KEY  — Clerk secret key for generating testing tokens (required)
 *   SCREENSHOT_DIR    — absolute path for writing PNGs (required by both specs)
 */
export default defineConfig({
  testDir: './tests/e2e/staging',

  // No testMatch — Playwright's default (**/*.spec.ts) applies. The explicit
  // file argument passed by each workflow is the real selector; we must not
  // narrow this further or the intersection produces "No tests found".

  // Individual test timeout — 60 s to handle CI runner latency (#1949)
  timeout: 60 * 1000,

  // Sequential — one worker against shared staging environment
  fullyParallel: false,
  workers: 1,

  // No retries — a flaky staging env should surface as a real failure
  retries: 0,

  forbidOnly: !!process.env.CI,

  reporter: [
    ['html', { open: 'never', outputFolder: 'playwright-report-visual-capture' }],
    ['list'],
    ...(process.env.CI ? [['github'] as const] : []),
  ],

  use: {
    // Staging SPA base URL — override with STAGING_SPA_URL env var if needed.
    // Use `||` so an empty-string CI secret falls back to the default.
    baseURL: process.env.STAGING_SPA_URL || 'https://stg-app.vaultmtg.app',

    // Collect trace on failure for debugging
    trace: 'on-first-retry',

    // Screenshot on failure
    screenshot: 'only-on-failure',
  },

  projects: [
    {
      name: 'visual-capture',
      use: { ...devices['Desktop Chrome'] },
    },
  ],

  // No webServer — staging SPA is already live
});
