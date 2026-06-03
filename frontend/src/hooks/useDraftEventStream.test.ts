import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { renderHook, act } from '@testing-library/react';

// ---------------------------------------------------------------------------
// Clerk useAuth mock — the hook calls getToken() on every (re)connect to
// append a fresh JWT as ?token=.  We control auth state per test.
// ---------------------------------------------------------------------------

const mockGetToken = vi.fn<[], Promise<string | null>>();
let mockIsLoaded = true;
let mockIsSignedIn = true;

vi.mock('@clerk/react', () => ({
  useAuth: () => ({
    getToken: mockGetToken,
    isLoaded: mockIsLoaded,
    isSignedIn: mockIsSignedIn,
  }),
}));

// ---------------------------------------------------------------------------
// Minimal EventSource mock
// ---------------------------------------------------------------------------

type EventCallback = (e: MessageEvent) => void;
type ErrorCallback = (e: Event) => void;

interface MockEventSourceInstance {
  url: string;
  withCredentials: boolean;
  readyState: number;
  onopen: (() => void) | null;
  onmessage: EventCallback | null;
  onerror: ErrorCallback | null;
  close: ReturnType<typeof vi.fn>;
  addEventListener: ReturnType<typeof vi.fn>;
  // Test helpers
  _triggerOpen: () => void;
  _triggerMessage: (data: string) => void;
  _triggerNamedEvent: (type: string, data: string) => void;
  _triggerError: () => void;
  _namedListeners: Map<string, EventCallback[]>;
}

let instances: MockEventSourceInstance[] = [];

const MockEventSource = vi.fn((url: string, opts?: { withCredentials?: boolean }) => {
  const namedListeners = new Map<string, EventCallback[]>();

  const instance: MockEventSourceInstance = {
    url,
    withCredentials: opts?.withCredentials ?? false,
    readyState: 0, // CONNECTING
    onopen: null,
    onmessage: null,
    onerror: null,
    close: vi.fn(() => {
      instance.readyState = 2; // CLOSED
    }),
    addEventListener: vi.fn((type: string, handler: EventCallback) => {
      const list = namedListeners.get(type) ?? [];
      list.push(handler);
      namedListeners.set(type, list);
    }),
    _namedListeners: namedListeners,
    _triggerOpen() {
      instance.readyState = 1; // OPEN
      instance.onopen?.();
    },
    _triggerMessage(data: string) {
      instance.onmessage?.({ data } as MessageEvent);
    },
    _triggerNamedEvent(type: string, data: string) {
      const handlers = namedListeners.get(type) ?? [];
      handlers.forEach((h) => h({ data } as MessageEvent));
    },
    _triggerError() {
      instance.onerror?.({} as Event);
    },
  };

  instances.push(instance);
  return instance;
});

