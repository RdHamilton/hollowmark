/**
 * useReadModelUpdates — app-wide readmodel.updated SSE subscription.
 *
 * ADR-084 §Implementation Notes (c): a single subscription to the
 * 'readmodel.updated' event dispatched by the BFF projection worker.
 * The event payload is { domains: string[] } where each domain maps
 * to one or more React Query / local-state reload callbacks.
 *
 * Usage: mount ONCE at the app level (or in a page that owns data for a
 * domain). Passing a callback registers it for that domain; no callback
 * for a domain means that domain is ignored for this mount.
 *
 * Domains (per ADR-084 Decision §Event-type→domain map):
 *   matches | drafts | quests | collection | decks | inventory | mastery
 */

import { useEffect, useLayoutEffect, useRef } from 'react';
import { EventsOn } from '@/services/websocketClient';

export interface ReadModelDomainCallbacks {
  onMatches?: () => void;
  onDrafts?: () => void;
  onQuests?: () => void;
  onCollection?: () => void;
  onDecks?: () => void;
  onInventory?: () => void;
  onMastery?: () => void;
}

type Domain = 'matches' | 'drafts' | 'quests' | 'collection' | 'decks' | 'inventory' | 'mastery';

/**
 * Subscribe to readmodel.updated SSE frames and call the matching domain
 * callback when the BFF notifies that a domain's read model has changed.
 *
 * The returned unsubscribe is called on unmount, so this is safe to use
 * from any component or hook — each mount gets its own subscription.
 *
 * Per ADR-084: do NOT add a toast here. Toast reintroduction requires a
 * Prof PLAYER_VERDICT first (tracked separately).
 */
export function useReadModelUpdates(callbacks: ReadModelDomainCallbacks): void {
  // Hold callbacks in a ref so the effect closure never goes stale even if
  // callers pass inline arrow functions (they'd otherwise recreate the effect
  // on every render, causing subscribe/unsubscribe churn).
  // Updated in useLayoutEffect (not during render) to satisfy react-hooks/refs.
  const callbacksRef = useRef(callbacks);
  useLayoutEffect(() => {
    callbacksRef.current = callbacks;
  });

  useEffect(() => {
    const unsubscribe = EventsOn('readmodel.updated', (raw: unknown) => {
      // Guard against malformed payloads — the BFF contract is
      // { domains: string[] } but we defensive-check before accessing.
      if (!raw || typeof raw !== 'object') return;
      const payload = raw as { domains?: unknown };
      if (!Array.isArray(payload.domains)) return;

      const domainMap: Record<Domain, (() => void) | undefined> = {
        matches: callbacksRef.current.onMatches,
        drafts: callbacksRef.current.onDrafts,
        quests: callbacksRef.current.onQuests,
        collection: callbacksRef.current.onCollection,
        decks: callbacksRef.current.onDecks,
        inventory: callbacksRef.current.onInventory,
        mastery: callbacksRef.current.onMastery,
      };

      for (const domain of payload.domains) {
        const cb = domainMap[domain as Domain];
        if (cb) cb();
      }
    });

    return unsubscribe;
  }, []); // mount once; callbacks accessed via ref
}
