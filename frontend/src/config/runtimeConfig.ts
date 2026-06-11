/**
 * runtimeConfig — ADR-077 SPA boot sidecar.
 *
 * Fetches `/config.json` from the same-origin CloudFront distribution before
 * any services or Clerk are initialised. This replaces all build-time-baked
 * VITE_* runtime values (ADR-075 §IV-3 byte-identity requirement).
 *
 * Error taxonomy (three distinct branches):
 *   ConfigNetworkError      — fetch throws or non-2xx response
 *   ConfigParseError        — response.ok=true but body is not valid JSON
 *                             (load-bearing case: CloudFront 403/404→index.html
 *                              rewrite arrives as HTTP 200 with HTML body)
 *   ConfigMissingFieldsError — valid JSON but required fields absent/empty/
 *                              or clerkPublishableKey fails format-shape check
 *
 * Beacon helper (AC11):
 *   fireBootBeacon(branch) — sends a `text/plain` Blob via sendBeacon to
 *   POST {bffOrigin}/api/v1/boot-signal. Called by main.tsx in the catch block
 *   BEFORE renderErrorScreen, never inside the component.
 *
 * Mapping helper (C2):
 *   mapErrorToBranches(err) — returns { screenBranch, beaconType } with the
 *   explicit translations:
 *     ConfigMissingFieldsError → screenBranch='missing-fields' (hyphen-plural,
 *                                 Tim spec prop/data-testid)
 *                              → beaconType='missing_field' (underscore-singular,
 *                                 Ben's #1212 receiver enum)
 *
 * DEV fallback:
 *   When import.meta.env.DEV is true and the fetch fails, loadConfig() falls back
 *   to VITE_* env vars so `vite dev` works without a local config.json. This branch
 *   is statically dead-code-eliminated in production builds (Vite replaces DEV=false).
 *
 * Singleton contract:
 *   loadConfig() → setRuntimeConfig(cfg) — populated after successful fetch.
 *   getRuntimeConfig() — call-time read; throws if called before loadConfig().
 *   isRuntimeConfigLoaded() — guard helper for dev-mode fallback paths.
 *   _resetRuntimeConfig() — test isolation only; never called in production.
 */

// ---------------------------------------------------------------------------
// Typed interface
// ---------------------------------------------------------------------------

export interface RuntimeConfig {
  /** Clerk publishable key — format: /^pk_(live|test)_[A-Za-z0-9]+$/ */
  clerkPublishableKey: string;
  /** BFF REST API base URL including /api/v1 suffix */
  bffUrl: string;
  /** Sentry DSN — optional; absent disables Sentry init */
  sentryDsn?: string;
  /** Sentry environment string — e.g. "production", "staging" */
  sentryEnv: string;
  /** PostHog project API key — optional; absent disables analytics init */
  posthogKey?: string;
  /** PostHog ingest host */
  posthogHost: string;
  /** Human-readable environment label — e.g. "production", "staging" */
  envLabel: string;
  /** Daemon local API base URL — e.g. "http://localhost:9001/api/v1" */
  daemonUrl: string;
}

// ---------------------------------------------------------------------------
// Typed error hierarchy
// ---------------------------------------------------------------------------

export class ConfigNetworkError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'ConfigNetworkError';
  }
}

export class ConfigParseError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'ConfigParseError';
  }
}

export class ConfigMissingFieldsError extends Error {
  readonly missingFields: string[];
  constructor(message: string, missingFields: string[]) {
    super(message);
    this.name = 'ConfigMissingFieldsError';
    this.missingFields = missingFields;
  }
}

// ---------------------------------------------------------------------------
// Singleton state
// ---------------------------------------------------------------------------

let _runtimeConfig: RuntimeConfig | null = null;

// DEV fallback toggle — controlled by _disableDevFallback() in tests.
// Production builds DCE this: import.meta.env.DEV === false, so the DEV
// fallback branches never execute regardless of this flag.
let _devFallbackDisabled = false;

/** Test-only — disables the DEV fallback so error-branch tests work correctly.
 *  Must be paired with a _resetRuntimeConfig() + re-enable in afterEach.
 *  Never call in production code. */
export function _disableDevFallback(): void {
  _devFallbackDisabled = true;
}

/** Test-only — re-enables the DEV fallback after error-branch tests. */
export function _enableDevFallback(): void {
  _devFallbackDisabled = false;
}

export function getRuntimeConfig(): RuntimeConfig {
  if (!_runtimeConfig) {
    throw new Error('[runtimeConfig] loadConfig() has not completed — do not call before boot');
  }
  return _runtimeConfig;
}

export function setRuntimeConfig(cfg: RuntimeConfig): void {
  _runtimeConfig = cfg;
}

/** Test-only — resets singleton between test cases. Never call in production. */
export function _resetRuntimeConfig(): void {
  _runtimeConfig = null;
}

export function isRuntimeConfigLoaded(): boolean {
  return _runtimeConfig !== null;
}

// ---------------------------------------------------------------------------
// Required fields and format-shape validation
// ---------------------------------------------------------------------------

