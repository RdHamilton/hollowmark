/**
 * Tests for runtimeConfig module — ADR-077 boot-refactor.
 *
 * Covers:
 *  AC2/AC5  — loadConfig() fetch-and-validate logic (incl. response.ok=true + HTML body trap)
 *  AC6      — Sentry.init NOT called on any failure branch
 *  AC11     — boot-signal beacon (fireBootBeacon) per-branch, including C2 mapping test
 *  AC7      — format-invalid clerkPublishableKey triggers ConfigMissingFieldsError
 *  Module   — singleton contract (getRuntimeConfig throws before load, isRuntimeConfigLoaded)
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

// ---------------------------------------------------------------------------
// Mock Sentry (AC6 — must NOT be initialized on failure)
// ---------------------------------------------------------------------------
vi.mock('@sentry/react', () => ({
  init: vi.fn(),
}));

import * as Sentry from '@sentry/react';

// ---------------------------------------------------------------------------
// Import module under test
// ---------------------------------------------------------------------------
import {
  loadConfig,
  getRuntimeConfig,
  setRuntimeConfig,
  _resetRuntimeConfig,
  _disableDevFallback,
  _enableDevFallback,
  isRuntimeConfigLoaded,
  ConfigNetworkError,
  ConfigParseError,
  ConfigMissingFieldsError,
  fireBootBeacon,
  mapErrorToBranches,
  type RuntimeConfig,
} from './runtimeConfig';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/** Read a Blob as text. Uses FileReader for jsdom compatibility (Blob.text() unavailable). */
function readBlob(blob: Blob): Promise<string> {
  return new Promise<string>((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => resolve(reader.result as string);
    reader.onerror = () => reject(reader.error);
    reader.readAsText(blob);
  });
}

// ---------------------------------------------------------------------------
// Test defaults
// ---------------------------------------------------------------------------
const VALID_CONFIG: RuntimeConfig = {
  clerkPublishableKey: 'pk_live_validkey123',
  bffUrl: 'https://api.vaultmtg.app/api/v1',
  sentryDsn: 'https://example@sentry.io/1',
  sentryEnv: 'production',
  posthogKey: 'phc_testkey',
  posthogHost: 'https://app.posthog.com',
  envLabel: 'production',
  daemonUrl: 'http://localhost:9001/api/v1',
};

// ---------------------------------------------------------------------------
// Mock fetch helpers
// ---------------------------------------------------------------------------

function mockFetchResponse(options: {
  ok: boolean;
  status?: number;
  body: string;
}): void {
  global.fetch = vi.fn().mockResolvedValue({
    ok: options.ok,
    status: options.status ?? (options.ok ? 200 : 500),
    text: () => Promise.resolve(options.body),
  });
}

function mockFetchThrows(error: Error = new TypeError('Failed to fetch')): void {
  global.fetch = vi.fn().mockRejectedValue(error);
}

// ---------------------------------------------------------------------------
// Setup / Teardown
// ---------------------------------------------------------------------------
beforeEach(() => {
  _resetRuntimeConfig();
  vi.clearAllMocks();
  Object.defineProperty(global.navigator, 'sendBeacon', {
    writable: true,
    configurable: true,
    value: vi.fn().mockReturnValue(true),
  });
  Object.defineProperty(window, 'location', {
    writable: true,
    configurable: true,
    value: { hostname: 'app.vaultmtg.app' },
  });
});

afterEach(() => {
  _resetRuntimeConfig();
});

// ===========================================================================
// Module contract
// ===========================================================================

describe('module contract', () => {
  it('getRuntimeConfig() throws before loadConfig() completes', () => {
    expect(() => getRuntimeConfig()).toThrow('[runtimeConfig]');
    expect(() => getRuntimeConfig()).toThrow('has not completed');
  });

  it('isRuntimeConfigLoaded() returns false before load, true after setRuntimeConfig', () => {
    expect(isRuntimeConfigLoaded()).toBe(false);
    setRuntimeConfig(VALID_CONFIG);
    expect(isRuntimeConfigLoaded()).toBe(true);
  });

  it('_resetRuntimeConfig() resets the singleton — getRuntimeConfig throws again', () => {
    setRuntimeConfig(VALID_CONFIG);
    expect(isRuntimeConfigLoaded()).toBe(true);
    _resetRuntimeConfig();
    expect(isRuntimeConfigLoaded()).toBe(false);
    expect(() => getRuntimeConfig()).toThrow('[runtimeConfig]');
  });

  it('getRuntimeConfig() returns the config set via setRuntimeConfig', () => {
    setRuntimeConfig(VALID_CONFIG);
    expect(getRuntimeConfig()).toEqual(VALID_CONFIG);
  });
});

