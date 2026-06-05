/**
 * WildcardAdvisorPanel — Collapsed-row content and error-state styling tests.
 *
 * Covers the three Prof-requested fixes from the v0.3.8 pre-beta review:
 *   1. Collapsed rows show archetype name + tier badge + wildcard-cost summary
 *      without requiring expansion (Prof's #1 priority).
 *   2. Error state uses neutral/muted styling, not red (Prof's fix #2).
 *   3. "Saving Toward" subtitle removed (Prof's fix #3).
 *
 * Split into its own file to stay under the per-worker heap ceiling on CI
 * (NODE_OPTIONS=6144, 2 vCPU, 7 GB RAM — see #2996).
 *
 * Clerk useAuth is globally mocked in src/test/setup.ts.
 * @/services/api is globally mocked in src/test/setup.ts via apiMock.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import WildcardAdvisorPanel from './WildcardAdvisorPanel';
import { ApiRequestError } from '@/services/apiClient';
import type {
  WildcardAdvisorResult,
  WildcardRecommendation,
} from '@/services/api/bffWildcardAdvisor';
import { bffWildcardAdvisor } from '@/services/api';

const mockGetRecs = vi.mocked(bffWildcardAdvisor.getWildcardRecommendations);

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

const BUDGET = { common: 10, uncommon: 8, rare: 4, mythic: 1 };

/** Build a minimal rec. Override fields to test specific behaviour. */
const makeRec = (overrides: Partial<WildcardRecommendation> = {}): WildcardRecommendation => ({
  arena_id: 1001,
  name: 'Test Card',
  rarity: 'rare' as const,
  owned_copies: 2,
  missing_copies: 2,
  ...overrides,
});

const makeResult = (
  recs: WildcardRecommendation[],
  overrides: Partial<WildcardAdvisorResult> = {}
): WildcardAdvisorResult => ({
  data: {
    format: 'Standard',
    recommendations: recs,
    wildcard_budget: BUDGET,
  },
  cacheDegraded: false,
  ...overrides,
});

// ---------------------------------------------------------------------------
// 1. Collapsed-row content
// ---------------------------------------------------------------------------

