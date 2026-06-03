import { useState, useEffect, useRef, useCallback } from 'react';
import { useAuth } from '@clerk/react';
import { RectangleStackIcon } from '@heroicons/react/24/outline';
import { getMatchHistory } from '@/services/api/bffMatchHistory';
import type { MatchHistoryItem } from '@/services/api/bffMatchHistory';
import { matches as matchesApi } from '@/services/api';
import { models } from '@/types/models';
import { EventsOn } from '@/services/websocketClient';
import LoadingSpinner from '../components/LoadingSpinner';
import ColorIdentity from '../components/ColorIdentity';
import EmptyState from '../components/EmptyState';
import MatchDetailsModal from '../components/MatchDetailsModal';
import { normalizeHistoryFormat } from '@/utils/formatNormalization';
import { trackEvent } from '@/services/analytics';
import './BffMatchHistory.css';

const FIRST_DATA_FLAG = 'vaultmtg_ph_funnel_first_data_loaded_fired';

const PAGE_SIZE = 20;

/**
 * Cursor entry for a page in the history stack.
 * The first page uses undefined cursor (no cursor params sent).
 * Subsequent pages use the cursor returned by the previous page's response.
 */
interface PageCursor {
  cursor_ts?: string;
  cursor_id?: string;
}

/**
 * Map a raw result string from MatchHistoryItem to a display value.
 *
 * - 'win' → 'WIN', 'loss' → 'LOSS' (preserved as-is, uppercased)
 * - 'unknown', empty string, or any indeterminate value → '–'
 *
 * This keeps the display clean for matches whose result is genuinely not
 * yet determined or whose data has not been enriched by the daemon pipeline.
 */
function displayResult(result: string): string {
  if (!result || result.toLowerCase() === 'unknown') return '–';
  return result.toUpperCase();
}

/**
 * Render the on-the-play / on-the-draw badge label.
 *
 * - true  → 'P' (on the play)
 * - false → 'D' (on the draw)
 * - null/undefined → null (render nothing — MUST NOT show a fake value)
 *
 * Prof requirement: null must never render a misleading value.
 * Pre-release matches and GRE-buffer misses produce null — those rows
 * render a blank cell, not a badge.
 */
function playDrawLabel(playerOnPlay: boolean | null | undefined): string | null {
  if (playerOnPlay === true) return 'P';
  if (playerOnPlay === false) return 'D';
  return null;
}

/**
 * Set of result values that are considered resolved/terminal.
 * Only rows with a resolved result are eligible to render.
 */
const RESOLVED_RESULTS = new Set(['win', 'loss', 'draw']);

/**
 * Determine whether a MatchHistoryItem is eligible for display.
 *
 * A row is eligible only when:
 *   1. Its result is a known terminal value (win / loss / draw).
 *      Null, empty, "unknown", or "-" are all ineligible.
 *   2. Its format resolves to a non-empty display string.
 *      Null, empty, or "Unknown" all produce '' from normalizeHistoryFormat
 *      and are therefore ineligible.
 *
 * This is the defensive gate Prof requires: wrong/placeholder data is worse
 * than no data. Edge cases (disconnects, timeouts, concedes) will always
 * produce partial rows — we hide them rather than display garbage.
 *
 * Note for Bob: the signal distinguishing resolved vs unresolved is
 * result-present (non-empty, non-unknown) + format-present (normalizeHistoryFormat
 * returns non-empty). If the BFF history response gains an explicit
 * "pending" or "processing" field in the future, this function is the
 * right place to consume it — but the current filter works off the
 * existing shape.
 */
function isEligibleRow(item: MatchHistoryItem): boolean {
  const hasValidResult = Boolean(item.result) && RESOLVED_RESULTS.has(item.result.toLowerCase());
  const hasResolvedFormat = Boolean(normalizeHistoryFormat(item.format));
  return hasValidResult && hasResolvedFormat;
}

