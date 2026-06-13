import { useCallback, useReducer } from 'react';

// ---------------------------------------------------------------------------
// Event shapes — mirror the daemon wire protocol
// ---------------------------------------------------------------------------

/** Envelope delivered over SSE from the BFF. */
export interface DraftEvent {
  type: string;
  payload?: DraftStartedPayload | DraftEndedPayload | DraftPackPayload | DraftPickPayload | Record<string, unknown>;
}

/** draft.started — scene transition into the draft screen. */
export interface DraftStartedPayload {
  session_id?: string;
  set_code?: string;
  draft_type?: string;
}

/** draft.ended — scene transition away from draft screen. */
export interface DraftEndedPayload {
  session_id?: string;
}

/** draft.pack — new pack offered to the player.
 *  Payload mirrors DraftPackPayload from daemon/logreader.
 */
export interface DraftPackPayload {
  CourseName?: string;
  draftPack?: {
    PackCards: number[];
    SelfPick: number;
  };
  /** Alternative flat shape used by some daemon versions. */
  pack_number?: number;
  pick_number?: number;
  card_ids?: number[];
}

/** draft.pick — player made a pick.
 *  Payload mirrors DraftPickPayload from daemon/logreader.
 */
export interface DraftPickPayload {
  CourseName?: string;
  pickedCards?: number[];
  PackNumber?: number;
  PickNumber?: number;
  /** Alternative flat shape. */
  card_id?: number;
  pack_number?: number;
  pick_number?: number;
}

// ---------------------------------------------------------------------------
// Hydration snapshot — produced by the mount-fetch cold-start path (#1421).
//
// The BFF does NOT replicate live pack cards (ADR-084 = ingest-time broadcast
// only), so currentPackCards is always empty on hydration; SSE fills it in on
// the next pack event. The snapshot carries picks from the BFF so the pick
// history is immediately visible without waiting for an SSE replay.
// ---------------------------------------------------------------------------

export interface DraftSessionSnapshot {
  /** Arena grpIds the player has already picked, derived from BFF pick rows. */
  pickedCardIds: number[];
  /**
   * 1-based pack number from the most-recent BFF pick row.
   * 0 when no picks have been recorded yet.
   */
  packNumber: number;
  /**
   * 1-based pick number from the most-recent BFF pick row.
   * 0 when no picks have been recorded yet.
   */
  pickNumber: number;
}

// ---------------------------------------------------------------------------
// State
// ---------------------------------------------------------------------------

export type SessionStatus = 'idle' | 'active' | 'complete';

export interface DraftSessionState {
  sessionStatus: SessionStatus;
  packNumber: number;
  pickNumber: number;
  /** Arena grpIds currently offered in the pack. */
  currentPackCards: number[];
  /** Arena grpIds the player has already picked this draft. */
  pickedCards: number[];
}

const INITIAL_STATE: DraftSessionState = {
  sessionStatus: 'idle',
  packNumber: 0,
  pickNumber: 0,
  currentPackCards: [],
  pickedCards: [],
};

// ---------------------------------------------------------------------------
// Reducer
// ---------------------------------------------------------------------------

type Action =
  | { type: 'DISPATCH_EVENT'; event: DraftEvent }
  | { type: 'HYDRATE'; snapshot: DraftSessionSnapshot };