describe('WildcardAdvisorPanel — collapsed-row content', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('renders archetype_name on the collapsed row (without expanding)', async () => {
    const rec = makeRec({ archetype_name: 'Mono White Aggro', missing_copies: 2 });
    mockGetRecs.mockResolvedValue(makeResult([rec]));

    render(<WildcardAdvisorPanel />);

    await waitFor(() =>
      expect(screen.getByTestId('wildcard-advisor-rec-name')).toBeInTheDocument()
    );

    // archetype_name must be visible WITHOUT expanding
    expect(screen.getByTestId('wildcard-advisor-rec-name')).toHaveTextContent('Mono White Aggro');
    // Drill-down must still be collapsed
    expect(screen.queryByTestId('wildcard-advisor-drill-down')).not.toBeInTheDocument();
  });

  it('falls back to card name when archetype_name is absent', async () => {
    const rec = makeRec({ name: 'Sunfall', archetype_name: undefined, missing_copies: 2 });
    mockGetRecs.mockResolvedValue(makeResult([rec]));

    render(<WildcardAdvisorPanel />);

    await waitFor(() =>
      expect(screen.getByTestId('wildcard-advisor-rec-name')).toBeInTheDocument()
    );

    expect(screen.getByTestId('wildcard-advisor-rec-name')).toHaveTextContent('Sunfall');
  });

  it('renders tier badge on the collapsed row when tier is present', async () => {
    const rec = makeRec({ archetype_name: 'Mono White Aggro', tier: 1 });
    mockGetRecs.mockResolvedValue(makeResult([rec]));

    render(<WildcardAdvisorPanel />);

    await waitFor(() =>
      expect(screen.getByTestId('wildcard-advisor-rec-tier')).toBeInTheDocument()
    );

    expect(screen.getByTestId('wildcard-advisor-rec-tier')).toHaveTextContent('Tier 1');
    // Drill-down must still be collapsed
    expect(screen.queryByTestId('wildcard-advisor-drill-down')).not.toBeInTheDocument();
  });

  it('renders "Tier S" for string tier value "S"', async () => {
    const rec = makeRec({ archetype_name: 'Domain Ramp', tier: 'S' });
    mockGetRecs.mockResolvedValue(makeResult([rec]));

    render(<WildcardAdvisorPanel />);

    await waitFor(() =>
      expect(screen.getByTestId('wildcard-advisor-rec-tier')).toBeInTheDocument()
    );

    expect(screen.getByTestId('wildcard-advisor-rec-tier')).toHaveTextContent('Tier S');
  });

  it('does not render tier badge when tier is absent', async () => {
    const rec = makeRec({ archetype_name: 'Unknown Deck', tier: undefined });
    mockGetRecs.mockResolvedValue(makeResult([rec]));

    render(<WildcardAdvisorPanel />);

    await waitFor(() =>
      expect(screen.getByTestId('wildcard-advisor-rec-name')).toBeInTheDocument()
    );

    expect(screen.queryByTestId('wildcard-advisor-rec-tier')).not.toBeInTheDocument();
  });

  it('renders wildcard cost summary from wildcards_required on the collapsed row', async () => {
    const rec = makeRec({
      archetype_name: 'Mono White Aggro',
      tier: 1,
      wildcards_required: { rare: 3, mythic: 1, uncommon: 0, common: 0 },
    });
    mockGetRecs.mockResolvedValue(makeResult([rec]));

    render(<WildcardAdvisorPanel />);

    await waitFor(() =>
      expect(screen.getByTestId('wildcard-advisor-rec-cost')).toBeInTheDocument()
    );

    // Mythic before Rare (RARITY_ORDER: mythic, rare, uncommon, common)
    expect(screen.getByTestId('wildcard-advisor-rec-cost')).toHaveTextContent('1 Mythic · 3 Rare away');
    // Not expanded
    expect(screen.queryByTestId('wildcard-advisor-drill-down')).not.toBeInTheDocument();
  });

  it('renders wildcard cost summary from missing_copies + rarity when wildcards_required is absent (scaffold fallback)', async () => {
    const rec = makeRec({
      name: 'Hopeful Initiate',
      rarity: 'rare',
      missing_copies: 2,
      wildcards_required: undefined,
    });
    mockGetRecs.mockResolvedValue(makeResult([rec]));

    render(<WildcardAdvisorPanel />);

    await waitFor(() =>
      expect(screen.getByTestId('wildcard-advisor-rec-cost')).toBeInTheDocument()
    );

    expect(screen.getByTestId('wildcard-advisor-rec-cost')).toHaveTextContent('2 Rare away');
  });

  it('cost summary shows "Complete!" when wildcards_required has all zeros', async () => {
    const rec = makeRec({
      archetype_name: 'Full Deck',
      wildcards_required: { rare: 0, mythic: 0, uncommon: 0, common: 0 },
    });
    mockGetRecs.mockResolvedValue(makeResult([rec]));

    render(<WildcardAdvisorPanel />);

    await waitFor(() =>
      expect(screen.getByTestId('wildcard-advisor-rec-cost')).toBeInTheDocument()
    );

    expect(screen.getByTestId('wildcard-advisor-rec-cost')).toHaveTextContent('Complete!');
  });

  it('cost summary is visible before expansion — collapsed row is informative at a glance', async () => {
    const rec = makeRec({
      archetype_name: 'Domain Ramp',
      tier: 2,
      wildcards_required: { rare: 4, mythic: 2, uncommon: 0, common: 0 },
    });
    mockGetRecs.mockResolvedValue(makeResult([rec]));

    render(<WildcardAdvisorPanel />);

    await waitFor(() =>
      expect(screen.getByTestId('wildcard-advisor-rec-cost')).toBeInTheDocument()
    );

    // All three key elements present in collapsed state
    expect(screen.getByTestId('wildcard-advisor-rec-name')).toHaveTextContent('Domain Ramp');
    expect(screen.getByTestId('wildcard-advisor-rec-tier')).toHaveTextContent('Tier 2');
    expect(screen.getByTestId('wildcard-advisor-rec-cost')).toHaveTextContent('2 Mythic · 4 Rare away');
    // Panel not expanded
    expect(screen.queryByTestId('wildcard-advisor-drill-down')).not.toBeInTheDocument();
  });
});