// ===========================================================================
// loadConfig() — happy path
// ===========================================================================

describe('loadConfig() — happy path', () => {
  it('returns RuntimeConfig when fetch returns valid JSON', async () => {
    mockFetchResponse({ ok: true, body: JSON.stringify(VALID_CONFIG) });
    const cfg = await loadConfig();
    expect(cfg.clerkPublishableKey).toBe('pk_live_validkey123');
    expect(cfg.bffUrl).toBe('https://api.vaultmtg.app/api/v1');
  });

  it('calls fetch("/config.json") — same-origin path, no explicit origin prefix', async () => {
    mockFetchResponse({ ok: true, body: JSON.stringify(VALID_CONFIG) });
    await loadConfig();
    expect(global.fetch).toHaveBeenCalledWith('/config.json');
  });

  it('does NOT call Sentry.init — that is the caller\'s job', async () => {
    mockFetchResponse({ ok: true, body: JSON.stringify(VALID_CONFIG) });
    await loadConfig();
    expect(Sentry.init).not.toHaveBeenCalled();
  });
});

// ===========================================================================
// loadConfig() — error branches
//
// Error-branch tests must disable the DEV fallback so that the test-environment's
// import.meta.env.DEV === true does not bypass the throw paths.
// ===========================================================================

describe('loadConfig() — network error (fetch throws)', () => {
  beforeEach(() => { _disableDevFallback(); });
  afterEach(() => { _enableDevFallback(); });

  it('throws ConfigNetworkError when fetch throws', async () => {
    mockFetchThrows();
    await expect(loadConfig()).rejects.toBeInstanceOf(ConfigNetworkError);
  });

  it('throws ConfigNetworkError (not ConfigParseError) when fetch throws', async () => {
    mockFetchThrows();
    await expect(loadConfig()).rejects.not.toBeInstanceOf(ConfigParseError);
  });
});

describe('loadConfig() — non-2xx responses', () => {
  beforeEach(() => { _disableDevFallback(); });
  afterEach(() => { _enableDevFallback(); });

  it('throws ConfigNetworkError unconditionally for 503 status', async () => {
    mockFetchResponse({ ok: false, status: 503, body: '<h1>Service Unavailable</h1>' });
    await expect(loadConfig()).rejects.toBeInstanceOf(ConfigNetworkError);
  });

  it('throws ConfigNetworkError for 404 with HTML body (not ConfigParseError) — Ray Issue 7', async () => {
    mockFetchResponse({ ok: false, status: 404, body: '<!DOCTYPE html><html><body>Not Found</body></html>' });
    await expect(loadConfig()).rejects.toBeInstanceOf(ConfigNetworkError);
  });

  it('throws ConfigNetworkError for 403 (non-2xx)', async () => {
    mockFetchResponse({ ok: false, status: 403, body: '{}' });
    await expect(loadConfig()).rejects.toBeInstanceOf(ConfigNetworkError);
  });
});

describe('loadConfig() — AC5: response.ok===true but body is HTML (CloudFront 403→index.html trap)', () => {
  it('throws ConfigParseError when response.ok=true but body is HTML', async () => {
    mockFetchResponse({
      ok: true,
      status: 200,
      body: '<!DOCTYPE html><html><head><title>VaultMTG</title></head><body><div id="root"></div></body></html>',
    });
    await expect(loadConfig()).rejects.toBeInstanceOf(ConfigParseError);
  });

  it('does NOT throw ConfigNetworkError for response.ok=true + HTML body', async () => {
    mockFetchResponse({
      ok: true,
      status: 200,
      body: '<!DOCTYPE html><html></html>',
    });
    await expect(loadConfig()).rejects.not.toBeInstanceOf(ConfigNetworkError);
  });

  it('never resolves when body is HTML despite ok=true', async () => {
    mockFetchResponse({ ok: true, status: 200, body: '<html>SPA Shell</html>' });
    await expect(loadConfig()).rejects.toThrow();
  });
});

describe('loadConfig() — parse error branch', () => {
  it('throws ConfigParseError when body is invalid JSON', async () => {
    mockFetchResponse({ ok: true, body: 'not-json-at-all' });
    await expect(loadConfig()).rejects.toBeInstanceOf(ConfigParseError);
  });

  it('throws ConfigParseError for truncated JSON', async () => {
    mockFetchResponse({ ok: true, body: '{ "clerkPublishableKey": "pk_live' });
    await expect(loadConfig()).rejects.toBeInstanceOf(ConfigParseError);
  });
});

