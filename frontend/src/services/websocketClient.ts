/**
 * SSE client for real-time event handling.
 *
 * Uses fetch-based SSE (not EventSource) so we can send the
 * Authorization: Bearer <jwt> header that the BFF requires.  The fetch
 * transport means we don't need the ?token= query-param fallback that the
 * EventSource path in useDraftEventStream uses (issue #1904).
 *
 * Auth: pulls the Clerk session JWT from apiClient.getClerkToken() on every
 * (re)connect so rotated tokens get picked up by the existing backoff loop.
 *
 * The public API (EventsOn, EventsOff, EventsEmit, connect, disconnect)
 * is identical to the old WebSocket client so every call site works unchanged.
 */

import { getClerkToken } from './apiClient';

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

export interface WebSocketConfig {
  url: string;
  /** Base delay for the first reconnect attempt (ms). Defaults to 1000. */
  reconnectBaseDelay?: number;
  /** Maximum reconnect delay cap (ms). Defaults to 30 000. */
  reconnectMaxDelay?: number;
  /**
   * @deprecated Ignored — reconnect never permanently gives up (ADR-084 §3).
   * Kept in the interface only for backward-compat with call sites that set it;
   * the value is never read.
   */
  maxReconnectAttempts?: number;
  /** @deprecated Use reconnectBaseDelay. Kept for backward-compat; ignored. */
  reconnectInterval?: number;
}

export interface WebSocketEvent {
  type: string;
  data: unknown;
}

type EventCallback = (data: unknown) => void;
type ConnectionStateCallback = (connected: boolean) => void;

// ---------------------------------------------------------------------------
// Module-level state
// ---------------------------------------------------------------------------

// ADR-077: URL placeholder — overridden at boot by configureWebSocket() in
// main.tsx after loadConfig() populates runtimeConfig. The placeholder
// 'http://localhost:8080/api/v1/events' is only ever used in local dev when
// config.json is unavailable and the DEV fallback path is active.
let config: WebSocketConfig = {
  url: `${import.meta.env.DEV ? (import.meta.env.VITE_BFF_URL ?? 'http://localhost:8080/api/v1') : 'http://localhost:8080/api/v1'}/events`,
  reconnectBaseDelay: 1000,
  reconnectMaxDelay: 30_000,
};

// ADR-084 §3: capped exponential backoff — no permanent give-up.
// Attempt 0 → base; attempt n → min(base * 2^n, max).
const RECONNECT_BASE_DELAY = 1000;
const RECONNECT_MAX_DELAY = 30_000;

let abortController: AbortController | null = null;
let reconnectAttempts = 0;
let reconnectTimeout: ReturnType<typeof setTimeout> | null = null;
let isIntentionalClose = false;
let connected = false;

// Track whether the tab-focus / online recovery handlers have been registered.
// We register them once when the first connect() call is made and never remove
// them (they guard no-op when the connection is already live or intentionally
// closed), so there is no double-registration risk.
let recoveryListenersAttached = false;

const eventListeners: Map<string, Set<EventCallback>> = new Map();
const connectionListeners: Set<ConnectionStateCallback> = new Set();

// ---------------------------------------------------------------------------
// Configuration
// ---------------------------------------------------------------------------

export function configureWebSocket(newConfig: Partial<WebSocketConfig>): void {
  config = { ...config, ...newConfig };
}

export function getWebSocketConfig(): WebSocketConfig {
  return { ...config };
}

