import { describe, it, expect, vi, beforeEach } from 'vitest';
import { renderHook, act } from '@testing-library/react';

// Mock posthog-js before any module that imports it.
vi.mock('posthog-js', () => ({
  default: {
    init: vi.fn(),
    capture: vi.fn(),
    identify: vi.fn(),
    reset: vi.fn(),
    register: vi.fn(),
    unregister: vi.fn(),
    startSessionRecording: vi.fn(),
    stopSessionRecording: vi.fn(),
  },
}));

// Mock analytics module so we can spy on identifyUser / trackEvent / hashPII / hashAccountID.
const mockIdentifyUser = vi.fn();
const mockTrackEvent = vi.fn();
const mockResetIdentity = vi.fn();
const mockStartSessionReplay = vi.fn();
const mockStopSessionReplay = vi.fn();
const mockRegisterSuperProperties = vi.fn();
const mockUnregisterSuperProperty = vi.fn();
// hashPII mock returns a deterministic fake hash — sufficiently unlike the raw input
// to validate the negative assertion that raw IDs are never forwarded.
const mockHashPII = vi.fn().mockResolvedValue('abcd1234abcd1234');
// hashAccountID mock: same contract as hashPII but semantically for Clerk user_id distinct_id.
const mockHashAccountID = vi.fn().mockResolvedValue('abcd1234abcd1234');

vi.mock('@/services/analytics', () => ({
  identifyUser: (...args: unknown[]) => mockIdentifyUser(...args),
  trackEvent: (...args: unknown[]) => mockTrackEvent(...args),
  hashPII: (...args: unknown[]) => mockHashPII(...args),
  hashAccountID: (...args: unknown[]) => mockHashAccountID(...args),
  resetIdentity: () => mockResetIdentity(),
  startSessionReplay: () => mockStartSessionReplay(),
  stopSessionReplay: () => mockStopSessionReplay(),
  registerSuperProperties: (...args: unknown[]) => mockRegisterSuperProperties(...args),
  unregisterSuperProperty: (...args: unknown[]) => mockUnregisterSuperProperty(...args),
}));

// Mock bffDaemons — default returns empty list; override per test.
const mockListDaemons = vi.fn();
vi.mock('@/services/api/bffDaemons', () => ({
  listDaemons: (...args: unknown[]) => mockListDaemons(...args),
}));

// Clerk mock — controlled per test.
const mockUseUser = vi.fn();
const mockGetToken = vi.fn().mockResolvedValue('test-token');
vi.mock('@clerk/react', () => ({
  useUser: () => mockUseUser(),
  useAuth: () => ({ getToken: mockGetToken }),
}));

const SESSION_KEY = 'vaultmtg_ph_funnel_sign_up_completed_fired';
const SESSION_STARTED_KEY = 'vaultmtg_ph_app_session_started_fired';