describe('loadConfig() — missing-fields branch', () => {
  it('throws ConfigMissingFieldsError when clerkPublishableKey is absent', async () => {
    const { clerkPublishableKey: _, ...withoutKey } = VALID_CONFIG;
    mockFetchResponse({ ok: true, body: JSON.stringify(withoutKey) });
    await expect(loadConfig()).rejects.toBeInstanceOf(ConfigMissingFieldsError);
  });

  it('throws ConfigMissingFieldsError when bffUrl is absent', async () => {
    const { bffUrl: _, ...withoutBff } = VALID_CONFIG;
    mockFetchResponse({ ok: true, body: JSON.stringify(withoutBff) });
    await expect(loadConfig()).rejects.toBeInstanceOf(ConfigMissingFieldsError);
  });

  it('throws ConfigMissingFieldsError when clerkPublishableKey is empty string', async () => {
    mockFetchResponse({ ok: true, body: JSON.stringify({ ...VALID_CONFIG, clerkPublishableKey: '' }) });
    await expect(loadConfig()).rejects.toBeInstanceOf(ConfigMissingFieldsError);
  });

  it('exposes missingFields list on ConfigMissingFieldsError', async () => {
    const { clerkPublishableKey: _, bffUrl: __, ...withoutTwo } = VALID_CONFIG;
    mockFetchResponse({ ok: true, body: JSON.stringify(withoutTwo) });
    try {
      await loadConfig();
      expect.fail('should have thrown');
    } catch (err) {
      expect(err).toBeInstanceOf(ConfigMissingFieldsError);
      expect((err as ConfigMissingFieldsError).missingFields).toContain('clerkPublishableKey');
      expect((err as ConfigMissingFieldsError).missingFields).toContain('bffUrl');
    }
  });
});

// ===========================================================================
// AC7 — format-shape validation for clerkPublishableKey
// ===========================================================================

describe('loadConfig() — AC7: clerkPublishableKey format-shape validation', () => {
  it('throws ConfigMissingFieldsError for a format-invalid clerkPublishableKey', async () => {
    mockFetchResponse({
      ok: true,
      body: JSON.stringify({ ...VALID_CONFIG, clerkPublishableKey: 'not-a-clerk-key' }),
    });
    await expect(loadConfig()).rejects.toBeInstanceOf(ConfigMissingFieldsError);
  });

  it('accepts pk_live_ prefixed keys (staging AND prod both use pk_live_)', async () => {
    mockFetchResponse({
      ok: true,
      body: JSON.stringify({ ...VALID_CONFIG, clerkPublishableKey: 'pk_live_stagingkey123' }),
    });
    const cfg = await loadConfig();
    expect(cfg.clerkPublishableKey).toBe('pk_live_stagingkey123');
  });

  it('accepts pk_test_ prefixed keys (test environments)', async () => {
    mockFetchResponse({
      ok: true,
      body: JSON.stringify({ ...VALID_CONFIG, clerkPublishableKey: 'pk_test_testkey456' }),
    });
    const cfg = await loadConfig();
    expect(cfg.clerkPublishableKey).toBe('pk_test_testkey456');
  });

  it('rejects keys with invalid characters', async () => {
    mockFetchResponse({
      ok: true,
      body: JSON.stringify({ ...VALID_CONFIG, clerkPublishableKey: 'pk_live_bad key!' }),
    });
    await expect(loadConfig()).rejects.toBeInstanceOf(ConfigMissingFieldsError);
  });

  it('includes clerkPublishableKey in missingFields when format check fails', async () => {
    mockFetchResponse({
      ok: true,
      body: JSON.stringify({ ...VALID_CONFIG, clerkPublishableKey: 'invalid-format' }),
    });
    try {
      await loadConfig();
      expect.fail('should have thrown');
    } catch (err) {
      expect(err).toBeInstanceOf(ConfigMissingFieldsError);
      expect((err as ConfigMissingFieldsError).missingFields).toContain('clerkPublishableKey');
    }
  });
});

// ===========================================================================
// AC6 — Sentry.init MUST NOT be called on any failure branch
// ===========================================================================

