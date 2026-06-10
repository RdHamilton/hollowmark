import { describe, it, expect, beforeEach, afterEach } from 'vitest';
import { renderHook, act } from '@testing-library/react';
import { useDaemonOnboarding, type AccountDataState } from './useDaemonOnboarding';

// NOTE: useDaemonOnboarding now accepts an optional collectionMode parameter.
// When collectionMode is 'manual' (default), the auto-show gate is blocked
// so the daemon onboarding modal does NOT fire for manual-mode users
// (they see ManualImportModal instead, controlled by useCollectionMode).
// When collectionMode is 'enhanced' (or undefined for backward compat),
// the original 5-gate auto-show logic applies unchanged.

const STORAGE_KEY = 'vaultmtg_onboarding_dismissed';
const STORAGE_COMPLETED_KEY = 'vaultmtg_onboarding_completed';

/**
 * useDaemonOnboarding — tri-state AccountDataState tests
 *
 * The hook now takes (daemonStatus, isSignedIn, accountDataState: AccountDataState).
 * autoShow fires ONLY when accountDataState === 'empty' AND daemonStatus === 'disconnected'.
 * Both 'pending' and 'has-data' suppress the modal.
 */
describe('useDaemonOnboarding', () => {
  beforeEach(() => {
    localStorage.clear();
  });

  afterEach(() => {
    localStorage.clear();
  });

  // ── AccountDataState tri-state tests ────────────────────────────────────────

  // NOTE: All AccountDataState tri-state tests pass collectionMode='enhanced'
  // because the daemon onboarding modal is only for enhanced-mode users (#895 D3).

  describe('AccountDataState tri-state — auto-show gating (enhanced mode)', () => {
    it("'empty' + disconnected + signed-in + enhanced = modal shows (genuine new user)", () => {
      const { result } = renderHook(() =>
        useDaemonOnboarding('disconnected', true, 'empty', 'enhanced')
      );
      expect(result.current.isOpen).toBe(true);
    });

    it("'pending' + disconnected + signed-in + enhanced = modal BLOCKED (fetch in flight)", () => {
      const { result } = renderHook(() =>
        useDaemonOnboarding('disconnected', true, 'pending', 'enhanced')
      );
      expect(result.current.isOpen).toBe(false);
    });

    it("'has-data' + disconnected + signed-in + enhanced = modal BLOCKED (returning user)", () => {
      const { result } = renderHook(() =>
        useDaemonOnboarding('disconnected', true, 'has-data', 'enhanced')
      );
      expect(result.current.isOpen).toBe(false);
    });

    it("'pending' blocks modal even when daemon is disconnected — slow-fetch safety", () => {
      const { result, rerender } = renderHook(
        ({ state }: { state: AccountDataState }) =>
          useDaemonOnboarding('disconnected', true, state, 'enhanced'),
        { initialProps: { state: 'pending' as AccountDataState } }
      );

      expect(result.current.isOpen).toBe(false);

      rerender({ state: 'empty' });
      expect(result.current.isOpen).toBe(true);
    });

    it("'pending' stays closed if fetch errors (fail-closed behavior)", () => {
      const { result, rerender } = renderHook(
        ({ state }: { state: AccountDataState }) =>
          useDaemonOnboarding('disconnected', true, state, 'enhanced'),
        { initialProps: { state: 'pending' as AccountDataState } }
      );

      expect(result.current.isOpen).toBe(false);

      rerender({ state: 'pending' });
      expect(result.current.isOpen).toBe(false);
    });

    it("manual open() works regardless of accountDataState (user explicitly clicks status indicator)", () => {
      const { result } = renderHook(() =>
        useDaemonOnboarding('disconnected', true, 'has-data', 'enhanced')
      );
      expect(result.current.isOpen).toBe(false);

      act(() => {
        result.current.open();
      });
      expect(result.current.isOpen).toBe(true);
    });

    it("manual open() works when state is 'pending' too", () => {
      const { result } = renderHook(() =>
        useDaemonOnboarding('disconnected', true, 'pending', 'enhanced')
      );
      expect(result.current.isOpen).toBe(false);

      act(() => {
        result.current.open();
      });
      expect(result.current.isOpen).toBe(true);
    });
  });

  // ── collectionMode gate (#895 D3) ──────────────────────────────────────────
  // The daemon onboarding modal only fires in enhanced mode.
  // Manual-mode users are routed to ManualImportModal via useCollectionMode.

  describe('collectionMode gate', () => {
    it('does NOT auto-show when collectionMode is manual (default)', () => {
      const { result } = renderHook(() =>
        useDaemonOnboarding('disconnected', true, 'empty', 'manual')
      );
      expect(result.current.isOpen).toBe(false);
    });

    it('auto-shows when collectionMode is enhanced (all other gates pass)', () => {
      const { result } = renderHook(() =>
        useDaemonOnboarding('disconnected', true, 'empty', 'enhanced')
      );
      expect(result.current.isOpen).toBe(true);
    });

    it('defaults to blocking when collectionMode is not passed (backward compat: treats as manual)', () => {
      // Without a collectionMode arg, the hook treats it as 'manual' and blocks
      // the daemon modal — safer default now that manual is the D3 default.
      const { result } = renderHook(() =>
        useDaemonOnboarding('disconnected', true, 'empty')
      );
      // Explicit: isOpen should be false because no mode = manual default
      expect(result.current.isOpen).toBe(false);
    });

    it('manual open() still works regardless of collectionMode', () => {
      const { result } = renderHook(() =>
        useDaemonOnboarding('disconnected', true, 'empty', 'manual')
      );
      expect(result.current.isOpen).toBe(false);

      act(() => {
        result.current.open();
      });
      expect(result.current.isOpen).toBe(true);
    });
  });

  // ── Legacy auto-show behavior (daemonStatus gates) ──────────────────────────

  describe('auto-show behavior — daemonStatus gates', () => {
    it('does NOT show modal when daemon is connected (even if empty)', () => {
      const { result } = renderHook(() =>
        useDaemonOnboarding('connected', true, 'empty')
      );
      expect(result.current.isOpen).toBe(false);
    });

    it('does NOT show modal when daemon is loading', () => {
      const { result } = renderHook(() =>
        useDaemonOnboarding('loading', true, 'empty')
      );
      expect(result.current.isOpen).toBe(false);
    });

    it('does NOT show modal when daemon is in error state', () => {
      const { result } = renderHook(() =>
        useDaemonOnboarding('error', true, 'empty')
      );
      expect(result.current.isOpen).toBe(false);
    });

    it('does NOT show modal when user is NOT signed in', () => {
      const { result } = renderHook(() =>
        useDaemonOnboarding('disconnected', false, 'empty')
      );
      expect(result.current.isOpen).toBe(false);
    });

    it('does NOT show modal when previously dismissed (localStorage)', () => {
      localStorage.setItem(STORAGE_KEY, 'true');
      const { result } = renderHook(() =>
        useDaemonOnboarding('disconnected', true, 'empty')
      );
      expect(result.current.isOpen).toBe(false);
    });

    it('does NOT show modal when previously completed (localStorage)', () => {
      localStorage.setItem(STORAGE_COMPLETED_KEY, 'true');
      const { result } = renderHook(() =>
        useDaemonOnboarding('disconnected', true, 'empty')
      );
      expect(result.current.isOpen).toBe(false);
    });
  });

  // ── hasSeenOnboarding ───────────────────────────────────────────────────────

  describe('hasSeenOnboarding', () => {
    it('is false initially when no localStorage entry', () => {
      const { result } = renderHook(() =>
        useDaemonOnboarding('connected', true, 'empty')
      );
      expect(result.current.hasSeenOnboarding).toBe(false);
    });

    it('is true when dismissed key is set in localStorage', () => {
      localStorage.setItem(STORAGE_KEY, 'true');
      const { result } = renderHook(() =>
        useDaemonOnboarding('connected', true, 'empty')
      );
      expect(result.current.hasSeenOnboarding).toBe(true);
    });

    it('is true when completed key is set in localStorage', () => {
      localStorage.setItem(STORAGE_COMPLETED_KEY, 'true');
      const { result } = renderHook(() =>
        useDaemonOnboarding('connected', true, 'empty')
      );
      expect(result.current.hasSeenOnboarding).toBe(true);
    });
  });

  // ── open() ──────────────────────────────────────────────────────────────────

  describe('open()', () => {
    it('opens the modal regardless of daemon status', () => {
      const { result } = renderHook(() =>
        useDaemonOnboarding('connected', true, 'empty')
      );
      expect(result.current.isOpen).toBe(false);

      act(() => {
        result.current.open();
      });

      expect(result.current.isOpen).toBe(true);
    });

    it('can re-open after dismiss', () => {
      const { result } = renderHook(() =>
        useDaemonOnboarding('disconnected', true, 'empty', 'enhanced')
      );

      act(() => {
        result.current.dismiss();
      });
      expect(result.current.isOpen).toBe(false);

      act(() => {
        result.current.open();
      });
      expect(result.current.isOpen).toBe(true);
    });
  });

  // ── dismiss() ───────────────────────────────────────────────────────────────

  describe('dismiss()', () => {
    it('closes the modal', () => {
      const { result } = renderHook(() =>
        useDaemonOnboarding('disconnected', true, 'empty', 'enhanced')
      );
      expect(result.current.isOpen).toBe(true);

      act(() => {
        result.current.dismiss();
      });

      expect(result.current.isOpen).toBe(false);
    });

    it('sets hasSeenOnboarding to true', () => {
      const { result } = renderHook(() =>
        useDaemonOnboarding('disconnected', true, 'empty', 'enhanced')
      );

      act(() => {
        result.current.dismiss();
      });

      expect(result.current.hasSeenOnboarding).toBe(true);
    });

    it('persists dismissed state to localStorage', () => {
      const { result } = renderHook(() =>
        useDaemonOnboarding('disconnected', true, 'empty', 'enhanced')
      );

      act(() => {
        result.current.dismiss();
      });

      expect(localStorage.getItem(STORAGE_KEY)).toBe('true');
    });

    it('prevents modal from re-appearing when daemon is still disconnected', () => {
      const { result, rerender } = renderHook(
        ({ status, state }: { status: 'disconnected' | 'connected'; state: AccountDataState }) =>
          useDaemonOnboarding(status, true, state, 'enhanced'),
        { initialProps: { status: 'disconnected' as const, state: 'empty' as AccountDataState } }
      );

      act(() => {
        result.current.dismiss();
      });

      rerender({ status: 'disconnected', state: 'empty' });
      expect(result.current.isOpen).toBe(false);
    });
  });

  // ── complete() ──────────────────────────────────────────────────────────────

  describe('complete()', () => {
    it('closes the modal', () => {
      const { result } = renderHook(() =>
        useDaemonOnboarding('disconnected', true, 'empty', 'enhanced')
      );

      act(() => {
        result.current.complete();
      });

      expect(result.current.isOpen).toBe(false);
    });

    it('sets hasSeenOnboarding to true', () => {
      const { result } = renderHook(() =>
        useDaemonOnboarding('disconnected', true, 'empty', 'enhanced')
      );

      act(() => {
        result.current.complete();
      });

      expect(result.current.hasSeenOnboarding).toBe(true);
    });

    it('persists completed state to localStorage', () => {
      const { result } = renderHook(() =>
        useDaemonOnboarding('disconnected', true, 'empty', 'enhanced')
      );

      act(() => {
        result.current.complete();
      });

      expect(localStorage.getItem(STORAGE_COMPLETED_KEY)).toBe('true');
      expect(localStorage.getItem(STORAGE_KEY)).toBe('true');
    });
  });
});
