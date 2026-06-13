import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor, fireEvent, cleanup } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import Decks from './Decks';
import { mockDecks } from '@/test/mocks/apiMock';
import { gui } from '@/types/models';

// Mock useNavigate
const mockNavigate = vi.fn();
vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual('react-router-dom');
  return {
    ...actual,
    useNavigate: () => mockNavigate,
  };
});

// Helper function to create mock deck list item
// eslint-disable-next-line @typescript-eslint/no-explicit-any
function createMockDeckListItem(overrides: Record<string, any> = {}): gui.DeckListItem {
  return new gui.DeckListItem({
    id: 'deck-1',
    name: 'Test Deck',
    format: 'standard',
    source: 'manual',
    colorIdentity: 'WU',
    cardCount: 60,
    matchesPlayed: 10,
    winRate: 0.6,
    modifiedAt: new Date('2024-01-15T10:00:00').toISOString(),
    lastPlayed: new Date('2024-01-14T10:00:00').toISOString(),
    tags: [],
    ...overrides,
  });
}

// Helper to create multiple mock decks
function createMockDeckList(): gui.DeckListItem[] {
  return [
    createMockDeckListItem({
      id: 'deck-1',
      name: 'Mono Red Aggro',
      format: 'standard',
      source: 'manual',
    }),
    createMockDeckListItem({
      id: 'deck-2',
      name: 'Azorius Control',
      format: 'historic',
      source: 'draft',
    }),
    createMockDeckListItem({
      id: 'deck-3',
      name: 'Imported Deck',
      format: 'explorer',
      source: 'import',
    }),
  ];
}

// Wrapper component with router
function renderWithRouter(ui: React.ReactElement) {
  return render(<MemoryRouter>{ui}</MemoryRouter>);
}

// Setup window.go to simulate Wails runtime being ready
function setupWailsRuntime() {
  (window as unknown as Record<string, unknown>).go = {};
}

function clearWailsRuntime() {
  delete (window as unknown as Record<string, unknown>).go;
}

