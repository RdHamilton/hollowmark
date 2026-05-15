import { useState, useEffect } from 'react';
import { useAuth } from '@clerk/react';
import { getDaemonHealth } from '@/services/api/bffHealth';
import { gui } from '@/types/models';

export interface UseDaemonConnectionReturn {
  /** Current connection status */
  connectionStatus: gui.ConnectionStatus;
}

const defaultConnectionStatus = new gui.ConnectionStatus({
  status: 'standalone',
  connected: false,
  mode: 'standalone',
  url: 'ws://localhost:9999',
  port: 9999,
});

export function useDaemonConnection(): UseDaemonConnectionReturn {
  const { getToken, isSignedIn } = useAuth();
  const [connectionStatus, setConnectionStatus] = useState<gui.ConnectionStatus>(defaultConnectionStatus);

  // Poll the BFF daemon health endpoint regardless of desktop/browser context.
  // DaemonHealthIndicator (nav bar) uses the same endpoint without an
  // isDesktopApp() guard — removing the guard here ensures both indicators
  // always read from the same source of truth (#2020 / #2021).
  useEffect(() => {
    if (!isSignedIn) {
      return;
    }

    let cancelled = false;

    const fetchStatus = async () => {
      try {
        const token = await getToken();
        if (!token || cancelled) return;

        const result = await getDaemonHealth(token);
        if (cancelled) return;

        const connected = result.status === 'connected';
        setConnectionStatus(
          gui.ConnectionStatus.createFrom({
            status: result.status,
            connected,
            mode: connected ? 'daemon' : 'standalone',
            url: 'ws://localhost:9999',
            port: 9999,
          }),
        );
      } catch {
        // Connection status load failed silently - UI will show default state
      }
    };

    fetchStatus();

    return () => {
      cancelled = true;
    };
  }, [getToken, isSignedIn]);

  return {
    connectionStatus,
  };
}
