/**
 * Component tests for WildcardAdvisorPanel.
 *
 * Covers all 6 states:
 *  1. loading
 *  2. affordable-split "Craft Tonight" / "Saving Toward" (data)
 *  3. 409 sync-CTA
 *  4. 200 empty (no recommendations)
 *  5. stale-warning banner (cacheDegraded + >24h old)
 *  6. 503 error-retry
 *
 * Also covers:
 *  - Format toggle button rendering and interaction.
 *  - Wildcard budget gem colors via --gem-color CSS variable.
 *  - Missing-cards drill-down per recommendation.
 *
 * Clerk useAuth is globally mocked in src/test/setup.ts to return a stub token.
 * bffWildcardAdvisor is mocked here per-test.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import WildcardAdvisorPanel from './WildcardAdvisorPanel';
import { ApiRequestError } from '@/services/apiClient';
import type {
  WildcardAdvisorResult,
} from '@/services/api/bffWildcardAdvisor';

// ---------------------------------------------------------------------------
// Mock bffWildcardAdvisor module
// ---------------------------------------------------------------------------

vi.mock('@/services/api', async () => {
  const actual = await vi.importActual<typeof import('@/services/api')>('@/services/api');
  return {
    ...actual,
    bffWildcardAdvisor: {
      getWildcardRecommendations: vi.fn(),
    },
  };
});

import { bffWildcardAdvisor } from '@/services/api';
const mockGetRecs = vi.mocked(bffWildcardAdvisor.getWildcardRecommendations);

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

const BUDGET = { common: 10, uncommon: 8, rare: 4, mythic: 1 };

const makeRec = (overrides: Partial<{
  arena_id: number;
  name: string;
  rarity: 'common' | 'uncommon' | 'rare' | 'mythic';
  owned_copies: number;
  missing_copies: number;
  gihwr: number;
  archetype_count: number;
  format_context: string;
  set_code: string;
}> = {}) => ({
  arena_id: 1001,
  name: 'Test Card',
  rarity: 'rare' as const,
  owned_copies: 2,
  missing_copies: 2,
  gihwr: 61.0,
  archetype_count: 3,
  format_context: 'Appears in 3 top Standard archetypes',
  set_code: 'TST',
  ...overrides,
});

const makeResult = (
  recs: ReturnType<typeof makeRec>[],
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
// Tests
// ---------------------------------------------------------------------------

describe('WildcardAdvisorPanel', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  // ── State 1: Loading ──────────────────────────────────────────────────────
  describe('State 1 — loading', () => {
    it('shows loading skeleton while fetch is in progress', async () => {
      // Never resolves during this test
      mockGetRecs.mockReturnValue(new Promise(() => undefined));

      render(<WildcardAdvisorPanel />);

      expect(screen.getByTestId('wildcard-advisor-loading')).toBeInTheDocument();
    });

    it('loading container has aria-busy="true"', async () => {
      mockGetRecs.mockReturnValue(new Promise(() => undefined));

      render(<WildcardAdvisorPanel />);

      const loadingEl = screen.getByTestId('wildcard-advisor-loading');
      expect(loadingEl).toHaveAttribute('aria-busy', 'true');
    });
  });

  // ── State 2: Data — affordable / aspirational split ───────────────────────
  describe('State 2 — data with recommendations', () => {
    it('renders "Craft Tonight" section for affordable cards', async () => {
      // Budget has 4 rare; card needs 2 → affordable
      const rec = makeRec({ rarity: 'rare', missing_copies: 2 });
      mockGetRecs.mockResolvedValue(makeResult([rec]));

      render(<WildcardAdvisorPanel />);

      await waitFor(() =>
        expect(screen.getByTestId('wildcard-advisor-craft-tonight')).toBeInTheDocument()
      );
      expect(screen.getByText('Craft Tonight')).toBeInTheDocument();
      expect(screen.getByText(rec.name)).toBeInTheDocument();
    });

    it('renders "Saving Toward" section for aspirational cards', async () => {
      // Budget has 1 mythic; card needs 4 → aspirational
      const rec = makeRec({ rarity: 'mythic', missing_copies: 4 });
      mockGetRecs.mockResolvedValue(makeResult([rec]));

      render(<WildcardAdvisorPanel />);

      await waitFor(() =>
        expect(screen.getByTestId('wildcard-advisor-saving-toward')).toBeInTheDocument()
      );
      expect(screen.getByText('Saving Toward')).toBeInTheDocument();
    });

    it('correctly splits affordable vs aspirational recommendations', async () => {
      const affordable = makeRec({ name: 'Cheap Card', rarity: 'rare', missing_copies: 2 });
      const aspirational = makeRec({ arena_id: 9999, name: 'Expensive Card', rarity: 'mythic', missing_copies: 4 });
      mockGetRecs.mockResolvedValue(makeResult([affordable, aspirational]));

      render(<WildcardAdvisorPanel />);

      await waitFor(() =>
        expect(screen.getByText('Craft Tonight')).toBeInTheDocument()
      );

      expect(screen.getByTestId('wildcard-advisor-craft-tonight')).toHaveTextContent('Cheap Card');
      expect(screen.getByTestId('wildcard-advisor-saving-toward')).toHaveTextContent('Expensive Card');
    });

    it('displays wildcard budget with gem tokens for each rarity', async () => {
      mockGetRecs.mockResolvedValue(makeResult([makeRec()]));

      render(<WildcardAdvisorPanel />);

      await waitFor(() =>
        expect(screen.getByTestId('wildcard-advisor-budget')).toBeInTheDocument()
      );

      expect(screen.getByTestId('wildcard-advisor-gem-mythic')).toBeInTheDocument();
      expect(screen.getByTestId('wildcard-advisor-gem-rare')).toBeInTheDocument();
      expect(screen.getByTestId('wildcard-advisor-gem-uncommon')).toBeInTheDocument();
      expect(screen.getByTestId('wildcard-advisor-gem-common')).toBeInTheDocument();
    });

    it('applies --gem-color CSS variable to each budget gem', async () => {
      mockGetRecs.mockResolvedValue(makeResult([makeRec()]));

      render(<WildcardAdvisorPanel />);

      await waitFor(() =>
        expect(screen.getByTestId('wildcard-advisor-gem-mythic')).toBeInTheDocument()
      );

      const mythicGem = screen.getByTestId('wildcard-advisor-gem-mythic');
      // The inline style sets --gem-color
      expect(mythicGem.getAttribute('style')).toContain('--gem-color: var(--vault-rarity-mythic)');
    });

    it('shows GIHWR for cards that have it, with "GIHWR" inline label', async () => {
      const rec = makeRec({ gihwr: 63.4 });
      mockGetRecs.mockResolvedValue(makeResult([rec]));

      render(<WildcardAdvisorPanel />);

      // The gihwr span has an aria-label with the full value; the inline label text also appears
      await waitFor(() =>
        expect(screen.getByLabelText('63.4% game-in-hand win rate')).toBeInTheDocument()
      );
      // Inline "GIHWR" label text should be present in the row
      expect(screen.getAllByText('GIHWR').length).toBeGreaterThanOrEqual(1);
    });

    it('GIHWR span has a tooltip (title attribute) explaining the stat', async () => {
      const rec = makeRec({ gihwr: 58.0 });
      mockGetRecs.mockResolvedValue(makeResult([rec]));

      render(<WildcardAdvisorPanel />);

      // aria-label is set on the gihwr span; find it that way
      await waitFor(() =>
        expect(screen.getByLabelText('58.0% game-in-hand win rate')).toBeInTheDocument()
      );

      const gihwrSpan = screen.getByLabelText('58.0% game-in-hand win rate');
      expect(gihwrSpan.getAttribute('title')).toContain('game-in-hand win rate');
    });

    it('drill-down is collapsed by default', async () => {
      const rec = makeRec({ name: 'Drill Card', missing_copies: 2, owned_copies: 2 });
      mockGetRecs.mockResolvedValue(makeResult([rec]));

      render(<WildcardAdvisorPanel />);

      await waitFor(() =>
        expect(screen.getByText('Drill Card')).toBeInTheDocument()
      );

      // drill-down must not be visible before any click
      expect(screen.queryByTestId('wildcard-advisor-drill-down')).not.toBeInTheDocument();
    });

    it('drill-down expands on click to show missing-cards detail', async () => {
      const rec = makeRec({ name: 'Drill Card', missing_copies: 2, owned_copies: 2 });
      mockGetRecs.mockResolvedValue(makeResult([rec]));

      render(<WildcardAdvisorPanel />);

      await waitFor(() =>
        expect(screen.getByText('Drill Card')).toBeInTheDocument()
      );

      expect(screen.queryByTestId('wildcard-advisor-drill-down')).not.toBeInTheDocument();

      fireEvent.click(screen.getByText('Drill Card'));

      await waitFor(() =>
        expect(screen.getByTestId('wildcard-advisor-drill-down')).toBeInTheDocument()
      );
    });

    it('drill-down toggle button uses SVG chevron icons, not Unicode glyphs', async () => {
      const rec = makeRec({ name: 'Chevron Card' });
      mockGetRecs.mockResolvedValue(makeResult([rec]));

      render(<WildcardAdvisorPanel />);

      await waitFor(() =>
        expect(screen.getByText('Chevron Card')).toBeInTheDocument()
      );

      // The rec-card container has an expand button — find the button inside the card
      const card = screen.getByTestId('wildcard-advisor-rec-card');
      const expandBtn = card.querySelector('button.wildcard-advisor__rec-main');
      expect(expandBtn).not.toBeNull();

      // There should be an SVG (heroicon chevron) inside the expand icon span
      const expandIconSpan = expandBtn?.querySelector('.wildcard-advisor__rec-expand-icon');
      expect(expandIconSpan?.querySelector('svg')).not.toBeNull();

      // Unicode characters '▸' and '▾' must not appear anywhere in the panel
      expect(document.body.textContent).not.toContain('▸');
      expect(document.body.textContent).not.toContain('▾');
    });

    it('drill-down shows format context when available', async () => {
      const rec = makeRec({ format_context: 'Appears in 3 top Standard archetypes' });
      mockGetRecs.mockResolvedValue(makeResult([rec]));

      render(<WildcardAdvisorPanel />);

      await waitFor(() =>
        expect(screen.getByText(rec.name)).toBeInTheDocument()
      );

      fireEvent.click(screen.getByText(rec.name));

      await waitFor(() =>
        expect(screen.getByText('Appears in 3 top Standard archetypes')).toBeInTheDocument()
      );
    });

    it('drill-down does not render Context row when format_context is absent', async () => {
      const rec = makeRec({ format_context: undefined });
      mockGetRecs.mockResolvedValue(makeResult([rec]));

      render(<WildcardAdvisorPanel />);

      await waitFor(() =>
        expect(screen.getByText(rec.name)).toBeInTheDocument()
      );

      fireEvent.click(screen.getByText(rec.name));

      await waitFor(() =>
        expect(screen.getByTestId('wildcard-advisor-drill-down')).toBeInTheDocument()
      );

      // "Context" label must not appear when format_context is absent
      expect(screen.queryByText('Context')).not.toBeInTheDocument();
    });
  });

  // ── State 3: 409 sync-CTA ─────────────────────────────────────────────────
  describe('State 3 — 409 collection not synced', () => {
    it('shows sync-CTA when API returns 409', async () => {
      mockGetRecs.mockRejectedValue(new ApiRequestError('collection_not_synced', 409));

      render(<WildcardAdvisorPanel />);

      await waitFor(() =>
        expect(screen.getByTestId('wildcard-advisor-sync-cta')).toBeInTheDocument()
      );
      expect(screen.getByText('Collection Not Synced')).toBeInTheDocument();
    });

    it('sync-CTA state is detected by status 409, not body string', async () => {
      // Use a different body message but same status → must still show sync-CTA
      mockGetRecs.mockRejectedValue(
        new ApiRequestError('something_completely_different', 409)
      );

      render(<WildcardAdvisorPanel />);

      await waitFor(() =>
        expect(screen.getByTestId('wildcard-advisor-sync-cta')).toBeInTheDocument()
      );
    });

    it('sync-CTA hero icon is an SVG element, not a raw Unicode glyph', async () => {
      mockGetRecs.mockRejectedValue(new ApiRequestError('collection_not_synced', 409));

      render(<WildcardAdvisorPanel />);

      await waitFor(() =>
        expect(screen.getByTestId('wildcard-advisor-sync-cta')).toBeInTheDocument()
      );

      const ctaContainer = screen.getByTestId('wildcard-advisor-sync-cta');
      // Must contain an SVG (heroicon ArrowPathIcon)
      expect(ctaContainer.querySelector('svg')).not.toBeNull();
      // The raw rotation glyph must not appear
      expect(ctaContainer.textContent).not.toContain('⟳');
    });
  });

  // ── State 4: 200 empty ────────────────────────────────────────────────────
  describe('State 4 — 200 with empty recommendations', () => {
    it('shows complete-collection state when ratings_cached_at is present and recs are empty', async () => {
      mockGetRecs.mockResolvedValue({
        ...makeResult([]),
        data: { ...makeResult([]).data, ratings_cached_at: '2026-06-04T00:00:00Z' },
      });

      render(<WildcardAdvisorPanel />);

      await waitFor(() =>
        expect(screen.getByTestId('wildcard-advisor-empty-complete')).toBeInTheDocument()
      );
      expect(screen.getByText('Collection looks complete!')).toBeInTheDocument();
      expect(screen.getByText(/nothing left to craft/i)).toBeInTheDocument();
    });

    it('shows no-data state when ratings_cached_at is absent and recs are empty', async () => {
      mockGetRecs.mockResolvedValue(makeResult([]));
      // makeResult does not set ratings_cached_at → no-data path

      render(<WildcardAdvisorPanel />);

      await waitFor(() =>
        expect(screen.getByTestId('wildcard-advisor-empty-no-data')).toBeInTheDocument()
      );
      expect(screen.getByText('No recommendations yet')).toBeInTheDocument();
      expect(screen.getByText(/keep playing/i)).toBeInTheDocument();
    });

    it('complete-collection state includes the selected format name', async () => {
      mockGetRecs.mockResolvedValue({
        ...makeResult([]),
        data: { ...makeResult([]).data, ratings_cached_at: '2026-06-04T00:00:00Z' },
      });

      render(<WildcardAdvisorPanel />);

      await waitFor(() =>
        expect(screen.getByTestId('wildcard-advisor-empty-complete')).toBeInTheDocument()
      );

      // Default format is Standard
      expect(screen.getByText(/Standard collection looks complete/i)).toBeInTheDocument();
    });
  });

  // ── State 5: Stale-warning banner ─────────────────────────────────────────
  describe('State 5 — stale-warning banner', () => {
    it('shows stale banner when cache is degraded and >24h old', async () => {
      mockGetRecs.mockResolvedValue(
        makeResult([makeRec()], { cacheDegraded: true, cacheAgeHours: 36 })
      );

      render(<WildcardAdvisorPanel />);

      await waitFor(() =>
        expect(screen.getByTestId('wildcard-advisor-stale-banner')).toBeInTheDocument()
      );
    });

    it('does not show stale banner when cache is fresh', async () => {
      mockGetRecs.mockResolvedValue(
        makeResult([makeRec()], { cacheDegraded: false, cacheAgeHours: 10 })
      );

      render(<WildcardAdvisorPanel />);

      await waitFor(() =>
        expect(screen.getByTestId('wildcard-advisor-craft-tonight')).toBeInTheDocument()
      );

      expect(screen.queryByTestId('wildcard-advisor-stale-banner')).not.toBeInTheDocument();
    });

    it('does not show stale banner when cache is degraded but <24h old', async () => {
      mockGetRecs.mockResolvedValue(
        makeResult([makeRec()], { cacheDegraded: true, cacheAgeHours: 12 })
      );

      render(<WildcardAdvisorPanel />);

      await waitFor(() =>
        expect(screen.queryByTestId('wildcard-advisor-loading')).not.toBeInTheDocument()
      );

      expect(screen.queryByTestId('wildcard-advisor-stale-banner')).not.toBeInTheDocument();
    });
  });

  // ── State 6: 503 error-retry ──────────────────────────────────────────────
  describe('State 6 — 503 error-retry', () => {
    it('shows error state when API returns 503', async () => {
      mockGetRecs.mockRejectedValue(new ApiRequestError('service_unavailable', 503));

      render(<WildcardAdvisorPanel />);

      await waitFor(() =>
        expect(screen.getByTestId('wildcard-advisor-error')).toBeInTheDocument()
      );
    });

    it('shows retry button in error state', async () => {
      mockGetRecs.mockRejectedValue(new ApiRequestError('service_unavailable', 503));

      render(<WildcardAdvisorPanel />);

      await waitFor(() =>
        expect(screen.getByTestId('wildcard-advisor-retry')).toBeInTheDocument()
      );
    });

    it('retry button triggers a new fetch', async () => {
      mockGetRecs.mockRejectedValueOnce(new ApiRequestError('service_unavailable', 503));
      mockGetRecs.mockResolvedValueOnce(makeResult([makeRec()]));

      render(<WildcardAdvisorPanel />);

      await waitFor(() =>
        expect(screen.getByTestId('wildcard-advisor-retry')).toBeInTheDocument()
      );

      fireEvent.click(screen.getByTestId('wildcard-advisor-retry'));

      await waitFor(() =>
        expect(screen.queryByTestId('wildcard-advisor-error')).not.toBeInTheDocument()
      );
      expect(mockGetRecs).toHaveBeenCalledTimes(2);
    });

    it('shows error state for non-409/non-503 errors too', async () => {
      mockGetRecs.mockRejectedValue(new ApiRequestError('internal_error', 500));

      render(<WildcardAdvisorPanel />);

      await waitFor(() =>
        expect(screen.getByTestId('wildcard-advisor-error')).toBeInTheDocument()
      );
    });
  });

  // ── Format toggle ─────────────────────────────────────────────────────────
  describe('Format toggle', () => {
    it('renders all 4 format buttons', async () => {
      mockGetRecs.mockResolvedValue(makeResult([]));

      render(<WildcardAdvisorPanel />);

      await waitFor(() =>
        expect(screen.getByTestId('wildcard-advisor-format-toggle')).toBeInTheDocument()
      );

      expect(screen.getByTestId('wildcard-advisor-format-standard')).toBeInTheDocument();
      expect(screen.getByTestId('wildcard-advisor-format-historic')).toBeInTheDocument();
      expect(screen.getByTestId('wildcard-advisor-format-explorer')).toBeInTheDocument();
      expect(screen.getByTestId('wildcard-advisor-format-alchemy')).toBeInTheDocument();
    });

    it('Standard is active by default', async () => {
      mockGetRecs.mockResolvedValue(makeResult([]));

      render(<WildcardAdvisorPanel />);

      await waitFor(() =>
        expect(screen.getByTestId('wildcard-advisor-format-standard')).toBeInTheDocument()
      );

      const standardBtn = screen.getByTestId('wildcard-advisor-format-standard');
      expect(standardBtn).toHaveAttribute('aria-pressed', 'true');
      expect(standardBtn.className).toContain('--active');
    });

    it('clicking Historic re-fetches with Historic format', async () => {
      mockGetRecs.mockResolvedValue(makeResult([]));

      render(<WildcardAdvisorPanel />);

      await waitFor(() =>
        expect(screen.getByTestId('wildcard-advisor-format-historic')).toBeInTheDocument()
      );

      fireEvent.click(screen.getByTestId('wildcard-advisor-format-historic'));

      await waitFor(() =>
        expect(mockGetRecs).toHaveBeenCalledWith('Historic', expect.anything())
      );
    });

    it('active format button has aria-pressed="true"', async () => {
      mockGetRecs.mockResolvedValue(makeResult([]));

      render(<WildcardAdvisorPanel />);

      await waitFor(() =>
        expect(screen.getByTestId('wildcard-advisor-format-explorer')).toBeInTheDocument()
      );

      fireEvent.click(screen.getByTestId('wildcard-advisor-format-explorer'));

      await waitFor(() => {
        const explorerBtn = screen.getByTestId('wildcard-advisor-format-explorer');
        expect(explorerBtn).toHaveAttribute('aria-pressed', 'true');
      });
    });
  });

  // ── General panel structure ───────────────────────────────────────────────
  describe('Panel structure', () => {
    it('renders the panel title', async () => {
      mockGetRecs.mockResolvedValue(makeResult([]));

      render(<WildcardAdvisorPanel />);

      expect(screen.getByText('Wildcard Advisor')).toBeInTheDocument();
    });

    it('renders close button and calls onClose when clicked', async () => {
      const onClose = vi.fn();
      mockGetRecs.mockResolvedValue(makeResult([]));

      render(<WildcardAdvisorPanel onClose={onClose} />);

      await waitFor(() =>
        expect(screen.getByTestId('wildcard-advisor-close')).toBeInTheDocument()
      );

      fireEvent.click(screen.getByTestId('wildcard-advisor-close'));
      expect(onClose).toHaveBeenCalledTimes(1);
    });

    it('does not render close button when onClose is not provided', async () => {
      mockGetRecs.mockResolvedValue(makeResult([]));

      render(<WildcardAdvisorPanel />);

      await waitFor(() =>
        expect(screen.queryByTestId('wildcard-advisor-loading')).not.toBeInTheDocument()
      );

      expect(screen.queryByTestId('wildcard-advisor-close')).not.toBeInTheDocument();
    });
  });
});