export function isConnected(): boolean {
  return connected;
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

function notifyConnectionState(state: boolean): void {
  connected = state;
  connectionListeners.forEach((cb) => {
    try {
      cb(state);
    } catch (err) {
      console.error('[SSE] Connection listener error:', err);
    }
  });
}

function dispatchEvent(eventType: string, data: unknown): void {
  const listeners = eventListeners.get(eventType);
  if (listeners) {
    listeners.forEach((cb) => {
      try {
        cb(data);
      } catch (err) {
        console.error(`[SSE] Event listener error for ${eventType}:`, err);
      }
    });
  }

  const wildcardListeners = eventListeners.get('*');
  if (wildcardListeners) {
    wildcardListeners.forEach((cb) => {
      try {
        cb({ type: eventType, data });
      } catch (err) {
        console.error('[SSE] Wildcard listener error:', err);
      }
    });
  }
}

function scheduleReconnect(): void {
  // ADR-084 §3: capped exponential backoff, NO permanent give-up.
  const base = config.reconnectBaseDelay ?? RECONNECT_BASE_DELAY;
  const max = config.reconnectMaxDelay ?? RECONNECT_MAX_DELAY;
  const delay = Math.min(base * Math.pow(2, reconnectAttempts), max);

  reconnectAttempts++;
  console.log(`[SSE] Reconnecting in ${delay}ms (attempt ${reconnectAttempts})`);

  reconnectTimeout = setTimeout(() => {
    connect().catch((err) => console.error('[SSE] Reconnect failed:', err));
  }, delay);
}

/**
 * Attach document.visibilitychange and window.online recovery listeners.
 * Registered once; each handler is a no-op when already connected or when
 * the close was intentional (e.g. sign-out).
 *
 * ADR-084 §3: reconnect on tab refocus and network restoration.
 */
function attachRecoveryListeners(): void {
  if (recoveryListenersAttached) return;
  recoveryListenersAttached = true;

  document.addEventListener('visibilitychange', () => {
    if (!document.hidden && !connected && !isIntentionalClose) {
      console.log('[SSE] Tab became visible — attempting reconnect');
      if (reconnectTimeout) {
        clearTimeout(reconnectTimeout);
        reconnectTimeout = null;
      }
      connect().catch((err) => console.error('[SSE] Visibility reconnect failed:', err));
    }
  });

  window.addEventListener('online', () => {
    if (!connected && !isIntentionalClose) {
      console.log('[SSE] Network online — attempting reconnect');
      if (reconnectTimeout) {
        clearTimeout(reconnectTimeout);
        reconnectTimeout = null;
      }
      connect().catch((err) => console.error('[SSE] Online reconnect failed:', err));
    }
  });
}

// ---------------------------------------------------------------------------
// Connection lifecycle
// ---------------------------------------------------------------------------

export function connect(): Promise<void> {
  // Register the tab-focus / network-online recovery handlers on the first call.
  // Safe to call repeatedly — attachRecoveryListeners guards against double-registration.
  if (typeof document !== 'undefined') {
    attachRecoveryListeners();
  }

  return new Promise((resolve, reject) => {
    if (connected) {
      resolve();
      return;
    }

    isIntentionalClose = false;
    abortController = new AbortController();

    // Resolve the Clerk JWT before opening the stream so the BFF's
    // RequireClerkAuthForSSE middleware can verify it.  When no provider is
    // registered (e.g. tests, or Clerk not yet hydrated) we send no
    // Authorization header and the BFF replies 401, which the existing
    // reconnect path will retry once Clerk is ready.
    getClerkToken().then((token) => {
      const headers: Record<string, string> = {
        Accept: 'text/event-stream',
      };
      if (token) {
        headers['Authorization'] = `Bearer ${token}`;
      }

      fetch(config.url, {
        method: 'GET',
        headers,
        signal: abortController!.signal,
      })
      .then((response) => {
        if (!response.ok || !response.body) {
          throw new Error(`[SSE] Bad response: ${response.status}`);
        }

        console.log('[SSE] Connected to', config.url);
        reconnectAttempts = 0;
        notifyConnectionState(true);
        resolve();

        const reader = response.body.getReader();
        const decoder = new TextDecoder();
        let buffer = '';

        function pump(): void {
          reader
            .read()
            .then(({ done, value }) => {
              if (done) {
                notifyConnectionState(false);
                if (!isIntentionalClose) {
                  scheduleReconnect();
                }
                return;
              }

              buffer += decoder.decode(value, { stream: true });

              const parts = buffer.split('\n\n');
              buffer = parts.pop() ?? '';

              for (const part of parts) {
                const lines = part.split('\n');
                let eventType = 'message';
                let dataPayload = '';

                for (const line of lines) {
                  if (line.startsWith('event:')) {
                    eventType = line.slice(6).trim();
                  } else if (line.startsWith('data:')) {
                    dataPayload = line.slice(5).trim();
                  }
                }

                if (dataPayload) {
                  try {
                    const parsed = JSON.parse(dataPayload) as WebSocketEvent;
                    if (parsed && typeof parsed === 'object' && 'type' in parsed) {
                      dispatchEvent(parsed.type as string, parsed.data);
                    } else {
                      dispatchEvent(eventType, parsed);
                    }
                  } catch {
                    console.error('[SSE] Failed to parse message:', dataPayload);
                  }
                }
              }

              pump();
            })
            .catch((err) => {
              if ((err as Error).name === 'AbortError') {
                return;
              }
              console.error('[SSE] Stream read error:', err);
              notifyConnectionState(false);
              if (!isIntentionalClose) {
                scheduleReconnect();
              }
            });
        }

        pump();
      })
      .catch((err) => {
        if ((err as Error).name === 'AbortError') {
          return;
        }
        console.error('[SSE] Connection error:', err);
        notifyConnectionState(false);
        reject(err);
        if (!isIntentionalClose) {
          scheduleReconnect();
        }
      });
    });
  });
}

export function disconnect(): void {
  isIntentionalClose = true;

  if (reconnectTimeout) {
    clearTimeout(reconnectTimeout);
    reconnectTimeout = null;
  }

  if (abortController) {
    abortController.abort();
    abortController = null;
  }

  notifyConnectionState(false);
}

// ---------------------------------------------------------------------------
// Event subscription API
// ---------------------------------------------------------------------------

export function EventsOn(eventType: string, callback: EventCallback): () => void {
  if (!eventListeners.has(eventType)) {
    eventListeners.set(eventType, new Set());
  }
  eventListeners.get(eventType)!.add(callback);

  return () => {
    const listeners = eventListeners.get(eventType);
    if (listeners) {
      listeners.delete(callback);
      if (listeners.size === 0) {
        eventListeners.delete(eventType);
      }
    }
  };
}

export function EventsOnce(eventType: string, callback: EventCallback): () => void {
  const wrappedCallback = (data: unknown) => {
    unsubscribe();
    callback(data);
  };
  const unsubscribe = EventsOn(eventType, wrappedCallback);
  return unsubscribe;
}

export function EventsOff(eventType: string, ...additionalEventTypes: string[]): void {
  [eventType, ...additionalEventTypes].forEach((type) => {
    eventListeners.delete(type);
  });
}

export function EventsEmit(eventType: string, data?: unknown): void {
  dispatchEvent(eventType, data);
}

export function onConnectionChange(callback: ConnectionStateCallback): () => void {
  connectionListeners.add(callback);
  callback(isConnected());
  return () => {
    connectionListeners.delete(callback);
  };
}

// ---------------------------------------------------------------------------
// Debug helpers
// ---------------------------------------------------------------------------

export function getListenerCount(eventType: string): number {
  return eventListeners.get(eventType)?.size ?? 0;
}

export function getRegisteredEventTypes(): string[] {
  return Array.from(eventListeners.keys());
}

export function WindowReloadApp(): void {
  window.location.reload();
}