const CLERK_KEY_REGEX = /^pk_(live|test)_[A-Za-z0-9]+$/;

const REQUIRED_FIELDS: (keyof RuntimeConfig)[] = [
  'clerkPublishableKey',
  'bffUrl',
  'sentryEnv',
  'envLabel',
  'daemonUrl',
  'posthogHost',
];

function validateConfig(parsed: unknown): RuntimeConfig {
  if (typeof parsed !== 'object' || parsed === null) {
    throw new ConfigMissingFieldsError('Config is not an object', ['(root)']);
  }
  const obj = parsed as Record<string, unknown>;
  const missingFields: string[] = [];

  for (const field of REQUIRED_FIELDS) {
    const val = obj[field];
    if (typeof val !== 'string' || val.trim().length === 0) {
      missingFields.push(field);
    }
  }

  // Format-shape check for clerkPublishableKey (AC7 — format only, not cross-env)
  const clerkKey = obj['clerkPublishableKey'];
  if (
    !missingFields.includes('clerkPublishableKey') &&
    typeof clerkKey === 'string' &&
    !CLERK_KEY_REGEX.test(clerkKey)
  ) {
    missingFields.push('clerkPublishableKey');
  }

  if (missingFields.length > 0) {
    throw new ConfigMissingFieldsError(
      `Config missing or invalid fields: ${missingFields.join(', ')}`,
      missingFields,
    );
  }

  return obj as unknown as RuntimeConfig;
}

// ---------------------------------------------------------------------------
// loadConfig() — fetch, parse, validate
// ---------------------------------------------------------------------------

export async function loadConfig(): Promise<RuntimeConfig> {
  let response: Response;

  try {
    response = await fetch('/config.json');
  } catch (err) {
    // DEV fallback: when running under `vite dev` (import.meta.env.DEV === true)
    // and the fetch fails (e.g. no local config.json exists yet), fall back to
    // VITE_* env vars baked at dev-server start. This branch is statically
    // dead-code-eliminated in production builds because Vite replaces
    // import.meta.env.DEV with the literal `false`.
    //
    // To opt out of the fallback in dev (e.g. to test the error screen locally),
    // create a public/config.json — it takes precedence over this fallback.
    if (import.meta.env.DEV && !_devFallbackDisabled) {
      return _devFallbackConfig();
    }
    throw new ConfigNetworkError(
      `[config] fetch failed: ${err instanceof Error ? err.message : String(err)}`,
    );
  }

  // Non-2xx → ConfigNetworkError unconditionally (Ray Issue 7 correction).
  // The parse/missing-fields branches only apply to 2xx bodies.
  if (!response.ok) {
    // DEV fallback for non-2xx (e.g. 404 from Vite dev server when no public/config.json).
    if (import.meta.env.DEV && !_devFallbackDisabled) {
      return _devFallbackConfig();
    }
    throw new ConfigNetworkError(
      `[config] Non-2xx response: ${response.status}`,
    );
  }

  // Always call response.text() and JSON.parse regardless of response.ok.
  // AC5 / CloudFront 403/404→index.html trap: a missing config.json causes
  // CloudFront to serve the SPA shell HTML with HTTP 200. response.ok is true
  // but the body is HTML — JSON.parse throws → ConfigParseError.
  let text: string;
  try {
    text = await response.text();
  } catch (err) {
    throw new ConfigNetworkError(
      `[config] Failed to read response body: ${err instanceof Error ? err.message : String(err)}`,
    );
  }

  let parsed: unknown;
  try {
    parsed = JSON.parse(text);
  } catch {
    throw new ConfigParseError(`[config] Response body is not valid JSON`);
  }

  // May throw ConfigMissingFieldsError
  const cfg = validateConfig(parsed);
  setRuntimeConfig(cfg);
  return cfg;
}

// ---------------------------------------------------------------------------
// DEV-only fallback — statically DCE'd in production builds.
// Reads VITE_* env vars baked at dev-server start time.
// This function is only ever called when import.meta.env.DEV is true.
// ---------------------------------------------------------------------------

function _devFallbackConfig(): RuntimeConfig {
  const cfg: RuntimeConfig = {
    clerkPublishableKey: (import.meta.env.VITE_CLERK_PUBLISHABLE_KEY as string | undefined) ?? '',
    bffUrl: (import.meta.env.VITE_BFF_URL as string | undefined) ?? 'http://localhost:8080/api/v1',
    sentryDsn: (import.meta.env.VITE_SENTRY_DSN as string | undefined) || undefined,
    sentryEnv: (import.meta.env.VITE_SENTRY_ENV as string | undefined) ?? 'development',
    posthogKey: (import.meta.env.VITE_POSTHOG_KEY as string | undefined) || undefined,
    posthogHost: (import.meta.env.VITE_POSTHOG_HOST as string | undefined) ?? 'https://app.posthog.com',
    envLabel: (import.meta.env.VITE_ENV_LABEL as string | undefined) ?? 'dev',
    daemonUrl: (import.meta.env.VITE_DAEMON_URL as string | undefined) ?? 'http://localhost:9001/api/v1',
  };
  setRuntimeConfig(cfg);
  return cfg;
}

