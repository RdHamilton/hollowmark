import { describe, it, expect, vi, beforeEach } from 'vitest';
import { renderHook, waitFor } from '@testing-library/react';
import { useDaemonConnection } from './useDaemonConnection';

// Mock Clerk useAuth
vi.mock('@clerk/react', () => ({
  useAuth: vi.fn(),
}));

// Mock the BFF health adapter
vi.mock('@/services/api/bffHealth', () => ({
  getDaemonHealth: vi.fn(),
}));

import { useAuth } from '@clerk/react';
import { getDaemonHealth } from '@/services/api/bffHealth';

const mockUseAuth = vi.mocked(useAuth);
const mockGetDaemonHealth = vi.mocked(getDaemonHealth);

/** Helper: set up Clerk mock as signed-in with a valid token. */
function signedInAuth(token = 'test-token') {
  mockUseAuth.mockReturnValue({
    getToken: vi.fn().mockResolvedValue(token),
    isSignedIn: true,
  } as unknown as ReturnType<typeof useAuth>);
}

/** Helper: set up Clerk mock as signed-out. */
function signedOutAuth() {
  mockUseAuth.mockReturnValue({
    getToken: vi.fn().mockResolvedValue(null),
    isSignedIn: false,
  } as unknown as ReturnType<typeof useAuth>);
}

describe('useDaemonConnection', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    // Default: signed in, BFF returns disconnected.
    signedInAuth();
    mockGetDaemonHealth.mockResolvedValue({ status: 'disconnected' });
  });

  describe('initial state', () => {
    it('returns default connection status', () => {
      const { result } = renderHook(() => useDaemonConnection());

      expect(result.current.connectionStatus.status).toBe('standalone');
      expect(result.current.connectionStatus.connected).toBe(false);
      expect(result.current.connectionStatus.mode).toBe('standalone');
      expect(result.current.connectionStatus.port).toBe(9999);
    });

    // AC7: no-op handlers and derived state are removed from the hook.
    it('does not expose daemonMode (AC7)', () => {
      const { result } = renderHook(() => useDaemonConnection());
      expect((result.current as any).daemonMode).toBeUndefined();
    });

    it('does not expose daemonPort (AC7)', () => {
      const { result } = renderHook(() => useDaemonConnection());
      expect((result.current as any).daemonPort).toBeUndefined();
    });

    it('does not expose isReconnecting (AC7)', () => {
      const { result } = renderHook(() => useDaemonConnection());
      expect((result.current as any).isReconnecting).toBeUndefined();
    });

    it('does not expose handleDaemonPortChange (AC7)', () => {
      const { result } = renderHook(() => useDaemonConnection());
      expect((result.current as any).handleDaemonPortChange).toBeUndefined();
    });

    it('does not expose handleReconnect (AC7)', () => {
      const { result } = renderHook(() => useDaemonConnection());
      expect((result.current as any).handleReconnect).toBeUndefined();
    });

    it('does not expose handleModeChange (AC7)', () => {
      const { result } = renderHook(() => useDaemonConnection());
      expect((result.current as any).handleModeChange).toBeUndefined();
    });
  });

  // AC2 (#2020 / #2021): useDaemonConnection uses the same BFF endpoint as DaemonHealthIndicator
  // regardless of desktop/browser context — no isDesktopApp() guard.
  describe('polls BFF health in all contexts — single source of truth (#2020)', () => {
    it('calls getDaemonHealth on mount when signed in (browser context)', async () => {
      // Browser context — previously this was gated by isDesktopApp(); now it must fire.
      mockGetDaemonHealth.mockResolvedValueOnce({ status: 'connected' });

      const { result } = renderHook(() => useDaemonConnection());

      await waitFor(() => {
        expect(mockGetDaemonHealth).toHaveBeenCalledWith('test-token');
      });

      expect(result.current.connectionStatus.status).toBe('connected');
      expect(result.current.connectionStatus.connected).toBe(true);
    });

    it('reflects connected status from BFF and sets connected=true', async () => {
      mockGetDaemonHealth.mockResolvedValueOnce({ status: 'connected' });

      const { result } = renderHook(() => useDaemonConnection());

      await waitFor(() => {
        expect(mockGetDaemonHealth).toHaveBeenCalled();
      });

      expect(result.current.connectionStatus.status).toBe('connected');
      expect(result.current.connectionStatus.connected).toBe(true);
      expect(result.current.connectionStatus.mode).toBe('daemon');
    });

    it('reflects disconnected status from BFF', async () => {
      mockGetDaemonHealth.mockResolvedValueOnce({ status: 'disconnected' });

      const { result } = renderHook(() => useDaemonConnection());

      await waitFor(() => {
        expect(mockGetDaemonHealth).toHaveBeenCalled();
      });

      expect(result.current.connectionStatus.connected).toBe(false);
    });

    it('handles BFF health error silently and keeps default state', async () => {
      mockGetDaemonHealth.mockRejectedValueOnce(new Error('network error'));

      const { result } = renderHook(() => useDaemonConnection());

      await waitFor(() => {
        expect(mockGetDaemonHealth).toHaveBeenCalled();
      });

      // On error we keep the default state (standalone/not-connected).
      expect(result.current.connectionStatus.status).toBe('standalone');
    });

    it('does NOT call getDaemonHealth when signed out', async () => {
      signedOutAuth();

      renderHook(() => useDaemonConnection());

      // Give any async effects a chance to run.
      await new Promise((r) => setTimeout(r, 50));

      expect(mockGetDaemonHealth).not.toHaveBeenCalled();
    });
  });

  // #112: authStatus return value
  describe('authStatus return value (#112)', () => {
    it('is undefined before first fetch completes', () => {
      mockGetDaemonHealth.mockReturnValue(new Promise(() => {})); // never resolves

      const { result } = renderHook(() => useDaemonConnection());

      expect(result.current.authStatus).toBeUndefined();
    });

    it('returns auth_status from BFF response after fetch', async () => {
      mockGetDaemonHealth.mockResolvedValueOnce({
        status: 'connected',
        auth_status: 'authenticated',
      });

      const { result } = renderHook(() => useDaemonConnection());

      await waitFor(() => {
        expect(result.current.authStatus).toBe('authenticated');
      });
    });

    it('returns auth_status: unknown (neutral, not error) from BFF response', async () => {
      mockGetDaemonHealth.mockResolvedValueOnce({
        status: 'disconnected',
        auth_status: 'unknown',
      });

      const { result } = renderHook(() => useDaemonConnection());

      await waitFor(() => {
        expect(result.current.authStatus).toBe('unknown');
      });
    });

    it('remains undefined on BFF error (hook handles silently)', async () => {
      mockGetDaemonHealth.mockRejectedValueOnce(new Error('network error'));

      const { result } = renderHook(() => useDaemonConnection());

      await waitFor(() => {
        expect(mockGetDaemonHealth).toHaveBeenCalled();
      });

      expect(result.current.authStatus).toBeUndefined();
    });
  });
});
