/**
 * WildcardAdvisorPanel — PostHog telemetry tests (#422).
 *
 * Covers:
 *  - feature_ml_suggestions_viewed fires once when recs load with count > 0
 *  - feature_ml_suggestions_viewed does NOT fire on empty / 409 / 503 paths
 *  - feature_ml_suggestions_viewed does NOT re-fire on re-renders (loop safety)
 *  - wildcard_recommendation_clicked fires on row expand (not collapse)
 *  - wildcard_recommendation_clicked does NOT fire on collapse
 *  - No event carries raw user_id or email (PII compliance, ADR-027)
 *
 * Split into its own file to stay under the per-worker heap ceiling on CI
 * (NODE_OPTIONS=6144, 2 vCPU, 7 GB RAM — see #2996).
 *
 * Clerk useAuth is globally mocked in src/test/setup.ts.
 * @/services/api is globally mocked in src/test/setup.ts via apiMock.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor, act } from '@testing-library/react';
import WildcardAdvisorPanel from './WildcardAdvisorPanel';
import { ApiRequestError } from '@/services/apiClient';
import type {
  WildcardAdvisorResult,
  WildcardRecommendation,
} from '@/services/api/bffWildcardAdvisor';
import { bffWildcardAdvisor } from '@/services/api';

const mockGetRecs = vi.mocked(bffWildcardAdvisor.getWildcardRecommendations);

// Mock analytics so we can assert PostHog events without a real PostHog key.
const mockTrackEvent = vi.fn();
vi.mock('@/services/analytics', () => ({
  trackEvent: (...args: unknown[]) => mockTrackEvent(...args),
}));

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

const BUDGET = { common: 10, uncommon: 8, rare: 4, mythic: 1 };

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
// Tests
// ---------------------------------------------------------------------------

describe('WildcardAdvisorPanel — PostHog telemetry (#422)', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockTrackEvent.mockReset();
  });

  // ── feature_ml_suggestions_viewed ────────────────────────────────────────

  describe('feature_ml_suggestions_viewed', () => {
    it('fires when recs load with count > 0', async () => {
      const recs = [
        makeRec({ arena_id: 1001, name: 'Sunfall', rarity: 'rare', missing_copies: 2 }),
        makeRec({ arena_id: 1002, name: 'Sheoldred', rarity: 'mythic', missing_copies: 4 }),
      ];
      mockGetRecs.mockResolvedValue(makeResult(recs));

      render(<WildcardAdvisorPanel />);

      await waitFor(() => {
        const calls = mockTrackEvent.mock.calls.filter(
          ([e]: [{ name: string }]) => e.name === 'feature_ml_suggestions_viewed',
        );
        expect(calls).toHaveLength(1);
        expect(calls[0][0].properties.suggestion_count).toBe(2);
        expect(calls[0][0].properties.context).toBe('collection');
      });
    });

    it('does NOT fire when the rec list is empty (200 empty state)', async () => {
      mockGetRecs.mockResolvedValue(makeResult([]));

      render(<WildcardAdvisorPanel />);

      // Wait for empty state to be visible so we know the effect has run
      await waitFor(() =>
        expect(screen.getByTestId('wildcard-advisor-empty-no-data')).toBeInTheDocument()
      );

      const calls = mockTrackEvent.mock.calls.filter(
        ([e]: [{ name: string }]) => e.name === 'feature_ml_suggestions_viewed',
      );
      expect(calls).toHaveLength(0);
    });

    it('does NOT fire on 409 (sync-CTA path)', async () => {
      mockGetRecs.mockRejectedValue(new ApiRequestError('collection_not_synced', 409));

      render(<WildcardAdvisorPanel />);

      await waitFor(() =>
        expect(screen.getByTestId('wildcard-advisor-sync-cta')).toBeInTheDocument()
      );

      const calls = mockTrackEvent.mock.calls.filter(
        ([e]: [{ name: string }]) => e.name === 'feature_ml_suggestions_viewed',
      );
      expect(calls).toHaveLength(0);
    });

    it('does NOT fire on 503 (error-retry path)', async () => {
      mockGetRecs.mockRejectedValue(new ApiRequestError('service_unavailable', 503));

      render(<WildcardAdvisorPanel />);

      await waitFor(() =>
        expect(screen.getByTestId('wildcard-advisor-error')).toBeInTheDocument()
      );

      const calls = mockTrackEvent.mock.calls.filter(
        ([e]: [{ name: string }]) => e.name === 'feature_ml_suggestions_viewed',
      );
      expect(calls).toHaveLength(0);
    });

    it('fires exactly ONCE per load — does not re-fire on re-render (loop-safety)', async () => {
      // This test directly validates the fix for the OOM caused by the
      // infinite render loop in #2996. The ref-keyed guard must prevent
      // re-emission for the same (format, count) pair across multiple renders.
      const recs = [makeRec({ arena_id: 1001, missing_copies: 2 })];
      mockGetRecs.mockResolvedValue(makeResult(recs));

      const { rerender } = render(<WildcardAdvisorPanel />);

      await waitFor(() => {
        const calls = mockTrackEvent.mock.calls.filter(
          ([e]: [{ name: string }]) => e.name === 'feature_ml_suggestions_viewed',
        );
        expect(calls).toHaveLength(1);
      });

      // Force multiple re-renders without a new data load
      await act(async () => {
        rerender(<WildcardAdvisorPanel />);
        rerender(<WildcardAdvisorPanel />);
        rerender(<WildcardAdvisorPanel />);
      });

      const calls = mockTrackEvent.mock.calls.filter(
        ([e]: [{ name: string }]) => e.name === 'feature_ml_suggestions_viewed',
      );
      // Still exactly one — the ref guard held
      expect(calls).toHaveLength(1);
    });

    it('fires again when format changes (new data load with recs)', async () => {
      const recs = [makeRec({ arena_id: 1001, missing_copies: 2 })];
      // Return the same count for both Standard and Historic
      mockGetRecs.mockResolvedValue(makeResult(recs));

      render(<WildcardAdvisorPanel />);

      // Wait for Standard load event
      await waitFor(() => {
        const calls = mockTrackEvent.mock.calls.filter(
          ([e]: [{ name: string }]) => e.name === 'feature_ml_suggestions_viewed',
        );
        expect(calls).toHaveLength(1);
      });

      // Switch to Historic
      fireEvent.click(screen.getByTestId('wildcard-advisor-format-historic'));

      await waitFor(() => {
        const calls = mockTrackEvent.mock.calls.filter(
          ([e]: [{ name: string }]) => e.name === 'feature_ml_suggestions_viewed',
        );
        // Historic load fires a second event (different format key)
        expect(calls).toHaveLength(2);
      });
    });
  });

  // ── wildcard_recommendation_clicked ──────────────────────────────────────

  describe('wildcard_recommendation_clicked', () => {
    it('fires on row expand with rarity and suggestion_count', async () => {
      const recs = [
        makeRec({ arena_id: 1001, name: 'Sunfall', rarity: 'rare', missing_copies: 2 }),
        makeRec({ arena_id: 1002, name: 'Sheoldred', rarity: 'mythic', missing_copies: 4 }),
      ];
      mockGetRecs.mockResolvedValue(makeResult(recs));

      render(<WildcardAdvisorPanel />);

      await waitFor(() =>
        expect(screen.getAllByTestId('wildcard-advisor-rec-name')[0]).toBeInTheDocument()
      );

      // Click the first rec card's expand button
      fireEvent.click(screen.getAllByTestId('wildcard-advisor-rec-card')[0].querySelector('button')!);

      await waitFor(() => {
        const calls = mockTrackEvent.mock.calls.filter(
          ([e]: [{ name: string }]) => e.name === 'wildcard_recommendation_clicked',
        );
        expect(calls).toHaveLength(1);
        expect(calls[0][0].properties.rarity).toBe('rare');
        expect(calls[0][0].properties.suggestion_count).toBe(2);
      });
    });

    it('does NOT fire on collapse (second click)', async () => {
      const recs = [makeRec({ arena_id: 1001, name: 'Sunfall', rarity: 'rare', missing_copies: 2 })];
      mockGetRecs.mockResolvedValue(makeResult(recs));

      render(<WildcardAdvisorPanel />);

      await waitFor(() =>
        expect(screen.getByTestId('wildcard-advisor-rec-name')).toBeInTheDocument()
      );

      const expandBtn = screen.getByTestId('wildcard-advisor-rec-card').querySelector('button')!;

      // First click — expand (fires event)
      fireEvent.click(expandBtn);
      await waitFor(() => {
        const calls = mockTrackEvent.mock.calls.filter(
          ([e]: [{ name: string }]) => e.name === 'wildcard_recommendation_clicked',
        );
        expect(calls).toHaveLength(1);
      });

      // Second click — collapse (must NOT fire a second event)
      fireEvent.click(expandBtn);
      await waitFor(() =>
        expect(screen.queryByTestId('wildcard-advisor-drill-down')).not.toBeInTheDocument()
      );

      const calls = mockTrackEvent.mock.calls.filter(
        ([e]: [{ name: string }]) => e.name === 'wildcard_recommendation_clicked',
      );
      expect(calls).toHaveLength(1);
    });

    it('fires independently for each distinct row expanded', async () => {
      const recs = [
        makeRec({ arena_id: 1001, name: 'Card A', rarity: 'rare', missing_copies: 2 }),
        makeRec({ arena_id: 1002, name: 'Card B', rarity: 'mythic', missing_copies: 4 }),
      ];
      mockGetRecs.mockResolvedValue(makeResult(recs));

      render(<WildcardAdvisorPanel />);

      await waitFor(() =>
        expect(screen.getAllByTestId('wildcard-advisor-rec-card')).toHaveLength(2)
      );

      const cards = screen.getAllByTestId('wildcard-advisor-rec-card');

      fireEvent.click(cards[0].querySelector('button')!);
      fireEvent.click(cards[1].querySelector('button')!);

      await waitFor(() => {
        const calls = mockTrackEvent.mock.calls.filter(
          ([e]: [{ name: string }]) => e.name === 'wildcard_recommendation_clicked',
        );
        expect(calls).toHaveLength(2);
      });
    });
  });

  // ── PII compliance (ADR-027) ──────────────────────────────────────────────

  describe('PII compliance', () => {
    it('no event carries raw user_id or email', async () => {
      const recs = [makeRec({ arena_id: 1001, name: 'Sunfall', rarity: 'rare', missing_copies: 2 })];
      mockGetRecs.mockResolvedValue(makeResult(recs));

      render(<WildcardAdvisorPanel />);

      await waitFor(() =>
        expect(screen.getByTestId('wildcard-advisor-rec-name')).toBeInTheDocument()
      );

      // Trigger the click event too
      fireEvent.click(screen.getByTestId('wildcard-advisor-rec-card').querySelector('button')!);

      await waitFor(() => {
        expect(mockTrackEvent).toHaveBeenCalled();
      });

      for (const [event] of mockTrackEvent.mock.calls as [{ properties: Record<string, unknown> }][]) {
        const payload = JSON.stringify(event.properties);
        expect(payload).not.toContain('user_id');
        expect(payload).not.toContain('@');
      }
    });
  });
});
