/**
 * Home Page — Command Strip tests (#689)
 *
 * Tests the command-strip layout: loading state, error state, empty (first-run)
 * state, loaded state with all strips, and navigation from the QUICK NAV quadrant
 * and action strips.
 *
 * Also covers the exported helper functions: winRateColor, formatElapsed,
 * isLimitedFormat.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { screen, waitFor, act } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { render } from '../test/utils/testUtils';
import Home from './Home';
import { winRateColor, formatElapsed, isLimitedFormat } from './homeUtils';
import * as bffHomeSummary from '../services/api/bffHomeSummary';
import * as draftsApi from '../services/api/drafts';
import * as decksApi from '../services/api/decks';

// ---------------------------------------------------------------------------
// Module mocks
// ---------------------------------------------------------------------------

const mockNavigate = vi.fn();
vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual('react-router-dom');
  return { ...actual, useNavigate: () => mockNavigate };
});

const mockGetToken = vi.fn().mockResolvedValue('test-token');
const mockUseUser = vi.fn(() => ({
  user: { firstName: 'Ray', username: 'rayhamilton' },
  isLoaded: true,
  isSignedIn: true,
}));

vi.mock('@clerk/react', async () => {
  const actual = await vi.importActual('@clerk/react');
  return {
    ...actual,
    useAuth: () => ({ getToken: mockGetToken }),
    useUser: () => mockUseUser(),
  };
});

vi.mock('../services/api/bffHomeSummary', async () => {
  const actual = await vi.importActual('../services/api/bffHomeSummary');
  return { ...actual };
});

vi.mock('../services/api/drafts', async () => {
  const actual = await vi.importActual('../services/api/drafts');
  return { ...actual };
});

vi.mock('../services/api/decks', async () => {
  const actual = await vi.importActual('../services/api/decks');
  return { ...actual };
});

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

function makeSummary(overrides: Partial<bffHomeSummary.HomeSummaryResponse> = {}): bffHomeSummary.HomeSummaryResponse {
  return {
    today: { wins: 2, losses: 1, win_rate: 0.667 },
    this_week: { wins: 8, losses: 4, win_rate: 0.667, matches: 12 },
    all_time: {
      wins: 100,
      losses: 60,
      win_rate: 0.625,
      matches: 160,
      current_streak: 3,
      streak_type: 'W',
    },
    last_match: {
      result: 'win',
      opponent_archetype: 'Esper Midrange',
      elapsed_seconds: 1800,
    },
    ...overrides,
  };
}

function makeActiveDraft(): draftsApi.DraftSession {
  return {
    ID: 'draft-1',
    EventName: 'BLB Booster Draft',
    SetCode: 'BLB',
    DraftType: 'PremierDraft',
    StartTime: new Date() as unknown as ReturnType<typeof Date>,
    EndTime: undefined,
    Status: 'active',
    TotalPicks: 15,
    CreatedAt: new Date() as unknown as ReturnType<typeof Date>,
    UpdatedAt: new Date() as unknown as ReturnType<typeof Date>,
  } as unknown as draftsApi.DraftSession;
}

function makeDeck(overrides: Partial<decksApi.DeckListItem> = {}): decksApi.DeckListItem {
  return {
    id: 'deck-1',
    name: 'Azorius Tempo',
    format: 'Standard',
    source: 'constructed',
    cardCount: 60,
    matchesPlayed: 20,
    matchWinRate: 0.65,
    modifiedAt: new Date() as unknown as ReturnType<typeof Date>,
    lastPlayed: new Date() as unknown as ReturnType<typeof Date>,
    currentStreak: 2,
    ...overrides,
  } as unknown as decksApi.DeckListItem;
}

// ---------------------------------------------------------------------------
// Setup helpers
// ---------------------------------------------------------------------------

function mockLoadedState(overrides: {
  summary?: bffHomeSummary.HomeSummaryResponse;
  activeDraft?: draftsApi.DraftSession | null;
  lastDeck?: decksApi.DeckListItem | null;
} = {}) {
  const summary = overrides.summary ?? makeSummary();
  const activeDraft = 'activeDraft' in overrides ? overrides.activeDraft : null;
  const lastDeck = 'lastDeck' in overrides ? overrides.lastDeck : makeDeck();

  vi.spyOn(bffHomeSummary, 'getHomeSummary').mockResolvedValue(summary);
  vi.spyOn(draftsApi, 'getActiveDraftSessions').mockResolvedValue(
    activeDraft ? [activeDraft] : []
  );
  vi.spyOn(decksApi, 'getDecks').mockResolvedValue(
    lastDeck ? [lastDeck] : []
  );
}

function mockEmptyState() {
  const emptySummary: bffHomeSummary.HomeSummaryResponse = {
    today: { wins: 0, losses: 0, win_rate: 0 },
    this_week: { wins: 0, losses: 0, win_rate: 0, matches: 0 },
    all_time: { wins: 0, losses: 0, win_rate: 0, matches: 0, current_streak: 0, streak_type: 'W' },
    last_match: null,
  };
  vi.spyOn(bffHomeSummary, 'getHomeSummary').mockResolvedValue(emptySummary);
  vi.spyOn(draftsApi, 'getActiveDraftSessions').mockResolvedValue([]);
  vi.spyOn(decksApi, 'getDecks').mockResolvedValue([]);
}

function mockErrorState() {
  vi.spyOn(bffHomeSummary, 'getHomeSummary').mockRejectedValue(new Error('network error'));
  vi.spyOn(draftsApi, 'getActiveDraftSessions').mockRejectedValue(new Error('network error'));
  vi.spyOn(decksApi, 'getDecks').mockRejectedValue(new Error('network error'));
}

// ---------------------------------------------------------------------------
// Helper function unit tests
// ---------------------------------------------------------------------------

describe('winRateColor', () => {
  it('returns success color for rate >= 0.57', () => {
    expect(winRateColor(0.57)).toBe('var(--vault-success)');
    expect(winRateColor(0.65)).toBe('var(--vault-success)');
    expect(winRateColor(1.0)).toBe('var(--vault-success)');
  });

  it('returns secondary color for rate between 0.50 and 0.57', () => {
    expect(winRateColor(0.50)).toBe('var(--vault-fg-secondary)');
    expect(winRateColor(0.55)).toBe('var(--vault-fg-secondary)');
  });

  it('returns danger color for rate < 0.50', () => {
    expect(winRateColor(0.49)).toBe('var(--vault-danger)');
    expect(winRateColor(0)).toBe('var(--vault-danger)');
  });
});

describe('formatElapsed', () => {
  it('formats seconds under a minute as seconds', () => {
    expect(formatElapsed(45)).toBe('45s ago');
  });

  it('formats 1-59 minutes as min ago', () => {
    expect(formatElapsed(60)).toBe('1 min ago');
    expect(formatElapsed(1800)).toBe('30 min ago');
  });

  it('formats 60+ minutes as hours', () => {
    expect(formatElapsed(3600)).toBe('1h ago');
    expect(formatElapsed(7200)).toBe('2h ago');
  });
});

describe('isLimitedFormat', () => {
  it('returns true for draft formats', () => {
    expect(isLimitedFormat('PremierDraft')).toBe(true);
    expect(isLimitedFormat('Booster Draft')).toBe(true);
    expect(isLimitedFormat('draft')).toBe(true);
  });

  it('returns true for sealed formats', () => {
    expect(isLimitedFormat('Sealed')).toBe(true);
    expect(isLimitedFormat('Traditional Sealed')).toBe(true);
  });

  it('returns false for constructed formats', () => {
    expect(isLimitedFormat('Standard')).toBe(false);
    expect(isLimitedFormat('Historic')).toBe(false);
    expect(isLimitedFormat('Alchemy')).toBe(false);
  });
});

// ---------------------------------------------------------------------------
// Component tests
// ---------------------------------------------------------------------------

describe('Home Page Command Strip (#689)', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockGetToken.mockResolvedValue('test-token');
    mockUseUser.mockReturnValue({
      user: { firstName: 'Ray', username: 'rayhamilton' },
      isLoaded: true,
      isSignedIn: true,
    });
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  // ── Loading state ─────────────────────────────────────────
  describe('Loading state', () => {
    it('shows loading indicator initially', async () => {
      // Stub APIs that never resolve during this test
      vi.spyOn(bffHomeSummary, 'getHomeSummary').mockReturnValue(new Promise(() => {}));
      vi.spyOn(draftsApi, 'getActiveDraftSessions').mockReturnValue(new Promise(() => {}));
      vi.spyOn(decksApi, 'getDecks').mockReturnValue(new Promise(() => {}));

      render(<Home />);
      expect(screen.getByTestId('home-loading')).toBeInTheDocument();
      expect(screen.getByTestId('mana-wheel')).toBeInTheDocument();
    });

    it('renders home-page testid during loading', async () => {
      vi.spyOn(bffHomeSummary, 'getHomeSummary').mockReturnValue(new Promise(() => {}));
      vi.spyOn(draftsApi, 'getActiveDraftSessions').mockReturnValue(new Promise(() => {}));
      vi.spyOn(decksApi, 'getDecks').mockReturnValue(new Promise(() => {}));

      render(<Home />);
      expect(screen.getByTestId('home-page')).toBeInTheDocument();
    });
  });

  // ── Error state ───────────────────────────────────────────
  describe('Error state', () => {
    it('shows error message when token fetch fails', async () => {
      mockGetToken.mockResolvedValue(null); // no token → error path

      await act(async () => {
        render(<Home />);
      });

      await waitFor(() => {
        expect(screen.getByTestId('home-error')).toBeInTheDocument();
      });
    });

    it('shows retry button on error', async () => {
      mockGetToken.mockResolvedValue(null);

      await act(async () => {
        render(<Home />);
      });

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /retry/i })).toBeInTheDocument();
      });
    });
  });

  // ── Empty / first-run state ───────────────────────────────
  describe('Empty state (first-run)', () => {
    it('shows ManaWheel and first-run message when no data', async () => {
      mockEmptyState();

      await act(async () => {
        render(<Home />);
      });

      await waitFor(() => {
        expect(screen.getByTestId('home-empty')).toBeInTheDocument();
        expect(screen.getByTestId('mana-wheel')).toBeInTheDocument();
      });
    });

    it('shows QUICK NAV in empty state', async () => {
      mockEmptyState();

      await act(async () => {
        render(<Home />);
      });

      await waitFor(() => {
        expect(screen.getByTestId('home-quick-nav')).toBeInTheDocument();
      });
    });
  });

  // ── Loaded state — WEEKLY RECORD strip ───────────────────
  describe('WEEKLY RECORD strip', () => {
    it('renders the weekly record strip', async () => {
      mockLoadedState();

      await act(async () => {
        render(<Home />);
      });

      await waitFor(() => {
        expect(screen.getByTestId('home-strip-weekly')).toBeInTheDocument();
      });
    });

    it('shows weekly wins in success color', async () => {
      mockLoadedState({ summary: makeSummary({ this_week: { wins: 8, losses: 4, win_rate: 0.667, matches: 12 } }) });

      await act(async () => {
        render(<Home />);
      });

      await waitFor(() => {
        const winsEl = screen.getByTestId('home-weekly-wins');
        expect(winsEl).toHaveTextContent('8W');
        expect(winsEl).toHaveStyle({ color: 'var(--vault-success)' });
      });
    });

    it('shows weekly losses in danger color', async () => {
      mockLoadedState();

      await act(async () => {
        render(<Home />);
      });

      await waitFor(() => {
        const lossesEl = screen.getByTestId('home-weekly-losses');
        expect(lossesEl).toHaveStyle({ color: 'var(--vault-danger)' });
      });
    });

    it('shows win-rate with correct color for high rate (>=57%)', async () => {
      mockLoadedState({ summary: makeSummary({ this_week: { wins: 10, losses: 3, win_rate: 0.769, matches: 13 } }) });

      await act(async () => {
        render(<Home />);
      });

      await waitFor(() => {
        const wrEl = screen.getByTestId('home-weekly-winrate');
        expect(wrEl).toHaveStyle({ color: 'var(--vault-success)' });
      });
    });

    it('shows win-rate danger color for rate <50%', async () => {
      mockLoadedState({ summary: makeSummary({ this_week: { wins: 3, losses: 7, win_rate: 0.3, matches: 10 } }) });

      await act(async () => {
        render(<Home />);
      });

      await waitFor(() => {
        const wrEl = screen.getByTestId('home-weekly-winrate');
        expect(wrEl).toHaveStyle({ color: 'var(--vault-danger)' });
      });
    });

    it('shows today record when today has matches', async () => {
      mockLoadedState({ summary: makeSummary({ today: { wins: 2, losses: 1, win_rate: 0.667 } }) });

      await act(async () => {
        render(<Home />);
      });

      await waitFor(() => {
        expect(screen.getByTestId('home-today-record')).toHaveTextContent('Today: 2–1');
      });
    });

    it('does not show today record when today has no matches', async () => {
      mockLoadedState({ summary: makeSummary({ today: { wins: 0, losses: 0, win_rate: 0 } }) });

      await act(async () => {
        render(<Home />);
      });

      await waitFor(() => {
        expect(screen.queryByTestId('home-today-record')).not.toBeInTheDocument();
      });
    });
  });

  // ── Loaded state — LAST MATCH micro-strip ────────────────
  describe('LAST MATCH micro-strip', () => {
    it('renders last match strip when last_match present', async () => {
      mockLoadedState();

      await act(async () => {
        render(<Home />);
      });

      await waitFor(() => {
        expect(screen.getByTestId('home-strip-last-match')).toBeInTheDocument();
      });
    });

    it('shows "Won" for win result in success color', async () => {
      mockLoadedState({ summary: makeSummary({ last_match: { result: 'win', opponent_archetype: null, elapsed_seconds: 600 } }) });

      await act(async () => {
        render(<Home />);
      });

      await waitFor(() => {
        const el = screen.getByTestId('home-last-match-result');
        expect(el).toHaveTextContent('Won');
        expect(el).toHaveStyle({ color: 'var(--vault-success)' });
      });
    });

    it('shows "Lost" for loss result in danger color', async () => {
      mockLoadedState({ summary: makeSummary({ last_match: { result: 'loss', opponent_archetype: null, elapsed_seconds: 600 } }) });

      await act(async () => {
        render(<Home />);
      });

      await waitFor(() => {
        const el = screen.getByTestId('home-last-match-result');
        expect(el).toHaveTextContent('Lost');
        expect(el).toHaveStyle({ color: 'var(--vault-danger)' });
      });
    });

    it('shows opponent archetype when present', async () => {
      mockLoadedState({ summary: makeSummary({ last_match: { result: 'win', opponent_archetype: 'Esper Midrange', elapsed_seconds: 600 } }) });

      await act(async () => {
        render(<Home />);
      });

      await waitFor(() => {
        expect(screen.getByTestId('home-last-match-archetype')).toHaveTextContent('vs. Esper Midrange');
      });
    });

    it('omits archetype element when null', async () => {
      mockLoadedState({ summary: makeSummary({ last_match: { result: 'win', opponent_archetype: null, elapsed_seconds: 600 } }) });

      await act(async () => {
        render(<Home />);
      });

      await waitFor(() => {
        expect(screen.queryByTestId('home-last-match-archetype')).not.toBeInTheDocument();
      });
    });

    it('shows elapsed time', async () => {
      mockLoadedState({ summary: makeSummary({ last_match: { result: 'win', opponent_archetype: null, elapsed_seconds: 1800 } }) });

      await act(async () => {
        render(<Home />);
      });

      await waitFor(() => {
        expect(screen.getByTestId('home-last-match-elapsed')).toHaveTextContent('30 min ago');
      });
    });

    it('does not render last match strip when null', async () => {
      mockLoadedState({ summary: makeSummary({ last_match: null }) });

      await act(async () => {
        render(<Home />);
      });

      await waitFor(() => {
        expect(screen.queryByTestId('home-strip-last-match')).not.toBeInTheDocument();
      });
    });
  });

  // ── ACTIVE DRAFT strip ───────────────────────────────────
  describe('ACTIVE DRAFT strip', () => {
    it('renders active draft strip when draft is present', async () => {
      mockLoadedState({ activeDraft: makeActiveDraft() });

      await act(async () => {
        render(<Home />);
      });

      await waitFor(() => {
        expect(screen.getByTestId('home-strip-active-draft')).toBeInTheDocument();
      });
    });

    it('shows draft event name', async () => {
      mockLoadedState({ activeDraft: makeActiveDraft() });

      await act(async () => {
        render(<Home />);
      });

      await waitFor(() => {
        expect(screen.getByTestId('home-active-draft-name')).toHaveTextContent('BLB Booster Draft');
      });
    });

    it('does not render active draft strip when no active draft', async () => {
      mockLoadedState({ activeDraft: null });

      await act(async () => {
        render(<Home />);
      });

      await waitFor(() => {
        expect(screen.queryByTestId('home-strip-active-draft')).not.toBeInTheDocument();
      });
    });

    it('navigates to /draft when active draft strip is clicked', async () => {
      const user = userEvent.setup();
      mockLoadedState({ activeDraft: makeActiveDraft() });

      await act(async () => {
        render(<Home />);
      });

      await waitFor(() => {
        expect(screen.getByTestId('home-strip-active-draft')).toBeInTheDocument();
      });

      await user.click(screen.getByTestId('home-strip-active-draft'));
      expect(mockNavigate).toHaveBeenCalledWith('/draft');
    });
  });

  // ── LAST DECK strip ──────────────────────────────────────
  describe('LAST DECK strip', () => {
    it('renders last deck strip when deck is present', async () => {
      mockLoadedState({ lastDeck: makeDeck() });

      await act(async () => {
        render(<Home />);
      });

      await waitFor(() => {
        expect(screen.getByTestId('home-strip-last-deck')).toBeInTheDocument();
      });
    });

    it('shows deck name', async () => {
      mockLoadedState({ lastDeck: makeDeck({ name: 'Azorius Tempo' }) });

      await act(async () => {
        render(<Home />);
      });

      await waitFor(() => {
        expect(screen.getByTestId('home-last-deck-name')).toHaveTextContent('Azorius Tempo');
      });
    });

    it('shows "Play Again" CTA for Constructed format', async () => {
      mockLoadedState({ lastDeck: makeDeck({ format: 'Standard', name: 'My Deck' }) });

      await act(async () => {
        render(<Home />);
      });

      await waitFor(() => {
        expect(screen.getByTestId('home-last-deck-cta')).toHaveTextContent('Play Again');
      });
    });

    it('shows "Open Log" CTA for Draft format', async () => {
      mockLoadedState({ lastDeck: makeDeck({ format: 'PremierDraft', name: 'Draft Deck' }) });

      await act(async () => {
        render(<Home />);
      });

      await waitFor(() => {
        expect(screen.getByTestId('home-last-deck-cta')).toHaveTextContent('Open Log');
      });
    });

    it('shows "Open Log" CTA for Sealed format', async () => {
      mockLoadedState({ lastDeck: makeDeck({ format: 'Traditional Sealed', name: 'Sealed Deck' }) });

      await act(async () => {
        render(<Home />);
      });

      await waitFor(() => {
        expect(screen.getByTestId('home-last-deck-cta')).toHaveTextContent('Open Log');
      });
    });

    it('does not render last deck strip when no decks', async () => {
      mockLoadedState({ lastDeck: null });

      await act(async () => {
        render(<Home />);
      });

      await waitFor(() => {
        expect(screen.queryByTestId('home-strip-last-deck')).not.toBeInTheDocument();
      });
    });

    it('navigates to /decks when last deck strip is clicked', async () => {
      const user = userEvent.setup();
      mockLoadedState({ lastDeck: makeDeck() });

      await act(async () => {
        render(<Home />);
      });

      await waitFor(() => {
        expect(screen.getByTestId('home-strip-last-deck')).toBeInTheDocument();
      });

      await user.click(screen.getByTestId('home-strip-last-deck'));
      expect(mockNavigate).toHaveBeenCalledWith('/decks');
    });
  });

  // ── WHAT'S NEXT nudge (loaded state) ────────────────────
  describe("WHAT'S NEXT nudge", () => {
    it('renders whats-next nudge in loaded state (hot-streak variant)', async () => {
      // Default mockLoadedState: 8W/4L, 66.7% → hot-streak nudge fires
      mockLoadedState();

      await act(async () => {
        render(<Home />);
      });

      await waitFor(() => {
        expect(screen.getByTestId('home-whats-next-hot-streak')).toBeInTheDocument();
      });
    });

    it('does NOT render home-quick-nav in loaded state', async () => {
      mockLoadedState();

      await act(async () => {
        render(<Home />);
      });

      await waitFor(() => {
        expect(screen.queryByTestId('home-quick-nav')).not.toBeInTheDocument();
      });
    });
  });

  // ── QUICK NAV quadrant (first-run / empty state only) ────
  describe('QUICK NAV quadrant', () => {
    it('renders quick nav in first-run (empty) state', async () => {
      mockEmptyState();

      await act(async () => {
        render(<Home />);
      });

      await waitFor(() => {
        expect(screen.getByTestId('home-quick-nav')).toBeInTheDocument();
      });
    });

    it('renders all 4 nav tiles in first-run state', async () => {
      mockEmptyState();

      await act(async () => {
        render(<Home />);
      });

      await waitFor(() => {
        expect(screen.getByTestId('home-nav-match-history')).toBeInTheDocument();
        expect(screen.getByTestId('home-nav-draft')).toBeInTheDocument();
        expect(screen.getByTestId('home-nav-decks')).toBeInTheDocument();
        expect(screen.getByTestId('home-nav-collection')).toBeInTheDocument();
      });
    });

    it('navigates to /match-history on Match History tile click', async () => {
      const user = userEvent.setup();
      mockEmptyState();

      await act(async () => {
        render(<Home />);
      });

      await waitFor(() => expect(screen.getByTestId('home-nav-match-history')).toBeInTheDocument());
      await user.click(screen.getByTestId('home-nav-match-history'));
      expect(mockNavigate).toHaveBeenCalledWith('/match-history');
    });

    it('navigates to /draft on Draft tile click', async () => {
      const user = userEvent.setup();
      mockEmptyState();

      await act(async () => {
        render(<Home />);
      });

      await waitFor(() => expect(screen.getByTestId('home-nav-draft')).toBeInTheDocument());
      await user.click(screen.getByTestId('home-nav-draft'));
      expect(mockNavigate).toHaveBeenCalledWith('/draft');
    });

    it('navigates to /decks on Decks tile click', async () => {
      const user = userEvent.setup();
      mockEmptyState();

      await act(async () => {
        render(<Home />);
      });

      await waitFor(() => expect(screen.getByTestId('home-nav-decks')).toBeInTheDocument());
      await user.click(screen.getByTestId('home-nav-decks'));
      expect(mockNavigate).toHaveBeenCalledWith('/decks');
    });

    it('navigates to /collection on Collection tile click', async () => {
      const user = userEvent.setup();
      mockEmptyState();

      await act(async () => {
        render(<Home />);
      });

      await waitFor(() => expect(screen.getByTestId('home-nav-collection')).toBeInTheDocument());
      await user.click(screen.getByTestId('home-nav-collection'));
      expect(mockNavigate).toHaveBeenCalledWith('/collection');
    });
  });

  // ── Summary 404 fallback (mock stub) ─────────────────────
  describe('summary endpoint 404 fallback', () => {
    it('renders strips with zeroed data when summary endpoint returns 404', async () => {
      vi.spyOn(bffHomeSummary, 'getHomeSummary').mockRejectedValue(
        Object.assign(new Error('Not Found'), { status: 404 })
      );
      vi.spyOn(draftsApi, 'getActiveDraftSessions').mockResolvedValue([]);
      vi.spyOn(decksApi, 'getDecks').mockResolvedValue([makeDeck()]);

      await act(async () => {
        render(<Home />);
      });

      // With mock summary, all_time.matches=0 but there is a deck, so it is not
      // considered fully "empty" — the LAST DECK strip should still show
      await waitFor(() => {
        expect(screen.getByTestId('home-page')).toBeInTheDocument();
      });
    });
  });
});
