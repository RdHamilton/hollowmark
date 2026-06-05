/**
 * BFF wildcard-advisor adapter.
 *
 * Targets GET /api/v1/recommendations/wildcards on the BFF (ADR-045 scaffold,
 * full implementation #420). Returns ranked craft targets for the authenticated
 * user's collection, filtered by format.
 *
 * Error semantics (by HTTP status code — NOT body strings):
 *   409 → collection not synced; caller should show a sync-CTA state.
 *   503 → BFF degraded / upstream unavailable; caller should show error-retry.
 *   401 → unauthenticated; will not happen under ProtectedRoute (treated as error).
 *   4xx/5xx other → generic error.
 *
 * Ray Hamilton note: detect 409/503 by `ApiRequestError.status`, NOT by
 * body string — the body "error" field is informational, not a stable contract.
 *
 * PostHog telemetry is EXCLUDED (tracked in #422).
 * Coupling: this adapter's format-toggle types must stay in sync with #420's
 * BFF implementation of GET /api/v1/recommendations/wildcards?format=<f>.
 */

import { getApiConfig, ApiRequestError } from '../apiClient';

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

/** MTGA Arena formats supported by the wildcard advisor. */
export type WildcardAdvisorFormat = 'Standard' | 'Historic' | 'Explorer' | 'Alchemy';

/** A single wildcard craft recommendation returned by the BFF. */
export interface WildcardRecommendation {
  arena_id: number;
  name: string;
  rarity: 'common' | 'uncommon' | 'rare' | 'mythic';
  /** How many copies the user currently owns (0–4). */
  owned_copies: number;
  /** How many copies needed to complete a playset (1–4). */
  missing_copies: number;
  /** Game-in-hand win rate, as a percentage (e.g. 63.2). Present when signal available. */
  gihwr?: number;
  /** Number of rated archetypes this card appears in. */
  archetype_count?: number;
  /** Display-ready format context snippet, e.g. "Appears in 3 top Standard archetypes". */
  format_context?: string;
  set_code?: string;
  image_uri?: string;

  // ── ADR-045 / #420 archetype-level fields ────────────────────────────────
  /**
   * Deck / archetype name, e.g. "Mono White Aggro".
   * Present in the full #420 BFF implementation; absent on the v0.3.7 scaffold.
   */
  archetype_name?: string;
  /**
   * Competitive tier from mtgzone_archetypes. Typically an integer 1–4 or the
   * string "S". Rendered as "Tier {tier}" in the collapsed row.
   * Present in the full #420 BFF implementation; absent on the v0.3.7 scaffold.
   */
  tier?: number | string;
  /**
   * Per-rarity wildcard count needed to complete the archetype.
   * Populated by the BFF after Phase 2 aggregation (ADR-045 §2).
   * Present in the full #420 BFF implementation; absent on the v0.3.7 scaffold.
   */
  wildcards_required?: Partial<WildcardBudget>;
  /**
   * Total number of missing cards across all rarities.
   * Populated by the BFF as `cards_needed` (ADR-045 §1 response shape).
   * Present in the full #420 BFF implementation; absent on the v0.3.7 scaffold.
   */
  cards_needed?: number;
}

/** The wildcard budget broken down by rarity. */
export interface WildcardBudget {
  common: number;
  uncommon: number;
  rare: number;
  mythic: number;
}

/** Response envelope from GET /api/v1/recommendations/wildcards. */
export interface WildcardAdvisorResponse {
  format: WildcardAdvisorFormat;
  recommendations: WildcardRecommendation[];
  wildcard_budget: WildcardBudget;
  /**
   * ISO-8601 timestamp of when the ratings data was last refreshed.
   * Present when data is available.
   */
  ratings_cached_at?: string;
}

/** Result returned by `getWildcardRecommendations`. */
export interface WildcardAdvisorResult {
  data: WildcardAdvisorResponse;
  /**
   * True when the BFF returned X-Cache-Degraded: true — ratings data may be stale.
   * The stale-warning banner should be shown when this is true and cacheAgeHours > 24.
   */
  cacheDegraded: boolean;
  /** Hours since ratings were last refreshed. Undefined when header is absent. */
  cacheAgeHours?: number;
}

// ---------------------------------------------------------------------------
// Adapter
// ---------------------------------------------------------------------------

/**
 * Fetch wildcard craft recommendations from the BFF.
 *
 * Targets: GET /api/v1/recommendations/wildcards?format=<format>
 *
 * This route is mounted under the BFF's Clerk-auth group (RequireClerkAuth).
 * The token MUST be the current Clerk session JWT from useAuth().getToken at
 * the call site. Null/empty token → header is omitted → BFF returns 401.
 *
 * Error status codes:
 *   409 → collection not synced yet; surface sync-CTA state (NOT a network error).
 *   503 → upstream degraded; surface error-retry state.
 *
 * @param format  Arena format to filter by. Defaults to 'Standard'.
 * @param token   Clerk session JWT, or null when unavailable.
 */
export async function getWildcardRecommendations(
  format: WildcardAdvisorFormat = 'Standard',
  token: string | null
): Promise<WildcardAdvisorResult> {
  const { baseUrl } = getApiConfig();
  const url = `${baseUrl}/recommendations/wildcards?format=${encodeURIComponent(format)}`;

  const headers: Record<string, string> = { 'Content-Type': 'application/json' };
  if (token) {
    headers.Authorization = `Bearer ${token}`;
  }

  const response = await fetch(url, {
    method: 'GET',
    headers,
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

  const data = (await response.json()) as WildcardAdvisorResponse;

  const cacheDegraded = response.headers.get('x-cache-degraded') === 'true';
  const rawAge = response.headers.get('x-cache-age-hours');
  const parsedAge = rawAge !== null ? parseFloat(rawAge) : undefined;
  const cacheAgeHours =
    parsedAge !== undefined && !isNaN(parsedAge) ? parsedAge : undefined;

  return { data, cacheDegraded, cacheAgeHours };
}
