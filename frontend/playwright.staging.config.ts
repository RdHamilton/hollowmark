import { defineConfig, devices } from '@playwright/test';

/**
 * Playwright configuration for staging smoke tests (#1444, tickets#759)
 *
 * Targets the live staging BFF at staging-api.hollowmark.app.
 * No webServer block — the staging BFF is already running.
 *
 * Run via:
 *   npm run test:staging
 *   npx playwright test --config=playwright.staging.config.ts
 *
 * Triggered by the staging deploy workflow after a successful deploy.
 * Failures here must cause the deploy workflow post-step to fail.
 *
 * Authentication (tickets#759): uses the Backend-API sign-in-token chain via
 * CLERK_SECRET_KEY (sk_live_* from SSM /vaultmtg/app/staging/CLERK_SECRET_KEY).
 * The former STAGING_SMOKE_TOKEN approach is removed — Clerk blocks testing
 * tokens on prod-type instances (staging is prod-type by design).
 *
 * Suite is intentionally small (< 60 s total):
 *   1. Health check — GET /healthz returns 200 + expected shape
 *   2. Auth-gated routes return 401 without a token
 *   3. GET /api/v1/matches — real Clerk auth, ≥1 row assertion
 *   4. SSE endpoint accepts a connection (200 or timeout = healthy stream)
 */
export default defineConfig({
  testDir: './tests/e2e/staging',

  // Exclude specs that require CLERK_SECRET_KEY or SCREENSHOT_DIR — this step
  // only injects STAGING_API_URL and CLERK_SECRET_KEY.  Each excluded spec
  // runs in its own dedicated workflow step or config where the required secrets
  // are present:
  //
  //   visual-auth-smoke, visual-auth-data-verify — SPA browser specs (not BFF),
  //     run via playwright.staging-spa.config.ts with CLERK_SECRET_KEY injected.
  //   wildcard-panel-visual-424 — screenshot capture, needs SCREENSHOT_DIR,
  //     runs as its own step in e2e-staging-auth-smoke.yml.
  //   r17-smoke, projection-golden-smoke, draft-ratings-*-verify — require
  //     additional fixtures/env vars not present in this step.
  //   staging-spa-smoke — belongs to playwright.staging-spa.config.ts only.
  //
  //   multi-device-433 — authenticated BFF tests using the Backend-API
  //     sign-in-token chain.  CLERK_SECRET_KEY is injected by the BFF smoke
  //     step, so this spec now runs as part of the generic sweep alongside
  //     staging-smoke.spec.ts and wildcard-advisor-424.spec.ts.  The former
  //     dedicated CI step that explicitly passed the file path (tickets#1150)
  //     has been removed because testIgnore in Playwright v1.x takes precedence
  //     over explicitly-passed CLI file arguments — the file was silently
  //     excluded even when named directly, causing "No tests found" exit 1 on
  //     every staging deploy since the step was wired (50+ consecutive failures).
  //     Fix: remove from testIgnore and let the generic sweep collect it.
  //   prof-visual-capture — screenshot capture driven by prof-visual-capture.yml;
  //     requires CLERK_SECRET_KEY + SCREENSHOT_DIR.  Collected here by mistake
  //     since the spec was added after the original exclusion list was written.
  //   wildcard-advisor-424 — rewired to Backend-API sign-in-token chain
  //     (tickets#1190); CLERK_SECRET_KEY is injected by this step so it now runs
  //     here alongside staging-smoke.spec.ts.
  testIgnore: [
    /visual-auth-smoke/,
    /visual-auth-data-verify/,
    /wildcard-panel-visual/,
    /r17-smoke/,
    /projection-golden-smoke/,
    /staging-spa-smoke/,
    /draft-ratings-.*-verify/,
    // Requires CLERK_SECRET_KEY + SCREENSHOT_DIR — dedicated workflow step only.
    /prof-visual-capture/,
  ],

  // Individual test timeout — keep the suite well under 60 s total.
  timeout: 20 * 1000,

  // Run staging tests sequentially to avoid overwhelming a shared staging env.
  fullyParallel: false,
  workers: 1,

  // No retries — a flaky staging env should surface as a real failure.
  retries: 0,

  forbidOnly: !!process.env.CI,

  reporter: [
    ['html', { open: 'never', outputFolder: 'playwright-report-staging' }],
    ['list'],
    ...(process.env.CI ? [['github'] as const] : []),
  ],

  use: {
    // Staging BFF base URL — override with STAGING_API_URL env var if needed.
    baseURL: process.env.STAGING_API_URL ?? 'https://staging-api.hollowmark.app',

    // No browser needed; tests use the `request` fixture (APIRequestContext).
    // Still keep a browser project so Playwright can schedule tests normally.
    trace: 'on-first-retry',
  },

  projects: [
    {
      name: 'staging-smoke',
      use: { ...devices['Desktop Chrome'] },
    },
  ],

  // No webServer — staging BFF is already live.
});