// ---------------------------------------------------------------------------
// 2. Error state: neutral styling (not red)
// ---------------------------------------------------------------------------

describe('WildcardAdvisorPanel — error state neutral styling', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('error state container does not use role="alert" (not an urgent error)', async () => {
    mockGetRecs.mockRejectedValue(new ApiRequestError('service_unavailable', 503));

    render(<WildcardAdvisorPanel />);

    await waitFor(() =>
      expect(screen.getByTestId('wildcard-advisor-error')).toBeInTheDocument()
    );

    // role="alert" communicates urgency/danger — the neutral state should not use it
    const errorEl = screen.getByTestId('wildcard-advisor-error');
    expect(errorEl.getAttribute('role')).not.toBe('alert');
  });

  it('error message element does not carry danger/error CSS class', async () => {
    mockGetRecs.mockRejectedValue(new ApiRequestError('service_unavailable', 503));

    render(<WildcardAdvisorPanel />);

    await waitFor(() =>
      expect(screen.getByTestId('wildcard-advisor-error')).toBeInTheDocument()
    );

    const errorMsg = screen.getByTestId('wildcard-advisor-error').querySelector('.wildcard-advisor__error-msg');
    expect(errorMsg).not.toBeNull();
    // Must not carry a danger/error modifier class
    expect(errorMsg!.className).not.toMatch(/danger|error--/);
  });

  it('error state retains the copy and Retry button', async () => {
    mockGetRecs.mockRejectedValue(new ApiRequestError('service_unavailable', 503));

    render(<WildcardAdvisorPanel />);

    await waitFor(() =>
      expect(screen.getByTestId('wildcard-advisor-error')).toBeInTheDocument()
    );

    expect(screen.getByText(/temporarily unavailable/i)).toBeInTheDocument();
    expect(screen.getByTestId('wildcard-advisor-retry')).toBeInTheDocument();
  });
});

// ---------------------------------------------------------------------------
// 3. "Saving Toward" subtitle removed
// ---------------------------------------------------------------------------

describe('WildcardAdvisorPanel — Saving Toward subtitle removed', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('does not render the old "High-value targets" subtitle under Saving Toward', async () => {
    // Budget: 1 mythic, card needs 4 → aspirational
    const rec = makeRec({ rarity: 'mythic', missing_copies: 4 });
    mockGetRecs.mockResolvedValue(makeResult([rec]));

    render(<WildcardAdvisorPanel />);

    await waitFor(() =>
      expect(screen.getByTestId('wildcard-advisor-saving-toward')).toBeInTheDocument()
    );

    expect(screen.queryByText(/high-value targets/i)).not.toBeInTheDocument();
    expect(screen.queryByText(/keep earning wildcards/i)).not.toBeInTheDocument();
  });

  it('Saving Toward section still renders the section title and recommendations', async () => {
    const rec = makeRec({ rarity: 'mythic', missing_copies: 4, archetype_name: 'Domain Ramp' });
    mockGetRecs.mockResolvedValue(makeResult([rec]));

    render(<WildcardAdvisorPanel />);

    await waitFor(() =>
      expect(screen.getByTestId('wildcard-advisor-saving-toward')).toBeInTheDocument()
    );

    expect(screen.getByText('Saving Toward')).toBeInTheDocument();
    expect(screen.getByTestId('wildcard-advisor-rec-name')).toHaveTextContent('Domain Ramp');
  });
});