describe('usePostHogIdentity', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    sessionStorage.clear();
    vi.resetModules();
    // Default: no devices paired — tests that need devices override this.
    mockListDaemons.mockResolvedValue({ devices: [] });
    mockGetToken.mockResolvedValue('test-token');
    // Reset hashPII to default deterministic fake hash.
    mockHashPII.mockResolvedValue('abcd1234abcd1234');
    // Reset hashAccountID to default deterministic fake hash.
    mockHashAccountID.mockResolvedValue('abcd1234abcd1234');
  });

  // ── Pre-existing behaviour (preserved) ─────────────────────────────────────

  it('does nothing when Clerk is not yet loaded', async () => {
    mockUseUser.mockReturnValue({ isLoaded: false, isSignedIn: false, user: null });
    const { usePostHogIdentity } = await import('./usePostHogIdentity');
    renderHook(() => usePostHogIdentity());

    expect(mockIdentifyUser).not.toHaveBeenCalled();
    expect(mockTrackEvent).not.toHaveBeenCalled();
    expect(mockStartSessionReplay).not.toHaveBeenCalled();
  });

  it('calls identifyUser with clerk user id when signed in', async () => {
    mockUseUser.mockReturnValue({
      isLoaded: true,
      isSignedIn: true,
      user: { id: 'user_abc' },
    });
    const { usePostHogIdentity } = await import('./usePostHogIdentity');
    renderHook(() => usePostHogIdentity());

    // identifyUser is now called inside an async IIFE — flush it.
    await act(async () => {
      await Promise.resolve();
    });

    // identifyUser is called with (userId, email?). When no primaryEmailAddress is
    // set, the second arg is undefined. The hook passes the raw id; analytics.ts
    // hashes it internally before calling posthog.identify.
    expect(mockIdentifyUser).toHaveBeenCalledWith('user_abc', undefined);
  });

  // ── #819: identifyUser receives email from Clerk primaryEmailAddress ───────

  it('calls identifyUser with email when user has a primary email address (#819)', async () => {
    mockUseUser.mockReturnValue({
      isLoaded: true,
      isSignedIn: true,
      user: {
        id: 'user_abc',
        primaryEmailAddress: { emailAddress: 'user@example.com' },
      },
    });
    const { usePostHogIdentity } = await import('./usePostHogIdentity');
    renderHook(() => usePostHogIdentity());

    // identifyUser is now inside an async IIFE — flush it.
    await act(async () => {
      await Promise.resolve();
    });

    expect(mockIdentifyUser).toHaveBeenCalledWith('user_abc', 'user@example.com');
  });

  it('NEGATIVE: identifyUser mock receives email but raw email is the hook arg — hash is analytics.ts responsibility', async () => {
    // The hook passes the raw email to identifyUser; analytics.ts does the hashing.
    // This test confirms the hook passes what Clerk gives it — not a pre-hashed value.
    mockUseUser.mockReturnValue({
      isLoaded: true,
      isSignedIn: true,
      user: {
        id: 'user_abc',
        primaryEmailAddress: { emailAddress: 'frank@vaultmtg.app' },
      },
    });
    const { usePostHogIdentity } = await import('./usePostHogIdentity');
    renderHook(() => usePostHogIdentity());

    // identifyUser is now inside an async IIFE — flush it.
    await act(async () => {
      await Promise.resolve();
    });

    const [, emailArg] = mockIdentifyUser.mock.calls[0] as [string, string];
    // Hook passes raw email — analytics.ts hashes it before calling posthog.identify.
    expect(emailArg).toBe('frank@vaultmtg.app');
  });

  it('starts session replay when user is signed in', async () => {
    mockUseUser.mockReturnValue({
      isLoaded: true,
      isSignedIn: true,
      user: { id: 'user_abc' },
    });
    const { usePostHogIdentity } = await import('./usePostHogIdentity');
    renderHook(() => usePostHogIdentity());

    expect(mockStartSessionReplay).toHaveBeenCalledOnce();
  });

  it('does NOT start session replay when user is not signed in', async () => {
    mockUseUser.mockReturnValue({ isLoaded: true, isSignedIn: false, user: null });
    const { usePostHogIdentity } = await import('./usePostHogIdentity');
    renderHook(() => usePostHogIdentity());

    expect(mockStartSessionReplay).not.toHaveBeenCalled();
  });

  it('fires funnel_sign_up_completed once per session when signed in', async () => {
    mockUseUser.mockReturnValue({
      isLoaded: true,
      isSignedIn: true,
      user: { id: 'user_abc' },
    });
    const { usePostHogIdentity } = await import('./usePostHogIdentity');
    renderHook(() => usePostHogIdentity());

    // Flush the async hashPII IIFE so trackEvent is called.
    await act(async () => {
      await Promise.resolve();
    });

    const funnelCalls = mockTrackEvent.mock.calls.filter(
      ([e]: [{ name: string }]) => e.name === 'funnel_sign_up_completed',
    );
    expect(funnelCalls).toHaveLength(1);
    expect(funnelCalls[0][0].properties.auth_method).toBe('email');
    expect(sessionStorage.getItem(SESSION_KEY)).toBe('1');
  });

  it('does NOT fire funnel_sign_up_completed if sessionStorage guard is already set', async () => {
    sessionStorage.setItem(SESSION_KEY, '1');
    mockUseUser.mockReturnValue({
      isLoaded: true,
      isSignedIn: true,
      user: { id: 'user_abc' },
    });
    const { usePostHogIdentity } = await import('./usePostHogIdentity');
    renderHook(() => usePostHogIdentity());

    await act(async () => {
      await Promise.resolve();
    });

    const funnelCalls = mockTrackEvent.mock.calls.filter(
      ([e]: [{ name: string }]) => e.name === 'funnel_sign_up_completed',
    );
    expect(funnelCalls).toHaveLength(0);
  });

  // ── ADR-027: funnel_sign_up_completed user_id hashing (#851) ─────────────────

  it('funnel_sign_up_completed sends hashed user_id (not raw Clerk id)', async () => {
    const rawUserId = 'user_clerk_raw_id_xyz';
    mockHashPII.mockResolvedValue('abcd1234abcd1234');
    mockUseUser.mockReturnValue({
      isLoaded: true,
      isSignedIn: true,
      user: { id: rawUserId },
    });
    const { usePostHogIdentity } = await import('./usePostHogIdentity');
    renderHook(() => usePostHogIdentity());

    await act(async () => {
      await Promise.resolve();
    });

    const funnelCalls = mockTrackEvent.mock.calls.filter(
      ([e]: [{ name: string }]) => e.name === 'funnel_sign_up_completed',
    );
    expect(funnelCalls).toHaveLength(1);
    // user_id property must be the hashed value, not the raw Clerk id.
    expect(funnelCalls[0][0].properties.user_id).toBe('abcd1234abcd1234');
  });

  it('NEGATIVE: funnel_sign_up_completed never sends raw Clerk user id as event property', async () => {
    const rawUserId = 'user_clerk_raw_secret_789';
    mockHashPII.mockResolvedValue('deadbeef12345678');
    mockUseUser.mockReturnValue({
      isLoaded: true,
      isSignedIn: true,
      user: { id: rawUserId },
    });
    const { usePostHogIdentity } = await import('./usePostHogIdentity');
    renderHook(() => usePostHogIdentity());

    await act(async () => {
      await Promise.resolve();
    });

    // Inspect ALL trackEvent calls — the raw Clerk user id must never appear
    // as a property value in any of them.
    for (const [event] of mockTrackEvent.mock.calls as [{ name: string; properties?: Record<string, unknown> }][]) {
      if (event.properties) {
        for (const value of Object.values(event.properties)) {
          expect(value).not.toBe(rawUserId);
        }
      }
    }
  });

  it('calls resetIdentity when user is signed out after being signed in', async () => {
    mockUseUser.mockReturnValue({
      isLoaded: true,
      isSignedIn: true,
      user: { id: 'user_abc' },
    });
    const { usePostHogIdentity } = await import('./usePostHogIdentity');
    const { rerender } = renderHook(() => usePostHogIdentity());

    mockUseUser.mockReturnValue({ isLoaded: true, isSignedIn: false, user: null });
    rerender();

    expect(mockResetIdentity).toHaveBeenCalledOnce();
  });

  it('stops session replay when user signs out', async () => {
    mockUseUser.mockReturnValue({
      isLoaded: true,
      isSignedIn: true,
      user: { id: 'user_abc' },
    });
    const { usePostHogIdentity } = await import('./usePostHogIdentity');
    const { rerender } = renderHook(() => usePostHogIdentity());

    mockUseUser.mockReturnValue({ isLoaded: true, isSignedIn: false, user: null });
    rerender();

    expect(mockStopSessionReplay).toHaveBeenCalledOnce();
  });

  // ── New behaviour (session lifecycle) ──────────────────────────────────────

  // #82 (AC3): app_user_identified now MUST include a hashed user_id field.
  // These tests replace the prior "no user_id" assertions — the prior behaviour
  // was intentional deferral; this ticket adds the field with the hashed value.

  it('#82 AC3: fires app_user_identified with hashed user_id field', async () => {
    const rawUserId = 'user_abc';
    mockHashAccountID.mockResolvedValue('abcd1234abcd1234');
    mockUseUser.mockReturnValue({
      isLoaded: true,
      isSignedIn: true,
      user: { id: rawUserId },
    });
    const { usePostHogIdentity } = await import('./usePostHogIdentity');
    renderHook(() => usePostHogIdentity());

    // Flush async hashAccountID IIFE so trackEvent is called.
    await act(async () => {
      await Promise.resolve();
    });

    const identifiedCalls = mockTrackEvent.mock.calls.filter(
      ([e]: [{ name: string }]) => e.name === 'app_user_identified',
    );
    expect(identifiedCalls).toHaveLength(1);
    // CRITICAL: user_id MUST appear in the payload (AC3) — this replaces the
    // prior "not.toHaveProperty('user_id')" assertion from the deferred state.
    expect(identifiedCalls[0][0].properties).toHaveProperty('user_id');
    expect(identifiedCalls[0][0].properties.user_id).toBe('abcd1234abcd1234');
  });

  it('#82 NEGATIVE: app_user_identified user_id is the hashed value, never the raw Clerk id', async () => {
    const rawUserId = 'user_secret_id_that_must_never_appear';
    mockHashAccountID.mockResolvedValue('deadbeef12345678');
    mockUseUser.mockReturnValue({
      isLoaded: true,
      isSignedIn: true,
      user: { id: rawUserId },
    });
    const { usePostHogIdentity } = await import('./usePostHogIdentity');
    renderHook(() => usePostHogIdentity());

    await act(async () => {
      await Promise.resolve();
    });

    mockTrackEvent.mock.calls
      .filter(([e]: [{ name: string }]) => e.name === 'app_user_identified')
      .forEach(([e]: [{ properties: Record<string, unknown> }]) => {
        // user_id must be present (AC3)
        expect(e.properties).toHaveProperty('user_id');
        // user_id must be the hashed value, not the raw Clerk id
        expect(e.properties.user_id).toBe('deadbeef12345678');
        expect(e.properties.user_id).not.toBe(rawUserId);
        // Must not carry 'user_' prefix — raw Clerk ids always start with 'user_'
        expect(e.properties.user_id as string).not.toMatch(/^user_/);
      });
  });

  it('fires app_user_signed_out BEFORE resetIdentity on sign-out', async () => {
    mockUseUser.mockReturnValue({
      isLoaded: true,
      isSignedIn: true,
      user: { id: 'user_abc' },
    });
    const { usePostHogIdentity } = await import('./usePostHogIdentity');
    const { rerender } = renderHook(() => usePostHogIdentity());

    const callOrder: string[] = [];
    mockTrackEvent.mockImplementation((e: { name: string }) => {
      callOrder.push(`trackEvent:${e.name}`);
    });
    mockResetIdentity.mockImplementation(() => {
      callOrder.push('resetIdentity');
    });

    mockUseUser.mockReturnValue({ isLoaded: true, isSignedIn: false, user: null });
    rerender();

    const signedOutIdx = callOrder.indexOf('trackEvent:app_user_signed_out');
    const resetIdx = callOrder.indexOf('resetIdentity');
    expect(signedOutIdx).toBeGreaterThanOrEqual(0);
    expect(resetIdx).toBeGreaterThan(signedOutIdx);
  });

  it('fires app_user_signed_out exactly once on sign-out', async () => {
    mockUseUser.mockReturnValue({
      isLoaded: true,
      isSignedIn: true,
      user: { id: 'user_abc' },
    });
    const { usePostHogIdentity } = await import('./usePostHogIdentity');
    const { rerender } = renderHook(() => usePostHogIdentity());

    mockUseUser.mockReturnValue({ isLoaded: true, isSignedIn: false, user: null });
    rerender();

    const signedOutCalls = mockTrackEvent.mock.calls.filter(
      ([e]: [{ name: string }]) => e.name === 'app_user_signed_out',
    );
    expect(signedOutCalls).toHaveLength(1);
  });

  // ── Super-properties ────────────────────────────────────────────────────────

  it('registers super-properties after successful identify', async () => {
    mockUseUser.mockReturnValue({
      isLoaded: true,
      isSignedIn: true,
      user: { id: 'user_abc' },
    });
    const { usePostHogIdentity } = await import('./usePostHogIdentity');
    renderHook(() => usePostHogIdentity());

    expect(mockRegisterSuperProperties).toHaveBeenCalledOnce();
    const props = mockRegisterSuperProperties.mock.calls[0][0] as Record<string, unknown>;
    // Narrowed set per Ray adj. #3: app_version, is_signed_in, platform only
    expect(props).toHaveProperty('app_version');
    expect(props).toHaveProperty('is_signed_in', true);
    expect(props).toHaveProperty('platform');
    // daemon_status must NOT be a super-property in this ticket (Ray adj. #3)
    expect(props).not.toHaveProperty('daemon_status');
  });

  it('super-properties include app_version string (unknown fallback allowed)', async () => {
    mockUseUser.mockReturnValue({
      isLoaded: true,
      isSignedIn: true,
      user: { id: 'user_abc' },
    });
    const { usePostHogIdentity } = await import('./usePostHogIdentity');
    renderHook(() => usePostHogIdentity());

    const props = mockRegisterSuperProperties.mock.calls[0][0] as Record<string, unknown>;
    expect(typeof props.app_version).toBe('string');
    // 'unknown' is an allowed fallback per Ray adj. #5
    expect(props.app_version).toBeTruthy();
  });

  it('updates is_signed_in super-property to false after sign-out', async () => {
    mockUseUser.mockReturnValue({
      isLoaded: true,
      isSignedIn: true,
      user: { id: 'user_abc' },
    });
    const { usePostHogIdentity } = await import('./usePostHogIdentity');
    const { rerender } = renderHook(() => usePostHogIdentity());

    mockUseUser.mockReturnValue({ isLoaded: true, isSignedIn: false, user: null });
    rerender();

    // Last registerSuperProperties call must reflect signed-out state
    const lastCall = mockRegisterSuperProperties.mock.calls.at(-1)!;
    const props = lastCall[0] as Record<string, unknown>;
    expect(props.is_signed_in).toBe(false);
  });

  // ── device_id super-property (T1–T6, #2633) ────────────────────────────────

  // T1: N == 1 → register device_id
  it('T1: registers device_id super-property when exactly one device is paired', async () => {
    mockListDaemons.mockResolvedValue({
      devices: [{ device_id: 'dev-uuid-001', platform: 'windows', daemon_ver: '1.0.0', paired_at: '2026-01-01T00:00:00Z', last_used_at: null }],
    });
    mockUseUser.mockReturnValue({
      isLoaded: true,
      isSignedIn: true,
      user: { id: 'user_abc' },
    });
    const { usePostHogIdentity } = await import('./usePostHogIdentity');
    const { unmount } = renderHook(() => usePostHogIdentity());

    // Wait for the async listDaemons call to resolve.
    await act(async () => {
      await Promise.resolve();
    });

    const deviceIdCall = mockRegisterSuperProperties.mock.calls.find(
      ([props]: [Record<string, unknown>]) => 'device_id' in props,
    );
    expect(deviceIdCall).toBeDefined();
    expect(deviceIdCall![0].device_id).toBe('dev-uuid-001');
    unmount();
  });

  // T2: N == 0 → device_id super-property is absent (unregister called)
  it('T2: does not register device_id when no devices are paired (unregister called)', async () => {
    mockListDaemons.mockResolvedValue({ devices: [] });
    mockUseUser.mockReturnValue({
      isLoaded: true,
      isSignedIn: true,
      user: { id: 'user_abc' },
    });
    const { usePostHogIdentity } = await import('./usePostHogIdentity');
    const { unmount } = renderHook(() => usePostHogIdentity());

    await act(async () => {
      await Promise.resolve();
    });

    // registerSuperProperties must not have been called with device_id
    const deviceIdCall = mockRegisterSuperProperties.mock.calls.find(
      ([props]: [Record<string, unknown>]) => 'device_id' in props,
    );
    expect(deviceIdCall).toBeUndefined();

    // unregisterSuperProperty('device_id') must have been called
    expect(mockUnregisterSuperProperty).toHaveBeenCalledWith('device_id');
    unmount();
  });

  // T3: ref-guard prevents double-register on re-render
  it('T3: ref-guard prevents double device_id registration on re-render', async () => {
    mockListDaemons.mockResolvedValue({
      devices: [{ device_id: 'dev-uuid-001', platform: 'windows', daemon_ver: '1.0.0', paired_at: '2026-01-01T00:00:00Z', last_used_at: null }],
    });
    mockUseUser.mockReturnValue({
      isLoaded: true,
      isSignedIn: true,
      user: { id: 'user_abc' },
    });
    const { usePostHogIdentity } = await import('./usePostHogIdentity');
    const { rerender, unmount } = renderHook(() => usePostHogIdentity());

    await act(async () => {
      await Promise.resolve();
    });

    // Re-render with same user — ref-guard should prevent second listDaemons call
    rerender();

    await act(async () => {
      await Promise.resolve();
    });

    // listDaemons called only once (ref-guard prevents second identify pass)
    expect(mockListDaemons).toHaveBeenCalledOnce();
    unmount();
  });

  // T4: PII boundary — no user identifier passed to listDaemons or registerSuperProperties
  it('T4: PII boundary — device_id super-property contains no user identifier', async () => {
    const deviceId = 'dev-uuid-opaque-001';
    mockListDaemons.mockResolvedValue({
      devices: [{ device_id: deviceId, platform: 'macos', daemon_ver: '1.0.0', paired_at: '2026-01-01T00:00:00Z', last_used_at: null }],
    });
    mockUseUser.mockReturnValue({
      isLoaded: true,
      isSignedIn: true,
      user: { id: 'user_secret_clerk_id' },
    });
    const { usePostHogIdentity } = await import('./usePostHogIdentity');
    const { unmount } = renderHook(() => usePostHogIdentity());

    await act(async () => {
      await Promise.resolve();
    });

    // No call to registerSuperProperties should contain the Clerk user ID
    for (const [props] of mockRegisterSuperProperties.mock.calls as [Record<string, unknown>][]) {
      expect(Object.values(props)).not.toContain('user_secret_clerk_id');
    }
    // The device_id registered is the opaque daemon UUID, not a user identifier
    const deviceIdCall = mockRegisterSuperProperties.mock.calls.find(
      ([props]: [Record<string, unknown>]) => 'device_id' in props,
    );
    expect(deviceIdCall![0].device_id).toBe(deviceId);
    unmount();
  });

  // T5 (Ray-mandated): N == 2 → unregister('device_id'), NOT registerSuperProperties with device_id
  it('T5: N=2 triggers unregisterSuperProperty(device_id) and does NOT call registerSuperProperties with device_id', async () => {
    mockListDaemons.mockResolvedValue({
      devices: [
        { device_id: 'dev-uuid-001', platform: 'windows', daemon_ver: '1.0.0', paired_at: '2026-01-01T00:00:00Z', last_used_at: null },
        { device_id: 'dev-uuid-002', platform: 'macos', daemon_ver: '1.0.0', paired_at: '2026-01-02T00:00:00Z', last_used_at: null },
      ],
    });
    mockUseUser.mockReturnValue({
      isLoaded: true,
      isSignedIn: true,
      user: { id: 'user_abc' },
    });
    const { usePostHogIdentity } = await import('./usePostHogIdentity');
    const { unmount } = renderHook(() => usePostHogIdentity());

    await act(async () => {
      await Promise.resolve();
    });

    // Must call unregister, not register with device_id
    expect(mockUnregisterSuperProperty).toHaveBeenCalledWith('device_id');
    const deviceIdRegisterCall = mockRegisterSuperProperties.mock.calls.find(
      ([props]: [Record<string, unknown>]) => 'device_id' in props,
    );
    expect(deviceIdRegisterCall).toBeUndefined();
    unmount();
  });

  // ── #82: hashAccountID called for app_user_identified user_id ───────────────

  it('#82: hashAccountID is called with the raw Clerk user_id for app_user_identified', async () => {
    const rawUserId = 'user_clerk_id_for_hash_test';
    mockHashAccountID.mockResolvedValue('cafe0000cafe0000');
    mockUseUser.mockReturnValue({
      isLoaded: true,
      isSignedIn: true,
      user: { id: rawUserId },
    });
    const { usePostHogIdentity } = await import('./usePostHogIdentity');
    renderHook(() => usePostHogIdentity());

    await act(async () => {
      await Promise.resolve();
    });

    // hashAccountID must have been called with the raw Clerk user_id
    expect(mockHashAccountID).toHaveBeenCalledWith(rawUserId);

    // app_user_identified must carry the result
    const identifiedCalls = mockTrackEvent.mock.calls.filter(
      ([e]: [{ name: string }]) => e.name === 'app_user_identified',
    );
    expect(identifiedCalls[0][0].properties.user_id).toBe('cafe0000cafe0000');
  });

  // T7 (Ray-mandated): unregisterSuperProperty('device_id') fires BEFORE resetIdentity on sign-out
  it('T7: unregisterSuperProperty(device_id) is called BEFORE resetIdentity on sign-out', async () => {
    mockUseUser.mockReturnValue({
      isLoaded: true,
      isSignedIn: true,
      user: { id: 'user_abc' },
    });
    const { usePostHogIdentity } = await import('./usePostHogIdentity');
    const { rerender } = renderHook(() => usePostHogIdentity());

    // Wire callLog AFTER the initial sign-in render so that sign-in-phase
    // device_id registration calls do not pollute the ordering log.
    const callLog: string[] = [];
    mockUnregisterSuperProperty.mockImplementation((name: string) => {
      callLog.push(`unregisterSuperProperty:${name}`);
    });
    mockResetIdentity.mockImplementation(() => {
      callLog.push('resetIdentity');
    });

    mockUseUser.mockReturnValue({ isLoaded: true, isSignedIn: false, user: null });
    rerender();

    const unregisterDeviceIdIdx = callLog.indexOf('unregisterSuperProperty:device_id');
    const resetIdx = callLog.indexOf('resetIdentity');
    expect(unregisterDeviceIdIdx).toBeGreaterThanOrEqual(0);
    expect(resetIdx).toBeGreaterThan(unregisterDeviceIdIdx);
  });

  // T6 (Ray-mandated): single-device → multi-device transition also triggers unregister
  it('T6: single-to-multi-device transition triggers unregisterSuperProperty(device_id)', async () => {
    // First render: 1 device → register device_id
    mockListDaemons.mockResolvedValueOnce({
      devices: [{ device_id: 'dev-uuid-001', platform: 'windows', daemon_ver: '1.0.0', paired_at: '2026-01-01T00:00:00Z', last_used_at: null }],
    });
    mockUseUser.mockReturnValue({
      isLoaded: true,
      isSignedIn: true,
      user: { id: 'user_abc' },
    });
    const { usePostHogIdentity } = await import('./usePostHogIdentity');
    const { rerender, unmount } = renderHook(() => usePostHogIdentity());

    await act(async () => {
      await Promise.resolve();
    });

    // device_id was registered from the single-device fetch
    const firstDeviceIdCall = mockRegisterSuperProperties.mock.calls.find(
      ([props]: [Record<string, unknown>]) => 'device_id' in props,
    );
    expect(firstDeviceIdCall).toBeDefined();

    // Sign out to reset identifiedRef, then sign back in with 2 devices
    mockListDaemons.mockResolvedValueOnce({
      devices: [
        { device_id: 'dev-uuid-001', platform: 'windows', daemon_ver: '1.0.0', paired_at: '2026-01-01T00:00:00Z', last_used_at: null },
        { device_id: 'dev-uuid-002', platform: 'macos', daemon_ver: '1.0.0', paired_at: '2026-01-02T00:00:00Z', last_used_at: null },
      ],
    });
    mockUseUser.mockReturnValue({ isLoaded: true, isSignedIn: false, user: null });
    rerender();

    mockUseUser.mockReturnValue({
      isLoaded: true,
      isSignedIn: true,
      user: { id: 'user_abc' },
    });
    rerender();

    await act(async () => {
      await Promise.resolve();
    });

    // After second sign-in with 2 devices, unregister must have been called
    // (at least once for the multi-device case — may also be called on sign-out)
    const unregisterCalls = mockUnregisterSuperProperty.mock.calls.filter(
      ([name]: [string]) => name === 'device_id',
    );
    expect(unregisterCalls.length).toBeGreaterThanOrEqual(1);
    // No register call with device_id after the second sign-in
    const deviceIdRegisterCallsAfterSecondSignIn = mockRegisterSuperProperties.mock.calls.filter(
      ([props]: [Record<string, unknown>]) => 'device_id' in props,
    );
    // Only the first sign-in should have registered device_id
    expect(deviceIdRegisterCallsAfterSecondSignIn).toHaveLength(1);
    unmount();
  });
});
