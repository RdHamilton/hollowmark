/**
 * BFF match history adapter.
 *
 * Targets GET /api/v1/history/matches on the BFF.
 * The endpoint is Clerk-protected and returns a cursor-paginated list of matches.
 *
 * Response shape (cursorPaginatedMatchResponse from history.go):
 *   {
 *     "data": MatchHistoryItem[],
 *     "has_more": boolean,
 *     "next_cursor_ts": string,   // omitempty — absent when has_more is false
 *     "next_cursor_id": string,   // omitempty — absent when has_more is false
 *     "limit": number
 *   }
 *
 * Query params:
 *   - limit:      page size (1–100, default 20)
 *   - format:     optional format filter
 *   - cursor_ts:  RFC3339Nano timestamp from previous response's next_cursor_ts
 *   - cursor_id:  match ID from previous response's next_cursor_id
 *
 * Both cursor_ts + cursor_id must be supplied together; omitting both returns
 * the first page.
 *
 * NOTE: This adapter calls fetch directly (does NOT use apiClient.get) because
 * the history endpoint returns a top-level JSON object — not the
 * { data: T } envelope that apiClient.get unwraps. Using apiClient here would
 * double-unwrap and lose the pagination tokens.
 */

import { getApiConfig, ApiRequestError } from '../apiClient';

// ---------------------------------------------------------------------------
// Types — mirror history.go matchResponse + cursorPaginatedMatchResponse
// ---------------------------------------------------------------------------

/**
 * A single match row from GET /api/v1/history/matches.
 * Field names match history.go matchResponse JSON tags exactly.
 */
export interface MatchHistoryItem {
  id: string;
  format: string;
  result: string;
  /** ISO-8601 timestamp of when the match was played. */
  timestamp: string;
  player_wins: number;
  opponent_wins: number;
  duration_seconds: number | null;
  deck_id: string | null;
  rank_before: string | null;
  rank_after: string | null;
  opponent_rank: string | null;
  /**
   * Whether the local player was on the play (true) or draw (false) in game 1.
   * Absent from JSON (omitempty) for pre-#687 matches or GRE-buffer misses —
   * treat absent/null as unknown; MUST NOT render a misleading value.
   */
  player_on_play?: boolean | null;
  /**
   * Opponent MTGA display name. Absent from JSON (omitempty) for bots or
   * pre-#003 events. Absent → show nothing.
   */
  opponent_name?: string;
}

/**
 * Query params accepted by GET /api/v1/history/matches.
 */
export interface MatchHistoryParams {
  limit?: number;
  format?: string;
  /** Keyset cursor — supply together with cursor_id to fetch the next page. */
  cursor_ts?: string;
  cursor_id?: string;
}

/**
 * The cursor-paginated envelope returned by the BFF history endpoint.
 * Matches cursorPaginatedMatchResponse in history.go.
 */
export interface MatchHistoryResponse {
  data: MatchHistoryItem[];
  has_more: boolean;
  /** Present only when has_more === true (omitempty in Go). */
  next_cursor_ts?: string;
  /** Present only when has_more === true (omitempty in Go). */
  next_cursor_id?: string;
  limit: number;
}

// ---------------------------------------------------------------------------
// Adapter
// ---------------------------------------------------------------------------

/**
 * Fetch cursor-paginated match history from the BFF.
 *
 * Targets: GET /api/v1/history/matches
 *
 * The endpoint is Clerk-protected — pass the Clerk session token obtained via
 * `useAuth().getToken()` as the `clerkToken` parameter.
 *
 * To page forward, pass next_cursor_ts + next_cursor_id from the previous
 * response as cursor_ts + cursor_id in params. Both must be supplied together
 * (or both omitted to get the first page).
 *
 * @param clerkToken  Clerk session JWT returned by useAuth().getToken()
 * @param params      Optional pagination + filter params
 */
export async function getMatchHistory(
  clerkToken: string,
  params: MatchHistoryParams = {}
): Promise<MatchHistoryResponse> {
  const { baseUrl } = getApiConfig();
  const url = new URL(`${baseUrl}/history/matches`);

  if (params.limit !== undefined) {
    url.searchParams.set('limit', String(params.limit));
  }
  if (params.format) {
    url.searchParams.set('format', params.format);
  }
  if (params.cursor_ts !== undefined) {
    url.searchParams.set('cursor_ts', params.cursor_ts);
  }
  if (params.cursor_id !== undefined) {
    url.searchParams.set('cursor_id', params.cursor_id);
  }

  const response = await fetch(url.toString(), {
    method: 'GET',
    headers: {
      'Content-Type': 'application/json',
      Authorization: `Bearer ${clerkToken}`,
    },
  });

  if (!response.ok) {
    let errorMessage = response.statusText || 'Request failed';
    try {
      const body = (await response.json()) as { error?: string; message?: string };
      errorMessage = body.message ?? body.error ?? errorMessage;
    } catch {
      // ignore parse failure — use statusText
    }
    throw new ApiRequestError(errorMessage, response.status);
  }

  return response.json() as Promise<MatchHistoryResponse>;
}
