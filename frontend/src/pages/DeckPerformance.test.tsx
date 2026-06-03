import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor, fireEvent, act } from '@testing-library/react';
import DeckPerformance from './DeckPerformance';
import { mockMatches } from '@/test/mocks/apiMock';
import { mockEventEmitter } from '@/test/mocks/websocketMock';
import { AppProvider } from '../context/AppContext';
import type { DeckPerformanceRow } from '@/services/api/matches';

// Helper: create a mock DeckPerformanceRow (the shape returned by GET /stats/deck-performance)
function makeDeckRow(overrides: Partial<DeckPerformanceRow> = {}): DeckPerformanceRow {
  return {
    deck_id: 'deck-1',
    deck_name: 'Test Deck',
    format: 'Ladder',
    wins: 12,
    losses: 8,
    draws: 0,
    total_games: 20,
    ...overrides,
  };
}

function makeDeckList(): DeckPerformanceRow[] {
  return [
    makeDeckRow({ deck_id: 'd1', deck_name: 'Mono Red Aggro', wins: 20, losses: 10, draws: 0, total_games: 30 }),
    makeDeckRow({ deck_id: 'd2', deck_name: 'Azorius Control', wins: 14, losses: 11, draws: 0, total_games: 25 }),
    makeDeckRow({ deck_id: 'd3', deck_name: 'Gruul Stompy', wins: 7, losses: 8, draws: 0, total_games: 15 }),
  ];
}

function renderWithProvider(ui: React.ReactElement) {
  return render(<AppProvider>{ui}</AppProvider>);
}

function getSelectByLabel(labelText: string): HTMLSelectElement {
  const label = screen.getByText(labelText);
  const filterGroup = label.closest('.filter-group');
  return filterGroup?.querySelector('select') as HTMLSelectElement;
}

