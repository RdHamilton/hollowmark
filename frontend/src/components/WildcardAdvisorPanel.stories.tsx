/**
 * WildcardAdvisorPanel — 6-state Storybook coverage
 *
 * Covers every distinct render path of WildcardAdvisorPanel (#845):
 *   1. Loading      — skeleton shimmer rows while the BFF call is in-flight
 *   2. SyncCta      — 409 response; collection not yet synced (daemon not paired)
 *   3. Empty        — 200 OK, zero recommendations, no ratings_cached_at (no-data path)
 *   4. Error        — non-409 API error / 503 with retry button
 *   5. WithRecommendations — loaded state, mixed affordable + aspirational sections
 *   6. StaleData    — loaded state with stale-data warning banner (>24h degraded cache)
 *
 * The component fetches via `bffWildcardAdvisor.getWildcardRecommendations` in a
 * useEffect. Each story uses Storybook's `beforeEach` hook (Storybook 8+) to spy
 * on the adapter function before the story renders — no MSW or separate mock infra
 * required. `@clerk/react` is aliased to the Storybook Clerk mock globally (main.ts
 * viteFinal), so `useAuth()` returns a stable mock token automatically.
 *
 * Ticket: RdHamilton/vault-mtg-tickets#845
 * Parent ticket: #421 (PR #2996)
 */

import type { Meta, StoryObj } from '@storybook/react';
import { spyOn } from 'storybook/test';
import WildcardAdvisorPanel from './WildcardAdvisorPanel';
import * as bffWildcardAdvisorModule from '@/services/api/bffWildcardAdvisor';
import type { WildcardAdvisorResult } from '@/services/api/bffWildcardAdvisor';
import { ApiRequestError } from '@/services/apiClient';
import './WildcardAdvisorPanel.css';

// ---------------------------------------------------------------------------
// Shared mock data fixtures
// ---------------------------------------------------------------------------

const BUDGET = { common: 10, uncommon: 8, rare: 4, mythic: 1 };

/**
 * Affordable recommendation — costs are within budget so it lands in "Craft Tonight".
 * rare: 4 owned, needs 0 more → but we set missing_copies: 2 and budget.rare: 4
 * so 4 >= 2 → affordable.
 */
const AFFORDABLE_REC = {
  arena_id: 88001,
  name: 'Sunfall',
  archetype_name: 'Mono White Aggro',
  rarity: 'rare' as const,
  owned_copies: 2,
  missing_copies: 2,
  gihwr: 61.4,
  archetype_count: 5,
  format_context: 'Appears in 5 top Standard archetypes',
  set_code: 'MOM',
  tier: 1,
  wildcards_required: { rare: 2 },
};

/**
 * Aspirational recommendation — costs exceed budget (mythic: 3, budget.mythic: 1).
 */
const ASPIRATIONAL_REC = {
  arena_id: 88002,
  name: 'Atraxa, Grand Unifier',
  archetype_name: 'Domain Ramp',
  rarity: 'mythic' as const,
  owned_copies: 1,
  missing_copies: 3,
  gihwr: 58.2,
  archetype_count: 3,
  format_context: 'Appears in 3 top Standard archetypes',
  set_code: 'ONE',
  tier: 2,
  wildcards_required: { mythic: 3 },
};

/** A second affordable rare for a richer loaded-state story. */
const AFFORDABLE_REC_2 = {
  arena_id: 88003,
  name: 'Virtue of Loyalty',
  archetype_name: 'Bant Toxic',
  rarity: 'rare' as const,
  owned_copies: 0,
  missing_copies: 4,
  gihwr: 59.1,
  archetype_count: 2,
  format_context: 'Appears in 2 top Standard archetypes',
  set_code: 'WOE',
  tier: 2,
  wildcards_required: { rare: 4 },
};

function makeResult(overrides: Partial<WildcardAdvisorResult> = {}): WildcardAdvisorResult {
  return {
    data: {
      format: 'Standard',
      recommendations: [AFFORDABLE_REC, ASPIRATIONAL_REC, AFFORDABLE_REC_2],
      wildcard_budget: BUDGET,
    },
    cacheDegraded: false,
    ...overrides,
  };
}

// ---------------------------------------------------------------------------
// Meta
// ---------------------------------------------------------------------------

const meta: Meta<typeof WildcardAdvisorPanel> = {
  title: 'Organisms/WildcardAdvisorPanel',
  component: WildcardAdvisorPanel,
  parameters: {
    layout: 'centered',
  },
  tags: ['autodocs'],
  // Panel has a fixed max-width; add some padding so the story canvas breathes.
  decorators: [
    (Story) => (
      <div style={{ width: 480, padding: 16 }}>
        <Story />
      </div>
    ),
  ],
};

export default meta;
type Story = StoryObj<typeof WildcardAdvisorPanel>;

