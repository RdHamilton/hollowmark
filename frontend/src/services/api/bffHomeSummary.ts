/**
 * BFF home summary adapter.
 *
 * Targets GET /api/v1/history/summary on the BFF.
 * The endpoint is Clerk-protected and returns a player's session-scoped summary
 * for the home command-strip: today's record, this-week record + win rate,
 * all-time record + streak, and last-match context.
 *
 * BFF contract (Bob implements; Frank wires against this shape):
 *   GET /api/v1/history/summary
 *   Response:
 *   {
 *     "today":     { "wins": int, "losses": int, "win_rate": float },
 *     "this_week": { "wins": int, "losses": int, "win_rate": float, "matches": int },
 *     "all_time":  { "wins": int, "losses": int, "win_rate": float, "matches": int,
 *                    "current_streak": int, "streak_type": "W" | "L" },
 *     "last_match": { "result": "win" | "loss", "opponent_archetype": string | null,
 *                     "elapsed_seconds": int }
 *   }
 *
 * TODO: When Bob ships GET /api/v1/history/summary, remove the mock stub from
 *       getHomeSummary and confirm the integration test in bffHomeSummary.test.ts
 *       passes against a real staging response.
 *
 * Contract test: bffHomeSummary.test.ts asserts the response shape so that any
 * field renames from Bob's side break a test before reaching prod.
 */

import { getApiConfig, ApiRequestError } from '../apiClient';

// ---------------------------------------------------------------------------
// Types — mirror the BFF summary response shape exactly
// ---------------------------------------------------------------------------

export interface TodayRecord {
  wins: number;
  losses: number;
  win_rate: number;
}

export interface WeekRecord {
  wins: number;
  losses: number;
  win_rate: number;
  matches: number;
}

export interface AllTimeRecord {
  wins: number;
  losses: number;
  win_rate: number;
  matches: number;
  current_streak: number;
  streak_type: 'W' | 'L';
}

export interface LastMatch {
  result: 'win' | 'loss';
  opponent_archetype: string | null;
  elapsed_seconds: number;
}

export interface HomeSummaryResponse {
  today: TodayRecord;
  this_week: WeekRecord;
  all_time: AllTimeRecord;
  last_match: LastMatch | null;
}

// ---------------------------------------------------------------------------
// Mock stub — swap out when Bob's endpoint is live
// ---------------------------------------------------------------------------

/**
 * Returns a zeroed-out HomeSummaryResponse.
 * Used when the BFF endpoint is not yet available.
 * TODO(#689): remove stub once GET /api/v1/history/summary ships.
 */
export function makeMockHomeSummary(): HomeSummaryResponse {
  return {
    today: { wins: 0, losses: 0, win_rate: 0 },
    this_week: { wins: 0, losses: 0, win_rate: 0, matches: 0 },
    all_time: { wins: 0, losses: 0, win_rate: 0, matches: 0, current_streak: 0, streak_type: 'W' },
    last_match: null,
  };
}

// ---------------------------------------------------------------------------
// Adapter
// ---------------------------------------------------------------------------

/**
 * Fetch the home summary from the BFF.
 *
 * Targets: GET /api/v1/history/summary
 *
 * The endpoint is Clerk-protected — pass the Clerk session token obtained via
 * `useAuth().getToken()` as the `clerkToken` parameter.
 *
 * TODO(#689): The BFF endpoint is not yet live. This function attempts the real
 * request; callers should handle the 404 case (endpoint not deployed yet) and
 * fall back to the mock stub via makeMockHomeSummary(). The mock swap is in
 * HomeCommandStrip.tsx — remove the fallback once Bob's endpoint is live.
 */
export async function getHomeSummary(clerkToken: string): Promise<HomeSummaryResponse> {
  const { baseUrl } = getApiConfig();
  const url = `${baseUrl}/history/summary`;

  const response = await fetch(url, {
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

  return response.json() as Promise<HomeSummaryResponse>;
}
