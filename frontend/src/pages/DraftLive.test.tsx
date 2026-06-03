/**
 * DraftLive component tests — ticket #1390
 *
 * Mocks:
 *   - useDraftEventStream  → controlled via mockStream
 *   - useDraftSession      → controlled via mockSession
 *   - getDraftRatings      → controlled via mockGetDraftRatings
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor, act } from '@testing-library/react';
import DraftLive from './DraftLive';
import type { DraftSessionState, UseDraftSessionReturn } from '@/hooks/useDraftSession';
import * as useFeatureFlagModule from '@/hooks/useFeatureFlag';
import * as analyticsModule from '@/services/analytics';

// ---------------------------------------------------------------------------
// Mock hooks and adapters
// ---------------------------------------------------------------------------

const mockStreamReturn = {
  latestEvent: null as import('@/hooks/useDraftEventStream').DaemonEvent | null,
  status: 'open' as import('@/hooks/useDraftEventStream').DraftEventStreamStatus,
};

const mockSessionReturn: UseDraftSessionReturn = {
  state: {
    sessionStatus: 'idle',
    packNumber: 0,
    pickNumber: 0,
    currentPackCards: [],
    pickedCards: [],
  },
  dispatch: vi.fn(),
};

vi.mock('@/hooks', () => ({
  useDraftEventStream: vi.fn(() => mockStreamReturn),
  useDraftSession: vi.fn(() => mockSessionReturn),
}));

vi.mock('@/services/api/bffDraftRatings', () => ({
  getDraftRatings: vi.fn(),
}));

// Clerk useAuth mock
vi.mock('@clerk/react', () => ({
  useAuth: vi.fn(() => ({ getToken: vi.fn().mockResolvedValue('test-token') })),
}));

// Feature flag mock — default ON (preserves existing test behaviour)
vi.mock('@/hooks/useFeatureFlag', () => ({
  useFeatureFlag: vi.fn().mockReturnValue({ enabled: true }),
}));

// Analytics mock — lets us assert telemetry suppression
vi.mock('@/services/analytics', () => ({
  trackEvent: vi.fn(),
}));

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

import { useDraftEventStream, useDraftSession } from '@/hooks';
import { getDraftRatings } from '@/services/api/bffDraftRatings';

const mockUseDraftEventStream = vi.mocked(useDraftEventStream);
const mockUseDraftSession = vi.mocked(useDraftSession);
const mockGetDraftRatings = vi.mocked(getDraftRatings);
const mockUseFeatureFlag = vi.mocked(useFeatureFlagModule.useFeatureFlag);
const mockTrackEvent = vi.mocked(analyticsModule.trackEvent);

function buildSession(overrides: Partial<DraftSessionState> = {}): DraftSessionState {
  return {
    sessionStatus: 'idle',
    packNumber: 0,
    pickNumber: 0,
    currentPackCards: [],
    pickedCards: [],
    ...overrides,
  };
}

function buildRatingsResult(cards: { arena_id: number; name: string; gihwr?: number }[]) {
  return {
    data: {
      set_code: 'ONE',
      draft_format: 'PremierDraft',
      cached_at: '2026-01-01T00:00:00Z',
      card_ratings: cards.map((c) => ({
        arena_id: c.arena_id,
        name: c.name,
        gihwr: c.gihwr,
      })),
      color_ratings: [],
    },
    cacheDegraded: false,
  };
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('DraftLive', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockGetDraftRatings.mockResolvedValue(buildRatingsResult([]));
    // Default: flag ON — preserves existing test behaviour
    mockUseFeatureFlag.mockReturnValue({ enabled: true });
  });

  // ── Empty / idle state ────────────────────────────────────────────────────

  describe('idle state (no active draft)', () => {
    it('renders empty state when session is idle', () => {
      mockUseDraftEventStream.mockReturnValue({ latestEvent: null, status: 'open' });
      mockUseDraftSession.mockReturnValue({
        state: buildSession({ sessionStatus: 'idle' }),
        dispatch: vi.fn(),
      });

      render(<DraftLive />);

      expect(screen.getByTestId('empty-state')).toBeInTheDocument();
      expect(screen.getByText('No active draft')).toBeInTheDocument();
      expect(
        screen.getByText('Start a draft in Arena to see your live pick recommendations')
      ).toBeInTheDocument();
    });

    it('shows container in idle state but no stream-status badge', () => {
      mockUseDraftEventStream.mockReturnValue({ latestEvent: null, status: 'connecting' });
      mockUseDraftSession.mockReturnValue({
        state: buildSession({ sessionStatus: 'idle' }),
        dispatch: vi.fn(),
      });

      render(<DraftLive />);

      expect(screen.getByTestId('draft-live-container')).toBeInTheDocument();
      // Stream-status badge is intentionally absent in the idle/empty state:
      // showing "Error" alongside "No active draft" is confusing and incorrect UX.
      expect(screen.queryByTestId('stream-status')).not.toBeInTheDocument();
    });

    it('does NOT show error badge when session is idle (Bug 6 regression)', () => {
      // Reproduces the staging bug: SSE error when there is no active draft
      // must not display an error badge alongside the "No active draft" empty state.
      mockUseDraftEventStream.mockReturnValue({ latestEvent: null, status: 'error' });
      mockUseDraftSession.mockReturnValue({
        state: buildSession({ sessionStatus: 'idle' }),
        dispatch: vi.fn(),
      });

      render(<DraftLive />);

      expect(screen.getByText('No active draft')).toBeInTheDocument();
      expect(screen.queryByTestId('stream-status')).not.toBeInTheDocument();
    });
  });

  // ── Complete state ────────────────────────────────────────────────────────

  describe('complete state', () => {
    it('shows draft complete empty state', () => {
      mockUseDraftEventStream.mockReturnValue({ latestEvent: null, status: 'open' });
      mockUseDraftSession.mockReturnValue({
        state: buildSession({ sessionStatus: 'complete' }),
        dispatch: vi.fn(),
      });

      render(<DraftLive />);

      expect(screen.getByText('Draft complete')).toBeInTheDocument();
    });
  });

  // ── Active state ──────────────────────────────────────────────────────────

  describe('active state — pack display', () => {
    it('renders pack section with card names and grades', async () => {
      mockGetDraftRatings.mockResolvedValue(
        buildRatingsResult([
          { arena_id: 101, name: 'Elesh Norn', gihwr: 67 },
          { arena_id: 102, name: 'Plains', gihwr: 50 },
        ])
      );

      const dispatchFn = vi.fn();
      mockUseDraftEventStream.mockReturnValue({ latestEvent: null, status: 'open' });
      mockUseDraftSession.mockReturnValue({
        state: buildSession({
          sessionStatus: 'active',
          packNumber: 1,
          pickNumber: 3,
          currentPackCards: [101, 102],
        }),
        dispatch: dispatchFn,
      });

      render(<DraftLive />);

      // Both cards appear after ratings load.
      await waitFor(() => {
        expect(screen.getByTestId('pack-card-101')).toBeInTheDocument();
        expect(screen.getByTestId('pack-card-102')).toBeInTheDocument();
      });
    });

    it('highlights the top pick card', async () => {
      mockGetDraftRatings.mockResolvedValue(
        buildRatingsResult([
          { arena_id: 201, name: 'Windfall', gihwr: 68 },
          { arena_id: 202, name: 'Island', gihwr: 49 },
        ])
      );

      const startedEvent: import('@/hooks/useDraftEventStream').DaemonEvent = {
        type: 'draft.started',
        account_id: 'acc1',
        event_id: 'e0',
        session_id: 's1',
        sequence: 0,
        occurred_at: '2026-05-08T00:00:00Z',
        payload: { set_code: 'ONE', draft_type: 'PremierDraft' },
      };

      mockUseDraftEventStream.mockReturnValue({ latestEvent: startedEvent, status: 'open' });
      mockUseDraftSession.mockReturnValue({
        state: buildSession({
          sessionStatus: 'active',
          packNumber: 1,
          pickNumber: 1,
          currentPackCards: [201, 202],
        }),
        dispatch: vi.fn(),
      });

      render(<DraftLive />);

      await waitFor(() => {
        expect(screen.getByTestId('top-pick-badge')).toBeInTheDocument();
      });

      // The card with higher GIHWR is marked as top pick.
      const topCard = screen.getByTestId('pack-card-201');
      expect(topCard).toHaveAttribute('data-top-pick', 'true');

      // The other card is NOT marked as top pick.
      const otherCard = screen.getByTestId('pack-card-202');
      expect(otherCard).not.toHaveAttribute('data-top-pick');
    });

    it('shows pack/pick progress metadata', async () => {
      mockUseDraftEventStream.mockReturnValue({ latestEvent: null, status: 'open' });
      mockUseDraftSession.mockReturnValue({
        state: buildSession({
          sessionStatus: 'active',
          packNumber: 2,
          pickNumber: 5,
          currentPackCards: [],
        }),
        dispatch: vi.fn(),
      });

      render(<DraftLive />);

      expect(screen.getByTestId('draft-live-progress')).toHaveTextContent('Pack 2 · Pick 5');
    });
  });

  // ── Pick history ─────────────────────────────────────────────────────────

  describe('pick history', () => {
    it('shows pick history section with picked cards', async () => {
      mockGetDraftRatings.mockResolvedValue(
        buildRatingsResult([
          { arena_id: 301, name: 'Black Lotus', gihwr: 72 },
          { arena_id: 302, name: 'Sol Ring', gihwr: 65 },
        ])
      );

      mockUseDraftEventStream.mockReturnValue({ latestEvent: null, status: 'open' });
      mockUseDraftSession.mockReturnValue({
        state: buildSession({
          sessionStatus: 'active',
          packNumber: 1,
          pickNumber: 3,
          currentPackCards: [],
          pickedCards: [301, 302],
        }),
        dispatch: vi.fn(),
      });

      render(<DraftLive />);

      await waitFor(() => {
        expect(screen.getByTestId('picked-card-301')).toBeInTheDocument();
        expect(screen.getByTestId('picked-card-302')).toBeInTheDocument();
      });
    });

    it('shows "No picks yet" when pickedCards is empty', () => {
      mockUseDraftEventStream.mockReturnValue({ latestEvent: null, status: 'open' });
      mockUseDraftSession.mockReturnValue({
        state: buildSession({
          sessionStatus: 'active',
          currentPackCards: [],
          pickedCards: [],
        }),
        dispatch: vi.fn(),
      });

      render(<DraftLive />);

      expect(screen.getByText('No picks yet')).toBeInTheDocument();
    });
  });

  // ── SSE dispatch ─────────────────────────────────────────────────────────

  describe('SSE event dispatch', () => {
    it('dispatches latestEvent to session state machine', async () => {
      const dispatchFn = vi.fn();
      const fakeEvent: import('@/hooks/useDraftEventStream').DaemonEvent = {
        type: 'draft.pack',
        account_id: 'acc1',
        event_id: 'evt1',
        session_id: 'sess1',
        sequence: 1,
        occurred_at: '2026-05-08T00:00:00Z',
        payload: { card_ids: [101, 102], pack_number: 0, pick_number: 0 },
      };

      mockUseDraftEventStream.mockReturnValue({ latestEvent: fakeEvent, status: 'open' });
      mockUseDraftSession.mockReturnValue({
        state: buildSession({ sessionStatus: 'active', currentPackCards: [101, 102] }),
        dispatch: dispatchFn,
      });

      render(<DraftLive />);

      await waitFor(() => {
        expect(dispatchFn).toHaveBeenCalledWith(
          expect.objectContaining({ type: 'draft.pack' })
        );
      });
    });
  });

  // ── Ratings fetch ─────────────────────────────────────────────────────────

  describe('ratings loading', () => {
    it('fetches ratings when set code and format are available from draft.started event', async () => {
      const startedEvent: import('@/hooks/useDraftEventStream').DaemonEvent = {
        type: 'draft.started',
        account_id: 'acc1',
        event_id: 'evt0',
        session_id: 'sess1',
        sequence: 0,
        occurred_at: '2026-05-08T00:00:00Z',
        payload: { set_code: 'ONE', draft_type: 'PremierDraft' },
      };

      mockUseDraftEventStream.mockReturnValue({ latestEvent: startedEvent, status: 'open' });
      mockUseDraftSession.mockReturnValue({
        state: buildSession({ sessionStatus: 'active' }),
        dispatch: vi.fn(),
      });

      render(<DraftLive />);

      await waitFor(() => {
        expect(mockGetDraftRatings).toHaveBeenCalledWith('ONE', 'Premier Draft');
      });
    });

    it('shows error message when ratings fetch fails', async () => {
      mockGetDraftRatings.mockRejectedValue(new Error('Network failure'));

      const startedEvent: import('@/hooks/useDraftEventStream').DaemonEvent = {
        type: 'draft.started',
        account_id: 'acc1',
        event_id: 'evt0',
        session_id: 'sess1',
        sequence: 0,
        occurred_at: '2026-05-08T00:00:00Z',
        payload: { set_code: 'BLB', draft_type: 'QuickDraft' },
      };

      mockUseDraftEventStream.mockReturnValue({ latestEvent: startedEvent, status: 'open' });
      mockUseDraftSession.mockReturnValue({
        state: buildSession({ sessionStatus: 'active' }),
        dispatch: vi.fn(),
      });

      render(<DraftLive />);

      await waitFor(() => {
        expect(screen.getByTestId('ratings-error')).toHaveTextContent('Network failure');
      });
    });

    it('shows set name and format label in meta bar', async () => {
      const startedEvent: import('@/hooks/useDraftEventStream').DaemonEvent = {
        type: 'draft.started',
        account_id: 'acc1',
        event_id: 'evt0',
        session_id: 'sess1',
        sequence: 0,
        occurred_at: '2026-05-08T00:00:00Z',
        payload: { set_code: 'MKM', draft_type: 'QuickDraft' },
      };

      mockUseDraftEventStream.mockReturnValue({ latestEvent: startedEvent, status: 'open' });
      mockUseDraftSession.mockReturnValue({
        state: buildSession({ sessionStatus: 'active' }),
        dispatch: vi.fn(),
      });

      render(<DraftLive />);

      await waitFor(() => {
        expect(screen.getByTestId('draft-live-set')).toHaveTextContent('MKM');
        expect(screen.getByTestId('draft-live-format')).toHaveTextContent('Quick Draft');
      });
    });
  });

  // ── Grade rendering ────────────────────────────────────────────────────────

  describe('grade rendering', () => {
    it('shows A+ grade for card with gihwr >= 65', async () => {
      mockGetDraftRatings.mockResolvedValue(
        buildRatingsResult([{ arena_id: 401, name: 'Mythic Bomb', gihwr: 66 }])
      );

      const startedEvent: import('@/hooks/useDraftEventStream').DaemonEvent = {
        type: 'draft.started',
        account_id: 'acc1',
        event_id: 'e0',
        session_id: 's1',
        sequence: 0,
        occurred_at: '2026-05-08T00:00:00Z',
        payload: { set_code: 'ONE', draft_type: 'PremierDraft' },
      };

      mockUseDraftEventStream.mockReturnValue({ latestEvent: startedEvent, status: 'open' });
      mockUseDraftSession.mockReturnValue({
        state: buildSession({
          sessionStatus: 'active',
          currentPackCards: [401],
        }),
        dispatch: vi.fn(),
      });

      render(<DraftLive />);

      await waitFor(() => {
        expect(screen.getByTestId('card-grade-401')).toHaveTextContent('A+');
      });
    });

    it('shows — grade for card with no ratings data', async () => {
      mockGetDraftRatings.mockResolvedValue(buildRatingsResult([]));

      mockUseDraftEventStream.mockReturnValue({ latestEvent: null, status: 'open' });
      mockUseDraftSession.mockReturnValue({
        state: buildSession({
          sessionStatus: 'active',
          currentPackCards: [999],
        }),
        dispatch: vi.fn(),
      });

      render(<DraftLive />);

      await act(async () => {});

      expect(screen.getByTestId('card-grade-999')).toHaveTextContent('—');
    });
  });

  // ── Stream status ─────────────────────────────────────────────────────────

  describe('stream status display', () => {
    it.each([
      ['open', 'open'],
      ['connecting', 'connecting'],
      ['error', 'error'],
      ['closed', 'closed'],
    ] as const)('shows stream status "%s"', (inputStatus, expectedText) => {
      mockUseDraftEventStream.mockReturnValue({ latestEvent: null, status: inputStatus });
      mockUseDraftSession.mockReturnValue({
        state: buildSession({ sessionStatus: 'active', currentPackCards: [] }),
        dispatch: vi.fn(),
      });

      render(<DraftLive />);

      expect(screen.getByTestId('stream-status')).toHaveTextContent(expectedText);
    });
  });

  // ── live_draft_advisor_enabled feature flag gate (vmt-t#628) ─────────────
  // Per Ray's ADR-047 condition: keep SSE stream alive; only suppress the
  // top-pick highlight + feature_draft_advisor_pick_viewed telemetry when OFF.
  describe('live_draft_advisor_enabled feature flag gate', () => {
    const startedEvent: import('@/hooks/useDraftEventStream').DaemonEvent = {
      type: 'draft.started',
      account_id: 'acc1',
      event_id: 'e0',
      session_id: 's1',
      sequence: 0,
      occurred_at: '2026-05-08T00:00:00Z',
      payload: { set_code: 'ONE', draft_type: 'PremierDraft' },
    };

    function setupActivePackWithRatings() {
      mockGetDraftRatings.mockResolvedValue(
        buildRatingsResult([
          { arena_id: 501, name: 'Top Bomb', gihwr: 70 },
          { arena_id: 502, name: 'Filler', gihwr: 48 },
        ])
      );
      mockUseDraftEventStream.mockReturnValue({ latestEvent: startedEvent, status: 'open' });
      mockUseDraftSession.mockReturnValue({
        state: buildSession({
          sessionStatus: 'active',
          packNumber: 1,
          pickNumber: 1,
          currentPackCards: [501, 502],
        }),
        dispatch: vi.fn(),
      });
    }

    it('flag ON — top-pick badge IS rendered and telemetry fires', async () => {
      mockUseFeatureFlag.mockReturnValue({ enabled: true });
      setupActivePackWithRatings();

      render(<DraftLive />);

      await waitFor(() => {
        expect(screen.getByTestId('top-pick-badge')).toBeInTheDocument();
      });

      const topCard = screen.getByTestId('pack-card-501');
      expect(topCard).toHaveAttribute('data-top-pick', 'true');

      await waitFor(() => {
        expect(mockTrackEvent).toHaveBeenCalledWith(
          expect.objectContaining({ name: 'feature_draft_advisor_pick_viewed' })
        );
      });
    });

    it('flag OFF — top-pick highlight suppressed and telemetry does NOT fire', async () => {
      mockUseFeatureFlag.mockReturnValue({ enabled: false });
      setupActivePackWithRatings();

      render(<DraftLive />);

      // Cards are still rendered (stream stays alive)
      await waitFor(() => {
        expect(screen.getByTestId('pack-card-501')).toBeInTheDocument();
        expect(screen.getByTestId('pack-card-502')).toBeInTheDocument();
      });

      // Top-pick badge must NOT appear
      expect(screen.queryByTestId('top-pick-badge')).not.toBeInTheDocument();

      // No card should have the top-pick attribute
      const topCard = screen.getByTestId('pack-card-501');
      expect(topCard).not.toHaveAttribute('data-top-pick');

      // Telemetry must be suppressed
      expect(mockTrackEvent).not.toHaveBeenCalledWith(
        expect.objectContaining({ name: 'feature_draft_advisor_pick_viewed' })
      );
    });

    it('flag null/undefined (loading) — top-pick IS shown and telemetry fires (optimistic-show)', async () => {
      mockUseFeatureFlag.mockReturnValue({ enabled: null });
      setupActivePackWithRatings();

      render(<DraftLive />);

      await waitFor(() => {
        expect(screen.getByTestId('top-pick-badge')).toBeInTheDocument();
      });

      await waitFor(() => {
        expect(mockTrackEvent).toHaveBeenCalledWith(
          expect.objectContaining({ name: 'feature_draft_advisor_pick_viewed' })
        );
      });
    });
  });
});
