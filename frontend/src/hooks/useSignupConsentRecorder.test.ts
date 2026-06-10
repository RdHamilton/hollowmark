/**
 * Tests for useSignupConsentRecorder (COPPA #884).
 *
 * The hook must:
 *   1. Fire recordSignupConsent() exactly once for a user whose account
 *      was created within the recency window (< 60 s) AND has no
 *      localStorage guard set.
 *   2. NOT fire for a returning user whose localStorage guard is already set.
 *   3. NOT fire again in a fresh tab (localStorage survives tab close;
 *      the regression from Ray's A4 amendment).
 *   4. NOT fire when the user is not signed in.
 *   5. NOT fire when Clerk has not finished loading.
 *   6. Expose { status: 'idle' | 'loading' | 'done' | 'error' } and
 *      retry() so ConsentGate can render appropriate states.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, act, waitFor } from '@testing-library/react';

// ── module-level mocks ──────────────────────────────────────────────────────
const mockRecordSignupConsent = vi.fn();
vi.mock('@/services/api/consent', () => ({
  recordSignupConsent: () => mockRecordSignupConsent(),
}));

const mockUseUser = vi.fn();
vi.mock('@clerk/react', () => ({
  useUser: () => mockUseUser(),
}));

import { useSignupConsentRecorder } from './useSignupConsentRecorder';

// ── localStorage helpers ────────────────────────────────────────────────────
const LS_PREFIX = 'vaultmtg_consent_signup_v1_';
const userId = 'user_abc123';
const lsKey = `${LS_PREFIX}${userId}`;

function clearLocalStorage() {
  localStorage.removeItem(lsKey);
}

// ── time helpers ─────────────────────────────────────────────────────────────
const NOW = Date.now();
const RECENT_CREATED_AT = new Date(NOW - 30_000); // 30 s ago — within window
const OLD_CREATED_AT = new Date(NOW - 120_000);   // 2 min ago — outside window

// ── signed-out / unloaded states ─────────────────────────────────────────────
const signedOut = () =>
  mockUseUser.mockReturnValue({ isLoaded: true, isSignedIn: false, user: null });

const notLoaded = () =>
  mockUseUser.mockReturnValue({ isLoaded: false, isSignedIn: false, user: null });

function signedIn(overrides: { createdAt?: Date } = {}) {
  mockUseUser.mockReturnValue({
    isLoaded: true,
    isSignedIn: true,
    user: {
      id: userId,
      createdAt: overrides.createdAt ?? RECENT_CREATED_AT,
    },
  });
}

// ─────────────────────────────────────────────────────────────────────────────

describe('useSignupConsentRecorder', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    clearLocalStorage();
    vi.setSystemTime(NOW);
  });

  afterEach(() => {
    clearLocalStorage();
    vi.useRealTimers();
  });

  // ── status: idle when not signed in ────────────────────────────────────────
  it('returns idle status when user is not signed in', () => {
    signedOut();
    const { result } = renderHook(() => useSignupConsentRecorder());
    expect(result.current.status).toBe('idle');
    expect(mockRecordSignupConsent).not.toHaveBeenCalled();
  });

  it('returns idle status while Clerk is still loading', () => {
    notLoaded();
    const { result } = renderHook(() => useSignupConsentRecorder());
    expect(result.current.status).toBe('idle');
    expect(mockRecordSignupConsent).not.toHaveBeenCalled();
  });

  // ── new user: fires once ────────────────────────────────────────────────────
  it('calls recordSignupConsent once for a new user (createdAt < 60 s, no LS guard)', async () => {
    mockRecordSignupConsent.mockResolvedValueOnce(undefined);
    signedIn({ createdAt: RECENT_CREATED_AT });

    const { result } = renderHook(() => useSignupConsentRecorder());

    await waitFor(() => expect(result.current.status).toBe('done'));
    expect(mockRecordSignupConsent).toHaveBeenCalledOnce();
    expect(localStorage.getItem(lsKey)).toBe('1');
  });

  // ── returning user: localStorage guard set ──────────────────────────────────
  it('does NOT call recordSignupConsent when localStorage guard is already set', async () => {
    localStorage.setItem(lsKey, '1');
    signedIn({ createdAt: RECENT_CREATED_AT });

    const { result } = renderHook(() => useSignupConsentRecorder());

    // Should immediately be 'done' (guard already set)
    expect(result.current.status).toBe('done');
    expect(mockRecordSignupConsent).not.toHaveBeenCalled();
  });

  // ── Ray's A4 regression: returning user in fresh tab ────────────────────────
  it('REGRESSION A4: returning user in a fresh tab does NOT re-POST (localStorage survives tab close)', async () => {
    // Simulate a fresh tab: sessionStorage is empty but localStorage has the guard
    localStorage.setItem(lsKey, '1');
    // Account is old (returning user)
    signedIn({ createdAt: OLD_CREATED_AT });

    const { result } = renderHook(() => useSignupConsentRecorder());

    // Must never fire
    expect(result.current.status).toBe('done');
    expect(mockRecordSignupConsent).not.toHaveBeenCalled();
  });

  // ── old account without guard: do NOT fire ─────────────────────────────────
  it('does NOT call recordSignupConsent for an account older than the recency window', async () => {
    // No LS guard, but account is old — could be a pre-feature user; don't back-fill
    signedIn({ createdAt: OLD_CREATED_AT });

    const { result } = renderHook(() => useSignupConsentRecorder());

    expect(result.current.status).toBe('done');
    expect(mockRecordSignupConsent).not.toHaveBeenCalled();
  });

  // ── error state ─────────────────────────────────────────────────────────────
  it('sets status to error when recordSignupConsent rejects', async () => {
    mockRecordSignupConsent.mockRejectedValueOnce(new Error('BFF down'));
    signedIn({ createdAt: RECENT_CREATED_AT });

    const { result } = renderHook(() => useSignupConsentRecorder());

    await waitFor(() => expect(result.current.status).toBe('error'));
    // Guard must NOT be set on failure so a retry is possible
    expect(localStorage.getItem(lsKey)).toBeNull();
  });

  // ── retry ────────────────────────────────────────────────────────────────────
  it('retry() re-fires recordSignupConsent after an error and sets status to done on success', async () => {
    mockRecordSignupConsent
      .mockRejectedValueOnce(new Error('transient'))
      .mockResolvedValueOnce(undefined);
    signedIn({ createdAt: RECENT_CREATED_AT });

    const { result } = renderHook(() => useSignupConsentRecorder());

    await waitFor(() => expect(result.current.status).toBe('error'));

    await act(async () => {
      result.current.retry();
    });

    await waitFor(() => expect(result.current.status).toBe('done'));
    expect(mockRecordSignupConsent).toHaveBeenCalledTimes(2);
    expect(localStorage.getItem(lsKey)).toBe('1');
  });

  // ── idempotency: double-mount doesn't double-fire ────────────────────────────
  it('does NOT fire twice if the hook re-renders before the first call resolves', async () => {
    let resolveFirst!: () => void;
    mockRecordSignupConsent.mockReturnValueOnce(
      new Promise<void>((res) => { resolveFirst = res; })
    );
    signedIn({ createdAt: RECENT_CREATED_AT });

    const { result, rerender } = renderHook(() => useSignupConsentRecorder());

    // Trigger a re-render while the promise is still in flight
    rerender();

    // Now resolve the first (and only) call
    await act(async () => { resolveFirst(); });

    await waitFor(() => expect(result.current.status).toBe('done'));
    expect(mockRecordSignupConsent).toHaveBeenCalledOnce();
  });
});
