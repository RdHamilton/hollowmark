/**
 * WildcardAdvisorPanel — Format toggle and panel structure tests.
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
