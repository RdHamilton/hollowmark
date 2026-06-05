/**
 * WildcardAdvisorPanel — State 3 (409 sync-CTA), State 4 (empty),
 * State 5 (stale-warning), State 6 (503 error-retry).
 *
 * Split from the original monolith to keep each vitest forks-pool worker well
 * under the heap ceiling on the CI runner (7 GB RAM, 2 vCPU).
 *
 * Clerk useAuth is globally mocked in src/test/setup.ts.
 * @/services/api is globally mocked in src/test/setup.ts via apiMock.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import WildcardAdvisorPanel from './WildcardAdvisorPanel';
import { ApiRequestError } from '@/services/apiClient';
import type {
  WildcardAdvisorResult,
} from '@/services/api/bffWildcardAdvisor';
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
});
