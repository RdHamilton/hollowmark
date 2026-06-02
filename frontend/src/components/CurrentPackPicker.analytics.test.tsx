/**
 * CurrentPackPicker — PostHog analytics event tests (#624)
 *
 * Covers:
 *   - feature_ml_suggestions_viewed fires when the recommendation surface
 *     renders with pack data (flag-on default path)
 *   - fires once per unique session/pack/pick key, not on every re-render
 *   - NEGATIVE: does NOT fire when no recommended_card is present
 *   - NEGATIVE: does NOT include user_id or any PII in the event payload
 */
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, waitFor } from '@testing-library/react';
import CurrentPackPicker from './CurrentPackPicker';
import { mockDrafts } from '@/test/mocks/apiMock';
import { gui } from '@/types/models';

vi.mock('@/services/analytics', () => ({
  trackEvent: vi.fn(),
}));

import { trackEvent } from '@/services/analytics';

const mockTrackEvent = vi.mocked(trackEvent);

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function makeCard(overrides: Partial<gui.PackCardWithRating> = {}): gui.PackCardWithRating {
  return new gui.PackCardWithRating({
    arena_id: '1',
    name: 'Lightning Bolt',
    image_url: '',
    rarity: 'common',
    colors: ['R'],
    mana_cost: '{R}',
    cmc: 1,
    type_line: 'Instant',
    gihwr: 65.0,
    alsa: 2.1,
    tier: 'A',
    is_recommended: false,
    score: 0.9,
    reasoning: 'High win rate',
    ...overrides,
  });
}

