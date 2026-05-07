import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import CardsToLookFor from './CardsToLookFor';
import { models, gui } from '@/types/models';

// ---------------------------------------------------------------------------
// Mock synergy utilities to control scores deterministically
// ---------------------------------------------------------------------------
vi.mock('../utils/synergy', () => ({
  analyzeSynergies: vi.fn(() => ({
    colors: ['W', 'U'],
    types: ['Creature'],
    keywords: [],
    avgCMC: 2.5,
    cmcDistribution: {},
  })),
  calculateCardSynergyScore: vi.fn(() => 70),
  getSynergyReason: vi.fn(() => 'matches colors'),
}));

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------
function makeCard(overrides: Partial<models.SetCard> = {}): models.SetCard {
  return {
    ID: 1,
    SetCode: 'M21',
    ArenaID: '12345',
    ScryfallID: 'scry-1',
    Name: 'Test Card',
    ManaCost: '{1}{W}',
    CMC: 2,
    Types: ['Creature'],
    Colors: ['W'],
    Rarity: 'Common',
    Text: 'Flying',
    Power: '2',
    Toughness: '2',
    ImageURL: '',
    ImageURLSmall: '',
    ImageURLArt: '',
    FetchedAt: {} as never,
    convertValues: () => null,
    ...overrides,
  } as unknown as models.SetCard;
}

function makeRating(mtgaId: number, tier = 'A', gihwr = 60): gui.CardRatingWithTier {
  return {
    mtga_id: mtgaId,
    tier,
    ever_drawn_win_rate: gihwr,
  } as gui.CardRatingWithTier;
}

describe('CardsToLookFor', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('shows empty prompt when no cards have been picked', () => {
    render(
      <CardsToLookFor
        pickedCards={[]}
        availableCards={[]}
        ratings={[]}
      />
    );
    expect(screen.getByText('Pick some cards to get suggestions!')).toBeInTheDocument();
  });

  it('renders the "Cards to Look For" heading', () => {
    render(
      <CardsToLookFor
        pickedCards={[]}
        availableCards={[]}
        ratings={[]}
      />
    );
    expect(screen.getByRole('heading', { name: 'Cards to Look For' })).toBeInTheDocument();
  });

  it('shows no-synergy message when picked cards yield no suggestions', async () => {
    const synergyModule = await import('../utils/synergy');
    const { calculateCardSynergyScore } = vi.mocked(synergyModule);
    calculateCardSynergyScore.mockReturnValue(10); // below 20 threshold -> no suggestions

    const picked = [makeCard({ ArenaID: '1', Name: 'Picked Card' })];
    const available = [makeCard({ ArenaID: '2', Name: 'Available Card' })];

    render(
      <CardsToLookFor
        pickedCards={picked}
        availableCards={available}
        ratings={[]}
      />
    );

    expect(screen.getByText('No strong synergies detected yet.')).toBeInTheDocument();

    calculateCardSynergyScore.mockReturnValue(70); // restore
  });

  it('renders suggestion cards when high-synergy cards are present', () => {
    const picked = [makeCard({ ArenaID: '1', Name: 'Picked Card' })];
    const available = [
      makeCard({ ArenaID: '2', Name: 'Synergy Card One' }),
      makeCard({ ArenaID: '3', Name: 'Synergy Card Two' }),
    ];
    const ratings = [makeRating(2), makeRating(3)];

    render(
      <CardsToLookFor
        pickedCards={picked}
        availableCards={available}
        ratings={ratings}
      />
    );

    // At least one suggestion card name should appear
    expect(screen.getAllByTitle(/Synergy Card/).length).toBeGreaterThan(0);
  });

  it('shows suggestion count in header when suggestions exist', () => {
    const picked = [makeCard({ ArenaID: '1', Name: 'Picked' })];
    const available = [makeCard({ ArenaID: '2', Name: 'Suggested' })];

    render(
      <CardsToLookFor
        pickedCards={picked}
        availableCards={available}
        ratings={[]}
      />
    );

    expect(screen.getByText(/suggestions/)).toBeInTheDocument();
  });

  it('fires onCardClick with the correct card when a suggestion is clicked', () => {
    const onCardClick = vi.fn();
    const picked = [makeCard({ ArenaID: '1', Name: 'Picked' })];
    const clickable = makeCard({ ArenaID: '2', Name: 'Clickable Card' });

    render(
      <CardsToLookFor
        pickedCards={picked}
        availableCards={[clickable]}
        ratings={[]}
        onCardClick={onCardClick}
      />
    );

    // Multiple sections may show the same card; click the first occurrence
    const cards = screen.getAllByTitle('Clickable Card');
    expect(cards.length).toBeGreaterThan(0);
    fireEvent.click(cards[0]);
    expect(onCardClick).toHaveBeenCalledWith(clickable);
  });

  it('excludes picked cards from available suggestions', () => {
    const sharedArenaId = '99';
    const picked = [makeCard({ ArenaID: sharedArenaId, Name: 'Already Picked' })];
    const available = [makeCard({ ArenaID: sharedArenaId, Name: 'Already Picked' })];

    render(
      <CardsToLookFor
        pickedCards={picked}
        availableCards={available}
        ratings={[]}
      />
    );

    // No suggestion cards should appear — all filtered out
    expect(screen.queryByTitle('Already Picked')).not.toBeInTheDocument();
  });
});
