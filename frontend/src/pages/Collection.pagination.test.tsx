// Collection.pagination.test.tsx — Vitest tests for #1325 pagination
//
// Tests for:
//   - totalCount header comes from response.totalCount (UniqueCards), not array length
//   - filterCount comes from response.filterCount (CountFilteredCollection), not array length
//   - search/sort params are sent in the API call (server-side), not done client-side
//   - processedCards useMemo is removed: cards are rendered directly from API response
//   - sort/search params forwarded to getCollectionWithMetadata

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { screen, waitFor, fireEvent } from '@testing-library/react';
import { gui } from '@/types/models';
import { mockCollection, mockCards as mockCardsApi } from '@/test/mocks/apiMock';
import { renderWithRouter } from '@/test/utils/testUtils';
import Collection from './Collection';

// Helper to create a minimal CollectionCard
function mkCard(overrides: Record<string, unknown> = {}): gui.CollectionCard {
  return new gui.CollectionCard({
    cardId: 1,
    arenaId: 1,
    quantity: 1,
    name: 'Test Card',
    setCode: 'tst',
    setName: 'Test Set',
    rarity: 'common',
    manaCost: '',
    cmc: 1,
    typeLine: 'Instant',
    colors: [],
    colorIdentity: [],
    imageUri: 'https://example.com/card.jpg',
    power: '',
    toughness: '',
    ...overrides,
  });
}

// Full paginated response shape including new fields
interface PaginatedCollectionResponse {
  cards: gui.CollectionCard[];
  totalCount: number;
  filterCount: number;
  totalPages: number;
  unknownCardsRemaining: number;
  unknownCardsFetched: number;
}

function mkResponse(cards: gui.CollectionCard[], totalCount: number, filterCount: number, totalPages: number): PaginatedCollectionResponse {
  return {
    cards,
    totalCount,
    filterCount,
    totalPages,
    unknownCardsRemaining: 0,
    unknownCardsFetched: 0,
  };
}

function setupWailsRuntime() {
  (window as unknown as Record<string, unknown>).go = {};
}

function clearWailsRuntime() {
  delete (window as unknown as Record<string, unknown>).go;
}

