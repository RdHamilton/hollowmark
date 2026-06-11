/**
 * useDraftEventStream — SSE consumer hook for live draft events.
 *
 * Opens an EventSource connection to the BFF `/api/v1/events` endpoint and
 * filters for events whose `type` field starts with `draft.`.
 *
 * Auth: appends a fresh Clerk session JWT as `?token=<jwt>` on every
 * (re)connect.  The Clerk `__session` cookie still works on production (same
 * parent domain), and the BFF's SSE middleware prefers it over the query
 * parameter — but on staging the SPA + Clerk Dev instance live on different
 * parent domains, so the cookie never reaches the BFF and we fall back to
 * the query param.  See issue #1904.  Nginx is configured with `access_log
 * off` on `/api/v1/events` so JWTs do not land in long-lived proxy logs.
 *
 * Features:
 * - Waits for Clerk to finish hydrating (`isLoaded && isSignedIn`) before
 *   opening the EventSource.  This prevents an unauthenticated SSE connection
 *   during the brief window where `getToken()` returns null at mount time even
 *   though the user's session is valid.  See issue #SSE-auth-bug /
 *   "Tim draft-verify Scenario-A SSE-auth".
 * - Reconnects with exponential backoff (100ms base, 30s cap) on error.
 *   Each reconnect re-fetches `getToken()` so rotated JWTs are picked up.
 * - Exposes `latestEvent` (last parsed draft event or null) and `status`.
 * - Cleans up the EventSource on unmount — no memory leaks.
 */

import { useAuth } from '@clerk/react';
import { useEffect, useRef, useState } from 'react';
import { getRuntimeConfig } from '../config/runtimeConfig';

/** Status of the underlying SSE connection. */
export type DraftEventStreamStatus = 'connecting' | 'open' | 'closed' | 'error' | 'waiting-for-auth';

/** Parsed wire format of a DaemonEvent sent by the BFF broker. */
export interface DaemonEvent {
  type: string;
  account_id: string;
  event_id: string;
  session_id: string;
  sequence: number;
  occurred_at: string;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  payload: Record<string, any> | null;
}

export interface UseDraftEventStreamReturn {
  /** Latest received draft event (type prefix `draft.`), or null. */
  latestEvent: DaemonEvent | null;
  /** Current SSE connection status. */
  status: DraftEventStreamStatus;
}

/** Prefix for draft-related event types. */
const DRAFT_EVENT_PREFIX = 'draft.';

/** Backoff config (ms). */
const BACKOFF_BASE_MS = 100;
const BACKOFF_MAX_MS = 30_000;

function computeBackoff(attempt: number): number {
  const exponential = BACKOFF_BASE_MS * Math.pow(2, attempt);
  return Math.min(exponential, BACKOFF_MAX_MS);
}

