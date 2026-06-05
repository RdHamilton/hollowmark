/**
 * WildcardAdvisorPanel — State 1 (loading) and State 2 (data/recommendations).
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
});