describe('Collection — pagination (#1325)', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    setupWailsRuntime();
    vi.useFakeTimers({ shouldAdvanceTime: true });
    mockCardsApi.getAllSetInfo.mockResolvedValue([]);
    mockCollection.getCollectionValue.mockResolvedValue({
      totalValueUsd: 0, totalValueEur: 0, uniqueCardsWithPrice: 0,
      cardCount: 0, valueByRarity: {}, topCards: [],
    });
  });

  afterEach(() => {
    clearWailsRuntime();
    vi.useRealTimers();
  });

  // ---------------------------------------------------------------------------
  // AC: totalCount comes from response field (UniqueCards), not array length
  // ---------------------------------------------------------------------------

  it('shows totalCount from response.totalCount, not array length', async () => {
    // 3 cards on this page, but account has 9500 total unique cards
    const cards = [mkCard({ cardId: 1, name: 'A' }), mkCard({ cardId: 2, name: 'B' }), mkCard({ cardId: 3, name: 'C' })];
    mockCollection.getCollectionWithMetadata.mockResolvedValue(
      mkResponse(cards, 9500, 3000, 60)
    );

    renderWithRouter(<Collection />);
    await vi.advanceTimersByTimeAsync(100);

    await waitFor(() => {
      // The "Total Cards:" stat should show 9500 (from response.totalCount / UniqueCards)
      expect(screen.getByText('9,500')).toBeInTheDocument();
    });
  });

  it('shows filterCount from response.filterCount, not array length', async () => {
    const cards = [mkCard({ cardId: 1, name: 'Fireball' })];
    // filterCount = 300 (many matching server-side), but only 1 on this page
    mockCollection.getCollectionWithMetadata.mockResolvedValue(
      mkResponse(cards, 9500, 300, 6)
    );

    renderWithRouter(<Collection />);
    await vi.advanceTimersByTimeAsync(100);

    await waitFor(() => {
      // "Showing X of Y" — X should be filterCount (300), Y totalCount (9500)
      expect(screen.getByText('Showing 300 of 9,500 cards')).toBeInTheDocument();
    });
  });

  // ---------------------------------------------------------------------------
  // AC: search sends to API (server-side), no client-side filtering
  // ---------------------------------------------------------------------------

  it('sends search term to API on debounce, does NOT filter client-side', async () => {
    const initialCards = [mkCard({ cardId: 1, name: 'Lightning Bolt' }), mkCard({ cardId: 2, name: 'Counterspell' })];
    mockCollection.getCollectionWithMetadata
      .mockResolvedValueOnce(mkResponse(initialCards, 2, 2, 1))
      .mockResolvedValueOnce(mkResponse([mkCard({ cardId: 1, name: 'Lightning Bolt' })], 2, 1, 1));

    renderWithRouter(<Collection />);
    await vi.advanceTimersByTimeAsync(100);

    await waitFor(() => {
      expect(screen.getByTestId('collection-search-input')).toBeInTheDocument();
    });

    const searchInput = screen.getByTestId('collection-search-input');
    fireEvent.change(searchInput, { target: { value: 'bolt' } });

    // Advance past the debounce
    await vi.advanceTimersByTimeAsync(350);

    await waitFor(() => {
      expect(mockCollection.getCollectionWithMetadata).toHaveBeenCalledTimes(2);
    });

    // The second call must include the search term
    const secondCall = mockCollection.getCollectionWithMetadata.mock.calls[1][0];
    expect(secondCall).toEqual(expect.objectContaining({ search: 'bolt' }));
  });

  // ---------------------------------------------------------------------------
  // AC: sort sends to API (server-side), no client-side sorting
  // ---------------------------------------------------------------------------

  it('sends sort_by and sort_desc to API when sort changes', async () => {
    mockCollection.getCollectionWithMetadata
      .mockResolvedValueOnce(mkResponse([mkCard()], 1, 1, 1))
      .mockResolvedValueOnce(mkResponse([mkCard()], 1, 1, 1));

    renderWithRouter(<Collection />);
    await vi.advanceTimersByTimeAsync(100);

    await waitFor(() => {
      expect(screen.getByTestId('collection-sort-select')).toBeInTheDocument();
    });

    const sortSelect = screen.getByTestId('collection-sort-select');
    fireEvent.change(sortSelect, { target: { value: 'price-desc' } });

    await vi.advanceTimersByTimeAsync(100);

    await waitFor(() => {
      expect(mockCollection.getCollectionWithMetadata).toHaveBeenCalledTimes(2);
    });

    const secondCall = mockCollection.getCollectionWithMetadata.mock.calls[1][0];
    expect(secondCall).toEqual(expect.objectContaining({ sort_by: 'price', sort_desc: true }));
  });

  // ---------------------------------------------------------------------------
  // AC: pagination sends page param to API
  // ---------------------------------------------------------------------------

  it('sends page=2 to API when clicking Next', async () => {
    // page 1: 50 cards; totalPages=3
    const page1Cards = Array.from({ length: 50 }, (_, i) => mkCard({ cardId: i + 1, arenaId: i + 1, name: `Card ${i + 1}` }));
    mockCollection.getCollectionWithMetadata
      .mockResolvedValueOnce(mkResponse(page1Cards, 150, 150, 3))
      .mockResolvedValueOnce(mkResponse(page1Cards, 150, 150, 3)); // page 2

    renderWithRouter(<Collection />);
    await vi.advanceTimersByTimeAsync(100);

    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Next' })).toBeInTheDocument();
    });

    fireEvent.click(screen.getByRole('button', { name: 'Next' }));

    await vi.advanceTimersByTimeAsync(100);

    await waitFor(() => {
      expect(mockCollection.getCollectionWithMetadata).toHaveBeenCalledTimes(2);
    });

    const secondCall = mockCollection.getCollectionWithMetadata.mock.calls[1][0];
    expect(secondCall).toEqual(expect.objectContaining({ page: 2 }));
  });

  // ---------------------------------------------------------------------------
  // AC: totalPages from response, not computed from array length
  // ---------------------------------------------------------------------------

  it('uses response.totalPages to determine pagination controls', async () => {
    // 50 cards on this page, but server says totalPages=10
    const cards = Array.from({ length: 50 }, (_, i) => mkCard({ cardId: i + 1, arenaId: i + 1, name: `Card ${i + 1}` }));
    mockCollection.getCollectionWithMetadata.mockResolvedValue(
      mkResponse(cards, 500, 500, 10)
    );

    renderWithRouter(<Collection />);
    await vi.advanceTimersByTimeAsync(100);

    await waitFor(() => {
      // Last button should be present (more than 1 page)
      expect(screen.getByRole('button', { name: 'Last' })).toBeInTheDocument();
    });

    // Navigate to last page — verifies totalPages=10 is respected
    fireEvent.click(screen.getByRole('button', { name: 'Last' }));

    await vi.advanceTimersByTimeAsync(100);

    // The page-jump input should now show 10
    await waitFor(() => {
      const jumpInput = screen.getByTestId('collection-page-jump') as HTMLInputElement;
      expect(jumpInput.value).toBe('10');
    });
  });

  // ---------------------------------------------------------------------------
  // AC: no client-side processedCards useMemo
  //     The cards rendered on screen are exactly what the API returned,
  //     not a re-sorted/re-filtered subset of a larger local array.
  // ---------------------------------------------------------------------------

  it('renders exactly the cards returned by API (no client-side re-sort)', async () => {
    // API returns cards in a specific order (B, A, C) — if client-side sort
    // were active they would be reordered to A, B, C (name-asc default).
    const cards = [
      mkCard({ cardId: 2, arenaId: 2, name: 'Brainwash' }),
      mkCard({ cardId: 1, arenaId: 1, name: 'Annihilate' }),
      mkCard({ cardId: 3, arenaId: 3, name: 'Counterspell' }),
    ];
    mockCollection.getCollectionWithMetadata.mockResolvedValue(
      mkResponse(cards, 3, 3, 1)
    );

    renderWithRouter(<Collection />);
    await vi.advanceTimersByTimeAsync(100);

    await waitFor(() => {
      expect(screen.getByAltText('Brainwash')).toBeInTheDocument();
    });

    // The order in the DOM must match the API order (B, A, C), not
    // client-side sorted (A, B, C). Scope query to the card grid to exclude
    // color-filter icons (also <img> elements).
    const grid = screen.getByTestId('collection-card-grid');
    const cardImages = grid.querySelectorAll('img');
    const names = Array.from(cardImages).map((img) => img.getAttribute('alt'));
    expect(names[0]).toBe('Brainwash');
    expect(names[1]).toBe('Annihilate');
    expect(names[2]).toBe('Counterspell');
  });

  // ---------------------------------------------------------------------------
  // AC: filterCount and totalCount correctly reflect header stat labels
  // ---------------------------------------------------------------------------

  it('header stats: Cards in Set shows filterCount, Total Cards shows totalCount', async () => {
    const cards = [mkCard()];
    mockCollection.getCollectionWithMetadata.mockResolvedValue(
      mkResponse(cards, 12000, 450, 9)
    );

    renderWithRouter(<Collection />);
    await vi.advanceTimersByTimeAsync(100);

    await waitFor(() => {
      expect(screen.getByTestId('collection-stats')).toBeInTheDocument();
    });

    const statsEl = screen.getByTestId('collection-stats');
    // Cards in Set (filterCount) = 450
    expect(statsEl.textContent).toContain('450');
    // Total Cards (totalCount) = 12000
    expect(statsEl.textContent).toContain('12,000');
  });
});
