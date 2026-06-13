import { describe, it, expect, vi, beforeEach } from 'vitest';
import { readFileSync } from 'node:fs';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { render } from '../test/utils/testUtils';
import CurrentPackPicker from './CurrentPackPicker';
import { mockDrafts } from '@/test/mocks/apiMock';
import { gui } from '@/types/models';

const CSS_PATH = join(dirname(fileURLToPath(import.meta.url)), 'CurrentPackPicker.css');

// — Design token compliance (AC2, #312) ————————————————————————————————
describe('CurrentPackPicker CSS — design token compliance (#312)', () => {
  const css = readFileSync(CSS_PATH, 'utf8');

  it('container uses canonical --bg-raised token, not legacy --bg-secondary', () => {
    expect(css).toContain('background: var(--bg-raised)');
    expect(css).not.toContain('var(--bg-secondary)');
  });

  it('header border uses canonical --border token, not legacy --border-color', () => {
    expect(css).toContain('border-bottom: 1px solid var(--border)');
    expect(css).not.toContain('var(--border-color)');
  });

  it('recommended banner uses sapphire-dim token, not raw gold rgba', () => {
    expect(css).toContain('var(--vault-sapphire-dim)');
    expect(css).not.toMatch(/rgba\(\s*255\s*,\s*215\s*,\s*0/);
  });

  it('recommended pack card uses sapphire shadow, not gold glow', () => {
    expect(css).toContain('var(--shadow-sapphire-vault)');
    expect(css).toContain('border-color: var(--accent)');
    expect(css).not.toMatch(/rgba\(\s*255\s*,\s*215\s*,\s*0\s*,\s*0\.3\)/);
  });

  it('card color indicators use mana-pip tokens, not raw hex (#328)', () => {
    expect(css).toContain('var(--vault-mtg-colorless)');
    // #328: MTG pip backgrounds migrated onto the canonical mana-pip token
    // contract (Ramone 2026-05-31) — no raw pip hex remains.
    expect(css).toContain('var(--mana-w-bg)');
    expect(css).toContain('var(--mana-u-bg)');
    expect(css).toContain('var(--mana-b-bg)');
    expect(css).toContain('var(--mana-r-bg)');
    expect(css).toContain('var(--mana-g-bg)');
    expect(css).toContain('var(--mana-pip-fg)');
    expect(css).not.toContain('#f9faf4');
    expect(css).not.toContain('#0e68ab');
    expect(css).not.toContain('#150b00');
    expect(css).not.toContain('#00733e');
    // But no legacy #555 border
    expect(css).not.toContain('#555');
  });

  it('text colors use canonical fg tokens, not legacy --text-* names', () => {
    expect(css).not.toContain('var(--text-primary)');
    expect(css).not.toContain('var(--text-secondary)');
    expect(css).not.toContain('var(--text-tertiary)');
    expect(css).toContain('var(--fg)');
    expect(css).toContain('var(--fg-secondary)');
    expect(css).toContain('var(--fg-muted)');
  });

  it('accent buttons use canonical --accent token, not legacy --accent-color', () => {
    expect(css).not.toContain('var(--accent-color)');
    expect(css).toContain('var(--accent)');
  });

  it('refresh/retry buttons use --fg-inverse for text on sapphire background', () => {
    expect(css).not.toContain('color: white');
    expect(css).toContain('var(--fg-inverse)');
  });
});
// ——————————————————————————————————————————————————————————————————————————

// ── Tier badge CSS token compliance (#686) ─────────────────────────────────
describe('CurrentPackPicker CSS — tier badge design tokens (#686)', () => {
  const css = readFileSync(CSS_PATH, 'utf8');

  it('tier badge uses design-system tier tokens, not raw hex (#686)', () => {
    expect(css).toContain('var(--vault-tier-a)');
    expect(css).toContain('var(--vault-tier-b)');
    expect(css).toContain('var(--vault-tier-c)');
    expect(css).toContain('var(--vault-tier-d)');
    expect(css).toContain('var(--vault-tier-f)');
    // Must not use the old raw hex values from the pre-#686 getTierColor function.
    expect(css).not.toContain('#ffd700');
    expect(css).not.toContain('#c0c0c0');
    expect(css).not.toContain('#cd7f32');
    expect(css).not.toContain('#4a9eff');
    expect(css).not.toContain('#888888');
    expect(css).not.toContain('#ff4444');
  });

  it('tier badge is 28px wide and 22px tall per §7.3', () => {
    expect(css).toContain('width: 28px');
    expect(css).toContain('height: 22px');
  });

  it('tier badge is positioned bottom-right (not top-right) per §7.3', () => {
    // The badge block must contain "bottom:" and NOT use top positioning.
    // We check the .tier-badge rule contains bottom, not top.
    const tierBadgeBlock = css.slice(css.indexOf('.tier-badge {'), css.indexOf('.tier-badge--a'));
    expect(tierBadgeBlock).toContain('bottom:');
    expect(tierBadgeBlock).not.toContain('top:');
  });

  it('tier badge uses --radius-sm border-radius (not 50%)', () => {
    const tierBadgeBlock = css.slice(css.indexOf('.tier-badge {'), css.indexOf('.tier-badge--a'));
    expect(tierBadgeBlock).toContain('var(--radius-sm)');
    expect(tierBadgeBlock).not.toContain('border-radius: 50%');
  });
});
// ——————————————————————————————————————————————————————————————————————————

function createMockPackCard(overrides: Partial<gui.PackCardWithRating> = {}): gui.PackCardWithRating {
  return new gui.PackCardWithRating({
    arena_id: '12345',
    name: 'Lightning Bolt',
    image_url: 'https://example.com/bolt.jpg',
    rarity: 'common',
    colors: ['R'],
    mana_cost: '{R}',
    cmc: 1,
    type_line: 'Instant',
    // gihwr is a FRACTION (0.0–1.0), per the #785/#787 end-to-end units
    // decision (Bob: daemon grade-pick path serves the BFF fraction verbatim).
    // 0.585 renders as "58.5%" at the display boundary.
    gihwr: 0.585,
    alsa: 3.2,
    tier: 'S',
    is_recommended: false,
    score: 0.85,
    reasoning: 'This card high win rate card.',
    ...overrides,
  });
}

function createMockPackResponse(overrides: Partial<gui.CurrentPackResponse> = {}): gui.CurrentPackResponse {
  return new gui.CurrentPackResponse({
    session_id: 'session-123',
    pack_number: 0,
    pick_number: 0,
    pack_label: 'Pack 1, Pick 1',
    cards: [
      createMockPackCard({ arena_id: '1', name: 'Lightning Bolt', score: 0.9, is_recommended: true }),
      createMockPackCard({ arena_id: '2', name: 'Counterspell', score: 0.8, colors: ['U'] }),
      createMockPackCard({ arena_id: '3', name: 'Llanowar Elves', score: 0.7, colors: ['G'] }),
    ],
    recommended_card: createMockPackCard({ arena_id: '1', name: 'Lightning Bolt', score: 0.9, is_recommended: true }),
    pool_colors: [],
    pool_size: 0,
    ...overrides,
  });
}

describe('CurrentPackPicker Component', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  describe('Loading State', () => {
    it('should show loading state initially', () => {
      mockDrafts.getCurrentPackWithRecommendation.mockImplementation(() => new Promise(() => {}));

      render(<CurrentPackPicker sessionID="test-session" />);

      expect(screen.getByText('Loading current pack...')).toBeInTheDocument();
    });
  });

  describe('Error State', () => {
    it('should show error message when loading fails', async () => {
      mockDrafts.getCurrentPackWithRecommendation.mockRejectedValue(new Error('Network error'));

      render(<CurrentPackPicker sessionID="test-session" />);

      await waitFor(() => {
        expect(screen.getByText('Network error')).toBeInTheDocument();
      });
    });

    it('should show retry button when error occurs', async () => {
      mockDrafts.getCurrentPackWithRecommendation.mockRejectedValue(new Error('Network error'));

      render(<CurrentPackPicker sessionID="test-session" />);

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /Retry/i })).toBeInTheDocument();
      });
    });

    it('should reload data when retry button is clicked', async () => {
      mockDrafts.getCurrentPackWithRecommendation.mockRejectedValueOnce(new Error('Network error'));
      const packData = createMockPackResponse();
      mockDrafts.getCurrentPackWithRecommendation.mockResolvedValueOnce(packData);

      render(<CurrentPackPicker sessionID="test-session" />);

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /Retry/i })).toBeInTheDocument();
      });

      await userEvent.click(screen.getByRole('button', { name: /Retry/i }));

      await waitFor(() => {
        expect(screen.getByText('Pack 1, Pick 1')).toBeInTheDocument();
      });
    });
  });

  describe('Empty State', () => {
    it('should show empty state when no pack data available', async () => {
      mockDrafts.getCurrentPackWithRecommendation.mockResolvedValue(null);

      render(<CurrentPackPicker sessionID="test-session" />);

      await waitFor(() => {
        expect(screen.getByText('No pack data available')).toBeInTheDocument();
      });
    });

    it('should show help text when no pack data', async () => {
      mockDrafts.getCurrentPackWithRecommendation.mockResolvedValue(null);

      render(<CurrentPackPicker sessionID="test-session" />);

      await waitFor(() => {
        expect(screen.getByText('Pack data will appear when you start a draft pick')).toBeInTheDocument();
      });
    });
  });

  describe('Display Pack Data', () => {
    it('should display pack label', async () => {
      const packData = createMockPackResponse({ pack_label: 'Pack 2, Pick 5' });
      mockDrafts.getCurrentPackWithRecommendation.mockResolvedValue(packData);

      render(<CurrentPackPicker sessionID="test-session" />);

      await waitFor(() => {
        expect(screen.getByText('Pack 2, Pick 5')).toBeInTheDocument();
      });
    });

    it('should display pool size', async () => {
      const packData = createMockPackResponse({ pool_size: 10 });
      mockDrafts.getCurrentPackWithRecommendation.mockResolvedValue(packData);

      render(<CurrentPackPicker sessionID="test-session" />);

      await waitFor(() => {
        expect(screen.getByText('Pool: 10 cards')).toBeInTheDocument();
      });
    });

    it('should display cards in the pack', async () => {
      const packData = createMockPackResponse();
      mockDrafts.getCurrentPackWithRecommendation.mockResolvedValue(packData);

      render(<CurrentPackPicker sessionID="test-session" />);

      await waitFor(() => {
        // Lightning Bolt appears twice (in banner and grid)
        expect(screen.getAllByText('Lightning Bolt').length).toBeGreaterThanOrEqual(1);
        expect(screen.getByText('Counterspell')).toBeInTheDocument();
        expect(screen.getByText('Llanowar Elves')).toBeInTheDocument();
      });
    });

    it('should display tier badges for each card', async () => {
      const packData = createMockPackResponse({
        cards: [
          createMockPackCard({ arena_id: '1', name: 'Card A', tier: 'S' }),
          createMockPackCard({ arena_id: '2', name: 'Card B', tier: 'A' }),
          createMockPackCard({ arena_id: '3', name: 'Card C', tier: 'B' }),
        ],
      });
      mockDrafts.getCurrentPackWithRecommendation.mockResolvedValue(packData);

      render(<CurrentPackPicker sessionID="test-session" />);

      await waitFor(() => {
        const tierBadges = screen.getAllByText(/^[SABCDF]$/);
        expect(tierBadges.length).toBeGreaterThan(0);
      });
    });
  });

  // ── Tier badge design-system compliance (#686) ───────────────────────────
  describe('Tier badge inline — design-system §7.3 (#686)', () => {
    it('renders tier badge with correct CSS class for tier A (sapphire)', async () => {
      const packData = createMockPackResponse({
        cards: [createMockPackCard({ arena_id: '10', name: 'Tier A Card', tier: 'A' })],
      });
      mockDrafts.getCurrentPackWithRecommendation.mockResolvedValue(packData);

      const { container } = render(<CurrentPackPicker sessionID="test-session" />);

      await waitFor(() => {
        const badge = container.querySelector('[data-testid="tier-badge-10"]');
        expect(badge).toBeInTheDocument();
        expect(badge).toHaveClass('tier-badge--a');
        expect(badge).not.toHaveClass('tier-badge--b');
      });
    });

    it('renders tier badge with correct CSS class for tier B (success-green)', async () => {
      const packData = createMockPackResponse({
        cards: [createMockPackCard({ arena_id: '11', name: 'Tier B Card', tier: 'B' })],
      });
      mockDrafts.getCurrentPackWithRecommendation.mockResolvedValue(packData);

      const { container } = render(<CurrentPackPicker sessionID="test-session" />);

      await waitFor(() => {
        const badge = container.querySelector('[data-testid="tier-badge-11"]');
        expect(badge).toBeInTheDocument();
        expect(badge).toHaveClass('tier-badge--b');
      });
    });

    it('renders tier badge with correct CSS class for tier C (slate)', async () => {
      const packData = createMockPackResponse({
        cards: [createMockPackCard({ arena_id: '12', name: 'Tier C Card', tier: 'C' })],
      });
      mockDrafts.getCurrentPackWithRecommendation.mockResolvedValue(packData);

      const { container } = render(<CurrentPackPicker sessionID="test-session" />);

      await waitFor(() => {
        const badge = container.querySelector('[data-testid="tier-badge-12"]');
        expect(badge).toBeInTheDocument();
        expect(badge).toHaveClass('tier-badge--c');
      });
    });

    it('renders tier badge with correct CSS class for tier D (yellow)', async () => {
      const packData = createMockPackResponse({
        cards: [createMockPackCard({ arena_id: '13', name: 'Tier D Card', tier: 'D' })],
      });
      mockDrafts.getCurrentPackWithRecommendation.mockResolvedValue(packData);

      const { container } = render(<CurrentPackPicker sessionID="test-session" />);

      await waitFor(() => {
        const badge = container.querySelector('[data-testid="tier-badge-13"]');
        expect(badge).toBeInTheDocument();
        expect(badge).toHaveClass('tier-badge--d');
      });
    });

    it('renders tier badge with correct CSS class for tier F (danger-red)', async () => {
      const packData = createMockPackResponse({
        cards: [createMockPackCard({ arena_id: '14', name: 'Tier F Card', tier: 'F' })],
      });
      mockDrafts.getCurrentPackWithRecommendation.mockResolvedValue(packData);

      const { container } = render(<CurrentPackPicker sessionID="test-session" />);

      await waitFor(() => {
        const badge = container.querySelector('[data-testid="tier-badge-14"]');
        expect(badge).toBeInTheDocument();
        expect(badge).toHaveClass('tier-badge--f');
      });
    });

    it('does NOT render a tier badge when tier is empty string', async () => {
      const packData = createMockPackResponse({
        cards: [createMockPackCard({ arena_id: '20', name: 'No Grade Card', tier: '' })],
      });
      mockDrafts.getCurrentPackWithRecommendation.mockResolvedValue(packData);

      const { container } = render(<CurrentPackPicker sessionID="test-session" />);

      await waitFor(() => {
        expect(screen.getByText('No Grade Card')).toBeInTheDocument();
        expect(container.querySelector('[data-testid="tier-badge-20"]')).not.toBeInTheDocument();
      });
    });

    it('does NOT render a tier badge when tier is undefined/falsy', async () => {
      const packData = createMockPackResponse({
        cards: [createMockPackCard({ arena_id: '21', name: 'Ungraded Card', tier: '' })],
      });
      // Explicitly drop the tier field.
      (packData.cards[0] as unknown as Record<string, unknown>)['tier'] = undefined;
      mockDrafts.getCurrentPackWithRecommendation.mockResolvedValue(packData);

      const { container } = render(<CurrentPackPicker sessionID="test-session" />);

      await waitFor(() => {
        expect(screen.getByText('Ungraded Card')).toBeInTheDocument();
        expect(container.querySelector('[data-testid="tier-badge-21"]')).not.toBeInTheDocument();
      });
    });

    it('tier badge has aria-label="Tier <X>" for accessibility', async () => {
      const packData = createMockPackResponse({
        cards: [createMockPackCard({ arena_id: '30', name: 'Aria Card', tier: 'A' })],
      });
      mockDrafts.getCurrentPackWithRecommendation.mockResolvedValue(packData);

      const { container } = render(<CurrentPackPicker sessionID="test-session" />);

      await waitFor(() => {
        const badge = container.querySelector('[data-testid="tier-badge-30"]');
        expect(badge).toHaveAttribute('aria-label', 'Tier A');
      });
    });

    it('tier badge has data-testid="tier-badge-{arenaId}" for E2E selection', async () => {
      const packData = createMockPackResponse({
        cards: [createMockPackCard({ arena_id: 'xyz99', name: 'E2E Card', tier: 'B' })],
      });
      mockDrafts.getCurrentPackWithRecommendation.mockResolvedValue(packData);

      const { container } = render(<CurrentPackPicker sessionID="test-session" />);

      await waitFor(() => {
        const badge = container.querySelector('[data-testid="tier-badge-xyz99"]');
        expect(badge).toBeInTheDocument();
        expect(badge?.textContent).toBe('B');
      });
    });
  });
  // ─────────────────────────────────────────────────────────────────────────

  describe('Recommended Pick', () => {
    it('should display recommended card banner', async () => {
      const packData = createMockPackResponse();
      // Verify the recommended_card is set
      expect(packData.recommended_card).toBeDefined();
      mockDrafts.getCurrentPackWithRecommendation.mockResolvedValue(packData);

      render(<CurrentPackPicker sessionID="test-session" />);

      await waitFor(() => {
        // Check that the recommended banner shows up
        const banner = screen.getByText('Recommended Pick:');
        expect(banner).toBeInTheDocument();
      });
    });

    it('should highlight the recommended card in the grid', async () => {
      const packData = createMockPackResponse();
      mockDrafts.getCurrentPackWithRecommendation.mockResolvedValue(packData);

      const { container } = render(<CurrentPackPicker sessionID="test-session" />);

      await waitFor(() => {
        const recommendedCard = container.querySelector('.pack-card.recommended');
        expect(recommendedCard).toBeInTheDocument();
      });
    });

    it('should display Best Pick indicator on recommended card', async () => {
      const packData = createMockPackResponse();
      mockDrafts.getCurrentPackWithRecommendation.mockResolvedValue(packData);

      render(<CurrentPackPicker sessionID="test-session" />);

      await waitFor(() => {
        expect(screen.getByText('Best Pick')).toBeInTheDocument();
      });
    });
  });

  describe('Refresh Functionality', () => {
    it('should have a refresh button', async () => {
      const packData = createMockPackResponse();
      mockDrafts.getCurrentPackWithRecommendation.mockResolvedValue(packData);

      render(<CurrentPackPicker sessionID="test-session" />);

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /Refresh/i })).toBeInTheDocument();
      });
    });

    it('should reload data when refresh button is clicked', async () => {
      const packData1 = createMockPackResponse({ pack_label: 'Pack 1, Pick 1' });
      const packData2 = createMockPackResponse({ pack_label: 'Pack 1, Pick 2' });
      mockDrafts.getCurrentPackWithRecommendation.mockResolvedValueOnce(packData1);
      mockDrafts.getCurrentPackWithRecommendation.mockResolvedValueOnce(packData2);

      render(<CurrentPackPicker sessionID="test-session" />);

      await waitFor(() => {
        expect(screen.getByText('Pack 1, Pick 1')).toBeInTheDocument();
      });

      await userEvent.click(screen.getByRole('button', { name: /Refresh/i }));

      await waitFor(() => {
        expect(screen.getByText('Pack 1, Pick 2')).toBeInTheDocument();
      });
    });

    it('should call onRefresh callback when refresh is clicked', async () => {
      const packData = createMockPackResponse();
      mockDrafts.getCurrentPackWithRecommendation.mockResolvedValue(packData);
      const onRefresh = vi.fn();

      render(<CurrentPackPicker sessionID="test-session" onRefresh={onRefresh} />);

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /Refresh/i })).toBeInTheDocument();
      });

      await userEvent.click(screen.getByRole('button', { name: /Refresh/i }));

      expect(onRefresh).toHaveBeenCalled();
    });
  });

  describe('Card Statistics', () => {
    it('should display GIHWR for each card', async () => {
      const packData = createMockPackResponse({
        cards: [
          createMockPackCard({ arena_id: '1', name: 'Card A', gihwr: 0.585 }),
        ],
      });
      mockDrafts.getCurrentPackWithRecommendation.mockResolvedValue(packData);

      render(<CurrentPackPicker sessionID="test-session" />);

      await waitFor(() => {
        expect(screen.getByText('58.5%')).toBeInTheDocument();
      });
    });

    // ── GIHWR units regression (#787) ──────────────────────────────────────
    // The daemon grade-pick path serves gihwr as a FRACTION (0.0–1.0). The
    // display multiplies by 100 at the boundary, so a 0.631 GIHWR card must
    // render "63.1%", NOT the buggy "0.6%" (raw fraction with a % suffix).
    it('renders a fractional gihwr as a percent (0.631 → "63.1%", not "0.6%")', async () => {
      const packData = createMockPackResponse({
        cards: [
          createMockPackCard({ arena_id: '1', name: 'Sire of Seven Deaths', gihwr: 0.631 }),
        ],
      });
      mockDrafts.getCurrentPackWithRecommendation.mockResolvedValue(packData);

      render(<CurrentPackPicker sessionID="test-session" />);

      await waitFor(() => {
        expect(screen.getByText('63.1%')).toBeInTheDocument();
      });
      expect(screen.queryByText('0.6%')).not.toBeInTheDocument();
    });

    it('renders an em-dash (not "0.0%") when gihwr is 0 / missing', async () => {
      const packData = createMockPackResponse({
        cards: [
          createMockPackCard({ arena_id: '1', name: 'No Data Card', gihwr: 0 }),
        ],
      });
      mockDrafts.getCurrentPackWithRecommendation.mockResolvedValue(packData);

      render(<CurrentPackPicker sessionID="test-session" />);

      await waitFor(() => {
        expect(screen.getByText('No Data Card')).toBeInTheDocument();
      });
      expect(screen.queryByText('0.0%')).not.toBeInTheDocument();
    });

    it('should display ALSA for each card', async () => {
      const packData = createMockPackResponse({
        cards: [
          createMockPackCard({ arena_id: '1', name: 'Card A', alsa: 3.2 }),
        ],
      });
      mockDrafts.getCurrentPackWithRecommendation.mockResolvedValue(packData);

      render(<CurrentPackPicker sessionID="test-session" />);

      await waitFor(() => {
        expect(screen.getByText('ALSA: 3.2')).toBeInTheDocument();
      });
    });
  });

  describe('Color Indicators', () => {
    it('should display mana pip indicators for colored cards', async () => {
      const packData = createMockPackResponse({
        cards: [
          createMockPackCard({ arena_id: '1', name: 'Card A', colors: ['R', 'U'] }),
        ],
      });
      mockDrafts.getCurrentPackWithRecommendation.mockResolvedValue(packData);

      render(<CurrentPackPicker sessionID="test-session" />);

      await waitFor(() => {
        expect(screen.getByTestId('mana-pip-r')).toBeInTheDocument();
        expect(screen.getByTestId('mana-pip-u')).toBeInTheDocument();
      });
    });

    it('should display colorless pip for colorless cards', async () => {
      const packData = createMockPackResponse({
        cards: [
          createMockPackCard({ arena_id: '1', name: 'Artifact', colors: [] }),
        ],
      });
      mockDrafts.getCurrentPackWithRecommendation.mockResolvedValue(packData);

      render(<CurrentPackPicker sessionID="test-session" />);

      await waitFor(() => {
        expect(screen.getByTestId('mana-pip-c')).toBeInTheDocument();
      });
    });
  });

  describe('Session ID Changes', () => {
    it('should reload data when sessionID changes', async () => {
      const packData1 = createMockPackResponse({ session_id: 'session-1' });
      const packData2 = createMockPackResponse({ session_id: 'session-2' });
      mockDrafts.getCurrentPackWithRecommendation.mockResolvedValueOnce(packData1);
      mockDrafts.getCurrentPackWithRecommendation.mockResolvedValueOnce(packData2);

      const { rerender } = render(<CurrentPackPicker sessionID="session-1" />);

      await waitFor(() => {
        expect(mockDrafts.getCurrentPackWithRecommendation).toHaveBeenCalledWith('session-1');
      });

      rerender(<CurrentPackPicker sessionID="session-2" />);

      await waitFor(() => {
        expect(mockDrafts.getCurrentPackWithRecommendation).toHaveBeenCalledWith('session-2');
      });
    });
  });

  describe('Card Reasoning', () => {
    it('should display reasoning when available', async () => {
      const packData = createMockPackResponse({
        cards: [
          createMockPackCard({
            arena_id: '1',
            name: 'Card A',
            reasoning: 'This card high win rate card and matches your colors.'
          }),
        ],
      });
      mockDrafts.getCurrentPackWithRecommendation.mockResolvedValue(packData);

      render(<CurrentPackPicker sessionID="test-session" />);

      await waitFor(() => {
        expect(screen.getByText('This card high win rate card and matches your colors.')).toBeInTheDocument();
      });
    });
  });

  // ── Phase A recommendation contract tests ────────────────────────────────
  // These tests verify that the daemon's snake_case recommendation payload
  // is correctly consumed by CurrentPackPicker. They exercise the contract
  // established in MH-ML1 (vmt-t#399): arena_id, is_recommended, score,
  // reasoning, recommended_card.

  describe('Phase A Recommendation Contract', () => {
    it('populates recommended_card from daemon response', async () => {
      // Simulate the daemon response shape for Phase A: top-pick card has
      // is_recommended=true and a plain-English reason (no raw % in reason).
      const recommendedCard = createMockPackCard({
        arena_id: '100',
        name: 'Lightning Bolt',
        is_recommended: true,
        score: 1.0,
        reasoning: 'Best pick in the pack',
        gihwr: 72.0,
      });
      const packData = createMockPackResponse({
        cards: [
          recommendedCard,
          createMockPackCard({ arena_id: '200', name: 'Bear', gihwr: 55.0, reasoning: 'Solid pick' }),
        ],
        recommended_card: recommendedCard,
      });
      mockDrafts.getCurrentPackWithRecommendation.mockResolvedValue(packData);

      render(<CurrentPackPicker sessionID="test-session" />);

      await waitFor(() => {
        expect(screen.getByText('Recommended Pick:')).toBeInTheDocument();
        expect(screen.getAllByText('Lightning Bolt').length).toBeGreaterThanOrEqual(1);
      });
    });

    it('displays plain-English reasoning (no raw GIHWR percentage in primary display)', async () => {
      // Prof gate: raw GIHWR % must not appear in the card reasoning field.
      // The reasoning text itself must not contain a literal "%".
      const packData = createMockPackResponse({
        cards: [
          createMockPackCard({
            arena_id: '1',
            name: 'Test Card',
            reasoning: 'Best pick in the pack',  // plain English, no %
            is_recommended: true,
            score: 1.0,
            // Override gihwr to 0 so the GIHWR span doesn't render a "%" near by.
            gihwr: 0,
          }),
        ],
        recommended_card: createMockPackCard({
          arena_id: '1',
          name: 'Test Card',
          reasoning: 'Best pick in the pack',
          is_recommended: true,
          gihwr: 0,
        }),
      });
      mockDrafts.getCurrentPackWithRecommendation.mockResolvedValue(packData);

      const { container } = render(<CurrentPackPicker sessionID="test-session" />);

      await waitFor(() => {
        // Find the .card-reasoning div and check its text content has no %.
        const reasonDivs = container.querySelectorAll('.card-reasoning');
        expect(reasonDivs.length).toBeGreaterThan(0);
        reasonDivs.forEach((div) => {
          expect(div.textContent).not.toMatch(/%/);
        });
      });
    });

    it('highlights recommended card with is_recommended class', async () => {
      const packData = createMockPackResponse({
        cards: [
          createMockPackCard({ arena_id: '1', name: 'Best Card', is_recommended: true }),
          createMockPackCard({ arena_id: '2', name: 'Other Card', is_recommended: false }),
        ],
        recommended_card: createMockPackCard({ arena_id: '1', name: 'Best Card', is_recommended: true }),
      });
      mockDrafts.getCurrentPackWithRecommendation.mockResolvedValue(packData);

      const { container } = render(<CurrentPackPicker sessionID="test-session" />);

      await waitFor(() => {
        const recommendedCards = container.querySelectorAll('.pack-card.recommended');
        expect(recommendedCards.length).toBe(1);
      });
    });

    it('renders gracefully when no recommended_card is present (N/A state)', async () => {
      // When the daemon returns no ratings for a new set, recommended_card
      // is absent — the component must not crash.
      const packData = createMockPackResponse({
        cards: [
          createMockPackCard({ arena_id: '1', name: 'Card A', gihwr: 0, reasoning: 'No rating data available for this set', is_recommended: false }),
        ],
        recommended_card: undefined,
      });
      mockDrafts.getCurrentPackWithRecommendation.mockResolvedValue(packData);

      render(<CurrentPackPicker sessionID="test-session" />);

      await waitFor(() => {
        expect(screen.getByText('Card A')).toBeInTheDocument();
        // Banner must not render when recommended_card is absent.
        expect(screen.queryByText('Recommended Pick:')).not.toBeInTheDocument();
      });
    });

    it('displays pool size from daemon response', async () => {
      const packData = createMockPackResponse({ pool_size: 7 });
      mockDrafts.getCurrentPackWithRecommendation.mockResolvedValue(packData);

      render(<CurrentPackPicker sessionID="test-session" />);

      await waitFor(() => {
        expect(screen.getByText('Pool: 7 cards')).toBeInTheDocument();
      });
    });

    it('uses pack_label from daemon response as heading', async () => {
      const packData = createMockPackResponse({ pack_label: 'Pack 2, Pick 8' });
      mockDrafts.getCurrentPackWithRecommendation.mockResolvedValue(packData);

      render(<CurrentPackPicker sessionID="test-session" />);

      await waitFor(() => {
        expect(screen.getByText('Pack 2, Pick 8')).toBeInTheDocument();
      });
    });
  });

  // ── low_confidence "Limited data" pill (vmt-t#646) ─────────────────────
  // Prof gate: backend emits low_confidence=true when sample size < 500 GIH
  // games. The SPA must surface a visible "Limited data" pill/badge so a
  // skimmer can distinguish a data-backed rec from a sub-500-sample one.

  describe('low_confidence indicator (vmt-t#646)', () => {
    it('shows "Limited data" pill when low_confidence is true on a card', async () => {
      const packData = createMockPackResponse({
        cards: [
          createMockPackCard({ arena_id: '1', name: 'Rare Card', low_confidence: true }),
          createMockPackCard({ arena_id: '2', name: 'Normal Card', low_confidence: false }),
        ],
      });
      mockDrafts.getCurrentPackWithRecommendation.mockResolvedValue(packData);

      render(<CurrentPackPicker sessionID="test-session" />);

      await waitFor(() => {
        expect(screen.getByTestId('low-confidence-1')).toBeInTheDocument();
        expect(screen.getByTestId('low-confidence-1')).toHaveTextContent('Limited data');
      });
    });

    it('does NOT show "Limited data" pill when low_confidence is false', async () => {
      const packData = createMockPackResponse({
        cards: [
          createMockPackCard({ arena_id: '1', name: 'Well-Sampled Card', low_confidence: false }),
        ],
      });
      mockDrafts.getCurrentPackWithRecommendation.mockResolvedValue(packData);

      render(<CurrentPackPicker sessionID="test-session" />);

      await waitFor(() => {
        expect(screen.queryByTestId('low-confidence-1')).not.toBeInTheDocument();
      });
    });

    it('does NOT show "Limited data" pill when low_confidence is undefined', async () => {
      const packData = createMockPackResponse({
        cards: [
          createMockPackCard({ arena_id: '1', name: 'Card Without Field' }),
        ],
      });
      // Explicitly drop low_confidence from the card object
      (packData.cards[0] as unknown as Record<string, unknown>)['low_confidence'] = undefined;
      mockDrafts.getCurrentPackWithRecommendation.mockResolvedValue(packData);

      render(<CurrentPackPicker sessionID="test-session" />);

      await waitFor(() => {
        expect(screen.queryByTestId('low-confidence-1')).not.toBeInTheDocument();
      });
    });

    it('pill has data-testid="low-confidence-{arenaId}" for E2E selection', async () => {
      const packData = createMockPackResponse({
        cards: [
          createMockPackCard({ arena_id: 'abc123', name: 'Low Sample', low_confidence: true }),
        ],
      });
      mockDrafts.getCurrentPackWithRecommendation.mockResolvedValue(packData);

      const { container } = render(<CurrentPackPicker sessionID="test-session" />);

      await waitFor(() => {
        const pill = container.querySelector('[data-testid="low-confidence-abc123"]');
        expect(pill).toBeInTheDocument();
      });
    });
  });

  // ── card-back fallback (Tim staging bug — dead Scryfall CDN) ───────────
  // The old CARD_BACK_URL pointed at a dead Scryfall CDN path that returned
  // 404, causing both the primary (null image_url) and the onError handler to
  // show a broken-image icon indefinitely.  The fix replaces both with the
  // local /back.png asset which is guaranteed to serve 200.

  describe('card-back fallback renders /back.png (not dead Scryfall URL)', () => {
    it('card with no image_url uses /back.png as src', async () => {
      const packData = createMockPackResponse({
        cards: [
          createMockPackCard({ arena_id: '1', name: 'Unknown Card', image_url: undefined }),
        ],
      });
      (packData.cards[0] as unknown as Record<string, unknown>)['image_url'] = undefined;
      mockDrafts.getCurrentPackWithRecommendation.mockResolvedValue(packData);

      const { container } = render(<CurrentPackPicker sessionID="test-session" />);

      await waitFor(() => {
        const img = container.querySelector('[data-testid="pack-card-1"] img') as HTMLImageElement | null;
        expect(img).toBeTruthy();
        expect(img!.src).toMatch(/\/back\.png$/);
      });
    });

    it('onError handler sets src to /back.png, not a remote Scryfall URL', async () => {
      const packData = createMockPackResponse({
        cards: [
          createMockPackCard({ arena_id: '2', name: 'Broken Image Card', image_url: 'https://example.com/broken.jpg' }),
        ],
      });
      mockDrafts.getCurrentPackWithRecommendation.mockResolvedValue(packData);

      const { container } = render(<CurrentPackPicker sessionID="test-session" />);

      await waitFor(() => {
        const img = container.querySelector('[data-testid="pack-card-2"] img') as HTMLImageElement | null;
        expect(img).toBeTruthy();
        // Simulate load failure
        img!.dispatchEvent(new Event('error', { bubbles: true }));
        expect(img!.src).toMatch(/\/back\.png$/);
      });
    });

    it('onError fallback does NOT point at the dead Scryfall CDN path', async () => {
      const packData = createMockPackResponse({
        cards: [
          createMockPackCard({ arena_id: '3', name: 'Any Card', image_url: 'https://example.com/card.jpg' }),
        ],
      });
      mockDrafts.getCurrentPackWithRecommendation.mockResolvedValue(packData);

      const { container } = render(<CurrentPackPicker sessionID="test-session" />);

      await waitFor(() => {
        const img = container.querySelector('[data-testid="pack-card-3"] img') as HTMLImageElement | null;
        expect(img).toBeTruthy();
        img!.dispatchEvent(new Event('error', { bubbles: true }));
        expect(img!.src).not.toContain('backs.scryfall.io');
      });
    });
  });

  // ── data-testid coverage (AC2, #624) ────────────────────────────────────
  // Tim's E2E needs stable testid selectors on the key recommendation-surface
  // elements instead of fragile CSS-class queries.

  describe('data-testid attributes (AC2 — #624)', () => {
    it('recommendation banner has data-testid="recommended-banner"', async () => {
      const packData = createMockPackResponse();
      mockDrafts.getCurrentPackWithRecommendation.mockResolvedValue(packData);

      const { container } = render(<CurrentPackPicker sessionID="test-session" />);

      await waitFor(() => {
        const banner = container.querySelector('[data-testid="recommended-banner"]');
        expect(banner).toBeInTheDocument();
      });
    });

    it('Best Pick indicator has data-testid="best-pick-indicator"', async () => {
      const packData = createMockPackResponse();
      mockDrafts.getCurrentPackWithRecommendation.mockResolvedValue(packData);

      const { container } = render(<CurrentPackPicker sessionID="test-session" />);

      await waitFor(() => {
        const indicator = container.querySelector('[data-testid="best-pick-indicator"]');
        expect(indicator).toBeInTheDocument();
      });
    });

    it('pack cards grid has data-testid="pack-cards-grid"', async () => {
      const packData = createMockPackResponse();
      mockDrafts.getCurrentPackWithRecommendation.mockResolvedValue(packData);

      const { container } = render(<CurrentPackPicker sessionID="test-session" />);

      await waitFor(() => {
        const grid = container.querySelector('[data-testid="pack-cards-grid"]');
        expect(grid).toBeInTheDocument();
      });
    });

    it('recommendation reasoning has data-testid="rec-reasoning"', async () => {
      const packData = createMockPackResponse();
      mockDrafts.getCurrentPackWithRecommendation.mockResolvedValue(packData);

      const { container } = render(<CurrentPackPicker sessionID="test-session" />);

      await waitFor(() => {
        const reasoning = container.querySelector('[data-testid="rec-reasoning"]');
        expect(reasoning).toBeInTheDocument();
      });
    });

    it('each pack card has data-testid="pack-card-{arenaId}"', async () => {
      const packData = createMockPackResponse({
        cards: [
          createMockPackCard({ arena_id: '100', name: 'Card A' }),
          createMockPackCard({ arena_id: '200', name: 'Card B' }),
        ],
      });
      mockDrafts.getCurrentPackWithRecommendation.mockResolvedValue(packData);

      const { container } = render(<CurrentPackPicker sessionID="test-session" />);

      await waitFor(() => {
        expect(container.querySelector('[data-testid="pack-card-100"]')).toBeInTheDocument();
        expect(container.querySelector('[data-testid="pack-card-200"]')).toBeInTheDocument();
      });
    });
  });

  // ── Recommended banner ADR-047 §2 disclosures (#1401) ──────────────────
  // AC1: inline "Limited data — early format" when low_confidence=true.
  // AC2: inline "Community consensus / No Arena data yet" when gihwr is
  //      null/undefined (no Arena sample at all).
  // Ray's required correction: gihwr==null (no data) MUST NOT conflate with
  // gihwr===0 (real 0% GIHWR — a legitimate, terrible card). The
  // disambiguation test is mandatory.

  describe('Recommended banner disclosure — ADR-047 §2 (#1401)', () => {
    // AC1 — low_confidence disclosure
    it('banner shows "Limited data — early format" when recommended_card.low_confidence is true', async () => {
      const packData = createMockPackResponse({
        recommended_card: createMockPackCard({ arena_id: '1', name: 'Lightning Bolt', low_confidence: true }),
      });
      mockDrafts.getCurrentPackWithRecommendation.mockResolvedValue(packData);

      const { container } = render(<CurrentPackPicker sessionID="test-session" />);

      await waitFor(() => {
        const el = container.querySelector('[data-testid="rec-low-confidence"]');
        expect(el).toBeInTheDocument();
        expect(el).toHaveTextContent('Limited data — early format');
      });
    });

    it('banner does NOT show low_confidence disclosure when low_confidence is false', async () => {
      const packData = createMockPackResponse({
        recommended_card: createMockPackCard({ arena_id: '1', name: 'Lightning Bolt', low_confidence: false }),
      });
      mockDrafts.getCurrentPackWithRecommendation.mockResolvedValue(packData);

      const { container } = render(<CurrentPackPicker sessionID="test-session" />);

      await waitFor(() => {
        // Lightning Bolt appears in both the banner and the card grid — use getAllByText.
        expect(screen.getAllByText('Lightning Bolt').length).toBeGreaterThanOrEqual(1);
      });
      expect(container.querySelector('[data-testid="rec-low-confidence"]')).not.toBeInTheDocument();
    });

    it('banner does NOT show low_confidence disclosure when low_confidence is undefined', async () => {
      const packData = createMockPackResponse({
        recommended_card: createMockPackCard({ arena_id: '1', name: 'Lightning Bolt' }),
      });
      (packData.recommended_card as unknown as Record<string, unknown>)['low_confidence'] = undefined;
      mockDrafts.getCurrentPackWithRecommendation.mockResolvedValue(packData);

      const { container } = render(<CurrentPackPicker sessionID="test-session" />);

      await waitFor(() => {
        // Lightning Bolt appears in both the banner and the card grid — use getAllByText.
        expect(screen.getAllByText('Lightning Bolt').length).toBeGreaterThanOrEqual(1);
      });
      expect(container.querySelector('[data-testid="rec-low-confidence"]')).not.toBeInTheDocument();
    });

    // AC2 — no Arena data disclosure
    it('banner shows "Community consensus / No Arena data yet" when gihwr is null (no data)', async () => {
      const packData = createMockPackResponse({
        recommended_card: createMockPackCard({ arena_id: '1', name: 'New Set Card', low_confidence: false }),
      });
      // Null gihwr = the BFF/daemon has no GIH sample at all for this card.
      (packData.recommended_card as unknown as Record<string, unknown>)['gihwr'] = null;
      mockDrafts.getCurrentPackWithRecommendation.mockResolvedValue(packData);

      const { container } = render(<CurrentPackPicker sessionID="test-session" />);

      await waitFor(() => {
        const el = container.querySelector('[data-testid="rec-no-arena-data"]');
        expect(el).toBeInTheDocument();
        expect(el).toHaveTextContent('Community consensus / No Arena data yet');
      });
    });

    it('banner shows "Community consensus / No Arena data yet" when gihwr is undefined (no data)', async () => {
      const packData = createMockPackResponse({
        recommended_card: createMockPackCard({ arena_id: '1', name: 'New Set Card', low_confidence: false }),
      });
      (packData.recommended_card as unknown as Record<string, unknown>)['gihwr'] = undefined;
      mockDrafts.getCurrentPackWithRecommendation.mockResolvedValue(packData);

      const { container } = render(<CurrentPackPicker sessionID="test-session" />);

      await waitFor(() => {
        const el = container.querySelector('[data-testid="rec-no-arena-data"]');
        expect(el).toBeInTheDocument();
      });
    });

    // Ray's required correction: legit-0% GIHWR (real data, terrible card)
    // MUST NOT trigger the no-Arena-data label.
    it('banner does NOT show no-Arena-data label when gihwr is exactly 0 (legit 0% win rate)', async () => {
      const packData = createMockPackResponse({
        recommended_card: createMockPackCard({ arena_id: '1', name: 'Terrible Card', gihwr: 0, low_confidence: false }),
      });
      mockDrafts.getCurrentPackWithRecommendation.mockResolvedValue(packData);

      const { container } = render(<CurrentPackPicker sessionID="test-session" />);

      await waitFor(() => {
        expect(screen.getByText('Terrible Card')).toBeInTheDocument();
      });
      // gihwr===0 is a legitimate data point — do not show the no-data label.
      expect(container.querySelector('[data-testid="rec-no-arena-data"]')).not.toBeInTheDocument();
    });

    it('banner does NOT show no-Arena-data label when gihwr is a normal positive value', async () => {
      const packData = createMockPackResponse({
        recommended_card: createMockPackCard({ arena_id: '1', name: 'Good Card', gihwr: 0.631 }),
      });
      mockDrafts.getCurrentPackWithRecommendation.mockResolvedValue(packData);

      const { container } = render(<CurrentPackPicker sessionID="test-session" />);

      await waitFor(() => {
        expect(screen.getByText('Good Card')).toBeInTheDocument();
      });
      expect(container.querySelector('[data-testid="rec-no-arena-data"]')).not.toBeInTheDocument();
    });

    // Both disclosures can appear simultaneously on a new-set card with < 500
    // game sample AND no GIHWR data at all.
    it('banner shows both disclosures simultaneously when low_confidence AND gihwr is null', async () => {
      const packData = createMockPackResponse({
        recommended_card: createMockPackCard({ arena_id: '1', name: 'Mystery Card', low_confidence: true }),
      });
      (packData.recommended_card as unknown as Record<string, unknown>)['gihwr'] = null;
      mockDrafts.getCurrentPackWithRecommendation.mockResolvedValue(packData);

      const { container } = render(<CurrentPackPicker sessionID="test-session" />);

      await waitFor(() => {
        expect(container.querySelector('[data-testid="rec-low-confidence"]')).toBeInTheDocument();
        expect(container.querySelector('[data-testid="rec-no-arena-data"]')).toBeInTheDocument();
      });
    });

    // CSS token compliance — new disclosure spans must use design tokens.
    it('CSS — rec-low-confidence uses --warning token for border and color', () => {
      const css = readFileSync(CSS_PATH, 'utf8');
      expect(css).toContain('.rec-low-confidence');
      expect(css).toContain('var(--warning)');
    });

    it('CSS — rec-no-arena-data uses --fg-muted and --border tokens', () => {
      const css = readFileSync(CSS_PATH, 'utf8');
      expect(css).toContain('.rec-no-arena-data');
      expect(css).toContain('var(--fg-muted)');
      expect(css).toContain('var(--border)');
    });
  });
  // ─────────────────────────────────────────────────────────────────────────
});
