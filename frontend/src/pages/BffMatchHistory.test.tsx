/**
 * BffMatchHistory component tests.
 *
 * Covers:
 *   - Regression guard for vmt-t#625 (cursor-paginated BFF shape)
 *   - Fix/match-history-detail-drilldown: row click → MatchDetailsModal
 *   - Fix 2: format normalization ('Ladder'→'Ranked', empty→'—')
 *   - Fix 3: result badge ('unknown' → '–', WIN/LOSS preserved)
 *   - fix/match-history-defensive-rendering: display-eligibility filter (Prof RED gate)
 *     Ineligible rows (bad result OR unresolved format) are hidden; all-ineligible → empty-state.
 *   - vmt-t#684: no Cormorant Garamond serif-italic in SPA page title (regression guard)
 *   - vmt-t#685: no lorebook affectations (§ Chapter / The Ledger) in page heading (regression guard)
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { readFileSync } from 'node:fs';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';
import { render, screen, waitFor, fireEvent, act } from '@testing-library/react';
import BffMatchHistory from './BffMatchHistory';
import type { MatchHistoryResponse, MatchHistoryItem } from '@/services/api/bffMatchHistory';
import { models } from '@/types/models';

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

vi.mock('@/services/api/bffMatchHistory', () => ({
  getMatchHistory: vi.fn(),
}));

// Mock matches.getMatch so row-click detail fetch works without a live BFF.
vi.mock('@/services/api', async (importOriginal) => {
  const original = await importOriginal<typeof import('@/services/api')>();
  return {
    ...original,
    matches: {
      ...original.matches,
      getMatch: vi.fn(),
    },
  };
});

// Mock MatchDetailsModal so component tests do not need game/timeline API calls.
vi.mock('../components/MatchDetailsModal', () => ({
  default: ({ onClose }: { onClose: () => void }) => (
    <div data-testid="match-details-modal">
      <button onClick={onClose}>Close</button>
    </div>
  ),
}));

// BffMatchHistory now uses useReadModelUpdates (ADR-084) instead of EventsOn.
// The global setup.ts mock for @/services/websocketClient exposes mockEventEmitter,
// so we import it to simulate readmodel.updated frames in the SSE tests below.
import { mockEventEmitter } from '@/test/mocks/websocketMock';

// Import after mock so we get the vi.fn() version.
import { getMatchHistory } from '@/services/api/bffMatchHistory';
import { matches as matchesApi } from '@/services/api';
const mockGetMatchHistory = vi.mocked(getMatchHistory);
const mockGetMatch = vi.mocked(matchesApi.getMatch);

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

/** Build a minimal models.Match for mocking matchesApi.getMatch responses. */
function makeFullMatch(overrides: Record<string, unknown> = {}): models.Match {
  return new models.Match({
    ID: 'match-1',
    AccountID: 1,
    EventID: 'event-1',
    EventName: 'Standard Event',
    Timestamp: '2026-05-01T14:30:00Z',
    PlayerWins: 2,
    OpponentWins: 0,
    PlayerTeamID: 1,
    Format: 'Standard',
    Result: 'win',
    CreatedAt: '2026-05-01T14:30:00Z',
    ...overrides,
  });
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
    mockEventEmitter.clear();
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
      expect(screen.getByText('No recent matches')).toBeInTheDocument();
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
  // SSE refresh — readmodel.updated matches domain (ADR-084 rewire)
  //
  // BffMatchHistory now subscribes to readmodel.updated (matches domain) instead
  // of the legacy match.completed dot-vocabulary that raced the projection layer
  // (ADR-084 §Context root cause 1). The new subscription fires only after the
  // projection worker has committed the read model — the race is closed.
  // --------------------------------------------------------------------------

  describe('SSE refresh on readmodel.updated matches domain (ADR-084)', () => {
    it('re-fetches matches when readmodel.updated fires for the matches domain', async () => {
      mockGetMatchHistory.mockResolvedValue(makeResponse({ data: [], has_more: false }));

      render(<BffMatchHistory />);

      await waitFor(() => {
        expect(mockGetMatchHistory).toHaveBeenCalledTimes(1);
      });

      // Simulate the BFF emitting readmodel.updated after projection completes.
      mockGetMatchHistory.mockResolvedValue(
        makeResponse({
          data: [makeItem({ id: 'new-match', format: 'Standard', result: 'win' })],
          has_more: false,
        })
      );

      await act(async () => {
        mockEventEmitter.emit('readmodel.updated', { domains: ['matches'] });
      });

      await waitFor(() => {
        expect(mockGetMatchHistory).toHaveBeenCalledTimes(2);
        expect(screen.getByTestId('match-history-table')).toBeInTheDocument();
      });
    });

    it('does NOT re-fetch when readmodel.updated fires for an unrelated domain', async () => {
      mockGetMatchHistory.mockResolvedValue(makeResponse({ data: [], has_more: false }));

      render(<BffMatchHistory />);

      await waitFor(() => {
        expect(mockGetMatchHistory).toHaveBeenCalledTimes(1);
      });

      await act(async () => {
        mockEventEmitter.emit('readmodel.updated', { domains: ['quests'] });
      });

      // Brief settle — should not have triggered a second fetch.
      await new Promise(resolve => setTimeout(resolve, 50));
      expect(mockGetMatchHistory).toHaveBeenCalledTimes(1);
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

  // --------------------------------------------------------------------------
  // Fix 1 — row click opens MatchDetailsModal (detail drill-down)
  // Regression fix: commit 2099d36d dropped the onClick from rows.
  // --------------------------------------------------------------------------

  describe('Row click → MatchDetailsModal (detail drill-down)', () => {
    beforeEach(() => {
      mockGetMatchHistory.mockResolvedValue(
        makeResponse({
          data: [makeItem({ id: 'match-42', format: 'Standard', result: 'win' })],
          has_more: false,
        })
      );
    });

    it('opens MatchDetailsModal when a match row is clicked', async () => {
      mockGetMatch.mockResolvedValue(makeFullMatch({ ID: 'match-42' }));

      render(<BffMatchHistory />);

      // Wait for table to render.
      const row = await screen.findByTestId('match-row');
      expect(screen.queryByTestId('match-details-modal')).not.toBeInTheDocument();

      await act(async () => {
        fireEvent.click(row);
      });

      await waitFor(() => {
        expect(screen.getByTestId('match-details-modal')).toBeInTheDocument();
      });
    });

    it('calls matchesApi.getMatch with the correct match id on row click', async () => {
      mockGetMatch.mockResolvedValue(makeFullMatch({ ID: 'match-42' }));

      render(<BffMatchHistory />);

      const row = await screen.findByTestId('match-row');
      await act(async () => {
        fireEvent.click(row);
      });

      await waitFor(() => {
        expect(mockGetMatch).toHaveBeenCalledWith('match-42');
      });
    });

    it('closes MatchDetailsModal when Close is clicked', async () => {
      mockGetMatch.mockResolvedValue(makeFullMatch({ ID: 'match-42' }));

      render(<BffMatchHistory />);

      const row = await screen.findByTestId('match-row');
      await act(async () => {
        fireEvent.click(row);
      });

      await waitFor(() => {
        expect(screen.getByTestId('match-details-modal')).toBeInTheDocument();
      });

      fireEvent.click(screen.getByRole('button', { name: 'Close' }));

      await waitFor(() => {
        expect(screen.queryByTestId('match-details-modal')).not.toBeInTheDocument();
      });
    });
  });

  // --------------------------------------------------------------------------
  // Fix 2 — format normalization
  // 'Ladder' → 'Ranked'; 'Play' → 'Play Queue'; empty/'Unknown' → '—'
  // --------------------------------------------------------------------------

  describe('Format normalization', () => {
    it('maps raw "Ladder" format to "Ranked"', async () => {
      mockGetMatchHistory.mockResolvedValue(
        makeResponse({
          data: [makeItem({ format: 'Ladder' })],
          has_more: false,
        })
      );

      render(<BffMatchHistory />);

      await waitFor(() => {
        expect(screen.getByTestId('match-history-table')).toBeInTheDocument();
      });

      expect(screen.getByText('Ranked')).toBeInTheDocument();
    });

    it('maps raw "Play" format to "Play Queue"', async () => {
      mockGetMatchHistory.mockResolvedValue(
        makeResponse({
          data: [makeItem({ format: 'Play' })],
          has_more: false,
        })
      );

      render(<BffMatchHistory />);

      await waitFor(() => {
        expect(screen.getByTestId('match-history-table')).toBeInTheDocument();
      });

      expect(screen.getByText('Play Queue')).toBeInTheDocument();
    });

    it('passes through a known format string unchanged (e.g. "Standard")', async () => {
      mockGetMatchHistory.mockResolvedValue(
        makeResponse({
          data: [makeItem({ format: 'Standard' })],
          has_more: false,
        })
      );

      render(<BffMatchHistory />);

      await waitFor(() => {
        expect(screen.getByTestId('match-history-table')).toBeInTheDocument();
      });

      expect(screen.getByText('Standard')).toBeInTheDocument();
    });

    it('hides row and shows empty-state when format is empty string (defensive filter)', async () => {
      // Defensive rendering gate: a row with an unresolved format is ineligible and
      // must be hidden entirely — not rendered as a "—" placeholder.
      mockGetMatchHistory.mockResolvedValue(
        makeResponse({
          data: [makeItem({ format: '' })],
          has_more: false,
        })
      );

      render(<BffMatchHistory />);

      await waitFor(() => {
        expect(screen.getByTestId('match-history-empty')).toBeInTheDocument();
      });
      expect(screen.queryByTestId('match-history-table')).not.toBeInTheDocument();
    });

    it('hides row and shows empty-state when format is "Unknown" (defensive filter)', async () => {
      // Defensive rendering gate: a row with an unresolved format is ineligible and
      // must be hidden entirely.
      mockGetMatchHistory.mockResolvedValue(
        makeResponse({
          data: [makeItem({ format: 'Unknown' })],
          has_more: false,
        })
      );

      render(<BffMatchHistory />);

      await waitFor(() => {
        expect(screen.getByTestId('match-history-empty')).toBeInTheDocument();
      });
      expect(screen.queryByTestId('match-history-table')).not.toBeInTheDocument();
      // Raw "Unknown" string must never appear
      expect(screen.queryByText('Unknown')).not.toBeInTheDocument();
    });
  });

  // --------------------------------------------------------------------------
  // Fix 3 — result badge
  // 'unknown'/'UNKNOWN' → '–'; 'win' → 'WIN'; 'loss' → 'LOSS'
  // --------------------------------------------------------------------------

  describe('Result badge display', () => {
    it('shows "WIN" for result "win"', async () => {
      mockGetMatchHistory.mockResolvedValue(
        makeResponse({ data: [makeItem({ result: 'win' })], has_more: false })
      );

      render(<BffMatchHistory />);

      await waitFor(() => expect(screen.getByText('WIN')).toBeInTheDocument());
    });

    it('shows "LOSS" for result "loss"', async () => {
      mockGetMatchHistory.mockResolvedValue(
        makeResponse({ data: [makeItem({ result: 'loss' })], has_more: false })
      );

      render(<BffMatchHistory />);

      await waitFor(() => expect(screen.getByText('LOSS')).toBeInTheDocument());
    });

    it('hides row and shows empty-state for result "unknown" (defensive filter)', async () => {
      // Defensive rendering gate: a row with an unresolved result is ineligible and
      // must be hidden entirely — not rendered with a "–" placeholder badge.
      mockGetMatchHistory.mockResolvedValue(
        makeResponse({ data: [makeItem({ result: 'unknown' })], has_more: false })
      );

      render(<BffMatchHistory />);

      await waitFor(() => {
        expect(screen.getByTestId('match-history-empty')).toBeInTheDocument();
      });
      expect(screen.queryByTestId('match-history-table')).not.toBeInTheDocument();
      // Must NOT show "UNKNOWN" in any form
      expect(screen.queryByText('UNKNOWN')).not.toBeInTheDocument();
    });

    it('hides row and shows empty-state for empty result string (defensive filter)', async () => {
      // Defensive rendering gate: a row with an empty result is ineligible.
      mockGetMatchHistory.mockResolvedValue(
        makeResponse({ data: [makeItem({ result: '' })], has_more: false })
      );

      render(<BffMatchHistory />);

      await waitFor(() => {
        expect(screen.getByTestId('match-history-empty')).toBeInTheDocument();
      });
      expect(screen.queryByTestId('match-history-table')).not.toBeInTheDocument();
    });
  });

  // --------------------------------------------------------------------------
  // fix/match-history-defensive-rendering — display-eligibility filter
  //
  // Prof RED gate: wrong/placeholder data is worse than no data.
  // A row is eligible ONLY if it has a valid result (win/loss/draw — NOT null,
  // empty, or "unknown") AND a resolved format (normalizeHistoryFormat returns
  // non-empty — NOT null, empty, or "Unknown").
  //
  // Ineligible rows MUST be hidden (not rendered as placeholders).
  // When ALL rows from the BFF are ineligible, the empty-state must show.
  // A 0-0 score must never render as real match data.
  // --------------------------------------------------------------------------

  describe('Defensive rendering — display-eligibility filter (Prof RED gate)', () => {
    // ---- Eligible rows render ---

    it('renders an eligible row that has a valid result AND a resolved format', async () => {
      mockGetMatchHistory.mockResolvedValue(
        makeResponse({
          data: [makeItem({ result: 'win', format: 'Standard', player_wins: 2, opponent_wins: 0 })],
          has_more: false,
        })
      );

      render(<BffMatchHistory />);

      await waitFor(() => {
        expect(screen.getByTestId('match-history-table')).toBeInTheDocument();
      });
      expect(screen.getAllByTestId('match-row')).toHaveLength(1);
    });

    it('renders an eligible loss row', async () => {
      mockGetMatchHistory.mockResolvedValue(
        makeResponse({
          data: [makeItem({ result: 'loss', format: 'Historic', player_wins: 0, opponent_wins: 2 })],
          has_more: false,
        })
      );

      render(<BffMatchHistory />);

      await waitFor(() => {
        expect(screen.getAllByTestId('match-row')).toHaveLength(1);
      });
    });

    // ---- Ineligible: bad result ---

    it('hides a row whose result is empty string', async () => {
      mockGetMatchHistory.mockResolvedValue(
        makeResponse({
          data: [makeItem({ result: '', format: 'Standard' })],
          has_more: false,
        })
      );

      render(<BffMatchHistory />);

      await waitFor(() => {
        // No table rows rendered — the empty-state shows instead.
        expect(screen.queryByTestId('match-row')).not.toBeInTheDocument();
      });
      expect(screen.getByTestId('match-history-empty')).toBeInTheDocument();
    });

    it('hides a row whose result is "unknown"', async () => {
      mockGetMatchHistory.mockResolvedValue(
        makeResponse({
          data: [makeItem({ result: 'unknown', format: 'Standard' })],
          has_more: false,
        })
      );

      render(<BffMatchHistory />);

      await waitFor(() => {
        expect(screen.queryByTestId('match-row')).not.toBeInTheDocument();
      });
      expect(screen.getByTestId('match-history-empty')).toBeInTheDocument();
    });

    it('hides a row whose result is "-" (single dash placeholder)', async () => {
      mockGetMatchHistory.mockResolvedValue(
        makeResponse({
          data: [makeItem({ result: '-', format: 'Standard' })],
          has_more: false,
        })
      );

      render(<BffMatchHistory />);

      await waitFor(() => {
        expect(screen.queryByTestId('match-row')).not.toBeInTheDocument();
      });
      expect(screen.getByTestId('match-history-empty')).toBeInTheDocument();
    });

    // ---- Ineligible: unresolved format ---

    it('hides a row whose format is empty string', async () => {
      mockGetMatchHistory.mockResolvedValue(
        makeResponse({
          data: [makeItem({ result: 'win', format: '' })],
          has_more: false,
        })
      );

      render(<BffMatchHistory />);

      await waitFor(() => {
        expect(screen.queryByTestId('match-row')).not.toBeInTheDocument();
      });
      expect(screen.getByTestId('match-history-empty')).toBeInTheDocument();
    });

    it('hides a row whose format is "Unknown"', async () => {
      mockGetMatchHistory.mockResolvedValue(
        makeResponse({
          data: [makeItem({ result: 'win', format: 'Unknown' })],
          has_more: false,
        })
      );

      render(<BffMatchHistory />);

      await waitFor(() => {
        expect(screen.queryByTestId('match-row')).not.toBeInTheDocument();
      });
      expect(screen.getByTestId('match-history-empty')).toBeInTheDocument();
    });

    it('hides a row whose format is null', async () => {
      mockGetMatchHistory.mockResolvedValue(
        makeResponse({
          data: [makeItem({ result: 'win', format: null as unknown as string })],
          has_more: false,
        })
      );

      render(<BffMatchHistory />);

      await waitFor(() => {
        expect(screen.queryByTestId('match-row')).not.toBeInTheDocument();
      });
      expect(screen.getByTestId('match-history-empty')).toBeInTheDocument();
    });

    // ---- Placeholder score: 0-0 must never appear ---

    it('never renders a 0-0 score row — hides the ineligible row instead', async () => {
      mockGetMatchHistory.mockResolvedValue(
        makeResponse({
          data: [makeItem({ result: 'unknown', format: 'Standard', player_wins: 0, opponent_wins: 0 })],
          has_more: false,
        })
      );

      render(<BffMatchHistory />);

      await waitFor(() => {
        expect(screen.queryByTestId('match-row')).not.toBeInTheDocument();
      });
      expect(screen.queryByText('0–0')).not.toBeInTheDocument();
      expect(screen.getByTestId('match-history-empty')).toBeInTheDocument();
    });

    // ---- Mixed rows: only eligible rows render ---

    it('renders only eligible rows when the response mixes eligible and ineligible', async () => {
      mockGetMatchHistory.mockResolvedValue(
        makeResponse({
          data: [
            makeItem({ id: 'good-1', result: 'win', format: 'Standard' }),
            makeItem({ id: 'bad-no-result', result: '', format: 'Standard' }),
            makeItem({ id: 'good-2', result: 'loss', format: 'Historic' }),
            makeItem({ id: 'bad-no-format', result: 'win', format: '' }),
            makeItem({ id: 'bad-unknown-result', result: 'unknown', format: 'Standard' }),
          ],
          has_more: false,
        })
      );

      render(<BffMatchHistory />);

      await waitFor(() => {
        const rows = screen.getAllByTestId('match-row');
        expect(rows).toHaveLength(2);
      });
      // Table still shows (there are eligible rows).
      expect(screen.getByTestId('match-history-table')).toBeInTheDocument();
      // The empty-state must NOT show when there are eligible rows.
      expect(screen.queryByTestId('match-history-empty')).not.toBeInTheDocument();
    });

    // ---- All ineligible → honest empty-state ---

    it('shows the honest empty-state when ALL rows from the BFF are ineligible', async () => {
      mockGetMatchHistory.mockResolvedValue(
        makeResponse({
          data: [
            makeItem({ id: 'b1', result: '', format: 'Standard' }),
            makeItem({ id: 'b2', result: 'unknown', format: 'Standard' }),
            makeItem({ id: 'b3', result: 'win', format: '' }),
            makeItem({ id: 'b4', result: '-', format: 'Standard' }),
          ],
          has_more: false,
        })
      );

      render(<BffMatchHistory />);

      await waitFor(() => {
        expect(screen.getByTestId('match-history-empty')).toBeInTheDocument();
      });
      // Must show the processing-aware empty message, not "No matches yet".
      expect(screen.getByText(/new matches usually appear/i)).toBeInTheDocument();
      // No table rows.
      expect(screen.queryByTestId('match-row')).not.toBeInTheDocument();
      expect(screen.queryByTestId('match-history-table')).not.toBeInTheDocument();
    });

    it('shows the processing-aware empty-state message for the honest empty-state', async () => {
      mockGetMatchHistory.mockResolvedValue(
        makeResponse({ data: [], has_more: false })
      );

      render(<BffMatchHistory />);

      await waitFor(() => {
        expect(screen.getByTestId('match-history-empty')).toBeInTheDocument();
      });
      // The honest empty-state must reflect that data is still loading/processing.
      expect(screen.getByText(/new matches usually appear/i)).toBeInTheDocument();
    });
  });

  // --------------------------------------------------------------------------
  // Font regression guard (#684): no Cormorant Garamond in the SPA CSS
  // --------------------------------------------------------------------------

  describe('Font regression — no Cormorant Garamond (#684)', () => {
    const CSS_PATH = join(dirname(fileURLToPath(import.meta.url)), 'BffMatchHistory.css');

    it('BffMatchHistory.css contains no Cormorant Garamond reference', () => {
      const css = readFileSync(CSS_PATH, 'utf8');
      expect(css.toLowerCase()).not.toContain('cormorant');
      expect(css.toLowerCase()).not.toContain('garamond');
    });
  });

  // --------------------------------------------------------------------------
  // Heading copy regression guard (#685): no lorebook affectations
  // --------------------------------------------------------------------------

  describe('Heading copy — no lorebook affectations (#685)', () => {
    it('page title reads "Match History" — no § Chapter / Ledger pattern', async () => {
      mockGetMatchHistory.mockResolvedValue(makeResponse({ data: [], has_more: false }));

      render(<BffMatchHistory />);

      await waitFor(() => {
        expect(screen.queryByText('Loading matches...')).not.toBeInTheDocument();
      });

      const h1 = screen.getByRole('heading', { level: 1 });
      expect(h1).toHaveTextContent('Match History');
      expect(h1.textContent).not.toMatch(/§|Chapter|Ledger|Compendium/);
    });
  });

  // --------------------------------------------------------------------------
  // vmt-t#687 — On-the-play / on-the-draw indicator
  //
  // Prof requirement: "every player asks 'was I on the play?' after a loss."
  // Null must NEVER render a misleading badge — blank cell only.
  // --------------------------------------------------------------------------

  describe('On-the-play / on-the-draw indicator (#687)', () => {
    it('renders "P" badge when player_on_play is true', async () => {
      mockGetMatchHistory.mockResolvedValue(
        makeResponse({
          data: [makeItem({ player_on_play: true })],
          has_more: false,
        })
      );

      render(<BffMatchHistory />);

      await waitFor(() => {
        expect(screen.getByTestId('play-draw-badge')).toBeInTheDocument();
      });
      expect(screen.getByTestId('play-draw-badge')).toHaveTextContent('P');
    });

    it('renders "D" badge when player_on_play is false', async () => {
      mockGetMatchHistory.mockResolvedValue(
        makeResponse({
          data: [makeItem({ player_on_play: false })],
          has_more: false,
        })
      );

      render(<BffMatchHistory />);

      await waitFor(() => {
        expect(screen.getByTestId('play-draw-badge')).toBeInTheDocument();
      });
      expect(screen.getByTestId('play-draw-badge')).toHaveTextContent('D');
    });

    it('renders no play-draw badge when player_on_play is null', async () => {
      mockGetMatchHistory.mockResolvedValue(
        makeResponse({
          data: [makeItem({ player_on_play: null })],
          has_more: false,
        })
      );

      render(<BffMatchHistory />);

      // Row must render (it is still eligible) but badge must be absent.
      await waitFor(() => {
        expect(screen.getByTestId('match-history-table')).toBeInTheDocument();
      });
      expect(screen.queryByTestId('play-draw-badge')).not.toBeInTheDocument();
      // Must not render 'P' or 'D' as loose text.
      expect(screen.queryByText('P')).not.toBeInTheDocument();
      expect(screen.queryByText('D')).not.toBeInTheDocument();
    });

    it('renders no play-draw badge when player_on_play is absent (undefined)', async () => {
      // Simulates pre-#687 match rows where the field is omitted from JSON.
      const item = makeItem();
      // delete the field to simulate omitempty absence
      delete (item as Partial<MatchHistoryItem>).player_on_play;

      mockGetMatchHistory.mockResolvedValue(
        makeResponse({
          data: [item],
          has_more: false,
        })
      );

      render(<BffMatchHistory />);

      await waitFor(() => {
        expect(screen.getByTestId('match-history-table')).toBeInTheDocument();
      });
      expect(screen.queryByTestId('play-draw-badge')).not.toBeInTheDocument();
    });

    it('P/D column header is present in the table', async () => {
      mockGetMatchHistory.mockResolvedValue(
        makeResponse({ data: [makeItem()], has_more: false })
      );

      render(<BffMatchHistory />);

      await waitFor(() => {
        expect(screen.getByRole('table')).toBeInTheDocument();
      });

      const headers = screen.getAllByRole('columnheader');
      const headerTexts = headers.map((h) => h.textContent);
      expect(headerTexts).toContain('P/D');
    });

    it('play-draw cell is present with testid even when no badge rendered', async () => {
      mockGetMatchHistory.mockResolvedValue(
        makeResponse({
          data: [makeItem({ player_on_play: null })],
          has_more: false,
        })
      );

      render(<BffMatchHistory />);

      await waitFor(() => {
        expect(screen.getByTestId('match-play-draw')).toBeInTheDocument();
      });
      // Cell must be present but empty (no badge).
      expect(screen.getByTestId('match-play-draw').children).toHaveLength(0);
    });
  });

  // --------------------------------------------------------------------------
  // vmt-t#687 — Game score column
  //
  // Score was already rendered (player_wins–opponent_wins) but now has an
  // explicit data-testid="match-score" and is verified here.
  // --------------------------------------------------------------------------

  describe('Game score column (#687)', () => {
    it('renders score as "2-1" from player_wins=2, opponent_wins=1', async () => {
      mockGetMatchHistory.mockResolvedValue(
        makeResponse({
          data: [makeItem({ player_wins: 2, opponent_wins: 1 })],
          has_more: false,
        })
      );

      render(<BffMatchHistory />);

      await waitFor(() => {
        expect(screen.getByTestId('match-score')).toBeInTheDocument();
      });
      expect(screen.getByTestId('match-score')).toHaveTextContent('2–1');
    });

    it('renders score as "2-0" from player_wins=2, opponent_wins=0', async () => {
      mockGetMatchHistory.mockResolvedValue(
        makeResponse({
          data: [makeItem({ player_wins: 2, opponent_wins: 0 })],
          has_more: false,
        })
      );

      render(<BffMatchHistory />);

      await waitFor(() => {
        expect(screen.getByTestId('match-score')).toHaveTextContent('2–0');
      });
    });

    it('renders score as "0-2" from player_wins=0, opponent_wins=2', async () => {
      mockGetMatchHistory.mockResolvedValue(
        makeResponse({
          data: [makeItem({ result: 'loss', player_wins: 0, opponent_wins: 2 })],
          has_more: false,
        })
      );

      render(<BffMatchHistory />);

      await waitFor(() => {
        expect(screen.getByTestId('match-score')).toHaveTextContent('0–2');
      });
    });
  });

  // --------------------------------------------------------------------------
  // vmt-t#687 — Opponent archetype / name column
  //
  // Render the opponent name when present; render nothing when absent.
  // Bot matches and pre-#003 events have no opponent_name.
  // --------------------------------------------------------------------------

  describe('Opponent archetype column (#687)', () => {
    it('renders opponent name when opponent_name is present', async () => {
      mockGetMatchHistory.mockResolvedValue(
        makeResponse({
          data: [makeItem({ opponent_name: 'Grixis Midrange' })],
          has_more: false,
        })
      );

      render(<BffMatchHistory />);

      await waitFor(() => {
        expect(screen.getByTestId('match-opponent')).toBeInTheDocument();
      });
      expect(screen.getByText('Grixis Midrange')).toBeInTheDocument();
    });

    it('renders empty opponent cell when opponent_name is absent (undefined)', async () => {
      const item = makeItem();
      // Ensure no opponent_name key (as it would be from omitempty JSON)
      delete (item as Partial<MatchHistoryItem>).opponent_name;

      mockGetMatchHistory.mockResolvedValue(
        makeResponse({ data: [item], has_more: false })
      );

      render(<BffMatchHistory />);

      await waitFor(() => {
        expect(screen.getByTestId('match-opponent')).toBeInTheDocument();
      });
      // Cell present but no text content inside it.
      expect(screen.getByTestId('match-opponent').children).toHaveLength(0);
    });

    it('renders empty opponent cell when opponent_name is empty string', async () => {
      mockGetMatchHistory.mockResolvedValue(
        makeResponse({
          data: [makeItem({ opponent_name: '' })],
          has_more: false,
        })
      );

      render(<BffMatchHistory />);

      await waitFor(() => {
        expect(screen.getByTestId('match-opponent')).toBeInTheDocument();
      });
      // Empty string is falsy — no inner span should render.
      expect(screen.getByTestId('match-opponent').children).toHaveLength(0);
    });

    it('renders Opponent column header in the table', async () => {
      mockGetMatchHistory.mockResolvedValue(
        makeResponse({ data: [makeItem()], has_more: false })
      );

      render(<BffMatchHistory />);

      await waitFor(() => {
        expect(screen.getByRole('table')).toBeInTheDocument();
      });

      const headers = screen.getAllByRole('columnheader');
      const headerTexts = headers.map((h) => h.textContent);
      expect(headerTexts).toContain('Opponent');
    });

    it('multiple rows with different opponent names each render their own', async () => {
      mockGetMatchHistory.mockResolvedValue(
        makeResponse({
          data: [
            makeItem({ id: 'a', opponent_name: 'Esper Reanimator' }),
            makeItem({ id: 'b', result: 'loss', opponent_name: 'Azorius Soldiers' }),
          ],
          has_more: false,
        })
      );

      render(<BffMatchHistory />);

      await waitFor(() => {
        expect(screen.getByText('Esper Reanimator')).toBeInTheDocument();
      });
      expect(screen.getByText('Azorius Soldiers')).toBeInTheDocument();
    });
  });

  // --------------------------------------------------------------------------
  // vmt-t#687 — Combined new fields rendering
  // --------------------------------------------------------------------------

  describe('Combined new fields — play/draw + score + opponent (#687)', () => {
    it('renders all three fields in a single row', async () => {
      mockGetMatchHistory.mockResolvedValue(
        makeResponse({
          data: [
            makeItem({
              player_wins: 2,
              opponent_wins: 1,
              player_on_play: true,
              opponent_name: 'Sultai Ramp',
            }),
          ],
          has_more: false,
        })
      );

      render(<BffMatchHistory />);

      await waitFor(() => {
        expect(screen.getByTestId('play-draw-badge')).toBeInTheDocument();
      });
      expect(screen.getByTestId('play-draw-badge')).toHaveTextContent('P');
      expect(screen.getByTestId('match-score')).toHaveTextContent('2–1');
      expect(screen.getByText('Sultai Ramp')).toBeInTheDocument();
    });

    it('renders row with null play/draw and no opponent (pre-release row)', async () => {
      mockGetMatchHistory.mockResolvedValue(
        makeResponse({
          data: [
            makeItem({
              player_wins: 1,
              opponent_wins: 2,
              result: 'loss',
              player_on_play: null,
            }),
          ],
          has_more: false,
        })
      );

      render(<BffMatchHistory />);

      await waitFor(() => {
        expect(screen.getByTestId('match-history-table')).toBeInTheDocument();
      });
      // No badge for null play/draw.
      expect(screen.queryByTestId('play-draw-badge')).not.toBeInTheDocument();
      // Score still renders.
      expect(screen.getByTestId('match-score')).toHaveTextContent('1–2');
      // No opponent text.
      expect(screen.getByTestId('match-opponent').children).toHaveLength(0);
    });
  });
});
