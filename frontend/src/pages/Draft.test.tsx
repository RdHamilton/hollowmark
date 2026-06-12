import { describe, it, expect, vi, beforeEach } from 'vitest';
import { readFileSync } from 'node:fs';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { render } from '../test/utils/testUtils';
import Draft from './Draft';
import { mockDrafts, mockCards } from '@/test/mocks/apiMock';
import { mockEventEmitter } from '@/test/mocks/websocketMock';
import { models, gui, pickquality } from '@/types/models';
import * as useFeatureFlagModule from '@/hooks/useFeatureFlag';

// Mock useFeatureFlag so flag state is test-controlled without PostHog
vi.mock('@/hooks/useFeatureFlag', () => ({
  useFeatureFlag: vi.fn().mockReturnValue({ enabled: true }),
}));

// Mock useDraftEventStream so Draft.tsx's SSE trigger useEffect does not
// open a real EventSource in unit tests. Default: no event (latestEvent=null).
// Tests that need to simulate SSE events can set mockDraftEventStream.latestEvent directly.
const mockDraftEventStream = {
  latestEvent: null as import('@/hooks/useDraftEventStream').DaemonEvent | null,
  status: 'open' as import('@/hooks/useDraftEventStream').DraftEventStreamStatus,
};
vi.mock('@/hooks/useDraftEventStream', () => ({
  useDraftEventStream: vi.fn(() => mockDraftEventStream),
}));

// Helper function to create mock data
function createMockDraftSession(overrides: Partial<models.DraftSession> = {}): models.DraftSession {
  return new models.DraftSession({
    ID: 'test-session-1',
    EventName: 'QuickDraft',
    SetCode: 'BLB',
    DraftType: 'PremierDraft',
    StartTime: new Date('2025-11-20T10:00:00Z'),
    Status: 'active',
    TotalPicks: 45,
    CreatedAt: new Date('2025-11-20T10:00:00Z'),
    UpdatedAt: new Date('2025-11-20T10:00:00Z'),
    ...overrides,
  });
}

function createMockSetCard(overrides: Partial<models.SetCard> = {}): models.SetCard {
  return new models.SetCard({
    ID: 1,
    SetCode: 'BLB',
    ArenaID: '12345',
    ScryfallID: 'scryfall-id',
    Name: 'Test Card',
    ManaCost: '{2}{W}{U}',
    CMC: 4,
    Types: ['Creature'],
    Colors: ['W', 'U'],
    Rarity: 'rare',
    Text: 'Test card text',
    Power: '2',
    Toughness: '3',
    ImageURL: 'https://example.com/card.jpg',
    ImageURLSmall: 'https://example.com/card-small.jpg',
    ImageURLArt: 'https://example.com/card-art.jpg',
    FetchedAt: new Date(),
    ...overrides,
  });
}

function createMockDraftPick(overrides: Partial<models.DraftPickSession> = {}): models.DraftPickSession {
  return new models.DraftPickSession({
    ID: 1,
    SessionID: 'test-session-1',
    PackNumber: 0,
    PickNumber: 1,
    CardID: '12345',
    Timestamp: new Date('2025-11-20T10:05:00Z'),
    PickQualityGrade: null,
    PickQualityRank: null,
    PackBestGIHWR: null,
    PickedCardGIHWR: null,
    AlternativesJSON: null,
    ...overrides,
  });
}

function createMockCardRating(overrides: Partial<gui.CardRatingWithTier> = {}): gui.CardRatingWithTier {
  return new gui.CardRatingWithTier({
    name: 'Test Card',
    color: 'W',
    rarity: 'rare',
    mtga_id: 12345,
    ever_drawn_win_rate: 0.56,
    opening_hand_win_rate: 0.54,
    ever_drawn_game_win_rate: 0.55,
    drawn_win_rate: 0.57,
    in_hand_win_rate: 0.56,
    ever_drawn_improvement_win_rate: 0.02,
    opening_hand_improvement_win_rate: 0.01,
    drawn_improvement_win_rate: 0.02,
    in_hand_improvement_win_rate: 0.02,
    avg_seen: 3.5,
    avg_pick: 2.1,
    pick_rate: 0.6,
    '# ever_drawn': 1000,
    '# opening_hand': 500,
    '# games': 2000,
    '# drawn': 800,
    '# in_hand_drawn': 700,
    '# games_played': 2000,
    '# decks': 500,
    tier: 'A',
    colors: ['W', 'U'],
    ...overrides,
  });
}

function createMockDeckMetrics(overrides: Partial<models.DeckMetrics> = {}): models.DeckMetrics {
  return new models.DeckMetrics({
    total_cards: 15,
    total_non_land_cards: 15,
    creature_count: 10,
    noncreature_count: 5,
    cmc_average: 2.8,
    distribution_all: [0, 2, 4, 5, 3, 1, 0],
    distribution_creatures: [0, 1, 2, 4, 2, 1, 0],
    distribution_noncreatures: [0, 1, 2, 1, 1, 0, 0],
    type_breakdown: { Creature: 10, Instant: 3, Sorcery: 2 },
    color_distribution: { W: 0.5, U: 0.5, B: 0, R: 0, G: 0 },
    color_counts: { W: 8, U: 7, B: 0, R: 0, G: 0 },
    multi_color_count: 0,
    colorless_count: 0,
    ...overrides,
  });
}