describe('DeckPerformance', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockEventEmitter.clear();
    localStorage.clear();
  });

  describe('Loading State', () => {
    it('shows loading spinner while fetching data', async () => {
      let resolve: (v: DeckPerformanceRow[]) => void;
      mockMatches.getDeckPerformance.mockReturnValue(new Promise((r) => { resolve = r; }));

      renderWithProvider(<DeckPerformance />);

      expect(screen.getByText('Loading deck statistics...')).toBeInTheDocument();

      resolve!([]);
      await waitFor(() => {
        expect(screen.queryByText('Loading deck statistics...')).not.toBeInTheDocument();
      });
    });
  });

  describe('Error State', () => {
    it('shows error heading when API fails', async () => {
      mockMatches.getDeckPerformance.mockRejectedValue(new Error('Database error'));

      renderWithProvider(<DeckPerformance />);

      await waitFor(() => {
        expect(screen.getByRole('heading', { name: 'Failed to load deck statistics' })).toBeInTheDocument();
      });
      expect(screen.getByText('Database error')).toBeInTheDocument();
    });

    it('shows generic error for non-Error rejections', async () => {
      mockMatches.getDeckPerformance.mockRejectedValue('Unknown');

      renderWithProvider(<DeckPerformance />);

      await waitFor(() => {
        expect(screen.getByRole('heading', { name: 'Failed to load deck statistics' })).toBeInTheDocument();
      });
    });
  });

  describe('Empty State', () => {
    it('shows empty state when API returns []', async () => {
      mockMatches.getDeckPerformance.mockResolvedValue([]);

      renderWithProvider(<DeckPerformance />);

      await waitFor(() => {
        expect(screen.getByText('No deck data')).toBeInTheDocument();
      });
    });
  });

  describe('Data Display', () => {
    it('renders deck cards with names', async () => {
      mockMatches.getDeckPerformance.mockResolvedValue(makeDeckList());

      renderWithProvider(<DeckPerformance />);

      await waitFor(() => {
        expect(screen.getByText('Mono Red Aggro')).toBeInTheDocument();
      });
      expect(screen.getByText('Azorius Control')).toBeInTheDocument();
      expect(screen.getByText('Gruul Stompy')).toBeInTheDocument();
    });

    it('renders win rate correctly (wins / total_games)', async () => {
      mockMatches.getDeckPerformance.mockResolvedValue([
        makeDeckRow({ deck_name: 'Test', wins: 13, losses: 7, total_games: 20 }),
      ]);

      renderWithProvider(<DeckPerformance />);

      await waitFor(() => {
        // 13/20 = 65.0%
        expect(screen.getByText('65%')).toBeInTheDocument();
      });
    });

    it('renders 0.0% win rate when total_games is 0', async () => {
      mockMatches.getDeckPerformance.mockResolvedValue([
        makeDeckRow({ deck_name: 'Test', wins: 0, losses: 0, total_games: 0 }),
      ]);

      renderWithProvider(<DeckPerformance />);

      await waitFor(() => {
        expect(screen.getByText('0.0%')).toBeInTheDocument();
      });
    });

    it('renders wins / losses from deck row fields', async () => {
      mockMatches.getDeckPerformance.mockResolvedValue([
        makeDeckRow({ deck_name: 'Test', wins: 15, losses: 5, total_games: 20 }),
      ]);

      renderWithProvider(<DeckPerformance />);

      await waitFor(() => {
        expect(screen.getByText('15W - 5L')).toBeInTheDocument();
      });
    });

    it('renders draws in wins/losses when draws > 0', async () => {
      mockMatches.getDeckPerformance.mockResolvedValue([
        makeDeckRow({ deck_name: 'Test', wins: 10, losses: 8, draws: 2, total_games: 20 }),
      ]);

      renderWithProvider(<DeckPerformance />);

      await waitFor(() => {
        expect(screen.getByText('10W - 8L - 2D')).toBeInTheDocument();
      });
    });

    it('renders "Unknown Deck" for empty deck_name', async () => {
      mockMatches.getDeckPerformance.mockResolvedValue([
        makeDeckRow({ deck_id: 'd0', deck_name: '', total_games: 5 }),
      ]);

      renderWithProvider(<DeckPerformance />);

      await waitFor(() => {
        expect(screen.getByText('Unknown Deck')).toBeInTheDocument();
      });
    });

    it('renders deck count badge', async () => {
      mockMatches.getDeckPerformance.mockResolvedValue(makeDeckList());

      renderWithProvider(<DeckPerformance />);

      await waitFor(() => {
        expect(screen.getByText('3 decks found')).toBeInTheDocument();
      });
    });

    it('renders singular deck count for one deck', async () => {
      mockMatches.getDeckPerformance.mockResolvedValue([makeDeckRow()]);

      renderWithProvider(<DeckPerformance />);

      await waitFor(() => {
        expect(screen.getByText('1 deck found')).toBeInTheDocument();
      });
    });

    it('renders human-readable format label', async () => {
      mockMatches.getDeckPerformance.mockResolvedValue([
        makeDeckRow({ deck_name: 'Brawl Deck', format: 'HISTORICBRAWLWITHALLOWLIST_20260126' }),
      ]);

      renderWithProvider(<DeckPerformance />);

      await waitFor(() => {
        expect(screen.getByText('Historic Brawl')).toBeInTheDocument();
      });
    });

    it('renders deck cards with data-testid', async () => {
      mockMatches.getDeckPerformance.mockResolvedValue([makeDeckRow()]);

      renderWithProvider(<DeckPerformance />);

      await waitFor(() => {
        expect(screen.getAllByTestId('deck-performance-card').length).toBe(1);
      });
    });
  });

  describe('Filters', () => {
    it('renders Format filter', async () => {
      mockMatches.getDeckPerformance.mockResolvedValue(makeDeckList());

      renderWithProvider(<DeckPerformance />);

      await waitFor(() => {
        expect(getSelectByLabel('Format')).toBeInTheDocument();
      });
    });

    it('renders Sort By filter with winRate default', async () => {
      mockMatches.getDeckPerformance.mockResolvedValue(makeDeckList());

      renderWithProvider(<DeckPerformance />);

      await waitFor(() => {
        const sel = getSelectByLabel('Sort By');
        expect(sel.value).toBe('winRate');
      });
    });

    it('renders Sort Order filter with desc default', async () => {
      mockMatches.getDeckPerformance.mockResolvedValue(makeDeckList());

      renderWithProvider(<DeckPerformance />);

      await waitFor(() => {
        const sel = getSelectByLabel('Sort Order');
        expect(sel.value).toBe('desc');
      });
    });

    it('count label updates to filtered count when format filter is applied', async () => {
      mockMatches.getDeckPerformance.mockResolvedValue([
        makeDeckRow({ deck_id: 'd1', deck_name: 'Ladder Deck', format: 'Ladder', wins: 10, losses: 5, total_games: 15 }),
        makeDeckRow({ deck_id: 'd2', deck_name: 'Draft Deck', format: 'QuickDraft_BLB', wins: 7, losses: 3, total_games: 10 }),
        makeDeckRow({ deck_id: 'd3', deck_name: 'Play Deck', format: 'Play', wins: 8, losses: 4, total_games: 12 }),
        makeDeckRow({ deck_id: 'd4', deck_name: 'Another Draft', format: 'PremierDraft_OTJ', wins: 5, losses: 5, total_games: 10 }),
      ]);

      renderWithProvider(<DeckPerformance />);

      await waitFor(() => {
        expect(screen.getByText('4 decks found')).toBeInTheDocument();
      });

      fireEvent.change(getSelectByLabel('Format'), { target: { value: 'constructed' } });

      await waitFor(() => {
        // Constructed = Ladder + Play = 2 decks
        expect(screen.getByText('2 decks found')).toBeInTheDocument();
      });
    });

    it('count label shows 0 decks found when filter matches nothing', async () => {
      mockMatches.getDeckPerformance.mockResolvedValue([
        makeDeckRow({ deck_id: 'd1', deck_name: 'Draft Deck', format: 'QuickDraft_BLB', wins: 7, losses: 3, total_games: 10 }),
      ]);

      renderWithProvider(<DeckPerformance />);

      await waitFor(() => {
        expect(screen.getByText('1 deck found')).toBeInTheDocument();
      });

      fireEvent.change(getSelectByLabel('Format'), { target: { value: 'constructed' } });

      await waitFor(() => {
        expect(screen.getByText('0 decks found')).toBeInTheDocument();
      });
    });

    it('client-side format filter shows only matching decks', async () => {
      mockMatches.getDeckPerformance.mockResolvedValue([
        makeDeckRow({ deck_id: 'd1', deck_name: 'Ladder Deck', format: 'Ladder' }),
        makeDeckRow({ deck_id: 'd2', deck_name: 'Draft Deck', format: 'QuickDraft_BLB' }),
      ]);

      renderWithProvider(<DeckPerformance />);

      await waitFor(() => {
        expect(screen.getByText('Ladder Deck')).toBeInTheDocument();
        expect(screen.getByText('Draft Deck')).toBeInTheDocument();
      });

      const formatSelect = getSelectByLabel('Format');
      fireEvent.change(formatSelect, { target: { value: 'limited' } });

      await waitFor(() => {
        expect(screen.queryByText('Ladder Deck')).not.toBeInTheDocument();
        expect(screen.getByText('Draft Deck')).toBeInTheDocument();
      });
    });
  });

  describe('Sorting', () => {
    it('sorts by win rate descending by default', async () => {
      mockMatches.getDeckPerformance.mockResolvedValue(makeDeckList());

      renderWithProvider(<DeckPerformance />);

      await waitFor(() => {
        const cards = screen.getAllByRole('heading', { level: 3 });
        // Mono Red 20/30 = 66.7%, Azorius 14/25 = 56%, Gruul 7/15 = 46.7%
        expect(cards[0]).toHaveTextContent('Mono Red Aggro');
        expect(cards[1]).toHaveTextContent('Azorius Control');
        expect(cards[2]).toHaveTextContent('Gruul Stompy');
      });
    });

    it('sorts by win rate ascending when sort order changed', async () => {
      mockMatches.getDeckPerformance.mockResolvedValue(makeDeckList());

      renderWithProvider(<DeckPerformance />);

      await waitFor(() => {
        expect(screen.getByText('Mono Red Aggro')).toBeInTheDocument();
      });

      fireEvent.change(getSelectByLabel('Sort Order'), { target: { value: 'asc' } });

      await waitFor(() => {
        const cards = screen.getAllByRole('heading', { level: 3 });
        expect(cards[0]).toHaveTextContent('Gruul Stompy');
        expect(cards[2]).toHaveTextContent('Mono Red Aggro');
      });
    });

    it('sorts by match count when selected', async () => {
      mockMatches.getDeckPerformance.mockResolvedValue(makeDeckList());

      renderWithProvider(<DeckPerformance />);

      await waitFor(() => {
        expect(screen.getByText('Mono Red Aggro')).toBeInTheDocument();
      });

      fireEvent.change(getSelectByLabel('Sort By'), { target: { value: 'matches' } });

      await waitFor(() => {
        const cards = screen.getAllByRole('heading', { level: 3 });
        expect(cards[0]).toHaveTextContent('Mono Red Aggro'); // 30 games
        expect(cards[1]).toHaveTextContent('Azorius Control'); // 25 games
        expect(cards[2]).toHaveTextContent('Gruul Stompy'); // 15 games
      });
    });

    it('sorts by deck name ascending', async () => {
      mockMatches.getDeckPerformance.mockResolvedValue(makeDeckList());

      renderWithProvider(<DeckPerformance />);

      await waitFor(() => {
        expect(screen.getByText('Mono Red Aggro')).toBeInTheDocument();
      });

      fireEvent.change(getSelectByLabel('Sort By'), { target: { value: 'name' } });
      fireEvent.change(getSelectByLabel('Sort Order'), { target: { value: 'asc' } });

      await waitFor(() => {
        const cards = screen.getAllByRole('heading', { level: 3 });
        expect(cards[0]).toHaveTextContent('Azorius Control');
        expect(cards[1]).toHaveTextContent('Gruul Stompy');
        expect(cards[2]).toHaveTextContent('Mono Red Aggro');
      });
    });
  });

  describe('Real-time Updates', () => {
    it('reloads data when stats:updated event fires', async () => {
      mockMatches.getDeckPerformance.mockResolvedValue(makeDeckList());

      renderWithProvider(<DeckPerformance />);

      await waitFor(() => {
        expect(screen.getByText('Mono Red Aggro')).toBeInTheDocument();
      });

      const initialCallCount = mockMatches.getDeckPerformance.mock.calls.length;

      mockMatches.getDeckPerformance.mockResolvedValue([
        makeDeckRow({ deck_id: 'd99', deck_name: 'Updated Deck' }),
      ]);

      await act(async () => {
        mockEventEmitter.emit('stats:updated', {});
      });

      await waitFor(() => {
        expect(mockMatches.getDeckPerformance.mock.calls.length).toBeGreaterThan(initialCallCount);
      });
    });

    it('unsubscribes from events on unmount', async () => {
      mockMatches.getDeckPerformance.mockResolvedValue(makeDeckList());

      const { unmount } = renderWithProvider(<DeckPerformance />);

      await waitFor(() => {
        expect(screen.getByText('Mono Red Aggro')).toBeInTheDocument();
      });

      unmount();

      const callCountAfterUnmount = mockMatches.getDeckPerformance.mock.calls.length;

      await act(async () => {
        mockEventEmitter.emit('stats:updated', {});
      });

      expect(mockMatches.getDeckPerformance.mock.calls.length).toBe(callCountAfterUnmount);
    });
  });

  describe('Page Title', () => {
    it('displays "Deck Performance" heading', async () => {
      mockMatches.getDeckPerformance.mockResolvedValue([]);

      renderWithProvider(<DeckPerformance />);

      await waitFor(() => {
        expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent('Deck Performance');
      });
    });
  });
});