function makePackResponse(overrides: Partial<gui.CurrentPackResponse> = {}): gui.CurrentPackResponse {
  const rec = makeCard({ arena_id: '1', name: 'Lightning Bolt', is_recommended: true });
  return new gui.CurrentPackResponse({
    session_id: 'session-abc',
    pack_number: 1,
    pick_number: 3,
    pack_label: 'Pack 1, Pick 3',
    cards: [
      rec,
      makeCard({ arena_id: '2', name: 'Counterspell', colors: ['U'], is_recommended: false }),
      makeCard({ arena_id: '3', name: 'Llanowar Elves', colors: ['G'], is_recommended: false }),
    ],
    recommended_card: rec,
    pool_colors: [],
    pool_size: 5,
    ...overrides,
  });
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('CurrentPackPicker — analytics (#624)', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  describe('feature_ml_suggestions_viewed', () => {
    it('fires when the recommendation surface renders with pack data', async () => {
      mockDrafts.getCurrentPackWithRecommendation.mockResolvedValue(makePackResponse());

      render(<CurrentPackPicker sessionID="session-abc" />);

      await waitFor(() => {
        const calls = mockTrackEvent.mock.calls.filter(
          ([e]) => e.name === 'feature_ml_suggestions_viewed',
        );
        expect(calls).toHaveLength(1);
      });
    });

    it('includes suggestion_count and context=draft in the payload', async () => {
      mockDrafts.getCurrentPackWithRecommendation.mockResolvedValue(makePackResponse());

      render(<CurrentPackPicker sessionID="session-abc" />);

      await waitFor(() => {
        const calls = mockTrackEvent.mock.calls.filter(
          ([e]) => e.name === 'feature_ml_suggestions_viewed',
        );
        expect(calls).toHaveLength(1);
        const [event] = calls[0];
        expect(event.name).toBe('feature_ml_suggestions_viewed');
        // Type-narrowed access
        if (event.name === 'feature_ml_suggestions_viewed') {
          expect(event.properties.suggestion_count).toBe(3);
          expect(event.properties.context).toBe('draft');
        }
      });
    });

    it('fires exactly once per unique session/pack/pick key — not on re-render', async () => {
      const packData = makePackResponse();
      // Mock resolves twice (component may re-fetch, but key dedup prevents double-fire)
      mockDrafts.getCurrentPackWithRecommendation.mockResolvedValue(packData);

      const { rerender } = render(<CurrentPackPicker sessionID="session-abc" />);

      await waitFor(() => {
        const calls = mockTrackEvent.mock.calls.filter(
          ([e]) => e.name === 'feature_ml_suggestions_viewed',
        );
        expect(calls).toHaveLength(1);
      });

      // Rerender with same props — must NOT fire again
      rerender(<CurrentPackPicker sessionID="session-abc" />);

      await new Promise((r) => setTimeout(r, 30));

      const calls = mockTrackEvent.mock.calls.filter(
        ([e]) => e.name === 'feature_ml_suggestions_viewed',
      );
      expect(calls).toHaveLength(1);
    });

    it('fires again when sessionID changes (new pack session)', async () => {
      const packData1 = makePackResponse({ session_id: 'session-1', pack_number: 1, pick_number: 1 });
      const packData2 = makePackResponse({ session_id: 'session-2', pack_number: 1, pick_number: 1 });
      mockDrafts.getCurrentPackWithRecommendation
        .mockResolvedValueOnce(packData1)
        .mockResolvedValueOnce(packData2);

      const { rerender } = render(<CurrentPackPicker sessionID="session-1" />);

      await waitFor(() => {
        const calls = mockTrackEvent.mock.calls.filter(
          ([e]) => e.name === 'feature_ml_suggestions_viewed',
        );
        expect(calls).toHaveLength(1);
      });

      rerender(<CurrentPackPicker sessionID="session-2" />);

      await waitFor(() => {
        const calls = mockTrackEvent.mock.calls.filter(
          ([e]) => e.name === 'feature_ml_suggestions_viewed',
        );
        expect(calls).toHaveLength(2);
      });
    });
  });

  describe('NEGATIVE — no event when recommendation surface is absent', () => {
    it('does NOT fire when packData has no recommended_card', async () => {
      const packNoRec = makePackResponse({ recommended_card: undefined });
      mockDrafts.getCurrentPackWithRecommendation.mockResolvedValue(packNoRec);

      render(<CurrentPackPicker sessionID="session-abc" />);

      // Wait for component to settle
      await waitFor(() => {
        // The pack cards should render (component loaded)
        expect(mockDrafts.getCurrentPackWithRecommendation).toHaveBeenCalled();
      });
      await new Promise((r) => setTimeout(r, 30));

      const calls = mockTrackEvent.mock.calls.filter(
        ([e]) => e.name === 'feature_ml_suggestions_viewed',
      );
      expect(calls).toHaveLength(0);
    });

    it('does NOT fire when packData is null (no pack available)', async () => {
      mockDrafts.getCurrentPackWithRecommendation.mockResolvedValue(null);

      render(<CurrentPackPicker sessionID="session-abc" />);

      await waitFor(() => {
        expect(mockDrafts.getCurrentPackWithRecommendation).toHaveBeenCalled();
      });
      await new Promise((r) => setTimeout(r, 30));

      const calls = mockTrackEvent.mock.calls.filter(
        ([e]) => e.name === 'feature_ml_suggestions_viewed',
      );
      expect(calls).toHaveLength(0);
    });
  });

  describe('NEGATIVE — no PII in event payload', () => {
    it('does not include user_id, session_id, or any account identifier in the payload', async () => {
      mockDrafts.getCurrentPackWithRecommendation.mockResolvedValue(makePackResponse());

      render(<CurrentPackPicker sessionID="session-abc" />);

      await waitFor(() => {
        const calls = mockTrackEvent.mock.calls.filter(
          ([e]) => e.name === 'feature_ml_suggestions_viewed',
        );
        expect(calls).toHaveLength(1);
      });

      const calls = mockTrackEvent.mock.calls.filter(
        ([e]) => e.name === 'feature_ml_suggestions_viewed',
      );
      const [event] = calls[0];
      expect(event).not.toHaveProperty('properties.user_id');
      expect(event).not.toHaveProperty('properties.session_id');
      expect(event).not.toHaveProperty('properties.account_id');
    });
  });
});
