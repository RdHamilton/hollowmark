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
  //   multi-device-433 — authenticated BFF tests that require CLERK_SECRET_KEY
  //     (Backend API sign-in-token flow).  This config injects CLERK_SECRET_KEY
  //     so multi-device-433 CAN run here; it is excluded only because it belongs
  //     to a separate CI step (e2e-staging-auth-smoke.yml) to keep this suite
  //     focused on the core /matches + /events + /healthz contract.
  //   prof-visual-capture — screenshot capture driven by prof-visual-capture.yml;
  //     requires CLERK_SECRET_KEY + SCREENSHOT_DIR.  Collected here by mistake
  //     since the spec was added after the original exclusion list was written.
  testIgnore: [
    /visual-auth-smoke/,
    /visual-auth-data-verify/,
    /wildcard-panel-visual/,
    /r17-smoke/,
    /projection-golden-smoke/,
    /staging-spa-smoke/,
    /draft-ratings-.*-verify/,
    // Requires CLERK_SECRET_KEY — not injected by this step; runs in auth step.
    /multi-device-433/,
    // Requires CLERK_SECRET_KEY + SCREENSHOT_DIR — dedicated workflow step only.
    /prof-visual-capture/,
    // Still uses STAGING_SMOKE_TOKEN (pre-existing, same root cause as tickets#759).
    // Needs a separate rewire (same auth chain) before it can run here; exclude until fixed.
    /wildcard-advisor-424/,
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
