/**
 * Tests for useCollectionMode hook.
 *
 * Covers:
 * - Default mode is 'manual'
 * - setCollectionMode persists to localStorage
 * - isImportModalOpen fires when mode=manual + accountDataState=empty + no import_completed flag
 * - isImportModalOpen does NOT fire when mode=enhanced
 * - isImportModalOpen does NOT fire when import_completed=true
 * - markImportCompleted writes localStorage and closes the modal
 * - dismissImportModal closes the modal without writing import_completed
 */

import { describe, it, expect, beforeEach, afterEach } from 'vitest';
import { renderHook, act } from '@testing-library/react';
import { useCollectionMode } from './useCollectionMode';

const MODE_KEY = 'vaultmtg_collection_mode';
const IMPORT_COMPLETED_KEY = 'vaultmtg_import_completed';

function clearStorage() {
  try {
    localStorage.removeItem(MODE_KEY);
    localStorage.removeItem(IMPORT_COMPLETED_KEY);
  } catch {
    // ignore
  }
}

describe('useCollectionMode', () => {
  beforeEach(() => {
    clearStorage();
  });

  afterEach(() => {
    clearStorage();
  });

  it('returns manual as the default collection mode', () => {
    const { result } = renderHook(() =>
      useCollectionMode({ isSignedIn: true, accountDataState: 'empty' })
    );
    expect(result.current.collectionMode).toBe('manual');
  });

  it('reads persisted mode from localStorage on mount', () => {
    localStorage.setItem(MODE_KEY, 'enhanced');
    const { result } = renderHook(() =>
      useCollectionMode({ isSignedIn: true, accountDataState: 'empty' })
    );
    expect(result.current.collectionMode).toBe('enhanced');
  });

  it('setCollectionMode persists mode to localStorage', () => {
    const { result } = renderHook(() =>
      useCollectionMode({ isSignedIn: true, accountDataState: 'empty' })
    );
    act(() => {
      result.current.setCollectionMode('enhanced');
    });
    expect(localStorage.getItem(MODE_KEY)).toBe('enhanced');
    expect(result.current.collectionMode).toBe('enhanced');
  });

  it('isImportModalOpen is true for signed-in new user in manual mode', () => {
    const { result } = renderHook(() =>
      useCollectionMode({ isSignedIn: true, accountDataState: 'empty' })
    );
    expect(result.current.isImportModalOpen).toBe(true);
  });

  it('isImportModalOpen is false when accountDataState is pending', () => {
    const { result } = renderHook(() =>
      useCollectionMode({ isSignedIn: true, accountDataState: 'pending' })
    );
    expect(result.current.isImportModalOpen).toBe(false);
  });

  it('isImportModalOpen is false when accountDataState is has-data', () => {
    const { result } = renderHook(() =>
      useCollectionMode({ isSignedIn: true, accountDataState: 'has-data' })
    );
    expect(result.current.isImportModalOpen).toBe(false);
  });

  it('isImportModalOpen is false when user is not signed in', () => {
    const { result } = renderHook(() =>
      useCollectionMode({ isSignedIn: false, accountDataState: 'empty' })
    );
    expect(result.current.isImportModalOpen).toBe(false);
  });

  it('isImportModalOpen is false when mode is enhanced', () => {
    localStorage.setItem(MODE_KEY, 'enhanced');
    const { result } = renderHook(() =>
      useCollectionMode({ isSignedIn: true, accountDataState: 'empty' })
    );
    expect(result.current.isImportModalOpen).toBe(false);
  });

  it('isImportModalOpen is false when import_completed flag is set', () => {
    localStorage.setItem(IMPORT_COMPLETED_KEY, 'true');
    const { result } = renderHook(() =>
      useCollectionMode({ isSignedIn: true, accountDataState: 'empty' })
    );
    expect(result.current.isImportModalOpen).toBe(false);
  });

  it('markImportCompleted writes the flag and closes the modal', () => {
    const { result } = renderHook(() =>
      useCollectionMode({ isSignedIn: true, accountDataState: 'empty' })
    );
    expect(result.current.isImportModalOpen).toBe(true);

    act(() => {
      result.current.markImportCompleted();
    });

    expect(localStorage.getItem(IMPORT_COMPLETED_KEY)).toBe('true');
    expect(result.current.isImportModalOpen).toBe(false);
  });

  it('dismissImportModal closes the modal without writing import_completed', () => {
    const { result } = renderHook(() =>
      useCollectionMode({ isSignedIn: true, accountDataState: 'empty' })
    );
    expect(result.current.isImportModalOpen).toBe(true);

    act(() => {
      result.current.dismissImportModal();
    });

    expect(localStorage.getItem(IMPORT_COMPLETED_KEY)).toBeNull();
    expect(result.current.isImportModalOpen).toBe(false);
  });

  it('openImportModal re-opens the modal after it was dismissed', () => {
    const { result } = renderHook(() =>
      useCollectionMode({ isSignedIn: true, accountDataState: 'empty' })
    );
    act(() => {
      result.current.dismissImportModal();
    });
    expect(result.current.isImportModalOpen).toBe(false);

    act(() => {
      result.current.openImportModal();
    });
    expect(result.current.isImportModalOpen).toBe(true);
  });
});
