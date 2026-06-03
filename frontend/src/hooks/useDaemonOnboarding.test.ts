import { describe, it, expect, beforeEach, afterEach } from 'vitest';
import { renderHook, act } from '@testing-library/react';
import { useDaemonOnboarding, type AccountDataState } from './useDaemonOnboarding';

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

  describe('AccountDataState tri-state — auto-show gating', () => {
    it("'empty' + disconnected + signed-in = modal shows (genuine new user)", () => {
      const { result } = renderHook(() =>
        useDaemonOnboarding('disconnected', true, 'empty')
      );
      expect(result.current.isOpen).toBe(true);
    });

    it("'pending' + disconnected + signed-in = modal BLOCKED (fetch in flight)", () => {
      const { result } = renderHook(() =>
        useDaemonOnboarding('disconnected', true, 'pending')
      );
      expect(result.current.isOpen).toBe(false);
    });

    it("'has-data' + disconnected + signed-in = modal BLOCKED (returning user)", () => {
      const { result } = renderHook(() =>
        useDaemonOnboarding('disconnected', true, 'has-data')
      );
      expect(result.current.isOpen).toBe(false);
    });

    it("'pending' blocks modal even when daemon is disconnected — slow-fetch safety", () => {
      // Simulate: user loads the app, fetch is in flight ('pending'), daemon already
      // reports disconnected.  Modal must NOT pop while the fetch is unresolved.
      const { result, rerender } = renderHook(
        ({ state }: { state: AccountDataState }) =>
          useDaemonOnboarding('disconnected', true, state),
        { initialProps: { state: 'pending' as AccountDataState } }
      );

      // While 'pending' — no modal
      expect(result.current.isOpen).toBe(false);

      // After fetch resolves as 'empty' — modal should fire
      rerender({ state: 'empty' });
      expect(result.current.isOpen).toBe(true);
    });

    it("'pending' stays closed if fetch errors (fail-closed behavior)", () => {
      // Caller keeps state at 'pending' on error — modal never shows.
      const { result, rerender } = renderHook(
        ({ state }: { state: AccountDataState }) =>
          useDaemonOnboarding('disconnected', true, state),
        { initialProps: { state: 'pending' as AccountDataState } }
      );

      expect(result.current.isOpen).toBe(false);

      // Fetch errors → caller leaves state at 'pending'
      rerender({ state: 'pending' });
      expect(result.current.isOpen).toBe(false);
    });

    it("manual open() works regardless of accountDataState (user explicitly clicks status indicator)", () => {
      const { result } = renderHook(() =>
        useDaemonOnboarding('disconnected', true, 'has-data')
      );
      expect(result.current.isOpen).toBe(false);

      act(() => {
        result.current.open();
      });
      expect(result.current.isOpen).toBe(true);
    });

    it("manual open() works when state is 'pending' too", () => {
      const { result } = renderHook(() =>
        useDaemonOnboarding('disconnected', true, 'pending')
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
        useDaemonOnboarding('disconnected', true, 'empty')
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
        useDaemonOnboarding('disconnected', true, 'empty')
      );
      expect(result.current.isOpen).toBe(true);

      act(() => {
        result.current.dismiss();
      });

      expect(result.current.isOpen).toBe(false);
    });

    it('sets hasSeenOnboarding to true', () => {
      const { result } = renderHook(() =>
        useDaemonOnboarding('disconnected', true, 'empty')
      );

      act(() => {
        result.current.dismiss();
      });

      expect(result.current.hasSeenOnboarding).toBe(true);
    });

    it('persists dismissed state to localStorage', () => {
      const { result } = renderHook(() =>
        useDaemonOnboarding('disconnected', true, 'empty')
      );

      act(() => {
        result.current.dismiss();
      });

      expect(localStorage.getItem(STORAGE_KEY)).toBe('true');
    });

    it('prevents modal from re-appearing when daemon is still disconnected', () => {
      const { result, rerender } = renderHook(
        ({ status, state }: { status: 'disconnected' | 'connected'; state: AccountDataState }) =>
          useDaemonOnboarding(status, true, state),
        { initialProps: { status: 'disconnected' as const, state: 'empty' as AccountDataState } }
      );

      act(() => {
        result.current.dismiss();
      });

      // Re-render with same disconnected status — modal should stay closed
      rerender({ status: 'disconnected', state: 'empty' });
      expect(result.current.isOpen).toBe(false);
    });
  });

  // ── complete() ──────────────────────────────────────────────────────────────

  describe('complete()', () => {
    it('closes the modal', () => {
      const { result } = renderHook(() =>
        useDaemonOnboarding('disconnected', true, 'empty')
      );

      act(() => {
        result.current.complete();
      });

      expect(result.current.isOpen).toBe(false);
    });

    it('sets hasSeenOnboarding to true', () => {
      const { result } = renderHook(() =>
        useDaemonOnboarding('disconnected', true, 'empty')
      );

      act(() => {
        result.current.complete();
      });

      expect(result.current.hasSeenOnboarding).toBe(true);
    });

    it('persists completed state to localStorage', () => {
      const { result } = renderHook(() =>
        useDaemonOnboarding('disconnected', true, 'empty')
      );

      act(() => {
        result.current.complete();
      });

      expect(localStorage.getItem(STORAGE_COMPLETED_KEY)).toBe('true');
      expect(localStorage.getItem(STORAGE_KEY)).toBe('true');
    });
  });
});
