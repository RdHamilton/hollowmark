import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import DeckSuggestionCard from './DeckSuggestionCard';
import { gui } from '@/types/models';

// ---------------------------------------------------------------------------
// Fixture builder
// ---------------------------------------------------------------------------
function makeSuggestion(overrides: Partial<gui.SuggestedDeckResponse> = {}): gui.SuggestedDeckResponse {
  return {
    colorCombo: {
      colors: ['W', 'U'],
      name: 'Azorius',
    },
    spells: [
      {
        cardID: 'spell-1',
        name: 'Counterspell',
        typeLine: 'Instant',
        manaCost: '{U}{U}',
        imageURI: '',
        cmc: 2,
        colors: ['U'],
        rarity: 'Common',
        score: 0.8,
        reasoning: 'Good counter',
        convertValues: () => null,
      } as unknown as gui.SuggestedCardResponse,
      {
        cardID: 'spell-2',
        name: 'Serra Angel',
        typeLine: 'Creature — Angel',
        manaCost: '{3}{W}{W}',
        imageURI: '',
        cmc: 5,
        colors: ['W'],
        rarity: 'Uncommon',
        score: 0.7,
        reasoning: 'Solid body',
        convertValues: () => null,
      } as unknown as gui.SuggestedCardResponse,
    ],
    lands: [
      {
        cardID: 'land-1',
        name: 'Plains',
        quantity: 9,
        convertValues: () => null,
      } as unknown as gui.SuggestedLandResponse,
    ],
    totalCards: 23,
    score: 0.75,
    viability: 'strong',
    analysis: {
      creatureCount: 1,
      spellCount: 1,
      averageCMC: 3.5,
      manaCurve: { '2': 1, '5': 1 },
      synergies: ['Flying matters'],
      convertValues: () => null,
    } as unknown as gui.DeckSuggestionAnalysisResponse,
    convertValues: () => null,
    ...overrides,
  } as unknown as gui.SuggestedDeckResponse;
}

describe('DeckSuggestionCard', () => {
  const defaultProps = {
    suggestion: makeSuggestion(),
    isExpanded: false,
    onToggleExpand: vi.fn(),
    onUseDeck: vi.fn(),
    onExport: vi.fn(),
    isApplying: false,
    isExporting: false,
    rank: 1,
  };

  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('renders the deck rank', () => {
    render(<DeckSuggestionCard {...defaultProps} />);
    expect(screen.getByText('#1')).toBeInTheDocument();
  });

  it('renders the color combo name', () => {
    render(<DeckSuggestionCard {...defaultProps} />);
    expect(screen.getByText('Azorius')).toBeInTheDocument();
  });

  it('renders score as rounded percentage', () => {
    render(<DeckSuggestionCard {...defaultProps} />);
    expect(screen.getByText('75%')).toBeInTheDocument();
  });

  it('renders viability badge', () => {
    render(<DeckSuggestionCard {...defaultProps} />);
    expect(screen.getByText('strong')).toBeInTheDocument();
  });

  it('calls onToggleExpand when header is clicked', () => {
    const onToggleExpand = vi.fn();
    render(<DeckSuggestionCard {...defaultProps} onToggleExpand={onToggleExpand} />);
    fireEvent.click(screen.getByText('Azorius'));
    expect(onToggleExpand).toHaveBeenCalledOnce();
  });

  it('shows expand icon ▶ when collapsed', () => {
    render(<DeckSuggestionCard {...defaultProps} isExpanded={false} />);
    expect(screen.getByText('▶')).toBeInTheDocument();
  });

  it('shows collapse icon ▼ when expanded', () => {
    render(<DeckSuggestionCard {...defaultProps} isExpanded={true} />);
    expect(screen.getByText('▼')).toBeInTheDocument();
  });

  it('does not render spell/land details when collapsed', () => {
    render(<DeckSuggestionCard {...defaultProps} isExpanded={false} />);
    expect(screen.queryByText('Counterspell')).not.toBeInTheDocument();
    expect(screen.queryByText('Plains')).not.toBeInTheDocument();
  });

  it('renders spell and land details when expanded', () => {
    render(<DeckSuggestionCard {...defaultProps} isExpanded={true} />);
    expect(screen.getByText('Counterspell')).toBeInTheDocument();
    expect(screen.getByText('Serra Angel')).toBeInTheDocument();
    expect(screen.getByText(/Plains/)).toBeInTheDocument();
  });

  it('shows creature and spell section counts when expanded', () => {
    render(<DeckSuggestionCard {...defaultProps} isExpanded={true} />);
    expect(screen.getByText('Creatures (1)')).toBeInTheDocument();
    expect(screen.getByText('Spells (1)')).toBeInTheDocument();
  });

  it('renders analysis stats when expanded', () => {
    render(<DeckSuggestionCard {...defaultProps} isExpanded={true} />);
    expect(screen.getByText('23')).toBeInTheDocument(); // totalCards
    expect(screen.getByText('3.50')).toBeInTheDocument(); // averageCMC
  });

  it('renders synergy tags when expanded', () => {
    render(<DeckSuggestionCard {...defaultProps} isExpanded={true} />);
    expect(screen.getByText('Flying matters')).toBeInTheDocument();
  });

  it('calls onUseDeck when "Use This Deck" button is clicked', () => {
    const onUseDeck = vi.fn();
    render(<DeckSuggestionCard {...defaultProps} isExpanded={true} onUseDeck={onUseDeck} />);
    fireEvent.click(screen.getByRole('button', { name: 'Use This Deck' }));
    expect(onUseDeck).toHaveBeenCalledOnce();
  });

  it('calls onExport when "Export" button is clicked', () => {
    const onExport = vi.fn();
    render(<DeckSuggestionCard {...defaultProps} isExpanded={true} onExport={onExport} />);
    fireEvent.click(screen.getByRole('button', { name: 'Export' }));
    expect(onExport).toHaveBeenCalledOnce();
  });

  it('disables action buttons while isApplying is true', () => {
    render(<DeckSuggestionCard {...defaultProps} isExpanded={true} isApplying={true} />);
    expect(screen.getByRole('button', { name: 'Applying...' })).toBeDisabled();
    expect(screen.getByRole('button', { name: 'Export' })).toBeDisabled();
  });

  it('disables action buttons while isExporting is true', () => {
    render(<DeckSuggestionCard {...defaultProps} isExpanded={true} isExporting={true} />);
    expect(screen.getByRole('button', { name: 'Use This Deck' })).toBeDisabled();
    expect(screen.getByRole('button', { name: 'Exporting...' })).toBeDisabled();
  });
});