function reducer(state: DraftSessionState, action: Action): DraftSessionState {
  switch (action.type) {
    case 'HYDRATE': {
      // Only hydrate from idle — if SSE already activated the session (race
      // where a pack arrives before the mount-fetch resolves), preserve the
      // live SSE state rather than overwriting it with the snapshot.
      if (state.sessionStatus !== 'idle') return state;
      const { snapshot } = action;
      return {
        ...INITIAL_STATE,
        sessionStatus: 'active',
        packNumber: snapshot.packNumber,
        pickNumber: snapshot.pickNumber,
        pickedCards: snapshot.pickedCardIds,
        // currentPackCards intentionally left empty — SSE fills it in.
        currentPackCards: [],
      };
    }

    case 'DISPATCH_EVENT': {
      const { event } = action;

      switch (event.type) {
        case 'draft.started':
          // Reset cleanly: new draft session begins.
          return {
            ...INITIAL_STATE,
            sessionStatus: 'active',
          };

        case 'draft.ended':
          return {
            ...state,
            sessionStatus: 'complete',
            currentPackCards: [],
          };

        case 'draft.pack': {
          const p = event.payload as DraftPackPayload | undefined;
          if (!p) return state;

          // Support both nested daemon shape and flat shape.
          const cards: number[] =
            p.draftPack?.PackCards ??
            p.card_ids ??
            [];

          // SelfPick is 1-based; PickNumber in flat shape is 0-based.
          // Normalise both to 1-based for consumer convenience.
          const rawSelfPick = p.draftPack?.SelfPick;
          const rawFlatPick = p.pick_number;
          const pickNumber =
            rawSelfPick !== undefined
              ? rawSelfPick
              : rawFlatPick !== undefined
              ? rawFlatPick + 1
              : state.pickNumber;

          // PackNumber in flat shape is 0-based; normalise to 1-based.
          const rawFlatPack = p.pack_number;
          const packNumber =
            rawFlatPack !== undefined ? rawFlatPack + 1 : state.packNumber;

          return {
            ...state,
            sessionStatus: 'active',
            packNumber,
            pickNumber,
            currentPackCards: cards,
          };
        }

        case 'draft.pick': {
          const p = event.payload as DraftPickPayload | undefined;
          if (!p) return state;

          const picked: number[] =
            p.pickedCards ??
            (p.card_id !== undefined ? [p.card_id] : []);

          // PackNumber and PickNumber from the daemon are 0-based.
          const packNumber =
            p.PackNumber !== undefined ? p.PackNumber + 1 : state.packNumber;
          const pickNumber =
            p.PickNumber !== undefined ? p.PickNumber + 1 : state.pickNumber;

          // Remove picked cards from currentPackCards.
          const remaining = state.currentPackCards.filter(
            (id) => !picked.includes(id)
          );

          return {
            ...state,
            sessionStatus: 'active',
            packNumber,
            pickNumber,
            currentPackCards: remaining,
            pickedCards: [...state.pickedCards, ...picked],
          };
        }

        default:
          return state;
      }
    }

    default:
      return state;
  }
}

// ---------------------------------------------------------------------------
// Hook
// ---------------------------------------------------------------------------

export interface UseDraftSessionReturn {
  state: DraftSessionState;
  /** Feed a parsed SSE event into the state machine. */
  dispatch: (event: DraftEvent) => void;
  /**
   * Hydrate the state machine from a BFF session snapshot (#1421 cold-start
   * path). Call this on mount when an active session exists in the DB so the
   * page does not show "No active draft" while waiting for the next SSE event.
   *
   * No-op if the session is already active (SSE beat the fetch to the punch).
   */
  hydrate: (snapshot: DraftSessionSnapshot) => void;
}

/**
 * useDraftSession manages the in-memory state machine for a live draft session.
 *
 * It is designed to consume events produced by the useDraftEventStream hook.
 * For mid-session resume, call hydrate() with the BFF session snapshot on
 * mount (#1421); the state machine activates immediately without requiring an
 * SSE event replay that will never come (ADR-084 = ingest-time broadcast only).
 */
export function useDraftSession(): UseDraftSessionReturn {
  const [state, dispatchAction] = useReducer(reducer, INITIAL_STATE);

  const dispatch = useCallback((event: DraftEvent) => {
    dispatchAction({ type: 'DISPATCH_EVENT', event });
  }, []);

  const hydrate = useCallback((snapshot: DraftSessionSnapshot) => {
    dispatchAction({ type: 'HYDRATE', snapshot });
  }, []);

  return { state, dispatch, hydrate };
}