const BffMatchHistory = () => {
  const { getToken, isSignedIn } = useAuth();
  // Stable ref so useCallback / useEffect deps don't re-fire on every render
  const getTokenRef = useRef(getToken);
  useEffect(() => { getTokenRef.current = getToken; });

  const [matches, setMatches] = useState<MatchHistoryItem[]>([]);
  const [hasMore, setHasMore] = useState(false);
  const [nextCursorTS, setNextCursorTS] = useState<string | undefined>(undefined);
  const [nextCursorID, setNextCursorID] = useState<string | undefined>(undefined);
  // Stack of cursors used to navigate backwards. Each entry is the cursor that
  // was used to fetch that page, so popping gives us the cursor for the previous page.
  const [cursorHistory, setCursorHistory] = useState<PageCursor[]>([]);
  const [currentPage, setCurrentPage] = useState(1);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // Detail modal state — null means closed.
  const [selectedMatch, setSelectedMatch] = useState<models.Match | null>(null);
  const [detailLoading, setDetailLoading] = useState(false);

  const fetchPage = useCallback(
    async (cursor: PageCursor, page: number, newHistory: PageCursor[]) => {
      if (!isSignedIn) return;
      setLoading(true);
      setError(null);
      try {
        const token = await getTokenRef.current();
        if (!token) throw new Error('No auth token');
        const resp = await getMatchHistory(token, {
          limit: PAGE_SIZE,
          cursor_ts: cursor.cursor_ts,
          cursor_id: cursor.cursor_id,
        });
        setMatches(resp.data);
        setHasMore(resp.has_more);
        setNextCursorTS(resp.next_cursor_ts);
        setNextCursorID(resp.next_cursor_id);
        setCursorHistory(newHistory);
        setCurrentPage(page);
        // Fire funnel_first_data_loaded once per user (localStorage guard).
        if (resp.data.length > 0 && !localStorage.getItem(FIRST_DATA_FLAG)) {
          trackEvent({ name: 'funnel_first_data_loaded', properties: { match_count: resp.data.length } });
          localStorage.setItem(FIRST_DATA_FLAG, '1');
        }
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Failed to load match history');
      } finally {
        setLoading(false);
      }
    },
    [isSignedIn]
  );

  // Load the first page on mount / sign-in change.
  const loadFirstPage = useCallback(() => {
    void fetchPage({}, 1, []);
  }, [fetchPage]);

  useEffect(() => {
    loadFirstPage();
  }, [loadFirstPage]);

  // Refresh the first page when BFF emits match.completed over SSE.
  // The BFF broker publishes contract.DaemonEvent.Type names; the correct event
  // name is "match.completed" (not "stats:updated" which is never emitted).
  useEffect(() => {
    const unsub = EventsOn('match.completed', loadFirstPage);
    return unsub;
  }, [loadFirstPage]);

  const handleNext = () => {
    if (!hasMore || !nextCursorTS || !nextCursorID) return;
    // Push the current page's cursor onto the history stack so we can go back.
    const newHistory: PageCursor[] = [
      ...cursorHistory,
      { cursor_ts: cursorHistory.length === 0 ? undefined : nextCursorTS, cursor_id: cursorHistory.length === 0 ? undefined : nextCursorID },
    ];
    void fetchPage({ cursor_ts: nextCursorTS, cursor_id: nextCursorID }, currentPage + 1, newHistory);
  };

  const handlePrev = () => {
    if (cursorHistory.length === 0) return;
    const newHistory = cursorHistory.slice(0, -1);
    const prevCursor = newHistory.length === 0 ? {} : cursorHistory[newHistory.length - 1];
    void fetchPage(prevCursor, currentPage - 1, newHistory);
  };

  const hasPrev = cursorHistory.length > 0;

  const formatDate = (iso: string) =>
    new Date(iso).toLocaleDateString(undefined, {
      year: 'numeric',
      month: 'short',
      day: 'numeric',
    });

  /**
   * Open the detail modal for a row.
   *
   * Fetches the full models.Match via GET /api/v1/matches/{id} (PascalCase shape)
   * which is the shape MatchDetailsModal already expects. The list view only has
   * the thin MatchHistoryItem, so we need a separate fetch.
   */
  const handleRowClick = useCallback(async (item: MatchHistoryItem) => {
    setDetailLoading(true);
    try {
      const fullMatch = await matchesApi.getMatch(item.id);
      setSelectedMatch(new models.Match(fullMatch));
      trackEvent({
        name: 'feature_match_details_opened',
        properties: {
          match_result: item.result.toLowerCase() as 'win' | 'loss' | 'draw',
          format: item.format ?? '',
        },
      });
    } catch {
      // Non-fatal — log silently; the row click fails gracefully.
      console.error('Failed to load match details for', item.id);
    } finally {
      setDetailLoading(false);
    }
  }, []);

  // Apply the display-eligibility filter: only rows with a resolved result
  // AND a resolved format are eligible to render. Ineligible rows are hidden.
  const eligibleMatches = matches.filter(isEligibleRow);

  const isEmpty = !loading && !error && eligibleMatches.length === 0;
  const hasData = !loading && !error && eligibleMatches.length > 0;

  return (
    <div className="page-container" data-testid="match-history-page">
      <div className="bff-match-history-header">
        <h1 className="page-title">Match History</h1>
      </div>

      {loading && <LoadingSpinner message="Loading matches..." />}

      {!loading && error && (
        <div className="error-state">
          <p>{error}</p>
        </div>
      )}

      {isEmpty && (
        <div data-testid="match-history-empty">
          <EmptyState
            icon={<RectangleStackIcon className="w-12 h-12" aria-hidden="true" style={{ color: 'var(--vault-fg-muted)' }} />}
            heading="No recent matches"
            subtext="Your recent matches are loading — new matches usually appear within a minute."
            variant="no-data"
          />
        </div>
      )}

      {hasData && (
        <>
          <div className="bff-match-history-table-wrapper">
            <table data-testid="match-history-table">
              <thead>
                <tr>
                  <th>Date</th>
                  <th>Format</th>
                  <th>Result</th>
                  <th>Score</th>
                  <th title="On the Play (P) or On the Draw (D)">P/D</th>
                  <th>Opponent</th>
                </tr>
              </thead>
              <tbody>
                {eligibleMatches.map((match) => {
                  const displayFormat = normalizeHistoryFormat(match.format);
                  const resultLabel = displayResult(match.result);
                  const resultClass = match.result.toLowerCase();
                  const pdLabel = playDrawLabel(match.player_on_play);
                  // Treat empty string the same as absent — only render when truthy.
                  const opponentDisplay = match.opponent_name || null;
                  // deck_color_identity: optional BFF field (planned) — null-safe, no pips if absent
                  const colorIdentity = (match as MatchHistoryItem & { deck_color_identity?: string[] }).deck_color_identity ?? null;
                  return (
                    <tr
                      key={match.id}
                      className={`result-${resultClass} clickable-row`}
                      onClick={() => { void handleRowClick(match); }}
                      title="Click to view match details"
                      style={{ cursor: 'pointer' }}
                      data-testid="match-row"
                    >
                      <td>{formatDate(match.timestamp)}</td>
                      <td>
                        <span style={{ display: 'inline-flex', alignItems: 'center', gap: 'var(--space-1)' }}>
                          {colorIdentity && colorIdentity.length > 0 && (
                            <ColorIdentity colors={colorIdentity} size="sm" />
                          )}
                          {displayFormat}
                        </span>
                      </td>
                      <td>
                        <span className={`result-badge ${resultClass}`}>
                          {resultLabel}
                        </span>
                      </td>
                      <td data-testid="match-score">{match.player_wins}–{match.opponent_wins}</td>
                      <td data-testid="match-play-draw">
                        {pdLabel !== null && (
                          <span className={`play-draw-badge ${pdLabel === 'P' ? 'on-play' : 'on-draw'}`} data-testid="play-draw-badge">
                            {pdLabel}
                          </span>
                        )}
                      </td>
                      <td data-testid="match-opponent">
                        {opponentDisplay !== null && (
                          <span className="opponent-name">{opponentDisplay}</span>
                        )}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>

          <div className="bff-match-history-footer">
            <div className="pagination">
              <button
                className="pagination-btn"
                onClick={handlePrev}
                disabled={!hasPrev}
              >
                Previous
              </button>
              <span className="pagination-info">
                Page {currentPage}
              </span>
              <button
                className="pagination-btn"
                onClick={handleNext}
                disabled={!hasMore}
              >
                Next
              </button>
            </div>
          </div>
        </>
      )}

      {/* Detail loading indicator — shown while fetching the full match on row click */}
      {detailLoading && (
        <div data-testid="detail-loading-indicator" style={{ display: 'none' }} aria-hidden="true" />
      )}

      {/* Match detail modal — opened when a row is clicked */}
      {selectedMatch && (
        <MatchDetailsModal
          match={selectedMatch}
          onClose={() => setSelectedMatch(null)}
        />
      )}
    </div>
  );
};

export default BffMatchHistory;