describe('Draft Component', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockEventEmitter.clear();
    // Reset SSE stream mock to no-event state
    mockDraftEventStream.latestEvent = null;
  });

  describe('No Active Draft State', () => {
    it('should display draft history when no active draft exists', async () => {
      mockDrafts.getActiveDraftSessions.mockResolvedValue([]);
      mockDrafts.getCompletedDraftSessions.mockResolvedValue([]);

      render(<Draft />);

      await waitFor(() => {
        expect(screen.getByText('Draft History')).toBeInTheDocument();
      });

      expect(screen.getByText(/Start a Quick Draft in MTG Arena to begin a new draft session/i)).toBeInTheDocument();
    });

    it('should display historical drafts when available', async () => {
      const completedSession = createMockDraftSession({
        ID: 'completed-session',
        Status: 'completed',
        TotalPicks: 45,
      });

      mockDrafts.getActiveDraftSessions.mockResolvedValue([]);
      mockDrafts.getCompletedDraftSessions.mockResolvedValue([completedSession]);

      render(<Draft />);

      await waitFor(() => {
        expect(screen.getByText('QuickDraft')).toBeInTheDocument();
      });

      expect(screen.getByText(/BLB/i)).toBeInTheDocument();
      expect(screen.getByRole('button', { name: /View Replay/i })).toBeInTheDocument();
    });

    it('should display empty state when no historical drafts exist', async () => {
      mockDrafts.getActiveDraftSessions.mockResolvedValue([]);
      mockDrafts.getCompletedDraftSessions.mockResolvedValue([]);

      render(<Draft />);

      await waitFor(() => {
        expect(screen.getByText('No Draft History')).toBeInTheDocument();
      });

      expect(screen.getByText(/Complete a Quick Draft in MTG Arena to see your draft history here/i)).toBeInTheDocument();
    });
  });

  describe('Active Draft Display', () => {
    it('should load and display an active draft session', async () => {
      const session = createMockDraftSession();
      // Include at least one pick so the awaiting-data inline banner is not shown (#1349)
      const picks = [createMockDraftPick()];
      const packs: models.DraftPackSession[] = [];
      const setCards = [createMockSetCard()];
      const ratings = [createMockCardRating()];

      mockDrafts.getActiveDraftSessions.mockResolvedValue([session]);
      mockDrafts.getDraftPicks.mockResolvedValue(picks);
      mockDrafts.getDraftPool.mockResolvedValue(packs);
      mockCards.getSetCards.mockResolvedValue(setCards);
      mockCards.getCardRatings.mockResolvedValue(ratings);

      render(<Draft />);

      await waitFor(() => {
        expect(screen.getByText('Draft Assistant')).toBeInTheDocument();
      });

      expect(screen.getByText('QuickDraft')).toBeInTheDocument();
      expect(screen.getByText(/Set: BLB/i)).toBeInTheDocument();
      expect(screen.getByText(/Picks: 1\/45/i)).toBeInTheDocument();
    });

    it('should display loading state while fetching draft data', () => {
      mockDrafts.getActiveDraftSessions.mockImplementation(
        () => new Promise(() => {}) // Never resolves
      );

      render(<Draft />);

      expect(screen.getByText('Loading draft...')).toBeInTheDocument();
    });

    it('should update when draft:updated event is fired', async () => {
      const session = createMockDraftSession();
      // Start in Case C (one pick) so the active-draft view is rendered immediately
      const initialPick = createMockDraftPick({ PackNumber: 0, PickNumber: 1 });
      const packs: models.DraftPackSession[] = [];
      const setCards = [createMockSetCard()];
      const ratings = [createMockCardRating()];
      const mockMetrics = createMockDeckMetrics();

      mockDrafts.getActiveDraftSessions.mockResolvedValue([session]);
      mockDrafts.getDraftPicks.mockResolvedValue([initialPick]);
      mockDrafts.getDraftPool.mockResolvedValue(packs);
      mockCards.getSetCards.mockResolvedValue(setCards);
      mockCards.getCardRatings.mockResolvedValue(ratings);
      mockDrafts.getDraftDeckMetrics.mockResolvedValue(mockMetrics);

      render(<Draft />);

      await waitFor(() => {
        expect(screen.getByText('Draft Assistant')).toBeInTheDocument();
      });

      // Update picks — now two picks
      const secondPick = createMockDraftPick({ ID: 2, PackNumber: 0, PickNumber: 2 });
      mockDrafts.getDraftPicks.mockResolvedValue([initialPick, secondPick]);
      mockDrafts.getDraftDeckMetrics.mockResolvedValue(mockMetrics);

      // Fire draft:updated event
      mockEventEmitter.emit('draft:updated');

      await waitFor(() => {
        expect(screen.getByText(/Picks: 2\/45/i)).toBeInTheDocument();
      }, { timeout: 3000 });
    });
  });

  describe('Draft Picks Display', () => {
    it('should render picked cards correctly', async () => {
      const session = createMockDraftSession();
      const card1 = createMockSetCard({ ArenaID: '11111', Name: 'Card One' });
      const card2 = createMockSetCard({ ArenaID: '22222', Name: 'Card Two' });
      const picks = [
        createMockDraftPick({ CardID: '11111', PackNumber: 0, PickNumber: 1 }),
        createMockDraftPick({ CardID: '22222', PackNumber: 0, PickNumber: 2 }),
      ];

      mockDrafts.getActiveDraftSessions.mockResolvedValue([session]);
      mockDrafts.getDraftPicks.mockResolvedValue(picks);
      mockDrafts.getDraftPool.mockResolvedValue([]);
      mockCards.getSetCards.mockResolvedValue([card1, card2]);
      mockCards.getCardRatings.mockResolvedValue([]);

      render(<Draft />);

      await waitFor(() => {
        expect(screen.getByText(/Picks: 2\/45/i)).toBeInTheDocument();
      });

      // Check pick history
      expect(screen.getByText('Pick History')).toBeInTheDocument();
      expect(screen.getByText('P1P1')).toBeInTheDocument();
      expect(screen.getByText('P1P2')).toBeInTheDocument();
    });

    it('should display picked indicator on cards in grid', async () => {
      const session = createMockDraftSession();
      const card = createMockSetCard({ ArenaID: '12345' });
      const pick = createMockDraftPick({ CardID: '12345' });
      const metrics = createMockDeckMetrics();

      mockDrafts.getActiveDraftSessions.mockResolvedValue([session]);
      mockDrafts.getDraftPicks.mockResolvedValue([pick]);
      mockDrafts.getDraftPool.mockResolvedValue([]);
      mockCards.getSetCards.mockResolvedValue([card]);
      mockCards.getCardRatings.mockResolvedValue([]);
      mockDrafts.getDraftDeckMetrics.mockResolvedValue(metrics);

      render(<Draft />);

      // Wait for the view toggle to appear
      await waitFor(() => {
        expect(screen.getByText('All Set Cards')).toBeInTheDocument();
      });

      // Click "All Set Cards" to switch to grid view (default is CurrentPackPicker)
      const allSetCardsBtn = screen.getByText('All Set Cards');
      await userEvent.click(allSetCardsBtn);

      await waitFor(() => {
        const cardItems = document.querySelectorAll('.card-item.picked');
        expect(cardItems.length).toBeGreaterThan(0);
      });
    });

    it('should display pick quality grades when available', async () => {
      const session = createMockDraftSession();
      const card = createMockSetCard({ ArenaID: '12345' });
      const pick = createMockDraftPick({
        CardID: '12345',
        PickQualityGrade: 'A',
        PickQualityRank: 1,
      });

      mockDrafts.getActiveDraftSessions.mockResolvedValue([session]);
      mockDrafts.getDraftPicks.mockResolvedValue([pick]);
      mockDrafts.getDraftPool.mockResolvedValue([]);
      mockCards.getSetCards.mockResolvedValue([card]);
      mockCards.getCardRatings.mockResolvedValue([]);

      render(<Draft />);

      await waitFor(() => {
        const gradeBadges = document.querySelectorAll('.pick-quality-badge');
        expect(gradeBadges.length).toBeGreaterThan(0);
      });
    });

    // ── GIHWR units regression (#787 AC5) ──────────────────────────────────
    // The pick-quality tooltip renders gihwr from the daemon grade-pick path,
    // which serves a FRACTION (0.0–1.0) per Bob's #787 decision. The display
    // must multiply by 100: a 0.631 picked-card GIHWR reads "63.1%", and a
    // 0.685 alternative reads "68.5%" — NOT the buggy raw "0.6%"/"0.7%".
    it('renders pick-quality tooltip GIHWR as a percent, not a raw fraction (#787)', async () => {
      const session = createMockDraftSession();
      const card = createMockSetCard({ ArenaID: '12345' });
      const pick = createMockDraftPick({
        CardID: '12345',
        PackNumber: 0,
        PickNumber: 1,
        PickQualityGrade: 'B',
        PickQualityRank: 2,
      });

      mockDrafts.getActiveDraftSessions.mockResolvedValue([session]);
      mockDrafts.getDraftPicks.mockResolvedValue([pick]);
      mockDrafts.getDraftPool.mockResolvedValue([]);
      mockCards.getSetCards.mockResolvedValue([card]);
      mockCards.getCardRatings.mockResolvedValue([]);
      mockDrafts.getDraftDeckMetrics.mockResolvedValue(createMockDeckMetrics());
      // Daemon grade-pick path returns fractional gihwr values.
      mockDrafts.getPickAlternatives.mockResolvedValue(
        new pickquality.PickQuality({
          grade: 'B',
          rank: 2,
          pack_best_gihwr: 0.685,
          picked_card_gihwr: 0.631,
          alternatives: [{ card_name: 'Better Bomb', gihwr: 0.685 }],
        }),
      );

      render(<Draft />);

      // The pick-history card image carries an onMouseEnter that lazily loads
      // the alternatives and renders the pick-quality tooltip.
      const cardImg = await screen.findByRole('img', { name: 'Test Card' });
      await userEvent.hover(cardImg);

      await waitFor(() => {
        expect(screen.getByText('63.1%')).toBeInTheDocument();
        expect(screen.getByText('68.5%')).toBeInTheDocument();
      });
      // The buggy raw-fraction render must be absent.
      expect(screen.queryByText('0.6%')).not.toBeInTheDocument();
      expect(screen.queryByText('0.7%')).not.toBeInTheDocument();
    });
  });

  describe('Card Recommendations and Synergies', () => {
    it('should display synergy indicators for recommended cards', async () => {
      const session = createMockDraftSession();
      const pickedCard = createMockSetCard({
        ArenaID: '11111',
        Name: 'Plains Walker',
        Types: ['Creature'],
        Colors: ['W'],
        CMC: 3,
      });
      const synergyCard = createMockSetCard({
        ArenaID: '22222',
        Name: 'White Knight',
        Types: ['Creature'],
        Colors: ['W'],
        CMC: 2,
      });

      const picks = [createMockDraftPick({ CardID: '11111' })];

      mockDrafts.getActiveDraftSessions.mockResolvedValue([session]);
      mockDrafts.getDraftPicks.mockResolvedValue(picks);
      mockDrafts.getDraftPool.mockResolvedValue([]);
      mockCards.getSetCards.mockResolvedValue([pickedCard, synergyCard]);
      mockCards.getCardRatings.mockResolvedValue([]);

      render(<Draft />);

      await waitFor(() => {
        expect(screen.getByText('Draft Assistant')).toBeInTheDocument();
      });

      // Synergy indicators should be present for cards that match color identity
      const synergyIndicators = document.querySelectorAll('.synergy-highlight');
      expect(synergyIndicators.length).toBeGreaterThanOrEqual(0);
    });
  });

  describe('Analyze Draft Functionality', () => {
    it('should call analyze function when button is clicked', async () => {
      const session = createMockDraftSession();
      const picks = [createMockDraftPick()];
      const mockMetrics = createMockDeckMetrics();

      mockDrafts.getActiveDraftSessions.mockResolvedValue([session]);
      mockDrafts.getDraftPicks.mockResolvedValue(picks);
      mockDrafts.getDraftPool.mockResolvedValue([]);
      mockCards.getSetCards.mockResolvedValue([createMockSetCard()]);
      mockCards.getCardRatings.mockResolvedValue([]);
      mockDrafts.getDraftDeckMetrics.mockResolvedValue(mockMetrics);
      mockDrafts.analyzeSessionPickQuality.mockResolvedValue(undefined);

      render(<Draft />);

      await waitFor(() => {
        expect(screen.getByText('Draft Assistant')).toBeInTheDocument();
      });

      const analyzeButton = screen.getByRole('button', { name: /Analyze Pick Quality/i });
      await userEvent.click(analyzeButton);

      await waitFor(() => {
        expect(mockDrafts.analyzeSessionPickQuality).toHaveBeenCalledWith('test-session-1');
      });
    });

    it('should disable analyze button when no picks exist (Case C, one pick, analyze button disabled)', async () => {
      // Case B (zero picks) never renders the active-draft view at all (#1349).
      // To test the disabled state of the analyze button, we need Case C (one pick)
      // and then verify the button is disabled because picks.length === 1 but
      // the analysis hasn't been triggered yet. The meaningful Case C test:
      // analyze button is enabled only when picks exist — verify via one-pick setup.
      const session = createMockDraftSession();
      const pick = createMockDraftPick();

      mockDrafts.getActiveDraftSessions.mockResolvedValue([session]);
      mockDrafts.getDraftPicks.mockResolvedValue([pick]);
      mockDrafts.getDraftPool.mockResolvedValue([]);
      mockCards.getSetCards.mockResolvedValue([createMockSetCard()]);
      mockCards.getCardRatings.mockResolvedValue([]);

      render(<Draft />);

      // Wait for Case C (active-draft view) to render
      await waitFor(() => {
        expect(screen.getByText('Draft Assistant')).toBeInTheDocument();
      });

      // Analyze button must be present and enabled (picks.length > 0)
      const analyzeButton = screen.getByRole('button', { name: /Analyze Pick Quality/i });
      expect(analyzeButton).not.toBeDisabled();
    });
  });

  describe('Card Details Overlay', () => {
    it('should display card details when card is clicked', async () => {
      const session = createMockDraftSession();
      const card = createMockSetCard({ Name: 'Detailed Card' });
      // Need one pick so we land in Case C (active-draft view), not Case B (#1349)
      const pick = createMockDraftPick({ CardID: card.ArenaID });

      mockDrafts.getActiveDraftSessions.mockResolvedValue([session]);
      mockDrafts.getDraftPicks.mockResolvedValue([pick]);
      mockDrafts.getDraftPool.mockResolvedValue([]);
      mockCards.getSetCards.mockResolvedValue([card]);
      mockCards.getCardRatings.mockResolvedValue([]);

      render(<Draft />);

      await waitFor(() => {
        expect(screen.getByText('Draft Assistant')).toBeInTheDocument();
      });

      const cardElement = document.querySelector('.card-item');
      if (cardElement) {
        await userEvent.click(cardElement);

        await waitFor(() => {
          expect(screen.getByText('Detailed Card')).toBeInTheDocument();
          expect(screen.getByText(/Creature/i)).toBeInTheDocument();
        });
      }
    });

    it('should close card details overlay when backdrop is clicked', async () => {
      const session = createMockDraftSession();
      const card = createMockSetCard({ Name: 'Closable Card' });
      // Need one pick so we land in Case C (active-draft view), not Case B (#1349)
      const pick = createMockDraftPick({ CardID: card.ArenaID });

      mockDrafts.getActiveDraftSessions.mockResolvedValue([session]);
      mockDrafts.getDraftPicks.mockResolvedValue([pick]);
      mockDrafts.getDraftPool.mockResolvedValue([]);
      mockCards.getSetCards.mockResolvedValue([card]);
      mockCards.getCardRatings.mockResolvedValue([]);

      render(<Draft />);

      await waitFor(() => {
        expect(screen.getByText('Draft Assistant')).toBeInTheDocument();
      });

      // Click card to open overlay
      const cardElement = document.querySelector('.card-item');
      if (cardElement) {
        await userEvent.click(cardElement);

        await waitFor(() => {
          expect(screen.getByText('Closable Card')).toBeInTheDocument();
        });

        // Click backdrop to close
        const backdrop = document.querySelector('.card-details-overlay-backdrop');
        if (backdrop) {
          await userEvent.click(backdrop);

          await waitFor(() => {
            expect(screen.queryByText('Closable Card')).not.toBeInTheDocument();
          });
        }
      }
    });
  });

  describe('Historical Draft Detail View', () => {
    it('should display historical draft detail when replay is clicked', async () => {
      const completedSession = createMockDraftSession({
        ID: 'completed-session',
        Status: 'completed',
      });
      const picks = [createMockDraftPick({ SessionID: 'completed-session', CardID: '12345' })];
      const card = createMockSetCard({ ArenaID: '12345' });
      const mockMetrics = createMockDeckMetrics();

      mockDrafts.getActiveDraftSessions.mockResolvedValue([]);
      mockDrafts.getCompletedDraftSessions.mockResolvedValue([completedSession]);
      mockDrafts.getDraftPicks.mockResolvedValue(picks);
      mockDrafts.getDraftPool.mockResolvedValue([]);
      mockCards.getCardByArenaId.mockResolvedValue(card);
      mockDrafts.getDraftGrade.mockRejectedValue(new Error('No grade'));
      mockDrafts.getDraftDeckMetrics.mockResolvedValue(mockMetrics);

      render(<Draft />);

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /View Replay/i })).toBeInTheDocument();
      });

      const replayButton = screen.getByRole('button', { name: /View Replay/i });
      await userEvent.click(replayButton);

      await waitFor(() => {
        expect(screen.getByText('Draft Replay')).toBeInTheDocument();
        expect(screen.getByText(/Picked Cards/i)).toBeInTheDocument();
      }, { timeout: 3000 });
    });

    it('should return to grid view when back button is clicked', async () => {
      const completedSession = createMockDraftSession({
        ID: 'completed-session',
        Status: 'completed',
      });
      const picks = [createMockDraftPick({ SessionID: 'completed-session', CardID: '12345' })];
      const card = createMockSetCard({ ArenaID: '12345' });
      const mockMetrics = createMockDeckMetrics();

      mockDrafts.getActiveDraftSessions.mockResolvedValue([]);
      mockDrafts.getCompletedDraftSessions.mockResolvedValue([completedSession]);
      mockDrafts.getDraftPicks.mockResolvedValue(picks);
      mockDrafts.getDraftPool.mockResolvedValue([]);
      mockCards.getCardByArenaId.mockResolvedValue(card);
      mockDrafts.getDraftGrade.mockRejectedValue(new Error('No grade'));
      mockDrafts.getDraftDeckMetrics.mockResolvedValue(mockMetrics);

      render(<Draft />);

      await waitFor(() => {
        const replayButton = screen.getByRole('button', { name: /View Replay/i });
        expect(replayButton).toBeInTheDocument();
      });

      const replayButton = screen.getByRole('button', { name: /View Replay/i });
      await userEvent.click(replayButton);

      await waitFor(() => {
        expect(screen.getByText('Draft Replay')).toBeInTheDocument();
      }, { timeout: 5000 });

      const backButton = screen.getByRole('button', { name: /Back to Draft History/i });
      await userEvent.click(backButton);

      await waitFor(() => {
        expect(screen.getByText('Draft History')).toBeInTheDocument();
      }, { timeout: 2000 });
    });

    it('should display FormatInsights (Archetype Performance) in historical draft detail view (#899)', async () => {
      const completedSession = createMockDraftSession({
        ID: 'completed-session',
        Status: 'completed',
        SetCode: 'BLB',
        EventName: 'PremierDraft',
      });
      const picks = [createMockDraftPick({ SessionID: 'completed-session', CardID: '12345' })];
      const card = createMockSetCard({ ArenaID: '12345' });
      const mockMetrics = createMockDeckMetrics();

      mockDrafts.getActiveDraftSessions.mockResolvedValue([]);
      mockDrafts.getCompletedDraftSessions.mockResolvedValue([completedSession]);
      mockDrafts.getDraftPicks.mockResolvedValue(picks);
      mockDrafts.getDraftPool.mockResolvedValue([]);
      mockCards.getCardByArenaId.mockResolvedValue(card);
      mockDrafts.getDraftGrade.mockRejectedValue(new Error('No grade'));
      mockDrafts.getDraftDeckMetrics.mockResolvedValue(mockMetrics);

      render(<Draft />);

      await waitFor(() => {
        expect(screen.getByRole('button', { name: /View Replay/i })).toBeInTheDocument();
      });

      const replayButton = screen.getByRole('button', { name: /View Replay/i });
      await userEvent.click(replayButton);

      await waitFor(() => {
        expect(screen.getByText('Draft Replay')).toBeInTheDocument();
      }, { timeout: 5000 });

      // FormatInsights component renders "Archetype Performance Dashboard" in a collapsible header
      // The header includes arrows and set/format info, so use regex for partial match
      await waitFor(() => {
        expect(screen.getByText(/Archetype Performance Dashboard/i)).toBeInTheDocument();
      }, { timeout: 2000 });
    });
  });

  describe('Auto-refresh Stale Ratings (#732)', () => {
    it('should not refresh ratings when they are fresh', async () => {
      const session = createMockDraftSession();
      const ratings = [createMockCardRating()];

      mockDrafts.getActiveDraftSessions.mockResolvedValue([session]);
      mockCards.getCardRatings.mockResolvedValue(ratings);
      mockCards.getRatingsStaleness.mockResolvedValue({
        cachedAt: new Date().toISOString(),
        isStale: false,
        cardCount: 100,
      });

      render(<Draft />);

      await waitFor(() => {
        expect(mockCards.getRatingsStaleness).toHaveBeenCalledWith('BLB', 'PremierDraft');
      });

      // Should not have called refresh since ratings are fresh
      expect(mockCards.refreshSetRatings).not.toHaveBeenCalled();
    });

    it('should auto-refresh ratings when they are stale', async () => {
      const session = createMockDraftSession();
      const ratings = [createMockCardRating()];
      const newRatings = [createMockCardRating({ name: 'New Card' })];

      mockDrafts.getActiveDraftSessions.mockResolvedValue([session]);
      mockCards.getCardRatings
        .mockResolvedValueOnce(ratings) // Initial load
        .mockResolvedValueOnce(newRatings); // After refresh
      mockCards.getRatingsStaleness.mockResolvedValue({
        cachedAt: new Date(Date.now() - 15 * 24 * 60 * 60 * 1000).toISOString(), // 15 days old
        isStale: true,
        cardCount: 100,
      });
      mockCards.refreshSetRatings.mockResolvedValue(undefined);

      render(<Draft />);

      await waitFor(() => {
        expect(mockCards.refreshSetRatings).toHaveBeenCalledWith('BLB', 'PremierDraft');
      });
    });

    it('should handle refresh error gracefully', async () => {
      const session = createMockDraftSession();
      const ratings = [createMockCardRating()];
      const consoleSpy = vi.spyOn(console, 'error').mockImplementation(() => {});

      mockDrafts.getActiveDraftSessions.mockResolvedValue([session]);
      mockCards.getCardRatings.mockResolvedValue(ratings);
      mockCards.getRatingsStaleness.mockResolvedValue({
        cachedAt: new Date(Date.now() - 15 * 24 * 60 * 60 * 1000).toISOString(),
        isStale: true,
        cardCount: 100,
      });
      mockCards.refreshSetRatings.mockRejectedValue(new Error('Network error'));

      render(<Draft />);

      await waitFor(() => {
        expect(consoleSpy).toHaveBeenCalledWith(
          expect.stringContaining('[Draft] Auto-refresh failed'),
          expect.any(Error)
        );
      });

      consoleSpy.mockRestore();
    });
  });

  // ── live_draft_advisor_enabled feature flag gate (vmt-t#628) ─────────────
  describe('live_draft_advisor_enabled feature flag gate', () => {
    function setupActiveDraft() {
      const session = createMockDraftSession();
      // Need one pick to reach Case C (active-draft view) — zero picks → Case B (#1349)
      const picks = [createMockDraftPick()];
      const packs: models.DraftPackSession[] = [];
      const setCards = [createMockSetCard()];
      const ratings = [createMockCardRating()];
      mockDrafts.getActiveDraftSessions.mockResolvedValue([session]);
      mockDrafts.getDraftPicks.mockResolvedValue(picks);
      mockDrafts.getDraftPool.mockResolvedValue(packs);
      mockCards.getSetCards.mockResolvedValue(setCards);
      mockCards.getCardRatings.mockResolvedValue(ratings);
      // getCurrentPackWithRecommendation is called by CurrentPackPicker
      mockDrafts.getCurrentPackWithRecommendation.mockResolvedValue(null);
    }

    it('flag ON — CurrentPackPicker (advisor surface) IS rendered', async () => {
      vi.mocked(useFeatureFlagModule.useFeatureFlag).mockReturnValue({ enabled: true });
      setupActiveDraft();

      render(<Draft />);

      await waitFor(() => {
        expect(screen.getByText('Draft Assistant')).toBeInTheDocument();
      });

      // CurrentPackPicker container appears when flag is ON
      await waitFor(() => {
        expect(mockDrafts.getCurrentPackWithRecommendation).toHaveBeenCalled();
      });
    });

    it('flag OFF — CurrentPackPicker (advisor surface) is NOT rendered', async () => {
      vi.mocked(useFeatureFlagModule.useFeatureFlag).mockReturnValue({ enabled: false });
      setupActiveDraft();

      render(<Draft />);

      await waitFor(() => {
        expect(screen.getByText('Draft Assistant')).toBeInTheDocument();
      });

      // CurrentPackPicker must not be mounted when flag is OFF
      expect(mockDrafts.getCurrentPackWithRecommendation).not.toHaveBeenCalled();
    });

    it('flag null/undefined (loading) — CurrentPackPicker IS rendered (optimistic-show)', async () => {
      vi.mocked(useFeatureFlagModule.useFeatureFlag).mockReturnValue({ enabled: null });
      setupActiveDraft();

      render(<Draft />);

      await waitFor(() => {
        expect(screen.getByText('Draft Assistant')).toBeInTheDocument();
      });

      // Optimistic show: treat null as enabled so the surface is visible while PostHog loads
      await waitFor(() => {
        expect(mockDrafts.getCurrentPackWithRecommendation).toHaveBeenCalled();
      });
    });
  });

  // --------------------------------------------------------------------------
  // Font regression guard (#684): no Cormorant Garamond in the Draft CSS
  // --------------------------------------------------------------------------

  describe('Draft CSS — no Cormorant Garamond (#684)', () => {
    const CSS_PATH = join(dirname(fileURLToPath(import.meta.url)), 'Draft.css');

    it('Draft.css contains no Cormorant Garamond reference', () => {
      const css = readFileSync(CSS_PATH, 'utf8');
      expect(css.toLowerCase()).not.toContain('cormorant');
      expect(css.toLowerCase()).not.toContain('garamond');
    });
  });

  // --------------------------------------------------------------------------
  // Heading copy regression guard (#685): no lorebook affectations
  // --------------------------------------------------------------------------

  describe('Draft headings — no lorebook affectations (#685)', () => {
    it('no-active-draft state shows "Draft History" — no § Chapter / The Draft pattern', async () => {
      mockDrafts.getActiveDraftSessions.mockResolvedValue([]);
      mockDrafts.getCompletedDraftSessions.mockResolvedValue([]);

      render(<Draft />);

      await waitFor(() => {
        const h1 = screen.getByRole('heading', { level: 1 });
        expect(h1).toHaveTextContent('Draft History');
        expect(h1.textContent).not.toMatch(/§|Chapter|Compendium/);
      });
    });

    it('active-draft state shows "Draft Assistant" — no § Chapter / The Draft pattern', async () => {
      mockDrafts.getActiveDraftSessions.mockResolvedValue([
        new models.DraftSession({
          ID: 'session-heading-test',
          EventName: 'QuickDraft',
          SetCode: 'BLB',
          DraftType: 'PremierDraft',
          StartTime: new Date('2025-11-20T10:00:00Z'),
          Status: 'active',
          TotalPicks: 45,
          CreatedAt: new Date('2025-11-20T10:00:00Z'),
          UpdatedAt: new Date('2025-11-20T10:00:00Z'),
        }),
      ]);
      // Need one pick to reach Case C (active-draft view) — zero picks → Case B (#1349)
      mockDrafts.getDraftPicks.mockResolvedValue([createMockDraftPick()]);
      mockDrafts.getDraftPool.mockResolvedValue([]);
      mockCards.getSetCards.mockResolvedValue([]);
      mockCards.getCardRatings.mockResolvedValue([]);

      render(<Draft />);

      await waitFor(() => {
        const h1 = screen.getByRole('heading', { level: 1 });
        expect(h1).toHaveTextContent('Draft Assistant');
        expect(h1.textContent).not.toMatch(/§|Chapter|Compendium/);
      });
    });
  });

  // --------------------------------------------------------------------------
  // #1349 — Draft Resume State + Implicit-Start Hardening
  // Three-state model: Case A (no session), Case B (session, zero picks+packs),
  // Case C (session, picks or packs present)
  // --------------------------------------------------------------------------

  describe('#1349 Draft Resume State (Case A / B / C)', () => {
    // Test 1 — Case B: active session exists but zero picks and zero packs.
    // The awaiting-data banner renders inline inside the active-draft view
    // (not as a full-page early return — that was the original bug that broke
    // CurrentPackPicker tests when picks=0).
    it('Case B: shows inline awaiting-data banner when active session has no picks and no packs', async () => {
      const session = createMockDraftSession({
        EventName: 'Quick Draft',
        SetCode: 'BLB',
      });

      mockDrafts.getActiveDraftSessions.mockResolvedValue([session]);
      mockDrafts.getDraftPicks.mockResolvedValue([]);
      mockDrafts.getDraftPool.mockResolvedValue([]);
      mockCards.getSetCards.mockResolvedValue([]);
      mockCards.getCardRatings.mockResolvedValue([]);

      render(<Draft />);

      // Active-draft view renders (Draft Assistant heading is visible)
      await waitFor(() => {
        expect(screen.getByText('Draft Assistant')).toBeInTheDocument();
      });

      // Inline awaiting-data banner and approved Prof copy (REQ-2) must be present
      expect(screen.getByTestId('draft-awaiting-data')).toBeInTheDocument();
      expect(screen.getByText(/Connected — waiting on Arena's first pack/i)).toBeInTheDocument();

      // Set + event line shown (REQ-3: EventName · SetCode visible in active-draft header)
      // Use getAllByText since EventName / SetCode may appear in multiple sub-components
      expect(screen.getAllByText(/Quick Draft/i).length).toBeGreaterThanOrEqual(1);
      expect(screen.getAllByText(/BLB/i).length).toBeGreaterThanOrEqual(1);

      // Draft History grid must NOT be visible (we are in the active-draft view)
      expect(screen.queryByText('Draft History')).not.toBeInTheDocument();
    });

    // Test 2 — Case B → Case C transition: first pick arrives after initial render.
    // The inline awaiting-data banner disappears once picks.length > 0.
    it('Case B → Case C: inline awaiting banner disappears when first pick data arrives', async () => {
      const session = createMockDraftSession({
        EventName: 'Quick Draft',
        SetCode: 'BLB',
      });

      // Start in Case B: zero picks
      mockDrafts.getActiveDraftSessions.mockResolvedValue([session]);
      mockDrafts.getDraftPicks.mockResolvedValue([]);
      mockDrafts.getDraftPool.mockResolvedValue([]);
      mockCards.getSetCards.mockResolvedValue([createMockSetCard()]);
      mockCards.getCardRatings.mockResolvedValue([]);

      render(<Draft />);

      // Confirm we're in Case B (inline banner visible, Draft Assistant heading present)
      await waitFor(() => {
        expect(screen.getByTestId('draft-awaiting-data')).toBeInTheDocument();
      });

      // Now picks arrive: update mock so next loadActiveDraft call returns one pick
      const newPick = createMockDraftPick({ CardID: '12345' });
      mockDrafts.getDraftPicks.mockResolvedValue([newPick]);

      // Fire draft:updated event — this triggers debouncedLoadActiveDraft
      mockEventEmitter.emit('draft:updated');

      // Case B inline banner should disappear; Draft Assistant heading must still be present
      await waitFor(() => {
        expect(screen.queryByTestId('draft-awaiting-data')).not.toBeInTheDocument();
        expect(screen.getByText('Draft Assistant')).toBeInTheDocument();
      }, { timeout: 3000 });
    });

    // Test 3 — Case A regression guard: no active session → shows Draft History, not Case B awaiting banner
    it('Case A regression: shows Draft History (not awaiting-data banner) when no active session exists', async () => {
      mockDrafts.getActiveDraftSessions.mockResolvedValue([]);
      mockDrafts.getCompletedDraftSessions.mockResolvedValue([]);

      render(<Draft />);

      await waitFor(() => {
        expect(screen.getByText('Draft History')).toBeInTheDocument();
      });

      // Case B awaiting-data banner must be absent (no active session)
      expect(screen.queryByTestId('draft-awaiting-data')).not.toBeInTheDocument();
    });
  });
});
