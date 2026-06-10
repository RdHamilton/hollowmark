/**
 * useSignupConsentRecorder — fires POST /api/v1/account/consent exactly once
 * for a newly created account (COPPA #884).
 *
 * New-user detection (Ray's A4 amendment):
 *   - Uses a durable localStorage key keyed to the Clerk user ID so the guard
 *     survives tab close — a returning user in a fresh tab must NOT re-POST.
 *   - Also gates on `user.createdAt` recency (< 60 s) so pre-feature existing
 *     accounts are not back-filled.
 *   - Both conditions must be true for the call to fire.  If the localStorage
 *     guard is already set the call is skipped regardless of createdAt.
 *
 * Guard lifecycle:
 *   - Set to '1' after a successful POST.
 *   - NOT set on failure — leaves room for a retry.
 *
 * Returns { status, retry }:
 *   - 'idle'    — user is not signed in (or Clerk has not loaded)
 *   - 'loading' — POST is in-flight
 *   - 'done'    — POST succeeded (or was skipped — guard already set / old account)
 *   - 'error'   — POST failed; caller should render a Retry UI
 */

import { useEffect, useRef, useState, useCallback } from 'react';
import { useUser } from '@clerk/react';
import { recordSignupConsent } from '@/services/api/consent';

export type ConsentStatus = 'idle' | 'loading' | 'done' | 'error';

const LS_PREFIX = 'vaultmtg_consent_signup_v1_';
const RECENCY_WINDOW_MS = 60_000; // 60 seconds

function lsKey(userId: string): string {
  return `${LS_PREFIX}${userId}`;
}

function isConsentGuardSet(userId: string): boolean {
  try {
    return localStorage.getItem(lsKey(userId)) === '1';
  } catch {
    return false;
  }
}

function setConsentGuard(userId: string): void {
  try {
    localStorage.setItem(lsKey(userId), '1');
  } catch {
    // Storage unavailable — guard not written; the hook will retry on next mount.
  }
}

function isNewAccount(createdAt: Date | null | undefined): boolean {
  if (!createdAt) return false;
  return Date.now() - createdAt.getTime() < RECENCY_WINDOW_MS;
}

export interface SignupConsentRecorderResult {
  status: ConsentStatus;
  retry: () => void;
}

export function useSignupConsentRecorder(): SignupConsentRecorderResult {
  const { isLoaded, isSignedIn, user } = useUser();
  const [status, setStatus] = useState<ConsentStatus>('idle');
  const inFlightRef = useRef(false);

  const fireConsent = useCallback(async (userId: string) => {
    if (inFlightRef.current) return;
    inFlightRef.current = true;
    setStatus('loading');
    try {
      await recordSignupConsent();
      setConsentGuard(userId);
      setStatus('done');
    } catch {
      setStatus('error');
    } finally {
      inFlightRef.current = false;
    }
  }, []);

  useEffect(() => {
    if (!isLoaded || !isSignedIn || !user?.id) {
      // Not ready or not signed in — keep 'idle'.
      return;
    }

    const userId = user.id;

    // Guard already set: returning user — skip without firing.
    if (isConsentGuardSet(userId)) {
      setStatus('done');
      return;
    }

    // Account is not new: pre-feature user or returning user without guard
    // (edge case: user who signed up before this feature shipped).
    // Do not back-fill consent — set done so the gate passes through.
    if (!isNewAccount(user.createdAt as Date | null | undefined)) {
      setStatus('done');
      return;
    }

    void fireConsent(userId);
    // fireConsent is stable (useCallback with no deps that change).
    // We intentionally do not list user.createdAt here — we only want to run
    // this effect when the user ID changes (i.e., a different user signs in).
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isLoaded, isSignedIn, user?.id, fireConsent]);

  const retry = useCallback(() => {
    if (!user?.id || status !== 'error') return;
    void fireConsent(user.id);
  }, [user?.id, status, fireConsent]);

  return { status, retry };
}