describe('Decks', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockNavigate.mockClear();
    setupWailsRuntime();
    vi.useFakeTimers({ shouldAdvanceTime: true });
  });

  afterEach(() => {
    clearWailsRuntime();
    vi.useRealTimers();
  });

  describe('Loading State', () => {
    it('should show loading spinner while fetching decks', async () => {
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      let resolvePromise: (value: any) => void;
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const loadingPromise = new Promise<any>((resolve) => {
        resolvePromise = resolve;
      });
      mockDecks.getDecks.mockReturnValue(loadingPromise);

      renderWithRouter(<Decks />);

      // Advance timers to trigger the interval check
      await vi.advanceTimersByTimeAsync(100);

      expect(screen.getByText('Loading decks...')).toBeInTheDocument();

      resolvePromise!(createMockDeckList());
      await waitFor(() => {
        expect(screen.queryByText('Loading decks...')).not.toBeInTheDocument();
      });
    });
  });

  describe('Error State', () => {
    it('should show error state when API fails', async () => {
      mockDecks.getDecks.mockRejectedValue(new Error('Database error'));

      renderWithRouter(<Decks />);

      await vi.advanceTimersByTimeAsync(100);

      await waitFor(() => {
        expect(screen.getByText('Error Loading Decks')).toBeInTheDocument();
      });
      expect(screen.getByText('Database error')).toBeInTheDocument();
    });

    it('should show generic error message for non-Error rejections', async () => {
      mockDecks.getDecks.mockRejectedValue('Unknown error');

      renderWithRouter(<Decks />);

      await vi.advanceTimersByTimeAsync(100);

      await waitFor(() => {
        expect(screen.getByText('Error Loading Decks')).toBeInTheDocument();
      });
      expect(screen.getByText('Failed to load decks')).toBeInTheDocument();
    });

    it('should have retry button that reloads decks', async () => {
      mockDecks.getDecks.mockRejectedValueOnce(new Error('Temporary error'));
      mockDecks.getDecks.mockResolvedValueOnce(createMockDeckList());

      renderWithRouter(<Decks />);

      await vi.advanceTimersByTimeAsync(100);

      await waitFor(() => {
        expect(screen.getByText('Error Loading Decks')).toBeInTheDocument();
      });

      const retryButton = screen.getByRole('button', { name: 'Retry' });
      fireEvent.click(retryButton);

      await waitFor(() => {
        expect(screen.getByText('Mono Red Aggro')).toBeInTheDocument();
      });
    });
  });

  describe('Empty State', () => {
    it('should show empty state when no decks exist', async () => {
      mockDecks.getDecks.mockResolvedValue([]);

      renderWithRouter(<Decks />);

      await vi.advanceTimersByTimeAsync(100);

      await waitFor(() => {
        expect(screen.getByText('No Decks Yet')).toBeInTheDocument();
      });
      expect(screen.getByText('Create your first deck to get started!')).toBeInTheDocument();
    });

    it('should show empty state when API returns null', async () => {
      mockDecks.getDecks.mockResolvedValue(null);

      renderWithRouter(<Decks />);

      await vi.advanceTimersByTimeAsync(100);

      await waitFor(() => {
        expect(screen.getByText('No Decks Yet')).toBeInTheDocument();
      });
    });

    it('should show create button in empty state', async () => {
      mockDecks.getDecks.mockResolvedValue([]);

      renderWithRouter(<Decks />);

      await vi.advanceTimersByTimeAsync(100);

      await waitFor(() => {
        expect(screen.getByText('+ Create New Deck')).toBeInTheDocument();
      });
    });
  });

  describe('Deck List Display', () => {
    it('should render deck cards when decks exist', async () => {
      mockDecks.getDecks.mockResolvedValue(createMockDeckList());

      renderWithRouter(<Decks />);

      await vi.advanceTimersByTimeAsync(100);

      await waitFor(() => {
        expect(screen.getByText('Mono Red Aggro')).toBeInTheDocument();
      });
      expect(screen.getByText('Azorius Control')).toBeInTheDocument();
      expect(screen.getByText('Imported Deck')).toBeInTheDocument();
    });

    it('should display page title', async () => {
      mockDecks.getDecks.mockResolvedValue(createMockDeckList());

      renderWithRouter(<Decks />);

      await vi.advanceTimersByTimeAsync(100);

      await waitFor(() => {
        expect(screen.getByRole('heading', { name: 'My Decks' })).toBeInTheDocument();
      });
    });

    it('should display format for each deck', async () => {
      mockDecks.getDecks.mockResolvedValue(createMockDeckList());

      renderWithRouter(<Decks />);

      await vi.advanceTimersByTimeAsync(100);

      await waitFor(() => {
        expect(screen.getByText('standard')).toBeInTheDocument();
      });
      expect(screen.getByText('historic')).toBeInTheDocument();
      expect(screen.getByText('explorer')).toBeInTheDocument();
    });

    it('displays human-readable label for HISTORICBRAWLWITHALLOWLIST format slug', async () => {
      mockDecks.getDecks.mockResolvedValue([
        createMockDeckListItem({
          id: 'brawl-deck',
          name: 'Brawl Deck',
          format: 'HISTORICBRAWLWITHALLOWLIST_20260126',
        }),
      ]);

      renderWithRouter(<Decks />);

      await vi.advanceTimersByTimeAsync(100);

      await waitFor(() => {
        expect(screen.getByText('Historic Brawl')).toBeInTheDocument();
        expect(screen.queryByText('HISTORICBRAWLWITHALLOWLIST_20260126')).not.toBeInTheDocument();
      });
    });

    it('should display draft badge for draft decks', async () => {
      mockDecks.getDecks.mockResolvedValue(createMockDeckList());

      renderWithRouter(<Decks />);

      await vi.advanceTimersByTimeAsync(100);

      await waitFor(() => {
        expect(screen.getByText('Draft')).toBeInTheDocument();
      });
    });

    it('should display import badge for imported decks', async () => {
      mockDecks.getDecks.mockResolvedValue(createMockDeckList());

      renderWithRouter(<Decks />);

      await vi.advanceTimersByTimeAsync(100);

      await waitFor(() => {
        expect(screen.getByText('Import')).toBeInTheDocument();
      });
    });

    it('should display archetype badge when primaryArchetype is present', async () => {
      mockDecks.getDecks.mockResolvedValue([
        createMockDeckListItem({
          id: 'deck-1',
          name: 'Boros Aggro',
          primaryArchetype: 'Boros Aggro',
        }),
      ]);

      renderWithRouter(<Decks />);

      await vi.advanceTimersByTimeAsync(100);

      await waitFor(() => {
        expect(screen.getByText('Boros Aggro', { selector: '.archetype-badge' })).toBeInTheDocument();
      });
    });

    it('should not display archetype badge when primaryArchetype is not present', async () => {
      mockDecks.getDecks.mockResolvedValue([
        createMockDeckListItem({
          id: 'deck-1',
          name: 'Test Deck',
          primaryArchetype: undefined,
        }),
      ]);

      renderWithRouter(<Decks />);

      await vi.advanceTimersByTimeAsync(100);

      await waitFor(() => {
        expect(screen.getByText('Test Deck')).toBeInTheDocument();
      });
      expect(document.querySelector('.archetype-badge')).not.toBeInTheDocument();
    });

    it('should display multiple badges (archetype and source) together', async () => {
      mockDecks.getDecks.mockResolvedValue([
        createMockDeckListItem({
          id: 'deck-1',
          name: 'Draft Midrange',
          source: 'draft',
          primaryArchetype: 'Dimir Midrange',
        }),
      ]);

      renderWithRouter(<Decks />);

      await vi.advanceTimersByTimeAsync(100);

      await waitFor(() => {
        expect(screen.getByText('Dimir Midrange', { selector: '.archetype-badge' })).toBeInTheDocument();
      });
      expect(screen.getByText('Draft', { selector: '.source-badge' })).toBeInTheDocument();
    });

    it('should display modified date when available', async () => {
      mockDecks.getDecks.mockResolvedValue([
        createMockDeckListItem({
          modifiedAt: new Date('2024-01-15T10:00:00').toISOString(),
        }),
      ]);

      renderWithRouter(<Decks />);

      await vi.advanceTimersByTimeAsync(100);

      await waitFor(() => {
        expect(screen.getByText(/Modified:/)).toBeInTheDocument();
      });
    });

    it('deck card Modified line includes time portion (#1360)', async () => {
      // modifiedAt ISO string has a non-midnight time — the rendered label must
      // include a time component (AM/PM or HH:MM) so same-day draft decks are
      // distinguishable. Currently formatDate calls toLocaleDateString() which
      // drops the time; this test pins the required behaviour.
      mockDecks.getDecks.mockResolvedValue([
        createMockDeckListItem({
          id: 'deck-time',
          name: 'Draft Deck',
          source: 'draft',
          modifiedAt: new Date('2024-01-15T14:30:00').toISOString(),
        }),
      ]);

      renderWithRouter(<Decks />);

      await vi.advanceTimersByTimeAsync(100);

      await waitFor(() => {
        expect(screen.getByText('Draft Deck')).toBeInTheDocument();
      });

      // The Modified: span text must include a time indicator — either AM/PM or HH:MM
      const modifiedEl = screen.getByText(/Modified:/);
      const modifiedText = modifiedEl.textContent ?? '';
      expect(modifiedText).toMatch(/\d{1,2}:\d{2}/);
    });

    it('should show create button in header when decks exist', async () => {
      mockDecks.getDecks.mockResolvedValue(createMockDeckList());

      renderWithRouter(<Decks />);

      await vi.advanceTimersByTimeAsync(100);

      await waitFor(() => {
        expect(screen.getByText('+ Create New Deck')).toBeInTheDocument();
      });
    });
  });

  describe('Navigation', () => {
    it('should navigate to deck builder when clicking deck card', async () => {
      mockDecks.getDecks.mockResolvedValue(createMockDeckList());

      renderWithRouter(<Decks />);

      await vi.advanceTimersByTimeAsync(100);

      await waitFor(() => {
        expect(screen.getByText('Mono Red Aggro')).toBeInTheDocument();
      });

      const deckCard = screen.getByText('Mono Red Aggro').closest('.deck-card');
      fireEvent.click(deckCard!);

      expect(mockNavigate).toHaveBeenCalledWith('/deck-builder/deck-1');
    });

    it('should navigate to deck builder when clicking edit button', async () => {
      mockDecks.getDecks.mockResolvedValue(createMockDeckList());

      renderWithRouter(<Decks />);

      await vi.advanceTimersByTimeAsync(100);

      await waitFor(() => {
        expect(screen.getByText('Mono Red Aggro')).toBeInTheDocument();
      });

      const editButtons = screen.getAllByRole('button', { name: 'Edit' });
      fireEvent.click(editButtons[0]);

      expect(mockNavigate).toHaveBeenCalledWith('/deck-builder/deck-1');
    });
  });

  describe('Create Deck Dialog', () => {
    it('should open create dialog when clicking create button', async () => {
      mockDecks.getDecks.mockResolvedValue(createMockDeckList());

      renderWithRouter(<Decks />);

      await vi.advanceTimersByTimeAsync(100);

      await waitFor(() => {
        expect(screen.getByText('+ Create New Deck')).toBeInTheDocument();
      });

      fireEvent.click(screen.getByText('+ Create New Deck'));

      expect(screen.getByText('Create New Deck')).toBeInTheDocument();
      expect(screen.getByLabelText('Deck Name')).toBeInTheDocument();
      expect(screen.getByLabelText('Format')).toBeInTheDocument();
    });

    it('should close create dialog when clicking cancel', async () => {
      mockDecks.getDecks.mockResolvedValue(createMockDeckList());

      renderWithRouter(<Decks />);

      await vi.advanceTimersByTimeAsync(100);

      await waitFor(() => {
        expect(screen.getByText('+ Create New Deck')).toBeInTheDocument();
      });

      fireEvent.click(screen.getByText('+ Create New Deck'));
      expect(screen.getByText('Create New Deck')).toBeInTheDocument();

      fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));

      await waitFor(() => {
        expect(screen.queryByText('Create New Deck')).not.toBeInTheDocument();
      });
    });

    it('should close create dialog when clicking close button', async () => {
      mockDecks.getDecks.mockResolvedValue(createMockDeckList());

      renderWithRouter(<Decks />);

      await vi.advanceTimersByTimeAsync(100);

      await waitFor(() => {
        expect(screen.getByText('+ Create New Deck')).toBeInTheDocument();
      });

      fireEvent.click(screen.getByText('+ Create New Deck'));
      expect(screen.getByText('Create New Deck')).toBeInTheDocument();

      fireEvent.click(screen.getByRole('button', { name: '×' }));

      await waitFor(() => {
        expect(screen.queryByText('Create New Deck')).not.toBeInTheDocument();
      });
    });

    it('should close create dialog when clicking overlay', async () => {
      mockDecks.getDecks.mockResolvedValue(createMockDeckList());

      renderWithRouter(<Decks />);

      await vi.advanceTimersByTimeAsync(100);

      await waitFor(() => {
        expect(screen.getByText('+ Create New Deck')).toBeInTheDocument();
      });

      fireEvent.click(screen.getByText('+ Create New Deck'));
      expect(screen.getByText('Create New Deck')).toBeInTheDocument();

      const overlay = document.querySelector('.modal-overlay');
      fireEvent.click(overlay!);

      await waitFor(() => {
        expect(screen.queryByText('Create New Deck')).not.toBeInTheDocument();
      });
    });

    it('should create deck and navigate to deck builder', async () => {
      mockDecks.getDecks.mockResolvedValue(createMockDeckList());
      mockDecks.createDeck.mockResolvedValue({ ID: 'new-deck-id' });

      renderWithRouter(<Decks />);

      await vi.advanceTimersByTimeAsync(100);

      await waitFor(() => {
        expect(screen.getByText('+ Create New Deck')).toBeInTheDocument();
      });

      fireEvent.click(screen.getByText('+ Create New Deck'));

      const nameInput = screen.getByLabelText('Deck Name');
      fireEvent.change(nameInput, { target: { value: 'My New Deck' } });

      fireEvent.click(screen.getByRole('button', { name: 'Create Deck' }));

      await waitFor(() => {
        expect(mockDecks.createDeck).toHaveBeenCalledWith({
          name: 'My New Deck',
          format: 'standard',
          source: 'manual',
        });
      });
      expect(mockNavigate).toHaveBeenCalledWith('/deck-builder/new-deck-id');
    });

    it('should show alert when creating deck with empty name', async () => {
      mockDecks.getDecks.mockResolvedValue(createMockDeckList());
      const alertMock = vi.spyOn(window, 'alert').mockImplementation(() => {});

      renderWithRouter(<Decks />);

      await vi.advanceTimersByTimeAsync(100);

      await waitFor(() => {
        expect(screen.getByText('+ Create New Deck')).toBeInTheDocument();
      });

      fireEvent.click(screen.getByText('+ Create New Deck'));
      fireEvent.click(screen.getByRole('button', { name: 'Create Deck' }));

      expect(alertMock).toHaveBeenCalledWith('Please enter a deck name');
      alertMock.mockRestore();
    });

    it('should allow selecting different formats', async () => {
      mockDecks.getDecks.mockResolvedValue(createMockDeckList());
      mockDecks.createDeck.mockResolvedValue({ ID: 'new-deck-id' });

      renderWithRouter(<Decks />);

      await vi.advanceTimersByTimeAsync(100);

      await waitFor(() => {
        expect(screen.getByText('+ Create New Deck')).toBeInTheDocument();
      });

      fireEvent.click(screen.getByText('+ Create New Deck'));

      const formatSelect = screen.getByLabelText('Format');
      fireEvent.change(formatSelect, { target: { value: 'historic' } });

      const nameInput = screen.getByLabelText('Deck Name');
      fireEvent.change(nameInput, { target: { value: 'Historic Deck' } });

      fireEvent.click(screen.getByRole('button', { name: 'Create Deck' }));

      await waitFor(() => {
        expect(mockDecks.createDeck).toHaveBeenCalledWith({
          name: 'Historic Deck',
          format: 'historic',
          source: 'manual',
        });
      });
    });

    it('should create deck when pressing Enter in name input', async () => {
      mockDecks.getDecks.mockResolvedValue(createMockDeckList());
      mockDecks.createDeck.mockResolvedValue({ ID: 'new-deck-id' });

      renderWithRouter(<Decks />);

      await vi.advanceTimersByTimeAsync(100);

      await waitFor(() => {
        expect(screen.getByText('+ Create New Deck')).toBeInTheDocument();
      });

      fireEvent.click(screen.getByText('+ Create New Deck'));

      const nameInput = screen.getByLabelText('Deck Name');
      fireEvent.change(nameInput, { target: { value: 'Enter Test Deck' } });
      fireEvent.keyDown(nameInput, { key: 'Enter' });

      await waitFor(() => {
        expect(mockDecks.createDeck).toHaveBeenCalledWith({
          name: 'Enter Test Deck',
          format: 'standard',
          source: 'manual',
        });
      });
    });

    it('should show error alert when deck creation fails', async () => {
      mockDecks.getDecks.mockResolvedValue(createMockDeckList());
      mockDecks.createDeck.mockRejectedValue(new Error('Creation failed'));
      const alertMock = vi.spyOn(window, 'alert').mockImplementation(() => {});

      renderWithRouter(<Decks />);

      await vi.advanceTimersByTimeAsync(100);

      await waitFor(() => {
        expect(screen.getByText('+ Create New Deck')).toBeInTheDocument();
      });

      fireEvent.click(screen.getByText('+ Create New Deck'));

      const nameInput = screen.getByLabelText('Deck Name');
      fireEvent.change(nameInput, { target: { value: 'Failing Deck' } });
      fireEvent.click(screen.getByRole('button', { name: 'Create Deck' }));

      await waitFor(() => {
        expect(alertMock).toHaveBeenCalledWith('Creation failed');
      });
      alertMock.mockRestore();
    });
  });

  describe('Delete Deck Dialog', () => {
    it('should open delete dialog when clicking delete button', async () => {
      mockDecks.getDecks.mockResolvedValue(createMockDeckList());

      renderWithRouter(<Decks />);

      await vi.advanceTimersByTimeAsync(100);

      await waitFor(() => {
        expect(screen.getByText('Mono Red Aggro')).toBeInTheDocument();
      });

      const deleteButtons = screen.getAllByRole('button', { name: 'Delete' });
      fireEvent.click(deleteButtons[0]);

      expect(screen.getByText('Delete Deck')).toBeInTheDocument();
      expect(screen.getByText(/Are you sure you want to delete/)).toBeInTheDocument();
      expect(screen.getByText('Mono Red Aggro', { selector: 'strong' })).toBeInTheDocument();
    });

    it('should close delete dialog when clicking cancel', async () => {
      mockDecks.getDecks.mockResolvedValue(createMockDeckList());

      renderWithRouter(<Decks />);

      await vi.advanceTimersByTimeAsync(100);

      await waitFor(() => {
        expect(screen.getByText('Mono Red Aggro')).toBeInTheDocument();
      });

      const deleteButtons = screen.getAllByRole('button', { name: 'Delete' });
      fireEvent.click(deleteButtons[0]);

      expect(screen.getByText('Delete Deck')).toBeInTheDocument();

      fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));

      await waitFor(() => {
        expect(screen.queryByText('Delete Deck')).not.toBeInTheDocument();
      });
    });

    it('should close delete dialog when clicking overlay', async () => {
      mockDecks.getDecks.mockResolvedValue(createMockDeckList());

      renderWithRouter(<Decks />);

      await vi.advanceTimersByTimeAsync(100);

      await waitFor(() => {
        expect(screen.getByText('Mono Red Aggro')).toBeInTheDocument();
      });

      const deleteButtons = screen.getAllByRole('button', { name: 'Delete' });
      fireEvent.click(deleteButtons[0]);

      const overlay = document.querySelector('.modal-overlay');
      fireEvent.click(overlay!);

      await waitFor(() => {
        expect(screen.queryByText('Delete Deck')).not.toBeInTheDocument();
      });
    });

    it('should delete deck when confirming deletion', async () => {
      mockDecks.getDecks.mockResolvedValue(createMockDeckList());
      mockDecks.deleteDeck.mockResolvedValue(undefined);

      renderWithRouter(<Decks />);

      await vi.advanceTimersByTimeAsync(100);

      await waitFor(() => {
        expect(screen.getByText('Mono Red Aggro')).toBeInTheDocument();
      });

      const deleteButtons = screen.getAllByRole('button', { name: 'Delete' });
      fireEvent.click(deleteButtons[0]);

      // Click the confirm delete button in the modal (has class delete-button-confirm)
      const confirmDeleteButton = document.querySelector('.delete-button-confirm') as HTMLButtonElement;
      fireEvent.click(confirmDeleteButton);

      await waitFor(() => {
        expect(mockDecks.deleteDeck).toHaveBeenCalledWith('deck-1');
      });
    });

    it('should reload decks after successful deletion', async () => {
      mockDecks.getDecks.mockResolvedValueOnce(createMockDeckList());
      mockDecks.getDecks.mockResolvedValueOnce([
        createMockDeckListItem({ id: 'deck-2', name: 'Azorius Control' }),
      ]);
      mockDecks.deleteDeck.mockResolvedValue(undefined);

      renderWithRouter(<Decks />);

      await vi.advanceTimersByTimeAsync(100);

      await waitFor(() => {
        expect(screen.getByText('Mono Red Aggro')).toBeInTheDocument();
      });

      const deleteButtons = screen.getAllByRole('button', { name: 'Delete' });
      fireEvent.click(deleteButtons[0]);

      // Click the confirm delete button in the modal
      const confirmDeleteButton = document.querySelector('.delete-button-confirm') as HTMLButtonElement;
      fireEvent.click(confirmDeleteButton);

      await waitFor(() => {
        expect(mockDecks.getDecks).toHaveBeenCalledTimes(2);
      });
    });

    it('should show error alert when deletion fails', async () => {
      mockDecks.getDecks.mockResolvedValue(createMockDeckList());
      mockDecks.deleteDeck.mockRejectedValue(new Error('Deletion failed'));
      const alertMock = vi.spyOn(window, 'alert').mockImplementation(() => {});

      renderWithRouter(<Decks />);

      await vi.advanceTimersByTimeAsync(100);

      await waitFor(() => {
        expect(screen.getByText('Mono Red Aggro')).toBeInTheDocument();
      });

      const deleteButtons = screen.getAllByRole('button', { name: 'Delete' });
      fireEvent.click(deleteButtons[0]);

      // Click the confirm delete button in the modal
      const confirmDeleteButton = document.querySelector('.delete-button-confirm') as HTMLButtonElement;
      fireEvent.click(confirmDeleteButton);

      await waitFor(() => {
        expect(alertMock).toHaveBeenCalledWith('Deletion failed');
      });
      alertMock.mockRestore();
    });

    it('should display warning text in delete dialog', async () => {
      mockDecks.getDecks.mockResolvedValue(createMockDeckList());

      renderWithRouter(<Decks />);

      await vi.advanceTimersByTimeAsync(100);

      await waitFor(() => {
        expect(screen.getByText('Mono Red Aggro')).toBeInTheDocument();
      });

      const deleteButtons = screen.getAllByRole('button', { name: 'Delete' });
      fireEvent.click(deleteButtons[0]);

      expect(screen.getByText('This action cannot be undone.')).toBeInTheDocument();
    });
  });

  describe('Format Options', () => {
    it('should have all format options in create dialog', async () => {
      mockDecks.getDecks.mockResolvedValue(createMockDeckList());

      renderWithRouter(<Decks />);

      await vi.advanceTimersByTimeAsync(100);

      await waitFor(() => {
        expect(screen.getByText('+ Create New Deck')).toBeInTheDocument();
      });

      fireEvent.click(screen.getByText('+ Create New Deck'));

      const formatSelect = screen.getByLabelText('Format') as HTMLSelectElement;
      const options = Array.from(formatSelect.options).map((opt) => opt.value);

      expect(options).toContain('standard');
      expect(options).toContain('alchemy');
      expect(options).toContain('explorer');
      expect(options).toContain('historic');
      expect(options).toContain('timeless');
      expect(options).toContain('brawl');
      expect(options).toContain('limited');
    });
  });

  describe('Export Deck Dialog', () => {
    it('should open export dialog when clicking export button', async () => {
      mockDecks.getDecks.mockResolvedValue(createMockDeckList());

      renderWithRouter(<Decks />);

      await vi.advanceTimersByTimeAsync(100);

      await waitFor(() => {
        expect(screen.getByText('Mono Red Aggro')).toBeInTheDocument();
      });

      const exportButtons = screen.getAllByRole('button', { name: 'Export' });
      fireEvent.click(exportButtons[0]);

      expect(screen.getByRole('heading', { name: 'Export Deck' })).toBeInTheDocument();
      expect(screen.getByLabelText('Export Format')).toBeInTheDocument();
      expect(screen.getByText('Mono Red Aggro', { selector: 'strong' })).toBeInTheDocument();
    });

    it('should close export dialog when clicking cancel', async () => {
      mockDecks.getDecks.mockResolvedValue(createMockDeckList());

      renderWithRouter(<Decks />);

      await vi.advanceTimersByTimeAsync(100);

      await waitFor(() => {
        expect(screen.getByText('Mono Red Aggro')).toBeInTheDocument();
      });

      const exportButtons = screen.getAllByRole('button', { name: 'Export' });
      fireEvent.click(exportButtons[0]);

      expect(screen.getByText('Export Deck')).toBeInTheDocument();

      fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));

      await waitFor(() => {
        expect(screen.queryByText('Export Deck')).not.toBeInTheDocument();
      });
    });

    it('should close export dialog when clicking overlay', async () => {
      mockDecks.getDecks.mockResolvedValue(createMockDeckList());

      renderWithRouter(<Decks />);

      await vi.advanceTimersByTimeAsync(100);

      await waitFor(() => {
        expect(screen.getByText('Mono Red Aggro')).toBeInTheDocument();
      });

      const exportButtons = screen.getAllByRole('button', { name: 'Export' });
      fireEvent.click(exportButtons[0]);

      expect(screen.getByText('Export Deck')).toBeInTheDocument();

      const overlay = document.querySelector('.modal-overlay');
      fireEvent.click(overlay!);

      await waitFor(() => {
        expect(screen.queryByText('Export Deck')).not.toBeInTheDocument();
      });
    });

    it('should have all export format options available', async () => {
      mockDecks.getDecks.mockResolvedValue(createMockDeckList());

      renderWithRouter(<Decks />);

      await vi.advanceTimersByTimeAsync(100);

      await waitFor(() => {
        expect(screen.getByText('Mono Red Aggro')).toBeInTheDocument();
      });

      const exportButtons = screen.getAllByRole('button', { name: 'Export' });
      fireEvent.click(exportButtons[0]);

      const formatSelect = screen.getByLabelText('Export Format') as HTMLSelectElement;
      const options = Array.from(formatSelect.options).map((opt) => opt.value);

      expect(options).toContain('arena');
      expect(options).toContain('moxfield');
      expect(options).toContain('archidekt');
      expect(options).toContain('mtgo');
      expect(options).toContain('mtggoldfish');
      expect(options).toContain('plaintext');
    });

    it('should show format hint when format is selected', async () => {
      mockDecks.getDecks.mockResolvedValue(createMockDeckList());

      renderWithRouter(<Decks />);

      await vi.advanceTimersByTimeAsync(100);

      await waitFor(() => {
        expect(screen.getByText('Mono Red Aggro')).toBeInTheDocument();
      });

      const exportButtons = screen.getAllByRole('button', { name: 'Export' });
      fireEvent.click(exportButtons[0]);

      // Default is arena
      expect(screen.getByText('Standard MTGA import format with set codes')).toBeInTheDocument();

      // Change to moxfield
      const formatSelect = screen.getByLabelText('Export Format') as HTMLSelectElement;
      fireEvent.change(formatSelect, { target: { value: 'moxfield' } });

      expect(screen.getByText('Import directly into Moxfield')).toBeInTheDocument();
    });

    it('should export deck and call API with correct format', async () => {
      mockDecks.getDecks.mockResolvedValue(createMockDeckList());
      mockDecks.exportDeck.mockResolvedValue({
        content: 'Deck\n4 Lightning Bolt (STA) 1',
        filename: 'Mono_Red_Aggro.txt',
        error: '',
      });

      // Mock URL methods and link creation/click to prevent actual download
      const originalCreateObjectURL = URL.createObjectURL;
      const originalRevokeObjectURL = URL.revokeObjectURL;
      URL.createObjectURL = vi.fn(() => 'blob:test');
      URL.revokeObjectURL = vi.fn();

      renderWithRouter(<Decks />);

      await vi.advanceTimersByTimeAsync(100);

      await waitFor(() => {
        expect(screen.getByText('Mono Red Aggro')).toBeInTheDocument();
      });

      const exportButtons = screen.getAllByRole('button', { name: 'Export' });
      fireEvent.click(exportButtons[0]);

      fireEvent.click(screen.getByRole('button', { name: 'Download File' }));

      await waitFor(() => {
        expect(mockDecks.exportDeck).toHaveBeenCalledWith('deck-1', { format: 'arena' });
      });

      // Restore URL mocks
      URL.createObjectURL = originalCreateObjectURL;
      URL.revokeObjectURL = originalRevokeObjectURL;
    });

    it('should copy deck to clipboard', async () => {
      cleanup();
      mockDecks.getDecks.mockResolvedValue(createMockDeckList());
      mockDecks.exportDeck.mockResolvedValue({
        content: 'Deck\n4 Lightning Bolt (STA) 1',
        filename: 'Mono_Red_Aggro.txt',
        error: '',
      });

      const writeTextMock = vi.fn().mockResolvedValue(undefined);
      Object.assign(navigator, {
        clipboard: {
          writeText: writeTextMock,
        },
      });
      const alertMock = vi.spyOn(window, 'alert').mockImplementation(() => {});

      renderWithRouter(<Decks />);

      await vi.advanceTimersByTimeAsync(100);

      await waitFor(() => {
        expect(screen.getByText('Mono Red Aggro')).toBeInTheDocument();
      });

      const exportButtons = screen.getAllByRole('button', { name: 'Export' });
      fireEvent.click(exportButtons[0]);

      fireEvent.click(screen.getByRole('button', { name: 'Copy to Clipboard' }));

      await waitFor(() => {
        expect(mockDecks.exportDeck).toHaveBeenCalledWith('deck-1', { format: 'arena' });
      });

      await waitFor(() => {
        expect(writeTextMock).toHaveBeenCalledWith('Deck\n4 Lightning Bolt (STA) 1');
      });

      await waitFor(() => {
        expect(alertMock).toHaveBeenCalledWith('Deck copied to clipboard!');
      });

      alertMock.mockRestore();
    });

    it('should show error alert when export fails', async () => {
      cleanup();
      mockDecks.getDecks.mockResolvedValue(createMockDeckList());
      mockDecks.exportDeck.mockResolvedValue({
        content: '',
        filename: '',
        error: 'deck not found',
      });

      const alertMock = vi.spyOn(window, 'alert').mockImplementation(() => {});

      renderWithRouter(<Decks />);

      await vi.advanceTimersByTimeAsync(100);

      await waitFor(() => {
        expect(screen.getByText('Mono Red Aggro')).toBeInTheDocument();
      });

      const exportButtons = screen.getAllByRole('button', { name: 'Export' });
      fireEvent.click(exportButtons[0]);

      fireEvent.click(screen.getByRole('button', { name: 'Download File' }));

      await waitFor(() => {
        expect(alertMock).toHaveBeenCalledWith('Export failed: deck not found');
      });

      alertMock.mockRestore();
    });

    it('should disable buttons while exporting', async () => {
      cleanup();
      mockDecks.getDecks.mockResolvedValue(createMockDeckList());
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      let resolveExport: (value: any) => void;
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const exportPromise = new Promise<any>((resolve) => {
        resolveExport = resolve;
      });
      mockDecks.exportDeck.mockReturnValue(exportPromise);

      renderWithRouter(<Decks />);

      await vi.advanceTimersByTimeAsync(100);

      await waitFor(() => {
        expect(screen.getByText('Mono Red Aggro')).toBeInTheDocument();
      });

      const exportButtons = screen.getAllByRole('button', { name: 'Export' });
      fireEvent.click(exportButtons[0]);

      fireEvent.click(screen.getByRole('button', { name: 'Download File' }));

      await waitFor(() => {
        expect(screen.getByRole('button', { name: 'Exporting...' })).toBeDisabled();
        expect(screen.getByRole('button', { name: 'Copying...' })).toBeDisabled();
      });

      resolveExport!({
        content: 'test',
        filename: 'test.txt',
        error: '',
      });
    });

    it('should export with moxfield format when selected', async () => {
      cleanup();
      mockDecks.getDecks.mockResolvedValue(createMockDeckList());
      mockDecks.exportDeck.mockResolvedValue({
        content: 'Deck\n4 Lightning Bolt',
        filename: 'Mono_Red_Aggro_moxfield.txt',
        error: '',
      });

      const writeTextMock = vi.fn().mockResolvedValue(undefined);
      Object.assign(navigator, {
        clipboard: {
          writeText: writeTextMock,
        },
      });
      const alertMock = vi.spyOn(window, 'alert').mockImplementation(() => {});

      renderWithRouter(<Decks />);

      await vi.advanceTimersByTimeAsync(100);

      await waitFor(() => {
        expect(screen.getByText('Mono Red Aggro')).toBeInTheDocument();
      });

      const exportButtons = screen.getAllByRole('button', { name: 'Export' });
      fireEvent.click(exportButtons[0]);

      // Change format to moxfield
      const formatSelect = screen.getByLabelText('Export Format') as HTMLSelectElement;
      fireEvent.change(formatSelect, { target: { value: 'moxfield' } });

      fireEvent.click(screen.getByRole('button', { name: 'Copy to Clipboard' }));

      await waitFor(() => {
        expect(mockDecks.exportDeck).toHaveBeenCalledWith('deck-1', { format: 'moxfield' });
      });

      alertMock.mockRestore();
    });

    it('should export with archidekt format when selected', async () => {
      cleanup();
      mockDecks.getDecks.mockResolvedValue(createMockDeckList());
      mockDecks.exportDeck.mockResolvedValue({
        content: '4 Lightning Bolt',
        filename: 'Mono_Red_Aggro_archidekt.txt',
        error: '',
      });

      const writeTextMock = vi.fn().mockResolvedValue(undefined);
      Object.assign(navigator, {
        clipboard: {
          writeText: writeTextMock,
        },
      });
      const alertMock = vi.spyOn(window, 'alert').mockImplementation(() => {});

      renderWithRouter(<Decks />);

      await vi.advanceTimersByTimeAsync(100);

      await waitFor(() => {
        expect(screen.getByText('Mono Red Aggro')).toBeInTheDocument();
      });

      const exportButtons = screen.getAllByRole('button', { name: 'Export' });
      fireEvent.click(exportButtons[0]);

      // Change format to archidekt
      const formatSelect = screen.getByLabelText('Export Format') as HTMLSelectElement;
      fireEvent.change(formatSelect, { target: { value: 'archidekt' } });

      fireEvent.click(screen.getByRole('button', { name: 'Copy to Clipboard' }));

      await waitFor(() => {
        expect(mockDecks.exportDeck).toHaveBeenCalledWith('deck-1', { format: 'archidekt' });
      });

      alertMock.mockRestore();
    });

    it('should show warning banner when export has unknown cards', async () => {
      cleanup();
      mockDecks.getDecks.mockResolvedValue(createMockDeckList());
      mockDecks.exportDeck.mockResolvedValue({
        content: 'Deck\n4 Lightning Bolt (STA) 1\n2 Unknown Card (81181)',
        filename: 'Mono_Red_Aggro.txt',
        error: '',
        unknownCardIds: [81181, 81182],
        unknownCount: 2,
      });

      // Mock URL methods and link creation/click to prevent actual download
      const originalCreateObjectURL = URL.createObjectURL;
      const originalRevokeObjectURL = URL.revokeObjectURL;
      URL.createObjectURL = vi.fn(() => 'blob:test');
      URL.revokeObjectURL = vi.fn();

      renderWithRouter(<Decks />);

      await vi.advanceTimersByTimeAsync(100);

      await waitFor(() => {
        expect(screen.getByText('Mono Red Aggro')).toBeInTheDocument();
      });

      const exportButtons = screen.getAllByRole('button', { name: 'Export' });
      fireEvent.click(exportButtons[0]);

      fireEvent.click(screen.getByRole('button', { name: 'Download File' }));

      await waitFor(() => {
        expect(screen.getByText(/2 cards in "Mono Red Aggro"/)).toBeInTheDocument();
      });

      expect(screen.getByText(/could not be found/)).toBeInTheDocument();

      // Restore URL mocks
      URL.createObjectURL = originalCreateObjectURL;
      URL.revokeObjectURL = originalRevokeObjectURL;
    });

    it('should dismiss warning banner when clicking dismiss button', async () => {
      cleanup();
      mockDecks.getDecks.mockResolvedValue(createMockDeckList());
      mockDecks.exportDeck.mockResolvedValue({
        content: 'Deck\n4 Lightning Bolt (STA) 1',
        filename: 'Mono_Red_Aggro.txt',
        error: '',
        unknownCardIds: [81181],
        unknownCount: 1,
      });

      // Mock URL methods
      const originalCreateObjectURL = URL.createObjectURL;
      const originalRevokeObjectURL = URL.revokeObjectURL;
      URL.createObjectURL = vi.fn(() => 'blob:test');
      URL.revokeObjectURL = vi.fn();

      renderWithRouter(<Decks />);

      await vi.advanceTimersByTimeAsync(100);

      await waitFor(() => {
        expect(screen.getByText('Mono Red Aggro')).toBeInTheDocument();
      });

      const exportButtons = screen.getAllByRole('button', { name: 'Export' });
      fireEvent.click(exportButtons[0]);

      fireEvent.click(screen.getByRole('button', { name: 'Download File' }));

      await waitFor(() => {
        expect(screen.getByText(/1 card in "Mono Red Aggro"/)).toBeInTheDocument();
      });

      // Click dismiss button
      const dismissButton = screen.getByRole('button', { name: 'Dismiss warning' });
      fireEvent.click(dismissButton);

      await waitFor(() => {
        expect(screen.queryByText(/1 card in "Mono Red Aggro"/)).not.toBeInTheDocument();
      });

      // Restore URL mocks
      URL.createObjectURL = originalCreateObjectURL;
      URL.revokeObjectURL = originalRevokeObjectURL;
    });

    it('should show warning in alert when copying to clipboard with unknown cards', async () => {
      cleanup();
      mockDecks.getDecks.mockResolvedValue(createMockDeckList());
      mockDecks.exportDeck.mockResolvedValue({
        content: 'Deck\n4 Lightning Bolt (STA) 1',
        filename: 'Mono_Red_Aggro.txt',
        error: '',
        unknownCardIds: [81181, 81182, 81183],
        unknownCount: 3,
      });

      const writeTextMock = vi.fn().mockResolvedValue(undefined);
      Object.assign(navigator, {
        clipboard: {
          writeText: writeTextMock,
        },
      });
      const alertMock = vi.spyOn(window, 'alert').mockImplementation(() => {});

      renderWithRouter(<Decks />);

      await vi.advanceTimersByTimeAsync(100);

      await waitFor(() => {
        expect(screen.getByText('Mono Red Aggro')).toBeInTheDocument();
      });

      const exportButtons = screen.getAllByRole('button', { name: 'Export' });
      fireEvent.click(exportButtons[0]);

      fireEvent.click(screen.getByRole('button', { name: 'Copy to Clipboard' }));

      await waitFor(() => {
        expect(alertMock).toHaveBeenCalledWith(
          'Deck copied to clipboard! Note: 3 card(s) could not be found and are listed as "Unknown Card".'
        );
      });

      alertMock.mockRestore();
    });

    it('should not show warning when export has no unknown cards', async () => {
      cleanup();
      mockDecks.getDecks.mockResolvedValue(createMockDeckList());
      mockDecks.exportDeck.mockResolvedValue({
        content: 'Deck\n4 Lightning Bolt (STA) 1',
        filename: 'Mono_Red_Aggro.txt',
        error: '',
        unknownCardIds: [],
        unknownCount: 0,
      });

      // Mock URL methods
      const originalCreateObjectURL = URL.createObjectURL;
      const originalRevokeObjectURL = URL.revokeObjectURL;
      URL.createObjectURL = vi.fn(() => 'blob:test');
      URL.revokeObjectURL = vi.fn();

      renderWithRouter(<Decks />);

      await vi.advanceTimersByTimeAsync(100);

      await waitFor(() => {
        expect(screen.getByText('Mono Red Aggro')).toBeInTheDocument();
      });

      const exportButtons = screen.getAllByRole('button', { name: 'Export' });
      fireEvent.click(exportButtons[0]);

      fireEvent.click(screen.getByRole('button', { name: 'Download File' }));

      await waitFor(() => {
        expect(mockDecks.exportDeck).toHaveBeenCalled();
      });

      // Wait a bit to ensure no warning appears
      await vi.advanceTimersByTimeAsync(100);

      expect(screen.queryByText(/could not be found/)).not.toBeInTheDocument();

      // Restore URL mocks
      URL.createObjectURL = originalCreateObjectURL;
      URL.revokeObjectURL = originalRevokeObjectURL;
    });
  });

  describe('Win-rate NaN guard (D1)', () => {
    it('should display fallback "—" when winRate is absent from BFF payload', async () => {
      mockDecks.getDecks.mockResolvedValue([
        createMockDeckListItem({
          id: 'deck-nan',
          name: 'NaN Deck',
          matchesPlayed: 1,
          winRate: undefined,
        }),
      ]);

      renderWithRouter(<Decks />);
      await vi.advanceTimersByTimeAsync(100);

      await waitFor(() => {
        expect(screen.getByText('NaN Deck')).toBeInTheDocument();
      });

      const winRateEl = document.querySelector('[data-testid="deck-win-rate"]');
      expect(winRateEl?.textContent).not.toMatch(/NaN/);
      expect(winRateEl?.textContent).toMatch(/—/);
    });

    it('should display fallback "—" when winRate is null in BFF payload', async () => {
      mockDecks.getDecks.mockResolvedValue([
        createMockDeckListItem({
          id: 'deck-null-wr',
          name: 'Null WR Deck',
          matchesPlayed: 3,
          // eslint-disable-next-line @typescript-eslint/no-explicit-any
          winRate: null as any,
        }),
      ]);

      renderWithRouter(<Decks />);
      await vi.advanceTimersByTimeAsync(100);

      await waitFor(() => {
        expect(screen.getByText('Null WR Deck')).toBeInTheDocument();
      });

      const winRateEl = document.querySelector('[data-testid="deck-win-rate"]');
      expect(winRateEl?.textContent).not.toMatch(/NaN/);
      expect(winRateEl?.textContent).toMatch(/—/);
    });

    it('should display fallback "—" when winRate is NaN in BFF payload (0/0 scenario)', async () => {
      // winRate = NaN (e.g. 0/0 computed server-side and serialised as NaN)
      mockDecks.getDecks.mockResolvedValue([
        createMockDeckListItem({
          id: 'deck-nan-rate',
          name: 'Zero Zero Deck',
          matchesPlayed: 1,
          winRate: NaN,
        }),
      ]);

      renderWithRouter(<Decks />);
      await vi.advanceTimersByTimeAsync(100);

      await waitFor(() => {
        expect(screen.getByText('Zero Zero Deck')).toBeInTheDocument();
      });

      const winRateEl = document.querySelector('[data-testid="deck-win-rate"]');
      expect(winRateEl?.textContent).not.toMatch(/NaN/);
      expect(winRateEl?.textContent).toMatch(/—/);
    });

    it('should render "0%" for winRate = 0 in BFF payload (zero wins, has played)', async () => {
      mockDecks.getDecks.mockResolvedValue([
        createMockDeckListItem({
          id: 'deck-zero-wins',
          name: 'Zero Wins Deck',
          matchesPlayed: 5,
          winRate: 0,
        }),
      ]);

      renderWithRouter(<Decks />);
      await vi.advanceTimersByTimeAsync(100);

      await waitFor(() => {
        expect(screen.getByText('Zero Wins Deck')).toBeInTheDocument();
      });

      const winRateEl = document.querySelector('[data-testid="deck-win-rate"]');
      expect(winRateEl?.textContent).not.toMatch(/NaN/);
      expect(winRateEl?.textContent).toMatch(/0%/);
    });

    it('should render correct percentage for valid winRate in BFF payload', async () => {
      mockDecks.getDecks.mockResolvedValue([
        createMockDeckListItem({
          id: 'deck-valid',
          name: 'Valid WR Deck',
          matchesPlayed: 10,
          winRate: 0.7,
        }),
      ]);

      renderWithRouter(<Decks />);
      await vi.advanceTimersByTimeAsync(100);

      await waitFor(() => {
        expect(screen.getByText('Valid WR Deck')).toBeInTheDocument();
      });

      const winRateEl = document.querySelector('[data-testid="deck-win-rate"]');
      expect(winRateEl?.textContent).not.toMatch(/NaN/);
      expect(winRateEl?.textContent).toMatch(/70%/);
    });

    it('should not render win-rate row when matchesPlayed is 0 (existing guard)', async () => {
      mockDecks.getDecks.mockResolvedValue([
        createMockDeckListItem({
          id: 'deck-no-matches',
          name: 'No Matches Deck',
          matchesPlayed: 0,
          winRate: 0,
        }),
      ]);

      renderWithRouter(<Decks />);
      await vi.advanceTimersByTimeAsync(100);

      await waitFor(() => {
        expect(screen.getByText('No Matches Deck')).toBeInTheDocument();
      });

      expect(document.querySelector('[data-testid="deck-win-rate"]')).not.toBeInTheDocument();
    });

    it('DeckListItem deserializes BFF wire key "winRate" into matchWinRate', () => {
      // Real-wire deserialization test: constructs DeckListItem from a raw BFF-shaped
      // source object (using the actual JSON key "winRate") and asserts the class
      // property matchWinRate is populated correctly. This catches key-contract drift
      // between the BFF serializer (winRate) and SPA model (matchWinRate).
      const bffPayload = {
        id: 'wire-deck',
        name: 'Wire Shape Deck',
        format: 'standard',
        source: 'manual',
        matchesPlayed: 8,
        winRate: 0.625, // BFF key — NOT matchWinRate
        currentStreak: 1,
        cardCount: 60,
        modifiedAt: new Date('2024-01-15T10:00:00').toISOString(),
      };
      const item = new gui.DeckListItem(bffPayload);
      expect(item.matchWinRate).toBe(0.625);
    });

    it('DeckListItem renders 60% from BFF wire payload with winRate: 0.6', async () => {
      // End-to-end wire path: source object uses the real BFF key "winRate".
      // After deserialization, Decks.tsx should render "60%".
      mockDecks.getDecks.mockResolvedValue([
        new gui.DeckListItem({
          id: 'deck-wire-render',
          name: 'Wire Render Deck',
          format: 'standard',
          source: 'manual',
          matchesPlayed: 10,
          winRate: 0.6,
          currentStreak: 0,
          cardCount: 60,
          modifiedAt: new Date('2024-01-15T10:00:00').toISOString(),
        }),
      ]);

      renderWithRouter(<Decks />);
      await vi.advanceTimersByTimeAsync(100);

      await waitFor(() => {
        expect(screen.getByText('Wire Render Deck')).toBeInTheDocument();
      });

      const winRateEl = document.querySelector('[data-testid="deck-win-rate"]');
      expect(winRateEl?.textContent).not.toMatch(/NaN/);
      expect(winRateEl?.textContent).toMatch(/60%/);
    });

    it('renders correct win-rate from DeckListItem deserialized from BFF wire shape (winRate: 0.8)', async () => {
      // Verifies the full render path post-fix: getDecks() maps raw BFF JSON
      // through DeckListItem.createFrom() (see decks.ts getDecks fix), which
      // bridges the BFF wire key "winRate" to the SPA property "matchWinRate".
      // The service test in decks.test.ts validates the deserialization step;
      // this test validates the render step given a correctly deserialized item.
      mockDecks.getDecks.mockResolvedValue([
        gui.DeckListItem.createFrom({
          id: 'deck-deserialized',
          name: 'Deserialized Deck',
          format: 'standard',
          source: 'manual',
          matchesPlayed: 5,
          winRate: 0.8, // BFF wire key — createFrom() maps this to matchWinRate
          currentStreak: 0,
          cardCount: 60,
          modifiedAt: new Date('2024-01-15T10:00:00').toISOString(),
        }),
      ]);

      renderWithRouter(<Decks />);
      await vi.advanceTimersByTimeAsync(100);

      await waitFor(() => {
        expect(screen.getByText('Deserialized Deck')).toBeInTheDocument();
      });

      const winRateEl = document.querySelector('[data-testid="deck-win-rate"]');
      expect(winRateEl?.textContent).not.toMatch(/NaN/);
      expect(winRateEl?.textContent).toMatch(/80%/);
    });
  });

  describe('Streak badge NaN guard (DEFECT-1)', () => {
    it('does not render streak badge when currentStreak is undefined (field absent from BFF)', async () => {
      // The BFF deckListItemResponse does not include currentStreak.
      // DeckListItem constructor reads source["currentStreak"] which is undefined.
      // formatStreak(undefined) must return null so no badge renders.
      mockDecks.getDecks.mockResolvedValue([
        new gui.DeckListItem({
          id: 'deck-no-streak',
          name: 'No Streak Deck',
          format: 'standard',
          source: 'manual',
          matchesPlayed: 1,
          winRate: 0,
          cardCount: 60,
          modifiedAt: new Date('2024-01-15T10:00:00').toISOString(),
          // currentStreak intentionally absent — mirrors real BFF wire shape
        }),
      ]);

      renderWithRouter(<Decks />);
      await vi.advanceTimersByTimeAsync(100);

      await waitFor(() => {
        expect(screen.getByText('No Streak Deck')).toBeInTheDocument();
      });

      // No streak badge must render; specifically no "NaN" text anywhere
      expect(document.querySelector('[data-testid="deck-streak"]')).not.toBeInTheDocument();
      expect(document.body.textContent).not.toMatch(/NaN/);
    });

    it('does not render streak badge when currentStreak is null', async () => {
      mockDecks.getDecks.mockResolvedValue([
        new gui.DeckListItem({
          id: 'deck-null-streak',
          name: 'Null Streak Deck',
          format: 'standard',
          source: 'manual',
          matchesPlayed: 2,
          winRate: 0.5,
          cardCount: 60,
          modifiedAt: new Date('2024-01-15T10:00:00').toISOString(),
          // eslint-disable-next-line @typescript-eslint/no-explicit-any
          currentStreak: null as any,
        }),
      ]);

      renderWithRouter(<Decks />);
      await vi.advanceTimersByTimeAsync(100);

      await waitFor(() => {
        expect(screen.getByText('Null Streak Deck')).toBeInTheDocument();
      });

      expect(document.querySelector('[data-testid="deck-streak"]')).not.toBeInTheDocument();
      expect(document.body.textContent).not.toMatch(/NaN/);
    });

    it('does not render streak badge when currentStreak is NaN', async () => {
      mockDecks.getDecks.mockResolvedValue([
        createMockDeckListItem({
          id: 'deck-nan-streak',
          name: 'Zero Matches No Streak Deck',
          matchesPlayed: 3,
          // eslint-disable-next-line @typescript-eslint/no-explicit-any
          currentStreak: NaN as any,
        }),
      ]);

      renderWithRouter(<Decks />);
      await vi.advanceTimersByTimeAsync(100);

      await waitFor(() => {
        expect(screen.getByText('Zero Matches No Streak Deck')).toBeInTheDocument();
      });

      // No streak badge rendered — NaN streak must not produce a visible badge
      expect(document.querySelector('[data-testid="deck-streak"]')).not.toBeInTheDocument();
      // No stray "NaN" text anywhere in the rendered output
      expect(document.body.textContent).not.toMatch(/NaN/);
    });

    it('does not render streak badge when currentStreak is 0', async () => {
      mockDecks.getDecks.mockResolvedValue([
        createMockDeckListItem({
          id: 'deck-zero-streak',
          name: 'Zero Streak Deck',
          matchesPlayed: 5,
          currentStreak: 0,
        }),
      ]);

      renderWithRouter(<Decks />);
      await vi.advanceTimersByTimeAsync(100);

      await waitFor(() => {
        expect(screen.getByText('Zero Streak Deck')).toBeInTheDocument();
      });

      expect(document.querySelector('[data-testid="deck-streak"]')).not.toBeInTheDocument();
    });

    it('renders a win-streak badge when currentStreak is positive', async () => {
      mockDecks.getDecks.mockResolvedValue([
        createMockDeckListItem({
          id: 'deck-win-streak',
          name: 'Win Streak Deck',
          matchesPlayed: 5,
          currentStreak: 3,
        }),
      ]);

      renderWithRouter(<Decks />);
      await vi.advanceTimersByTimeAsync(100);

      await waitFor(() => {
        expect(screen.getByText('Win Streak Deck')).toBeInTheDocument();
      });

      const badge = document.querySelector('[data-testid="deck-streak"]');
      expect(badge).toBeInTheDocument();
      expect(badge?.textContent).toMatch(/3W/);
      expect(badge?.className).toMatch(/win-streak/);
      expect(document.body.textContent).not.toMatch(/NaN/);
    });

    it('renders a loss-streak badge when currentStreak is negative', async () => {
      mockDecks.getDecks.mockResolvedValue([
        createMockDeckListItem({
          id: 'deck-loss-streak',
          name: 'Loss Streak Deck',
          matchesPlayed: 4,
          currentStreak: -2,
        }),
      ]);

      renderWithRouter(<Decks />);
      await vi.advanceTimersByTimeAsync(100);

      await waitFor(() => {
        expect(screen.getByText('Loss Streak Deck')).toBeInTheDocument();
      });

      const badge = document.querySelector('[data-testid="deck-streak"]');
      expect(badge).toBeInTheDocument();
      expect(badge?.textContent).toMatch(/2L/);
      expect(badge?.className).toMatch(/loss-streak/);
      expect(document.body.textContent).not.toMatch(/NaN/);
    });
  });

  describe('data-testid coverage (DEFECT-3)', () => {
    it('deck cards render data-testid="deck-card-{id}"', async () => {
      mockDecks.getDecks.mockResolvedValue(createMockDeckList());

      renderWithRouter(<Decks />);
      await vi.advanceTimersByTimeAsync(100);

      await waitFor(() => {
        expect(screen.getByText('Mono Red Aggro')).toBeInTheDocument();
      });

      expect(document.querySelector('[data-testid="deck-card-deck-1"]')).toBeInTheDocument();
      expect(document.querySelector('[data-testid="deck-card-deck-2"]')).toBeInTheDocument();
      expect(document.querySelector('[data-testid="deck-card-deck-3"]')).toBeInTheDocument();
    });

    it('edit buttons render data-testid="deck-edit-{id}"', async () => {
      mockDecks.getDecks.mockResolvedValue(createMockDeckList());

      renderWithRouter(<Decks />);
      await vi.advanceTimersByTimeAsync(100);

      await waitFor(() => {
        expect(screen.getByText('Mono Red Aggro')).toBeInTheDocument();
      });

      expect(document.querySelector('[data-testid="deck-edit-deck-1"]')).toBeInTheDocument();
    });

    it('clicking deck-card navigates to /deck-builder/:id', async () => {
      mockDecks.getDecks.mockResolvedValue([
        createMockDeckListItem({ id: 'abc123', name: 'Click Me Deck' }),
      ]);

      renderWithRouter(<Decks />);
      await vi.advanceTimersByTimeAsync(100);

      await waitFor(() => {
        expect(screen.getByText('Click Me Deck')).toBeInTheDocument();
      });

      const card = document.querySelector('[data-testid="deck-card-abc123"]') as HTMLElement;
      fireEvent.click(card);

      expect(mockNavigate).toHaveBeenCalledWith('/deck-builder/abc123');
    });

    it('clicking deck-edit navigates to /deck-builder/:id', async () => {
      mockDecks.getDecks.mockResolvedValue([
        createMockDeckListItem({ id: 'def456', name: 'Edit Me Deck' }),
      ]);

      renderWithRouter(<Decks />);
      await vi.advanceTimersByTimeAsync(100);

      await waitFor(() => {
        expect(screen.getByText('Edit Me Deck')).toBeInTheDocument();
      });

      const editBtn = document.querySelector('[data-testid="deck-edit-def456"]') as HTMLElement;
      fireEvent.click(editBtn);

      expect(mockNavigate).toHaveBeenCalledWith('/deck-builder/def456');
    });
  });

  describe('Create Deck Modal — Format Select Positioning (#2011)', () => {
    it('AC1: Format select renders inside modal without scrolling ancestor', async () => {
      mockDecks.getDecks.mockResolvedValue(createMockDeckList());

      renderWithRouter(<Decks />);

      await vi.advanceTimersByTimeAsync(100);

      await waitFor(() => {
        expect(screen.getByText('+ Create New Deck')).toBeInTheDocument();
      });

      fireEvent.click(screen.getByText('+ Create New Deck'));

      const formatSelect = screen.getByLabelText('Format');
      expect(formatSelect).toBeInTheDocument();

      // AC1: The modal-content ancestor must NOT have overflow-y set to auto/scroll.
      // overflow-y on the containing block creates a new stacking context
      // that positions native <select> dropdowns incorrectly.
      const modalContent = document.querySelector('.modal-content') as HTMLElement;
      expect(modalContent).toBeInTheDocument();
      const computedStyle = window.getComputedStyle(modalContent);
      expect(computedStyle.overflowY).not.toBe('auto');
      expect(computedStyle.overflowY).not.toBe('scroll');
    });

    it('AC2: Selecting a format reflects the selection in the field', async () => {
      mockDecks.getDecks.mockResolvedValue(createMockDeckList());
      mockDecks.createDeck.mockResolvedValue({ ID: 'new-deck-id' });

      renderWithRouter(<Decks />);

      await vi.advanceTimersByTimeAsync(100);

      await waitFor(() => {
        expect(screen.getByText('+ Create New Deck')).toBeInTheDocument();
      });

      fireEvent.click(screen.getByText('+ Create New Deck'));

      const formatSelect = screen.getByLabelText('Format') as HTMLSelectElement;
      fireEvent.change(formatSelect, { target: { value: 'alchemy' } });
      expect(formatSelect.value).toBe('alchemy');

      // No layout shift — modal and form are still visible
      expect(screen.getByText('Create New Deck')).toBeInTheDocument();
      expect(screen.getByLabelText('Deck Name')).toBeInTheDocument();
    });

    it('AC3: Other modal behaviors are not regressed by the fix', async () => {
      mockDecks.getDecks.mockResolvedValue(createMockDeckList());
      mockDecks.createDeck.mockResolvedValue({ ID: 'new-deck-id' });

      renderWithRouter(<Decks />);

      await vi.advanceTimersByTimeAsync(100);

      await waitFor(() => {
        expect(screen.getByText('+ Create New Deck')).toBeInTheDocument();
      });

      // Open modal
      fireEvent.click(screen.getByText('+ Create New Deck'));
      expect(screen.getByText('Create New Deck')).toBeInTheDocument();

      // Cancel closes modal
      fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
      await waitFor(() => {
        expect(screen.queryByText('Create New Deck')).not.toBeInTheDocument();
      });

      // Re-open and submit creates deck
      fireEvent.click(screen.getByText('+ Create New Deck'));
      const nameInput = screen.getByLabelText('Deck Name');
      fireEvent.change(nameInput, { target: { value: 'AC3 Deck' } });
      fireEvent.click(screen.getByRole('button', { name: 'Create Deck' }));

      await waitFor(() => {
        expect(mockDecks.createDeck).toHaveBeenCalledWith({
          name: 'AC3 Deck',
          format: 'standard',
          source: 'manual',
        });
      });
    });
  });
});
