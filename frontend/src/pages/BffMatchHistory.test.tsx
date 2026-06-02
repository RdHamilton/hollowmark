/**
 * BffMatchHistory component tests — regression guard for vmt-t#625.
 *
 * ROOT CAUSE: bffMatchHistory.ts previously declared MatchHistoryResponse as
 *   { matches: [...], total, limit, offset }
 * but the BFF GET /api/v1/history/matches actually returns:
 *   { data: [...], has_more, next_cursor_ts, next_cursor_id, limit }
 * (cursor-paginated, since PR #2031 migrated the endpoint to keyset pagination).
 *
 * Result: data.matches was always undefined → total === 0 → "No matches yet"
 * even when the BFF returned rows.
 *
 * These tests MUST fail against the old buggy code (which read data.matches)
 * and PASS after the fix (which reads data.data). The "renders table" test is
 * the canonical regression guard.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor, fireEvent, act } from '@testing-library/react';
import BffMatchHistory from './BffMatchHistory';
import type { MatchHistoryResponse, MatchHistoryItem } from '@/services/api/bffMatchHistory';

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

vi.mock('@/services/api/bffMatchHistory', () => ({
  getMatchHistory: vi.fn(),
}));

// Track registered SSE event callbacks so tests can fire them manually.
// The correct event name is 'match.completed' (not the legacy 'stats:updated').
const registeredCallbacks: Record<string, (() => void) | null> = {
  'match.completed': null,
};

vi.mock('@/services/websocketClient', () => ({
  EventsOn: vi.fn((event: string, cb: () => void) => {
    registeredCallbacks[event] = cb;
    return () => {
      registeredCallbacks[event] = null;
    };
  }),
}));

// Import after mock so we get the vi.fn() version.
import { getMatchHistory } from '@/services/api/bffMatchHistory';
const mockGetMatchHistory = vi.mocked(getMatchHistory);

// ---------------------------------------------------------------------------
// Fixture helpers
// ---------------------------------------------------------------------------

function makeItem(overrides: Partial<MatchHistoryItem> = {}): MatchHistoryItem {
  return {
    id: 'match-1',
    format: 'Standard',
    result: 'win',
    timestamp: '2026-05-01T14:30:00Z',
    player_wins: 2,
    opponent_wins: 0,
    duration_seconds: 1800,
    deck_id: null,
    rank_before: null,
    rank_after: null,
    opponent_rank: null,
    ...overrides,
  };
}

/**
 * Build a MatchHistoryResponse with the ACTUAL BFF cursor-paginated shape.
 *
 * This is the shape returned by GET /api/v1/history/matches (history.go
 * cursorPaginatedMatchResponse). The key is that the array lives under "data",
 * NOT "matches". Using this factory in tests ensures we test against the real
 * contract, not the old broken expectation.
 */
