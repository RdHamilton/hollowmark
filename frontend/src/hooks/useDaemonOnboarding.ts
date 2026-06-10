/**
 * useDaemonOnboarding
 *
 * Manages onboarding modal visibility for new users who haven't connected
 * the daemon yet.
 *
 * Rules:
 * - Show modal ONLY when accountDataState === 'empty' AND daemonStatus === 'disconnected'
 *   AND collectionMode === 'enhanced' (#895 D3: daemon modal only fires in enhanced mode;
 *   manual-mode new users see ManualImportModal instead, controlled by useCollectionMode)
 * - 'pending' (fetch in flight or errored) and 'has-data' (returning user) both block the modal
 * - On summary fetch error → caller keeps state at 'pending' (fail-closed; no pop for uncertain state)
 * - 'has-data' is persisted to sessionStorage by Layout.tsx to survive route navigation
 * - "Dismissed" state is stored in localStorage so it persists across sessions
 * - User can manually re-open via the status indicator in the nav
 * - Once daemon connects (status = connected), the modal auto-completes
 */

import { useState, useCallback } from 'react';
import type { CollectionMode } from './useCollectionMode';

const STORAGE_KEY = 'vaultmtg_onboarding_dismissed';
const STORAGE_COMPLETED_KEY = 'vaultmtg_onboarding_completed';

/** Session-level key that caches the 'has-data' state to prevent route-navigation re-races. */
export const SESSION_HAS_ACCOUNT_DATA_KEY = 'vaultmtg_has_account_data';

/**
 * Tri-state that represents whether the account has existing BFF data.
 *
 * - 'pending'  — fetch not yet resolved (or errored); fail-closed: block the modal
 * - 'has-data' — getHomeSummary resolved with all_time.matches > 0; returning user
 * - 'empty'    — getHomeSummary positively confirmed 0 matches; genuine new user
 */
export type AccountDataState = 'pending' | 'has-data' | 'empty';

export type DaemonOnboardingStatus = 'connected' | 'disconnected' | 'reconnecting' | 'loading' | 'error';

export interface UseDaemonOnboardingResult {
  /** Whether the onboarding modal should be shown */
  isOpen: boolean;
  /** Open the onboarding modal (e.g. from the status indicator) */
  open: () => void;
  /** Dismiss the modal without completing */
  dismiss: () => void;
  /** Mark onboarding as fully completed (daemon connected) */
  complete: () => void;
  /** Whether the user has previously dismissed or completed onboarding */
  hasSeenOnboarding: boolean;
}

function readHasSeen(): boolean {
  try {
    return (
      localStorage.getItem(STORAGE_KEY) === 'true' ||
      localStorage.getItem(STORAGE_COMPLETED_KEY) === 'true'
    );
  } catch {
    return false;
  }
}

/**
 * Hook that controls onboarding modal visibility based on daemon status,
 * account data state, collection mode, and whether the user has previously
 * seen/dismissed the modal.
 *
 * @param daemonStatus      Current daemon health status from the health indicator
 * @param isSignedIn        Whether the user is signed in (from Clerk useAuth)
 * @param accountDataState  Tri-state resolved by getHomeSummary in Layout.tsx
 * @param collectionMode    'manual' | 'enhanced' — daemon modal only fires in enhanced mode (#895 D3)
 */
export function useDaemonOnboarding(
  daemonStatus: DaemonOnboardingStatus,
  isSignedIn: boolean,
  accountDataState: AccountDataState,
  collectionMode: CollectionMode = 'manual'
): UseDaemonOnboardingResult {
  // manualOpen: true when the user explicitly opens the modal
  // manualClosed: true when the user has explicitly dismissed it this session
  const [manualOpen, setManualOpen] = useState(false);
  const [manualClosed, setManualClosed] = useState(false);
  const [hasSeenOnboarding, setHasSeenOnboarding] = useState(readHasSeen);

  // Auto-show condition: all SIX gates must be satisfied.
  //   1. User is signed in
  //   2. Account data is positively confirmed empty (NOT 'pending' or 'has-data')
  //   3. Daemon is disconnected
  //   4. Not previously seen/dismissed
  //   5. Not manually closed this session
  //   6. collectionMode === 'enhanced' (#895 D3: manual-mode users see ManualImportModal)
  //
  // 'pending' deliberately blocks the modal so that a slow network does not
  // flash the first-run flow at a returning user before the fetch resolves.
  // 'has-data' suppresses the modal unconditionally for returning users.
  const autoShow =
    isSignedIn &&
    accountDataState === 'empty' &&
    daemonStatus === 'disconnected' &&
    !hasSeenOnboarding &&
    !manualClosed &&
    collectionMode === 'enhanced';

  const isOpen = manualOpen || autoShow;

  const open = useCallback(() => {
    setManualOpen(true);
    setManualClosed(false);
  }, []);

  const dismiss = useCallback(() => {
    setManualOpen(false);
    setManualClosed(true);
    setHasSeenOnboarding(true);
    try {
      localStorage.setItem(STORAGE_KEY, 'true');
    } catch {
      // ignore storage errors
    }
  }, []);

  const complete = useCallback(() => {
    setManualOpen(false);
    setManualClosed(true);
    setHasSeenOnboarding(true);
    try {
      localStorage.setItem(STORAGE_COMPLETED_KEY, 'true');
      localStorage.setItem(STORAGE_KEY, 'true');
    } catch {
      // ignore storage errors
    }
  }, []);

  return { isOpen, open, dismiss, complete, hasSeenOnboarding };
}