// ---------------------------------------------------------------------------
// C2 — explicit error→(screenBranch, beaconType) mapping
//
// Tim's spec uses 'missing-fields' (hyphen-plural) for the screen prop and
// data-testids. Ben's #1212 receiver expects 'missing_field' (underscore-singular).
// These are different strings. This mapping function is the single place where
// the translation lives — every consumer uses this, never derives its own mapping.
// ---------------------------------------------------------------------------

export type ConfigErrorScreenBranch = 'network' | 'parse' | 'missing-fields';
export type BeaconFailureType = 'network' | 'parse' | 'missing_field';

export function mapErrorToBranches(err: unknown): {
  screenBranch: ConfigErrorScreenBranch;
  beaconType: BeaconFailureType;
} {
  if (err instanceof ConfigMissingFieldsError) {
    return { screenBranch: 'missing-fields', beaconType: 'missing_field' };
  }
  if (err instanceof ConfigParseError) {
    return { screenBranch: 'parse', beaconType: 'parse' };
  }
  // ConfigNetworkError and any unexpected error
  return { screenBranch: 'network', beaconType: 'network' };
}

// ---------------------------------------------------------------------------
// BFF origin derivation — no full-domain literals in bundle (AC11 / ADR-077 FF-10)
//
// Derives the BFF origin from window.location.hostname at runtime. No string
// literals like 'api.vaultmtg.app' or 'staging-api.vaultmtg.app' appear in
// the bundle (those literals would trip the FF-10 CI check in #1209).
//
// Topology (verified by Ray against CDN/DNS templates):
//   app.vaultmtg.app       → api.vaultmtg.app       (cdn.yml)
//   stg-app.vaultmtg.app   → staging-api.vaultmtg.app (hollowmark-stg-cdn.yml)
//   app.hollowmark.app     → api.hollowmark.app      (hollowmark-cdn.yml)
//   stg-app.hollowmark.app → staging-api.hollowmark.app
//
// TRANSITIONAL GAP (C3): api.hollowmark.app does NOT yet exist (NXDOMAIN).
// Until the I-57 v0.4.x API cutover creates that DNS record, beacons from
// canonical-domain visitors will resolve to NXDOMAIN and be silently lost.
// Fire-and-forget — boot continues to the error screen regardless.
// Tracked separately (Ray routing an api.hollowmark.app DNS ticket).
//
// Unknown hostname: skip beacon (return null). Firing at api.<unknown-apex>
// would send failure metadata to an arbitrary third-party origin.
// ---------------------------------------------------------------------------

function isLocalHostname(hostname: string): boolean {
  return hostname === 'localhost' || hostname === '127.0.0.1';
}

function deriveStagingFromHostname(hostname: string): boolean {
  const parts = hostname.split('.');
  if (parts.length < 2) return false;
  return parts[0].startsWith('stg-');
}

function deriveBffOrigin(): string | null {
  const hostname = window.location.hostname;

  if (isLocalHostname(hostname)) return null;

  const parts = hostname.split('.');
  if (parts.length < 2) return null;

  const apex = parts.slice(1).join('.');
  const label = parts[0];

  // Staging: hostname label starts with 'stg-' (covers stg-app.vaultmtg.app,
  // stg-app.hollowmark.app). The dead branch 'staging-app' is removed (C5).
  if (label.startsWith('stg-')) {
    return `https://staging-api.${apex}`;
  }

  // Production: hostname label is 'app'
  if (label === 'app') {
    return `https://api.${apex}`;
  }

  // Unknown host — skip beacon (C5 ruling)
  return null;
}

// ---------------------------------------------------------------------------
// fireBootBeacon — AC11
//
// Called by main.tsx in the catch block for all three error branches, BEFORE
// renderErrorScreen. Lives in runtimeConfig (not ConfigErrorScreen) because
// the beacon fires BEFORE the component mounts.
// ---------------------------------------------------------------------------

export function fireBootBeacon(branch: BeaconFailureType): void {
  const bffOrigin = deriveBffOrigin();
  if (!bffOrigin) return; // skip in dev/localhost/unknown-host

  const isStaging = deriveStagingFromHostname(window.location.hostname);
  const environment = isStaging ? 'staging' : 'production';

  const payload = JSON.stringify({
    failure_type: branch,
    // VITE_APP_VERSION stays baked — artifact identity constant, not a per-env value.
    app_version: import.meta.env.VITE_APP_VERSION ?? 'unknown',
    timestamp: new Date().toISOString(),
    environment,
  });

  try {
    navigator.sendBeacon(
      `${bffOrigin}/api/v1/boot-signal`,
      new Blob([payload], { type: 'text/plain' }),
    );
  } catch {
    // sendBeacon failure must never propagate — boot continues to error screen.
    // Can throw SecurityError in certain privacy-hardened browser environments.
  }
}