function makeResponse(overrides: Partial<MatchHistoryResponse> = {}): MatchHistoryResponse {
  return {
    data: [],
    has_more: false,
    limit: 20,
    ...overrides,
  };
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('BffMatchHistory', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    registeredCallbacks['match.completed'] = null;
  });

  // --------------------------------------------------------------------------
  // REGRESSION GUARD (vmt-t#625)
  //
  // This test is the primary regression guard. It MUST:
  //   - FAIL against the old buggy code (which read data.matches → undefined)
  //   - PASS after the fix (which reads data.data → the real array)
  // --------------------------------------------------------------------------

  describe('Regression guard — BFF response shape (vmt-t#625)', () => {
    it('[REGRESSION GUARD] renders match table when BFF returns actual cursor-paginated shape', async () => {
      // This is the exact shape the BFF returns. If the adapter reads data.matches
      // instead of data.data, this test fails with "No matches yet" and total === 0.
      mockGetMatchHistory.mockResolvedValue(
        makeResponse({
          data: [
            makeItem({ id: 'abc-1', format: 'Standard', result: 'win', timestamp: '2026-05-01T14:30:00Z' }),
            makeItem({ id: 'abc-2', format: 'Standard', result: 'loss', timestamp: '2026-05-02T10:00:00Z' }),
          ],
          has_more: false,
          limit: 20,
        })
      );

      render(<BffMatchHistory />);

      // Must render the table — NOT the empty state.
      await waitFor(() => {
        expect(screen.getByTestId('match-history-table')).toBeInTheDocument();
      });
      expect(screen.queryByTestId('match-history-empty')).not.toBeInTheDocument();
    });

    it('[REGRESSION GUARD] does NOT show empty state when BFF returns matches', async () => {
      mockGetMatchHistory.mockResolvedValue(
        makeResponse({
          data: [makeItem()],
          has_more: false,
        })
      );

      render(<BffMatchHistory />);

      await waitFor(() => {
        expect(screen.queryByTestId('match-history-empty')).not.toBeInTheDocument();
      });
      expect(screen.getByTestId('match-history-table')).toBeInTheDocument();
    });
  });

  // --------------------------------------------------------------------------
  // Loading state
  // --------------------------------------------------------------------------

  describe('Loading state', () => {
    it('renders loading spinner initially', async () => {
      let resolve: (v: MatchHistoryResponse) => void;
      mockGetMatchHistory.mockReturnValue(
        new Promise((r) => { resolve = r; })
      );

      render(<BffMatchHistory />);

      expect(screen.getByText('Loading matches...')).toBeInTheDocument();

      resolve!(makeResponse());
      await waitFor(() => {
        expect(screen.queryByText('Loading matches...')).not.toBeInTheDocument();
      });
    });
  });

  // --------------------------------------------------------------------------
  // Empty state — true empty (BFF returns data: [])
  // --------------------------------------------------------------------------

  describe('Empty state', () => {
    it('renders empty state when BFF returns data: []', async () => {
      mockGetMatchHistory.mockResolvedValue(makeResponse({ data: [], has_more: false }));

      render(<BffMatchHistory />);

      await waitFor(() => {
        expect(screen.getByTestId('match-history-empty')).toBeInTheDocument();
      });
      expect(screen.getByText('No matches yet')).toBeInTheDocument();
    });

    it('does not render table when data is empty', async () => {
      mockGetMatchHistory.mockResolvedValue(makeResponse({ data: [], has_more: false }));

      render(<BffMatchHistory />);

      await waitFor(() => {
        expect(screen.queryByTestId('match-history-table')).not.toBeInTheDocument();
      });
    });
  });

  // --------------------------------------------------------------------------
  // Table rendering
  // --------------------------------------------------------------------------

  describe('Table rendering', () => {
    it('renders column headers: Date, Format, Result, Score', async () => {
      mockGetMatchHistory.mockResolvedValue(
        makeResponse({ data: [makeItem()], has_more: false })
      );

      render(<BffMatchHistory />);

      await waitFor(() => {
        expect(screen.getByRole('table')).toBeInTheDocument();
      });

      const headers = screen.getAllByRole('columnheader');
      const headerTexts = headers.map((h) => h.textContent);
      expect(headerTexts).toContain('Date');
      expect(headerTexts).toContain('Format');
      expect(headerTexts).toContain('Result');
    });

    it('renders WIN badge for a win result', async () => {
      mockGetMatchHistory.mockResolvedValue(
        makeResponse({
          data: [makeItem({ result: 'win', format: 'Standard', player_wins: 2, opponent_wins: 1 })],
          has_more: false,
        })
      );

      render(<BffMatchHistory />);

      await waitFor(() => {
        expect(screen.getByText('WIN')).toBeInTheDocument();
      });
      expect(screen.getByText('Standard')).toBeInTheDocument();
    });

    it('renders LOSS badge for a loss result', async () => {
      mockGetMatchHistory.mockResolvedValue(
        makeResponse({
          data: [makeItem({ result: 'loss', format: 'Historic', player_wins: 0, opponent_wins: 2 })],
          has_more: false,
        })
      );

      render(<BffMatchHistory />);

      await waitFor(() => {
        expect(screen.getByText('LOSS')).toBeInTheDocument();
      });
    });

    it('renders score column from player_wins / opponent_wins', async () => {
      mockGetMatchHistory.mockResolvedValue(
        makeResponse({
          data: [makeItem({ player_wins: 2, opponent_wins: 1 })],
          has_more: false,
        })
      );

      render(<BffMatchHistory />);

      await waitFor(() => {
        expect(screen.getByText('2–1')).toBeInTheDocument();
      });
    });

    it('renders multiple matches as table rows', async () => {
      mockGetMatchHistory.mockResolvedValue(
        makeResponse({
          data: [
            makeItem({ id: 'a', result: 'win' }),
            makeItem({ id: 'b', result: 'loss' }),
            makeItem({ id: 'c', result: 'win' }),
          ],
          has_more: false,
        })
      );

      render(<BffMatchHistory />);

      await waitFor(() => {
        const rows = screen.getAllByRole('row');
        // thead row + 3 tbody rows = 4
        expect(rows).toHaveLength(4);
      });
    });
  });

  // --------------------------------------------------------------------------
  // Cursor-based pagination
  // --------------------------------------------------------------------------

  describe('Cursor-based pagination', () => {
    it('Previous button is disabled on the first page', async () => {
      mockGetMatchHistory.mockResolvedValue(
        makeResponse({
          data: Array.from({ length: 20 }, (_, i) => makeItem({ id: String(i + 1) })),
          has_more: true,
          next_cursor_ts: '2026-05-01T00:00:00Z',
          next_cursor_id: '20',
        })
      );

      render(<BffMatchHistory />);

      await waitFor(() => {
        expect(screen.getByRole('button', { name: 'Previous' })).toBeDisabled();
      });
    });

    it('Next button is disabled when has_more is false', async () => {
      mockGetMatchHistory.mockResolvedValue(
        makeResponse({
          data: Array.from({ length: 5 }, (_, i) => makeItem({ id: String(i + 1) })),
          has_more: false,
        })
      );

      render(<BffMatchHistory />);

      await waitFor(() => {
        expect(screen.getByRole('button', { name: 'Next' })).toBeDisabled();
      });
    });

    it('Next button is enabled when has_more is true', async () => {
      mockGetMatchHistory.mockResolvedValue(
        makeResponse({
          data: Array.from({ length: 20 }, (_, i) => makeItem({ id: String(i + 1) })),
          has_more: true,
          next_cursor_ts: '2026-05-01T00:00:00Z',
          next_cursor_id: '20',
        })
      );

      render(<BffMatchHistory />);

      await waitFor(() => {
        expect(screen.getByRole('button', { name: 'Next' })).toBeEnabled();
      });
    });

    it('clicking Next fetches the next page using cursor params', async () => {
      const page1 = makeResponse({
        data: Array.from({ length: 20 }, (_, i) =>
          makeItem({ id: String(i + 1), format: 'Standard' })
        ),
        has_more: true,
        next_cursor_ts: '2026-04-15T10:00:00Z',
        next_cursor_id: 'cursor-abc',
        limit: 20,
      });
      const page2 = makeResponse({
        data: [makeItem({ id: 'p2-1', format: 'Historic', result: 'loss' })],
        has_more: false,
        limit: 20,
      });

      mockGetMatchHistory
        .mockResolvedValueOnce(page1)
        .mockResolvedValueOnce(page2);

      render(<BffMatchHistory />);

      await waitFor(() => {
        expect(screen.getByRole('button', { name: 'Next' })).toBeEnabled();
      });

      fireEvent.click(screen.getByRole('button', { name: 'Next' }));

      await waitFor(() => {
        expect(screen.getByText('LOSS')).toBeInTheDocument();
      });

      // The second call must pass the cursor from the first response.
      expect(mockGetMatchHistory).toHaveBeenCalledTimes(2);
      expect(mockGetMatchHistory).toHaveBeenNthCalledWith(
        2,
        'clerk-test-token-stub',
        expect.objectContaining({
          cursor_ts: '2026-04-15T10:00:00Z',
          cursor_id: 'cursor-abc',
          limit: 20,
        })
      );
    });

    it('Previous button is enabled after navigating to page 2', async () => {
      const page1 = makeResponse({
        data: Array.from({ length: 20 }, (_, i) => makeItem({ id: String(i + 1) })),
        has_more: true,
        next_cursor_ts: '2026-04-15T10:00:00Z',
        next_cursor_id: 'cursor-abc',
      });
      const page2 = makeResponse({
        data: [makeItem({ id: 'p2-1' })],
        has_more: false,
      });

      mockGetMatchHistory
        .mockResolvedValueOnce(page1)
        .mockResolvedValueOnce(page2);

      render(<BffMatchHistory />);

      await waitFor(() => {
        expect(screen.getByRole('button', { name: 'Next' })).toBeEnabled();
      });

      fireEvent.click(screen.getByRole('button', { name: 'Next' }));

      await waitFor(() => {
        expect(screen.getByRole('button', { name: 'Previous' })).toBeEnabled();
      });
    });

    it('clicking Previous after Next returns to the first page', async () => {
      const page1 = makeResponse({
        data: [makeItem({ id: 'first-page', format: 'Standard', result: 'win' })],
        has_more: true,
        next_cursor_ts: '2026-04-15T10:00:00Z',
        next_cursor_id: 'cursor-abc',
      });
      const page2 = makeResponse({
        data: [makeItem({ id: 'second-page', format: 'Historic', result: 'loss' })],
        has_more: false,
      });
      // Page 1 again after going back.
      const page1Again = makeResponse({
        data: [makeItem({ id: 'first-page', format: 'Standard', result: 'win' })],
        has_more: true,
        next_cursor_ts: '2026-04-15T10:00:00Z',
        next_cursor_id: 'cursor-abc',
      });

      mockGetMatchHistory
        .mockResolvedValueOnce(page1)
        .mockResolvedValueOnce(page2)
        .mockResolvedValueOnce(page1Again);

      render(<BffMatchHistory />);

      // Navigate to page 2.
      await waitFor(() => {
        expect(screen.getByRole('button', { name: 'Next' })).toBeEnabled();
      });
      fireEvent.click(screen.getByRole('button', { name: 'Next' }));
      await waitFor(() => {
        expect(screen.getByText('LOSS')).toBeInTheDocument();
      });

      // Navigate back.
      fireEvent.click(screen.getByRole('button', { name: 'Previous' }));

      await waitFor(() => {
        expect(screen.getByText('WIN')).toBeInTheDocument();
      });

      // Third call should use no cursor (first page).
      expect(mockGetMatchHistory).toHaveBeenCalledTimes(3);
      expect(mockGetMatchHistory).toHaveBeenNthCalledWith(
        3,
        'clerk-test-token-stub',
        expect.objectContaining({ limit: 20 })
      );
      // cursor_ts and cursor_id must NOT be set for the first page.
      const thirdCall = mockGetMatchHistory.mock.calls[2][1] as Record<string, unknown>;
      expect(thirdCall.cursor_ts).toBeUndefined();
      expect(thirdCall.cursor_id).toBeUndefined();
    });

    it('displays page number', async () => {
      mockGetMatchHistory.mockResolvedValue(
        makeResponse({ data: [makeItem()], has_more: false })
      );

      render(<BffMatchHistory />);

      await waitFor(() => {
        expect(screen.getByText(/Page 1/)).toBeInTheDocument();
      });
    });
  });

  // --------------------------------------------------------------------------
  // Page title
  // --------------------------------------------------------------------------

  describe('Page title', () => {
    it('renders Match History heading', async () => {
      mockGetMatchHistory.mockResolvedValue(makeResponse());

      render(<BffMatchHistory />);

      await waitFor(() => {
        expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent('Match History');
      });
    });
  });

  // --------------------------------------------------------------------------
  // SSE refresh — match.completed (secondary fix, vmt-t#625)
  //
  // The BFF broker publishes contract.DaemonEvent.Type names. The correct event
  // name is "match.completed". The old code subscribed to "stats:updated" which
  // the BFF never emits — meaning the list never auto-refreshed.
  // --------------------------------------------------------------------------

  describe('SSE refresh on match.completed', () => {
    it('registers listener on the match.completed event (not stats:updated)', async () => {
      mockGetMatchHistory.mockResolvedValue(makeResponse());

      render(<BffMatchHistory />);

      await waitFor(() => {
        expect(mockGetMatchHistory).toHaveBeenCalledTimes(1);
      });

      // The component must have registered a callback for 'match.completed'.
      // registeredCallbacks is populated by the EventsOn mock at the top of this file.
      expect(registeredCallbacks['match.completed']).not.toBeNull();
      // Must NOT subscribe to 'stats:updated' — that event is never emitted by the BFF.
      expect(registeredCallbacks['stats:updated']).toBeUndefined();
    });

    it('re-fetches matches when match.completed fires', async () => {
      registeredCallbacks['match.completed'] = null;
      mockGetMatchHistory.mockResolvedValue(makeResponse({ data: [], has_more: false }));

      render(<BffMatchHistory />);

      await waitFor(() => {
        expect(mockGetMatchHistory).toHaveBeenCalledTimes(1);
      });

      // The component must have registered a callback for match.completed.
      expect(registeredCallbacks['match.completed']).not.toBeNull();

      // Simulate the BFF emitting a match.completed event after a new match.
      mockGetMatchHistory.mockResolvedValue(
        makeResponse({
          data: [makeItem({ id: 'new-match', format: 'Standard', result: 'win' })],
          has_more: false,
        })
      );

      await act(async () => {
        registeredCallbacks['match.completed']!();
      });

      await waitFor(() => {
        expect(mockGetMatchHistory).toHaveBeenCalledTimes(2);
        // The refreshed data must be visible.
        expect(screen.getByTestId('match-history-table')).toBeInTheDocument();
      });
    });
  });

  // --------------------------------------------------------------------------
  // Error state
  // --------------------------------------------------------------------------

  describe('Error state', () => {
    it('renders error message when the fetch rejects', async () => {
      mockGetMatchHistory.mockRejectedValue(new Error('Network error'));

      render(<BffMatchHistory />);

      await waitFor(() => {
        expect(screen.getByText('Network error')).toBeInTheDocument();
      });
      expect(screen.queryByTestId('match-history-table')).not.toBeInTheDocument();
      expect(screen.queryByTestId('match-history-empty')).not.toBeInTheDocument();
    });
  });
});