describe('AC6: Sentry.init not called on any failure branch', () => {
  beforeEach(() => { _disableDevFallback(); });
  afterEach(() => { _enableDevFallback(); });

  it('Sentry.init NOT called when fetch throws (ConfigNetworkError)', async () => {
    mockFetchThrows();
    await expect(loadConfig()).rejects.toBeInstanceOf(ConfigNetworkError);
    expect(Sentry.init).not.toHaveBeenCalled();
  });

  it('Sentry.init NOT called on ConfigParseError (ok=true + HTML body)', async () => {
    mockFetchResponse({ ok: true, body: '<html>Not JSON</html>' });
    await expect(loadConfig()).rejects.toBeInstanceOf(ConfigParseError);
    expect(Sentry.init).not.toHaveBeenCalled();
  });

  it('Sentry.init NOT called on ConfigMissingFieldsError', async () => {
    const { clerkPublishableKey: _, ...withoutKey } = VALID_CONFIG;
    mockFetchResponse({ ok: true, body: JSON.stringify(withoutKey) });
    await expect(loadConfig()).rejects.toBeInstanceOf(ConfigMissingFieldsError);
    expect(Sentry.init).not.toHaveBeenCalled();
  });
});

// ===========================================================================
// AC11 — boot-signal beacon
// ===========================================================================

describe('AC11: fireBootBeacon — sendBeacon called per branch', () => {
  it('calls sendBeacon with /api/v1/boot-signal endpoint for network branch', () => {
    fireBootBeacon('network');
    expect(navigator.sendBeacon).toHaveBeenCalledOnce();
    const [url] = (navigator.sendBeacon as ReturnType<typeof vi.fn>).mock.calls[0] as [string, Blob];
    expect(url).toMatch(/\/api\/v1\/boot-signal$/);
  });

  it('calls sendBeacon with text/plain Blob for parse branch', () => {
    fireBootBeacon('parse');
    expect(navigator.sendBeacon).toHaveBeenCalledOnce();
    const [, blob] = (navigator.sendBeacon as ReturnType<typeof vi.fn>).mock.calls[0] as [string, Blob];
    expect(blob).toBeInstanceOf(Blob);
    expect(blob.type).toBe('text/plain');
  });

  it('sends failure_type === "missing_field" (singular underscore) for missing_field branch', async () => {
    fireBootBeacon('missing_field');
    expect(navigator.sendBeacon).toHaveBeenCalledOnce();
    const [, blob] = (navigator.sendBeacon as ReturnType<typeof vi.fn>).mock.calls[0] as [string, Blob];
    const text = await readBlob(blob);
    const payload = JSON.parse(text) as { failure_type: string };
    expect(payload.failure_type).toBe('missing_field');
  });

  it('sends failure_type === "network" for network branch', async () => {
    fireBootBeacon('network');
    const [, blob] = (navigator.sendBeacon as ReturnType<typeof vi.fn>).mock.calls[0] as [string, Blob];
    const text = await readBlob(blob);
    const payload = JSON.parse(text) as { failure_type: string };
    expect(payload.failure_type).toBe('network');
  });

  it('sends failure_type === "parse" for parse branch', async () => {
    fireBootBeacon('parse');
    const [, blob] = (navigator.sendBeacon as ReturnType<typeof vi.fn>).mock.calls[0] as [string, Blob];
    const text = await readBlob(blob);
    const payload = JSON.parse(text) as { failure_type: string };
    expect(payload.failure_type).toBe('parse');
  });

  it('skips sendBeacon on localhost hostname', () => {
    Object.defineProperty(window, 'location', {
      writable: true,
      configurable: true,
      value: { hostname: 'localhost' },
    });
    fireBootBeacon('network');
    expect(navigator.sendBeacon).not.toHaveBeenCalled();
  });

  it('skips sendBeacon on 127.0.0.1 hostname', () => {
    Object.defineProperty(window, 'location', {
      writable: true,
      configurable: true,
      value: { hostname: '127.0.0.1' },
    });
    fireBootBeacon('network');
    expect(navigator.sendBeacon).not.toHaveBeenCalled();
  });

  it('sendBeacon throw does not propagate — fireBootBeacon is silent on error', () => {
    (navigator.sendBeacon as ReturnType<typeof vi.fn>).mockImplementation(() => {
      throw new DOMException('Not allowed', 'SecurityError');
    });
    expect(() => fireBootBeacon('network')).not.toThrow();
  });

  it('uses text/plain Blob — CORS simple request, not application/json', () => {
    fireBootBeacon('parse');
    const [, blob] = (navigator.sendBeacon as ReturnType<typeof vi.fn>).mock.calls[0] as [string, Blob];
    expect(blob.type).toBe('text/plain');
    expect(blob.type).not.toBe('application/json');
  });
});