// ---------------------------------------------------------------------------
// Story 1: Loading
// ---------------------------------------------------------------------------

/**
 * Loading — skeleton shimmer rows are visible while the BFF call is in-flight.
 * The `getWildcardRecommendations` mock never resolves so the skeleton persists.
 */
export const Loading: Story = {
  name: 'Loading',
  beforeEach: () => {
    spyOn(bffWildcardAdvisorModule, 'getWildcardRecommendations').mockImplementation(
      () => new Promise(() => {}) // never resolves — skeleton stays visible
    );
  },
};

// ---------------------------------------------------------------------------
// Story 2: SyncCta (409 — collection not synced)
// ---------------------------------------------------------------------------

/**
 * SyncCta — the BFF returns a 409 because the daemon has not yet synced the
 * user's MTGA collection. The panel shows a "Collection Not Synced" prompt.
 *
 * Per the component's implementation the 409 is detected by HTTP status code,
 * not by the error body string (Ray's note in the component source).
 */
export const SyncCta: Story = {
  name: 'SyncCta — Collection Not Synced',
  beforeEach: () => {
    spyOn(bffWildcardAdvisorModule, 'getWildcardRecommendations').mockRejectedValue(
      new ApiRequestError('collection_not_synced', 409)
    );
  },
};

// ---------------------------------------------------------------------------
// Story 3: Empty (no-data)
// ---------------------------------------------------------------------------

/**
 * Empty — 200 OK with zero recommendations and no `ratings_cached_at`.
 * The component shows "No recommendations yet — keep playing."
 *
 * Note: when `ratings_cached_at` IS present and recs are empty the panel shows
 * "Collection looks complete!" — that is a separate path (not covered here).
 * This story covers the no-data path (ratings pipeline has no signal yet).
 */
export const Empty: Story = {
  name: 'Empty — No Recommendations Yet',
  beforeEach: () => {
    spyOn(bffWildcardAdvisorModule, 'getWildcardRecommendations').mockResolvedValue({
      data: {
        format: 'Standard',
        recommendations: [],
        wildcard_budget: BUDGET,
        // ratings_cached_at intentionally absent → triggers "no data" branch
      },
      cacheDegraded: false,
    });
  },
};

// ---------------------------------------------------------------------------
// Story 4: Error (503 with retry button)
// ---------------------------------------------------------------------------

/**
 * Error — the BFF returns a 503 (or any non-409 error). The panel shows
 * "Recommendations are temporarily unavailable" and a Retry button.
 */
export const Error: Story = {
  name: 'Error — 503 Retry',
  beforeEach: () => {
    spyOn(bffWildcardAdvisorModule, 'getWildcardRecommendations').mockRejectedValue(
      new ApiRequestError('service_unavailable', 503)
    );
  },
};

// ---------------------------------------------------------------------------
// Story 5: WithRecommendations (loaded, Craft Tonight + Saving Toward)
// ---------------------------------------------------------------------------

/**
 * WithRecommendations — the BFF returns a healthy response with both affordable
 * and aspirational recommendations. The panel shows two sections:
 *   - "Craft Tonight" for cards within the wildcard budget
 *   - "Saving Toward" for cards that require more wildcards than are available
 *
 * Budget: mythic=1, rare=4, uncommon=8, common=10
 *   - AFFORDABLE_REC (Sunfall): needs 2 rare → 4 >= 2 → Craft Tonight
 *   - AFFORDABLE_REC_2 (Virtue of Loyalty): needs 4 rare → 4 >= 4 → Craft Tonight
 *   - ASPIRATIONAL_REC (Atraxa): needs 3 mythic → 1 < 3 → Saving Toward
 */
export const WithRecommendations: Story = {
  name: 'WithRecommendations — Craft Tonight + Saving Toward',
  beforeEach: () => {
    spyOn(bffWildcardAdvisorModule, 'getWildcardRecommendations').mockResolvedValue(
      makeResult()
    );
  },
};

// ---------------------------------------------------------------------------
// Story 6: StaleData (loaded + stale-data warning banner)
// ---------------------------------------------------------------------------

/**
 * StaleData — the BFF response includes `X-Cache-Degraded: true` and the cache
 * is >24 hours old. The panel renders the stale-warning banner above the
 * recommendation list: "Ratings data is over N hours old — crafting advice may
 * be slightly outdated."
 *
 * `cacheDegraded: true` + `cacheAgeHours: 36` → banner visible (36 > 24h threshold).
 */
export const StaleData: Story = {
  name: 'StaleData — Stale Warning Banner',
  beforeEach: () => {
    spyOn(bffWildcardAdvisorModule, 'getWildcardRecommendations').mockResolvedValue(
      makeResult({ cacheDegraded: true, cacheAgeHours: 36 })
    );
  },
};

