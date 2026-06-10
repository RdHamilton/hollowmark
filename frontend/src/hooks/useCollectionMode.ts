/**
 * useCollectionMode
 *
 * Manages the user's collection mode preference (manual vs. enhanced) and
 * controls the manual-import modal auto-show for new users.
 *
 * Q2 ruling (Ray): extracted as a separate hook so useDaemonOnboarding stays
 * focused on daemon connection state.
 *
 * Q5 ruling (Ray): on successful import with zero matches, suppress re-fire
 * via an import_completed flag here — do NOT mutate accountDataState.
 *
 * Q4 ruling (Ray): #895 writes localStorage only — NO consent record, NO DB
 * write, NO C-18 handshake (that is #893's contract).
 */

import { useState, useCallback } from 'react';
import type { AccountDataState } from './useDaemonOnboarding';

export type CollectionMode = 'manual' | 'enhanced';

/** localStorage key for the user's collection mode preference. */
export const COLLECTION_MODE_KEY = 'vaultmtg_collection_mode';

/** localStorage key set after a successful import to suppress re-fire. */
export const IMPORT_COMPLETED_KEY = 'vaultmtg_import_completed';

function readMode(): CollectionMode {
  try {
    const stored = localStorage.getItem(COLLECTION_MODE_KEY);
    if (stored === 'enhanced') return 'enhanced';
  } catch {
    // ignore storage errors
  }
  return 'manual';
}

function readImportCompleted(): boolean {
  try {
    return localStorage.getItem(IMPORT_COMPLETED_KEY) === 'true';
  } catch {
    return false;
  }
}

export interface UseCollectionModeOptions {
  isSignedIn: boolean;
  accountDataState: AccountDataState;
}

export interface UseCollectionModeResult {
  /** Current collection mode, defaults to 'manual'. */
  collectionMode: CollectionMode;
  /** Update and persist the collection mode. */
  setCollectionMode: (mode: CollectionMode) => void;
  /**
   * Whether the manual-import modal should auto-show.
   *
   * True only when ALL of the following hold:
   *   1. User is signed in
   *   2. accountDataState === 'empty' (genuine new user, positively confirmed)
   *   3. collectionMode === 'manual'
   *   4. import_completed flag is not set
   *   5. Not manually dismissed this session
   */
  isImportModalOpen: boolean;
  /** Mark that a successful import completed — suppresses re-fire. */
  markImportCompleted: () => void;
  /** Dismiss the modal this session without writing the completed flag. */
  dismissImportModal: () => void;
  /** Manually re-open the modal (e.g. from a CTA). */
  openImportModal: () => void;
}

/**
 * Hook that owns collection mode preference and manual-import modal visibility.
 */
export function useCollectionMode({
  isSignedIn,
  accountDataState,
}: UseCollectionModeOptions): UseCollectionModeResult {
  const [collectionMode, setCollectionModeState] = useState<CollectionMode>(readMode);
  const [importCompleted, setImportCompleted] = useState(readImportCompleted);
  const [manuallyDismissed, setManuallyDismissed] = useState(false);
  const [manuallyOpened, setManuallyOpened] = useState(false);

  const setCollectionMode = useCallback((mode: CollectionMode) => {
    setCollectionModeState(mode);
    try {
      localStorage.setItem(COLLECTION_MODE_KEY, mode);
    } catch {
      // ignore storage errors
    }
  }, []);

  // Auto-show gates:
  //   1. signed in
  //   2. accountDataState positively confirmed empty (not 'pending' or 'has-data')
  //   3. collectionMode is 'manual'
  //   4. import_completed flag not set
  //   5. not manually dismissed this session
  const autoShow =
    isSignedIn &&
    accountDataState === 'empty' &&
    collectionMode === 'manual' &&
    !importCompleted &&
    !manuallyDismissed;

  const isImportModalOpen = manuallyOpened || autoShow;

  const markImportCompleted = useCallback(() => {
    setImportCompleted(true);
    setManuallyOpened(false);
    try {
      localStorage.setItem(IMPORT_COMPLETED_KEY, 'true');
    } catch {
      // ignore storage errors
    }
  }, []);

  const dismissImportModal = useCallback(() => {
    setManuallyOpened(false);
    setManuallyDismissed(true);
  }, []);

  const openImportModal = useCallback(() => {
    setManuallyOpened(true);
    setManuallyDismissed(false);
  }, []);

  return {
    collectionMode,
    setCollectionMode,
    isImportModalOpen,
    markImportCompleted,
    dismissImportModal,
    openImportModal,
  };
}
