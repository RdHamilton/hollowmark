/**
 * TDD — RED phase: tests for the useReadModelUpdates hook.
 *
 * The hook:
 *  - Subscribes once (app-wide) to 'readmodel.updated' SSE events.
 *  - On receipt, calls the per-domain callbacks registered by callers.
 *  - Domain payload: { domains: string[] }
 *  - Cleans up its SSE subscription on unmount.
 *
 * These tests fail until the hook is implemented (GREEN phase).
 */

import { renderHook, act } from '@testing-library/react';
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { EventsEmit, EventsOff } from '@/services/websocketClient';

// The hook is not yet implemented; importing it will fail (RED).
import { useReadModelUpdates } from './useReadModelUpdates';

// @/services/websocketClient is mocked globally in src/test/setup.ts.
// We use the mock's EventsEmit/EventsOff to simulate frames and cleanup.

describe('useReadModelUpdates', () => {
  beforeEach(() => {
    EventsOff('readmodel.updated');
  });

  afterEach(() => {
    vi.clearAllMocks();
  });

  it('calls the matches callback when readmodel.updated is received for the matches domain', () => {
    const onMatches = vi.fn();
    renderHook(() => useReadModelUpdates({ onMatches }));

    act(() => {
      EventsEmit('readmodel.updated', { domains: ['matches'] });
    });

    expect(onMatches).toHaveBeenCalledTimes(1);
  });

  it('calls the drafts callback when readmodel.updated carries the drafts domain', () => {
    const onDrafts = vi.fn();
    renderHook(() => useReadModelUpdates({ onDrafts }));

    act(() => {
      EventsEmit('readmodel.updated', { domains: ['drafts'] });
    });

    expect(onDrafts).toHaveBeenCalledTimes(1);
  });

  it('calls the quests callback when readmodel.updated carries the quests domain', () => {
    const onQuests = vi.fn();
    renderHook(() => useReadModelUpdates({ onQuests }));

    act(() => {
      EventsEmit('readmodel.updated', { domains: ['quests'] });
    });

    expect(onQuests).toHaveBeenCalledTimes(1);
  });

  it('calls the collection callback when readmodel.updated carries the collection domain', () => {
    const onCollection = vi.fn();
    renderHook(() => useReadModelUpdates({ onCollection }));

    act(() => {
      EventsEmit('readmodel.updated', { domains: ['collection'] });
    });

    expect(onCollection).toHaveBeenCalledTimes(1);
  });

  it('calls the decks callback when readmodel.updated carries the decks domain', () => {
    const onDecks = vi.fn();
    renderHook(() => useReadModelUpdates({ onDecks }));

    act(() => {
      EventsEmit('readmodel.updated', { domains: ['decks'] });
    });

    expect(onDecks).toHaveBeenCalledTimes(1);
  });

  it('calls the inventory callback when readmodel.updated carries the inventory domain', () => {
    const onInventory = vi.fn();
    renderHook(() => useReadModelUpdates({ onInventory }));

    act(() => {
      EventsEmit('readmodel.updated', { domains: ['inventory'] });
    });

    expect(onInventory).toHaveBeenCalledTimes(1);
  });

  it('calls the mastery callback when readmodel.updated carries the mastery domain', () => {
    const onMastery = vi.fn();
    renderHook(() => useReadModelUpdates({ onMastery }));

    act(() => {
      EventsEmit('readmodel.updated', { domains: ['mastery'] });
    });

    expect(onMastery).toHaveBeenCalledTimes(1);
  });

  it('calls multiple domain callbacks for a multi-domain frame', () => {
    const onMatches = vi.fn();
    const onDecks = vi.fn();
    const onInventory = vi.fn();
    renderHook(() => useReadModelUpdates({ onMatches, onDecks, onInventory }));

    act(() => {
      EventsEmit('readmodel.updated', { domains: ['matches', 'decks', 'inventory'] });
    });

    expect(onMatches).toHaveBeenCalledTimes(1);
    expect(onDecks).toHaveBeenCalledTimes(1);
    expect(onInventory).toHaveBeenCalledTimes(1);
  });

  it('does not call unregistered domain callbacks when a different domain fires', () => {
    const onMatches = vi.fn();
    renderHook(() => useReadModelUpdates({ onMatches }));

    act(() => {
      EventsEmit('readmodel.updated', { domains: ['quests'] });
    });

    expect(onMatches).not.toHaveBeenCalled();
  });

  it('ignores frames with unknown domain names without throwing', () => {
    const onMatches = vi.fn();
    renderHook(() => useReadModelUpdates({ onMatches }));

    expect(() => {
      act(() => {
        EventsEmit('readmodel.updated', { domains: ['unknown_domain_xyz'] });
      });
    }).not.toThrow();
    expect(onMatches).not.toHaveBeenCalled();
  });

  it('ignores frames with malformed/null data without throwing', () => {
    const onMatches = vi.fn();
    renderHook(() => useReadModelUpdates({ onMatches }));

    expect(() => {
      act(() => { EventsEmit('readmodel.updated', null); });
    }).not.toThrow();
    expect(() => {
      act(() => { EventsEmit('readmodel.updated', 'not-an-object'); });
    }).not.toThrow();
    expect(onMatches).not.toHaveBeenCalled();
  });

  it('unsubscribes from readmodel.updated on unmount', () => {
    const onMatches = vi.fn();
    const { unmount } = renderHook(() => useReadModelUpdates({ onMatches }));

    unmount();

    act(() => {
      EventsEmit('readmodel.updated', { domains: ['matches'] });
    });

    expect(onMatches).not.toHaveBeenCalled();
  });
});