vi.stubGlobal('EventSource', MockEventSource);

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function makeDraftEvent(type: string, overrides: Partial<Record<string, unknown>> = {}): string {
  return JSON.stringify({
    type,
    account_id: 'acc_1',
    event_id: 'evt_1',
    session_id: 'sess_1',
    sequence: 1,
    occurred_at: '2026-05-08T00:00:00Z',
    payload: { draft_id: 'draft_abc' },
    ...overrides,
  });
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('useDraftEventStream', () => {
  beforeEach(() => {
    instances = [];
    MockEventSource.mockClear();
    mockGetToken.mockReset();
    // Default: signed-in + Clerk loaded, with a stable test JWT.
    mockIsLoaded = true;
    mockIsSignedIn = true;
    mockGetToken.mockResolvedValue('clerk-test-jwt');
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.resetModules();
  });

  // Drains the microtask queue so the hook's async connect() finishes (it
  // awaits getToken() before opening the EventSource).  All assertions that
  // touch `instances[N]` after a render/reconnect must call this first.
  async function flushConnect() {
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });
  }

  // ---------------------------------------------------------------------------
  // Auth-gate tests (primary bug fix — Clerk hydration race)
  // ---------------------------------------------------------------------------

  it('does NOT open an EventSource when isLoaded=false (Clerk still hydrating)', async () => {
    mockIsLoaded = false;
    mockIsSignedIn = false;

    const { useDraftEventStream } = await import('./useDraftEventStream');
    const { result } = renderHook(() => useDraftEventStream());
    await flushConnect();

    expect(instances).toHaveLength(0);
    expect(result.current.status).toBe('waiting-for-auth');
    expect(mockGetToken).not.toHaveBeenCalled();
  });

  it('does NOT open an EventSource when isLoaded=true but isSignedIn=false', async () => {
    mockIsLoaded = true;
    mockIsSignedIn = false;

    const { useDraftEventStream } = await import('./useDraftEventStream');
    const { result } = renderHook(() => useDraftEventStream());
    await flushConnect();

    expect(instances).toHaveLength(0);
    expect(result.current.status).toBe('waiting-for-auth');
    expect(mockGetToken).not.toHaveBeenCalled();
  });

  it('opens the EventSource once isLoaded=true and isSignedIn=true (Clerk hydration resolved)', async () => {
    // Simulate Clerk not yet hydrated at mount.
    mockIsLoaded = false;
    mockIsSignedIn = false;
    mockGetToken.mockResolvedValue('jwt-after-hydration');

    const { useDraftEventStream } = await import('./useDraftEventStream');

    // Start with unhydrated state — no EventSource should open.
    const { result, rerender } = renderHook(() => useDraftEventStream());
    await flushConnect();

    expect(instances).toHaveLength(0);
    expect(result.current.status).toBe('waiting-for-auth');

    // Simulate Clerk finishing hydration — flip the module-level vars and
    // rerender so the hook sees new values from useAuth().
    mockIsLoaded = true;
    mockIsSignedIn = true;
    rerender();
    await flushConnect();

    expect(instances).toHaveLength(1);
    const url = new URL(instances[0].url);
    expect(url.searchParams.get('token')).toBe('jwt-after-hydration');
  });

  it('reconnect re-acquires the token via getTokenRef (null-then-token scenario)', async () => {
    // First connect: getToken returns a valid JWT (isSignedIn gate is already
    // satisfied, so we reach connect(); this tests that each reconnect calls
    // the live getToken, not a stale mount-time snapshot).
    mockGetToken
      .mockResolvedValueOnce('jwt-first')
      .mockResolvedValueOnce('jwt-second');

    const { useDraftEventStream } = await import('./useDraftEventStream');
    renderHook(() => useDraftEventStream());
    await flushConnect();

    expect(instances).toHaveLength(1);
    expect(new URL(instances[0].url).searchParams.get('token')).toBe('jwt-first');

    // Force a reconnect by triggering an error + advancing past backoff.
    act(() => {
      instances[0]._triggerError();
    });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(150);
    });
    await flushConnect();

    // The second connection must use the freshly-acquired token, not the
    // stale jwt-first value from the first mount-time call.
    expect(instances).toHaveLength(2);
    expect(new URL(instances[1].url).searchParams.get('token')).toBe('jwt-second');
    expect(mockGetToken).toHaveBeenCalledTimes(2);
  });

  // ---------------------------------------------------------------------------
  // Existing connection behaviour tests
  // ---------------------------------------------------------------------------

  it('starts with status "waiting-for-auth" before Clerk is loaded', async () => {
    mockIsLoaded = false;
    mockIsSignedIn = false;

    const { useDraftEventStream } = await import('./useDraftEventStream');
    const { result } = renderHook(() => useDraftEventStream());

    expect(result.current.status).toBe('waiting-for-auth');
    expect(result.current.latestEvent).toBeNull();
  });

  it('starts with status "connecting" when Clerk is already loaded and signed in', async () => {
    const { useDraftEventStream } = await import('./useDraftEventStream');
    const { result } = renderHook(() => useDraftEventStream());

    // Status is 'connecting' once the effect runs (isLoaded+isSignedIn=true).
    // The async connect() hasn't resolved yet at this point so EventSource
    // hasn't opened, but the status transitions to 'connecting' synchronously.
    expect(result.current.status).toBe('connecting');
    expect(result.current.latestEvent).toBeNull();
  });

  it('transitions to "open" on EventSource open', async () => {
    const { useDraftEventStream } = await import('./useDraftEventStream');
    const { result } = renderHook(() => useDraftEventStream());
    await flushConnect();

    expect(instances).toHaveLength(1);

    act(() => {
      instances[0]._triggerOpen();
    });

    expect(result.current.status).toBe('open');
  });

  it('opens EventSource with withCredentials: true', async () => {
    const { useDraftEventStream } = await import('./useDraftEventStream');
    renderHook(() => useDraftEventStream());
    await flushConnect();

    expect(instances).toHaveLength(1);
    expect(instances[0].withCredentials).toBe(true);
  });

  it('appends the Clerk JWT as ?token= on connect', async () => {
    mockGetToken.mockResolvedValue('jwt-abc-123');

    const { useDraftEventStream } = await import('./useDraftEventStream');
    renderHook(() => useDraftEventStream());
    await flushConnect();

    expect(instances).toHaveLength(1);
    const url = new URL(instances[0].url);
    expect(url.searchParams.get('token')).toBe('jwt-abc-123');
  });

  it('opens EventSource without ?token= when getToken returns null while signed-in (mid-session rotation)', async () => {
    // Even when signed in, if getToken() unexpectedly returns null (e.g.
    // during a brief JWT rotation window) we still open the connection and
    // let the backoff/retry pick up a fresh token on the next attempt.
    mockGetToken.mockResolvedValue(null);

    const { useDraftEventStream } = await import('./useDraftEventStream');
    renderHook(() => useDraftEventStream());
    await flushConnect();

    expect(instances).toHaveLength(1);
    const url = new URL(instances[0].url);
    expect(url.searchParams.get('token')).toBeNull();
  });

  it('re-fetches getToken on every reconnect (picks up rotated Clerk JWTs)', async () => {
    mockGetToken
      .mockResolvedValueOnce('jwt-first')
      .mockResolvedValueOnce('jwt-second');

    const { useDraftEventStream } = await import('./useDraftEventStream');
    renderHook(() => useDraftEventStream());
    await flushConnect();

    expect(instances).toHaveLength(1);
    expect(new URL(instances[0].url).searchParams.get('token')).toBe('jwt-first');

    // Force a reconnect by triggering an error + advancing past backoff.
    act(() => {
      instances[0]._triggerError();
    });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(150);
    });
    await flushConnect();

    expect(instances).toHaveLength(2);
    expect(new URL(instances[1].url).searchParams.get('token')).toBe('jwt-second');
    expect(mockGetToken).toHaveBeenCalledTimes(2);
  });

  it('opens EventSource without ?token= when getToken throws', async () => {
    mockGetToken.mockRejectedValue(new Error('clerk error'));

    const { useDraftEventStream } = await import('./useDraftEventStream');
    renderHook(() => useDraftEventStream());
    await flushConnect();

    expect(instances).toHaveLength(1);
    const url = new URL(instances[0].url);
    expect(url.searchParams.get('token')).toBeNull();
  });

  it('updates latestEvent when a draft.started message arrives', async () => {
    const { useDraftEventStream } = await import('./useDraftEventStream');
    const { result } = renderHook(() => useDraftEventStream());
    await flushConnect();

    act(() => {
      instances[0]._triggerOpen();
      instances[0]._triggerMessage(makeDraftEvent('draft.started'));
    });

    expect(result.current.latestEvent?.type).toBe('draft.started');
  });

  it('updates latestEvent when a draft.pack message arrives', async () => {
    const { useDraftEventStream } = await import('./useDraftEventStream');
    const { result } = renderHook(() => useDraftEventStream());
    await flushConnect();

    act(() => {
      instances[0]._triggerOpen();
      instances[0]._triggerMessage(makeDraftEvent('draft.pack'));
    });

    expect(result.current.latestEvent?.type).toBe('draft.pack');
  });

  it('updates latestEvent when a draft.ended message arrives', async () => {
    const { useDraftEventStream } = await import('./useDraftEventStream');
    const { result } = renderHook(() => useDraftEventStream());
    await flushConnect();

    act(() => {
      instances[0]._triggerOpen();
      instances[0]._triggerMessage(makeDraftEvent('draft.ended'));
    });

    expect(result.current.latestEvent?.type).toBe('draft.ended');
  });

  it('ignores non-draft events', async () => {
    const { useDraftEventStream } = await import('./useDraftEventStream');
    const { result } = renderHook(() => useDraftEventStream());
    await flushConnect();

    act(() => {
      instances[0]._triggerOpen();
      instances[0]._triggerMessage(makeDraftEvent('match.completed'));
    });

    expect(result.current.latestEvent).toBeNull();
  });

  it('handles named event frames (draft.pack as addEventListener)', async () => {
    const { useDraftEventStream } = await import('./useDraftEventStream');
    const { result } = renderHook(() => useDraftEventStream());
    await flushConnect();

    act(() => {
      instances[0]._triggerOpen();
      instances[0]._triggerNamedEvent('draft.pack', makeDraftEvent('draft.pack'));
    });

    expect(result.current.latestEvent?.type).toBe('draft.pack');
  });

  it('ignores malformed JSON without throwing', async () => {
    const { useDraftEventStream } = await import('./useDraftEventStream');
    const { result } = renderHook(() => useDraftEventStream());
    await flushConnect();

    expect(() => {
      act(() => {
        instances[0]._triggerOpen();
        instances[0]._triggerMessage('not-valid-json');
      });
    }).not.toThrow();

    expect(result.current.latestEvent).toBeNull();
  });

  it('sets status to "error" on EventSource error', async () => {
    const { useDraftEventStream } = await import('./useDraftEventStream');
    const { result } = renderHook(() => useDraftEventStream());
    await flushConnect();

    act(() => {
      instances[0]._triggerOpen();
    });

    act(() => {
      instances[0]._triggerError();
    });

    expect(result.current.status).toBe('error');
  });

  it('closes the first EventSource on error', async () => {
    const { useDraftEventStream } = await import('./useDraftEventStream');
    renderHook(() => useDraftEventStream());
    await flushConnect();

    act(() => {
      instances[0]._triggerOpen();
      instances[0]._triggerError();
    });

    expect(instances[0].close).toHaveBeenCalled();
  });

  it('reconnects after exponential backoff on error', async () => {
    const { useDraftEventStream } = await import('./useDraftEventStream');
    renderHook(() => useDraftEventStream());
    await flushConnect();

    // Trigger an error — should schedule a reconnect
    act(() => {
      instances[0]._triggerError();
    });

    expect(instances).toHaveLength(1); // not reconnected yet

    // Advance past the first backoff (100ms base)
    await act(async () => {
      await vi.advanceTimersByTimeAsync(150);
    });
    await flushConnect();

    expect(instances).toHaveLength(2); // new EventSource created
  });

  it('increases backoff delay on successive errors', async () => {
    const { useDraftEventStream } = await import('./useDraftEventStream');
    renderHook(() => useDraftEventStream());
    await flushConnect();

    // First error — 100ms backoff
    act(() => {
      instances[0]._triggerError();
    });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(150);
    });
    await flushConnect();
    expect(instances).toHaveLength(2);

    // Second error — 200ms backoff
    act(() => {
      instances[1]._triggerError();
    });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(150);
    });
    await flushConnect();
    // 150ms < 200ms — not reconnected yet
    expect(instances).toHaveLength(2);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(100);
    });
    await flushConnect();
    expect(instances).toHaveLength(3);
  });

  it('resets backoff attempt counter on successful open', async () => {
    const { useDraftEventStream } = await import('./useDraftEventStream');
    renderHook(() => useDraftEventStream());
    await flushConnect();

    // Fail once and reconnect
    act(() => {
      instances[0]._triggerError();
    });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(150);
    });
    await flushConnect();
    expect(instances).toHaveLength(2);

    // Succeed on second connection
    act(() => {
      instances[1]._triggerOpen();
    });

    // Fail again — backoff should be reset to 100ms (attempt 0)
    act(() => {
      instances[1]._triggerError();
    });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(150);
    });
    await flushConnect();
    expect(instances).toHaveLength(3); // reconnected at 100ms
  });

  it('cleans up EventSource on unmount', async () => {
    const { useDraftEventStream } = await import('./useDraftEventStream');
    const { unmount } = renderHook(() => useDraftEventStream());
    await flushConnect();

    act(() => {
      instances[0]._triggerOpen();
    });

    unmount();

    expect(instances[0].close).toHaveBeenCalled();
  });

  it('cancels pending reconnect timer on unmount', async () => {
    const { useDraftEventStream } = await import('./useDraftEventStream');
    const { unmount } = renderHook(() => useDraftEventStream());
    await flushConnect();

    // Trigger error to schedule reconnect
    act(() => {
      instances[0]._triggerError();
    });

    // Unmount before the timer fires
    unmount();

    // Advance time — no new EventSource should be created
    await act(async () => {
      await vi.advanceTimersByTimeAsync(5000);
    });
    await flushConnect();

    expect(instances).toHaveLength(1);
  });

  it('closes the EventSource on unmount (verifies no-leak cleanup)', async () => {
    // React setState calls after unmount do not propagate to result.current —
    // instead we verify the underlying EventSource was closed, which is the
    // leak-free behaviour we care about.
    const { useDraftEventStream } = await import('./useDraftEventStream');
    const { unmount } = renderHook(() => useDraftEventStream());
    await flushConnect();

    act(() => {
      instances[0]._triggerOpen();
    });

    expect(instances[0].close).not.toHaveBeenCalled();

    act(() => {
      unmount();
    });

    expect(instances[0].close).toHaveBeenCalledOnce();
  });

  it('caps backoff at 30 seconds', async () => {
    const { useDraftEventStream } = await import('./useDraftEventStream');
    renderHook(() => useDraftEventStream());
    await flushConnect();

    // Trigger many errors to push past the cap (2^n * 100ms > 30000ms at n=9)
    for (let i = 0; i < 10; i++) {
      const idx = instances.length - 1;
      act(() => {
        instances[idx]._triggerError();
      });
      // Advance max delay to ensure reconnect always happens
      await act(async () => {
        await vi.advanceTimersByTimeAsync(35_000);
      });
      await flushConnect();
    }

    // After 10 cycles we should have 11 EventSource instances (1 original + 10 reconnects)
    expect(instances).toHaveLength(11);
  });
});
