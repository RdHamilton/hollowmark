/**
 * Playwright config for ADR-077 config-error E2E tests.
 *
 * These tests assert the error screens rendered when /config.json is unavailable,
 * malformed, or contains invalid fields. They MUST run against the production
 * build (vite preview) because:
 *
 * 1. The production build has no DEV fallback in loadConfig(). If the
 *    Playwright route intercept aborts/errors the /config.json request, the
 *    error screen renders unconditionally.
 * 2. The Vite dev server's DEV fallback would bypass the route intercepts,
 *    causing the full SPA to mount instead of the error screen.
 *
 * Usage:
 *   npm run build:test && npx playwright test --config playwright.config-error.ts
 *
 * Or with the convenience script:
 *   npm run test:e2e:config-error
 *
 * NOTE: dist/config.json is intentionally NOT created for this test run.
 * The config-error tests each intercept /config.json with their own route handler.
 * Any tests that do NOT intercept /config.json will see the network error screen
 * (no config = ConfigNetworkError), which is the expected behavior.
 */

import { defineConfig, devices } from '@playwright/test';

export default defineConfig({
  testDir: './tests/e2e',
  testMatch: '**/config-error.spec.ts',

  timeout: 30 * 1000,
  fullyParallel: false,  // Sequential to avoid port conflicts
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,

  expect: { timeout: 30_000 },

  reporter: [
    ['html', { open: 'never', outputFolder: 'playwright-report-config-error' }],
    ['list'],
    ...(process.env.CI ? [['github'] as const] : []),
  ],

  use: {
    baseURL: 'http://localhost:4173',
    actionTimeout: 30_000,
    trace: 'on-first-retry',
    screenshot: 'only-on-failure',
    video: 'retain-on-failure',
  },

  projects: [
    {
      name: 'config-error',
      use: { ...devices['Desktop Chrome'] },
    },
  ],

  // Build then serve via vite preview. The production build has import.meta.env.DEV=false
  // so the DEV fallback in loadConfig() is statically eliminated. Route intercepts work.
  //
  // dist/config.json is NOT created here intentionally. Each test intercepts /config.json.
  webServer: [
    // Go BFF (needed by initializeServices() — allowed to fail since we're testing
    // the config load error path, which short-circuits before services are initialized).
    {
      command: process.env.CI
        ? '../bin/mtga-bff'
        : 'cd .. && go run ./services/bff/cmd/main.go',
      url: 'http://localhost:8080/health',
      timeout: 120 * 1000,
      reuseExistingServer: true,  // Always reuse; BFF state doesn't affect config-error tests
      stdout: 'pipe',
      stderr: 'pipe',
      ignoreHTTPSErrors: true,
    },
    // Production build + preview. No dist/config.json — each test intercepts it.
    {
      command: 'VITE_USE_REST_API=true VITE_CLERK_TEST_MODE=true VITE_BFF_URL=http://localhost:8080/api/v1 npm run build && npx vite preview --port 4173',
      url: 'http://localhost:4173',
      timeout: 300 * 1000,  // Build can take time on cold runners
      reuseExistingServer: !process.env.CI,  // Reuse local; always rebuild in CI
      stdout: 'pipe',
      stderr: 'pipe',
    },
  ],
});
