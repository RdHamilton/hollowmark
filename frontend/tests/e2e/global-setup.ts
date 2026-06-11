/**
 * Playwright global setup — ADR-077 runtime config provisioning.
 *
 * When running against the preview server (CI=true or PLAYWRIGHT_PREVIEW=true),
 * the production build has no /config.json (it is deployed separately per
 * ADR-077 / deploy pipeline #1209). This setup creates dist/config.json from
 * VITE_* env vars so the non-config-error E2E tests can boot normally.
 *
 * Config-error tests override the route via page.route() and therefore work
 * correctly regardless of whether dist/config.json exists.
 *
 * In dev mode (default local): the Vite dev server's DEV fallback handles
 * config bootstrapping; this setup is a no-op.
 */

import fs from 'fs';
import path from 'path';
import { fileURLToPath } from 'url';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

export default async function globalSetup(): Promise<void> {
  const isPreview = process.env.CI === 'true' || process.env.PLAYWRIGHT_PREVIEW === 'true';
  if (!isPreview) return;

  const distDir = path.resolve(__dirname, '../../dist');
  const configPath = path.join(distDir, 'config.json');

  // Idempotent — if config.json already exists (e.g. injected by the deploy
  // pipeline), don't overwrite it.
  if (fs.existsSync(configPath)) return;

  // Build test config from VITE_* env vars set by the CI build command.
  const testConfig = {
    clerkPublishableKey: process.env.VITE_CLERK_PUBLISHABLE_KEY ?? 'pk_test_placeholder',
    bffUrl: process.env.VITE_BFF_URL ?? 'http://localhost:8080/api/v1',
    sentryDsn: process.env.VITE_SENTRY_DSN ?? '',
    sentryEnv: process.env.VITE_SENTRY_ENV ?? 'test',
    posthogKey: process.env.VITE_POSTHOG_KEY ?? '',
    posthogHost: process.env.VITE_POSTHOG_HOST ?? 'https://app.posthog.com',
    envLabel: process.env.VITE_ENV_LABEL ?? 'test',
    daemonUrl: process.env.VITE_DAEMON_URL ?? 'http://localhost:9001/api/v1',
  };

  fs.mkdirSync(distDir, { recursive: true });
  fs.writeFileSync(configPath, JSON.stringify(testConfig, null, 2), 'utf8');
}