// ===========================================================================
// C2 — mapping test: ConfigMissingFieldsError → screen branch 'missing-fields'
//      vs wire enum 'missing_field'. The single catch-block branch variable
//      must use the correct value for each consumer.
// ===========================================================================

describe('C2: ConfigMissingFieldsError → explicit mapping — screen branch vs wire enum', () => {
  it('ConfigMissingFieldsError maps to screenBranch="missing-fields" (hyphen-plural, Tim spec)', () => {
    const err = new ConfigMissingFieldsError('missing', ['clerkPublishableKey']);
    const { screenBranch } = mapErrorToBranches(err);
    expect(screenBranch).toBe('missing-fields');
  });

  it('ConfigMissingFieldsError maps to beaconType="missing_field" (underscore-singular, Ben receiver)', () => {
    const err = new ConfigMissingFieldsError('missing', ['clerkPublishableKey']);
    const { beaconType } = mapErrorToBranches(err);
    expect(beaconType).toBe('missing_field');
  });

  it('ConfigNetworkError maps to screenBranch="network" and beaconType="network"', () => {
    const err = new ConfigNetworkError('network failure');
    const { screenBranch, beaconType } = mapErrorToBranches(err);
    expect(screenBranch).toBe('network');
    expect(beaconType).toBe('network');
  });

  it('ConfigParseError maps to screenBranch="parse" and beaconType="parse"', () => {
    const err = new ConfigParseError('bad json');
    const { screenBranch, beaconType } = mapErrorToBranches(err);
    expect(screenBranch).toBe('parse');
    expect(beaconType).toBe('parse');
  });

  it('sendBeacon payload has missing_field (underscore) when using beaconType from mapErrorToBranches', async () => {
    const err = new ConfigMissingFieldsError('missing', ['clerkPublishableKey']);
    const { beaconType } = mapErrorToBranches(err);
    fireBootBeacon(beaconType);
    const [, blob] = (navigator.sendBeacon as ReturnType<typeof vi.fn>).mock.calls[0] as [string, Blob];
    const text = await readBlob(blob);
    const payload = JSON.parse(text) as { failure_type: string };
    // Must be "missing_field" — "missing-fields" would 400 at Ben's receiver
    expect(payload.failure_type).toBe('missing_field');
    expect(payload.failure_type).not.toBe('missing-fields');
  });
});

// ===========================================================================
// Module-load-capture regression: importing runtimeConfig must not throw
// ===========================================================================

describe('module-load-capture regression', () => {
  it('importing runtimeConfig does not throw before loadConfig() is called', async () => {
    await expect(import('./runtimeConfig')).resolves.toBeDefined();
  });
});

// ===========================================================================
// DEV fallback — loadConfig() falls back to VITE_* env vars in dev mode
//
// These tests verify the DEV fallback path (import.meta.env.DEV === true with
// _devFallbackDisabled === false). The error-branch tests above use
// _disableDevFallback() to force the production error path in test environments.
// ===========================================================================

describe('loadConfig() — DEV fallback (import.meta.env.DEV = true)', () => {
  // Note: in Vitest, import.meta.env.DEV is true by default, so _devFallbackDisabled
  // being false (the default after _enableDevFallback()) is sufficient.

  it('returns a config without throwing when fetch throws and DEV fallback is active', async () => {
    mockFetchThrows();
    // DEV fallback is enabled by default (no _disableDevFallback call)
    const cfg = await loadConfig();
    // Falls back to VITE_* defaults (empty/undefined in test env)
    expect(cfg).toBeDefined();
    expect(cfg.bffUrl).toBe('http://localhost:8080/api/v1'); // default fallback value
  });

  it('returns a config without throwing when response is non-2xx and DEV fallback is active', async () => {
    mockFetchResponse({ ok: false, status: 404, body: 'Not Found' });
    const cfg = await loadConfig();
    expect(cfg).toBeDefined();
  });

  it('does NOT use DEV fallback when fetch succeeds with valid JSON', async () => {
    mockFetchResponse({ ok: true, body: JSON.stringify(VALID_CONFIG) });
    const cfg = await loadConfig();
    // Should use the fetched config, not the fallback
    expect(cfg.clerkPublishableKey).toBe('pk_live_validkey123');
    expect(cfg.bffUrl).toBe('https://api.vaultmtg.app/api/v1');
  });

  it('setRuntimeConfig is called on DEV fallback — isRuntimeConfigLoaded returns true', async () => {
    mockFetchThrows();
    await loadConfig();
    expect(isRuntimeConfigLoaded()).toBe(true);
  });
});