export function useDraftEventStream(): UseDraftEventStreamReturn {
  const { getToken, isLoaded, isSignedIn } = useAuth();

  const [latestEvent, setLatestEvent] = useState<DaemonEvent | null>(null);
  const [status, setStatus] = useState<DraftEventStreamStatus>('waiting-for-auth');

  // All mutable state lives in refs so callbacks captured in EventSource
  // handlers always see the current value without triggering re-renders.
  const sourceRef = useRef<EventSource | null>(null);
  const reconnectTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const attemptRef = useRef<number>(0);
  const unmountedRef = useRef<boolean>(false);

  // setLatestEvent / setStatus are stable, so we expose them through refs to
  // keep the effect-internal `connect` function free of hook dependencies.
  const setLatestEventRef = useRef(setLatestEvent);
  const setStatusRef = useRef(setStatus);

  // getToken changes identity on every Clerk session rotation; keep the latest
  // reference in a ref so the EventSource effect doesn't tear down + recreate
  // the SSE connection just because Clerk minted a new JWT.  Update the ref in
  // a separate effect, not during render, per react-hooks/refs.
  const getTokenRef = useRef(getToken);
  useEffect(() => {
    getTokenRef.current = getToken;
  }, [getToken]);

  // Re-run the main connection effect when Clerk auth state changes so we
  // connect as soon as isLoaded+isSignedIn become true (fixes the mount-time
  // race where getToken() returns null before Clerk JS hydrates).
  useEffect(() => {
    unmountedRef.current = false;

    // Capture stable setState dispatchers in locals so the cleanup function
    // does not trigger the react-hooks/exhaustive-deps ref-in-cleanup warning.
    const setLatestEventLocal = setLatestEventRef.current;
    const setStatusLocal = setStatusRef.current;

    // Guard: do not open an EventSource until Clerk has finished loading and
    // confirmed the user is signed in.  getToken() returns null while
    // isLoaded=false (Clerk JS still hydrating) — opening the connection in
    // that state produces a tokenless URL that the BFF registers as userID=0,
    // which receives no events and never self-heals because `onopen` resets
    // the backoff counter.
    if (!isLoaded || !isSignedIn) {
      setStatusLocal('waiting-for-auth');
      return;
    }

    async function connect() {
      if (unmountedRef.current) return;

      setStatusLocal('connecting');

      // ADR-077: derive SSE URL at connect time from runtimeConfig so this
      // hook never captures the BFF URL at module-load time.
      const sseUrl = `${getRuntimeConfig().bffUrl}/events`;

      // Fresh JWT every (re)connect so a rotated Clerk session picks up on
      // the next backoff cycle.
      let url = sseUrl;
      try {
        const token = await getTokenRef.current();
        if (token) {
          const u = new URL(sseUrl);
          u.searchParams.set('token', token);
          url = u.toString();
        }
        // If getToken() returns null here (very unlikely given the isSignedIn
        // guard above, but possible during a mid-session sign-out race), fall
        // through — onerror backoff will retry, and the next attempt re-calls
        // getTokenRef.current() which will have the fresh function reference.
      } catch {
        // Token fetch failure — fall through and let SSE 401 + backoff retry.
      }

      if (unmountedRef.current) return;

      const source = new EventSource(url, { withCredentials: true });
      sourceRef.current = source;

      source.onopen = () => {
        if (unmountedRef.current) {
          source.close();
          return;
        }
        attemptRef.current = 0;
        setStatusLocal('open');
      };

      const handleDraftMessage = (e: MessageEvent) => {
        if (unmountedRef.current) return;
        try {
          const ev = JSON.parse(e.data as string) as DaemonEvent;
          if (ev.type?.startsWith(DRAFT_EVENT_PREFIX)) {
            setLatestEventLocal(ev);
          }
        } catch {
          // Malformed JSON — ignore silently.
        }
      };

      // Unnamed data frames arrive via onmessage.
      source.onmessage = handleDraftMessage;

      // Named event frames (e.g. `event: draft.pack`) are dispatched as named
      // events on the EventSource and do NOT fire onmessage.
      source.addEventListener('draft.started', handleDraftMessage);
      source.addEventListener('draft.pack', handleDraftMessage);
      source.addEventListener('draft.ended', handleDraftMessage);

      source.onerror = () => {
        if (unmountedRef.current) {
          source.close();
          return;
        }

        setStatusLocal('error');
        source.close();
        sourceRef.current = null;

        const delay = computeBackoff(attemptRef.current);
        attemptRef.current += 1;

        reconnectTimerRef.current = setTimeout(() => {
          if (!unmountedRef.current) {
            connect();
          }
        }, delay);
      };
    }

    connect();

    return () => {
      unmountedRef.current = true;

      if (reconnectTimerRef.current !== null) {
        clearTimeout(reconnectTimerRef.current);
        reconnectTimerRef.current = null;
      }

      if (sourceRef.current) {
        sourceRef.current.close();
        sourceRef.current = null;
      }

      setStatusLocal('closed');
    };
  // Re-run when Clerk auth state resolves so the connection is made as soon
  // as isLoaded+isSignedIn become true.  getToken is intentionally excluded
  // from this dep array — it is kept current via getTokenRef so a JWT rotation
  // does not tear down the SSE connection.
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isLoaded, isSignedIn]);

  return { latestEvent, status };
}
