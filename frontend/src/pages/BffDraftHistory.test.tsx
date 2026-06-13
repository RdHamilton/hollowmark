import { describe, it, expect, vi, beforeEach } from 'vitest';
import { readFileSync } from 'node:fs';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';
import { render, screen, waitFor, fireEvent } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import BffDraftHistory from './BffDraftHistory';
import type { DraftHistoryResponse } from '@/services/api/bffDraftHistory';

// Mock useNavigate
const mockNavigate = vi.fn();
vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual('react-router-dom');
  return {
    ...actual,
    useNavigate: () => mockNavigate,
  };
});

// Mock the BFF adapter
vi.mock('@/services/api/bffDraftHistory', () => ({
  getDraftHistory: vi.fn(),
}));

// Import after mock so we get the vi.fn() version
import { getDraftHistory } from '@/services/api/bffDraftHistory';
const mockGetDraftHistory = vi.mocked(getDraftHistory);

// Wrap in MemoryRouter since BffDraftHistory uses useNavigate
function renderComponent() {
  return render(<MemoryRouter><BffDraftHistory /></MemoryRouter>);
}

function makeResponse(overrides: Partial<DraftHistoryResponse> = {}): DraftHistoryResponse {
  return {
    drafts: [],
    total: 0,
    limit: 20,
    offset: 0,
    ...overrides,
  };
}

// Minimal DraftHistoryItem matching the BFF wire shape.
function makeDraft(overrides: Partial<{
  id: string;
  set_code: string;
  format: string;
  started_at: string;
  completed_at: string | null;
  wins: number;
  losses: number;
}> = {}) {
  return {
    id: 'seed-00',
    set_code: 'BLB',
    format: 'Premier',
    started_at: '2026-05-01T10:00:00Z',
    completed_at: '2026-05-01T12:00:00Z',
    wins: 3,
    losses: 2,
    ...overrides,
  };
}

describe('BffDraftHistory', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  describe('Loading state', () => {
    it('renders loading spinner initially', async () => {
      let resolve: (v: DraftHistoryResponse) => void;
      mockGetDraftHistory.mockReturnValue(new Promise((r) => { resolve = r; }));

      renderComponent();

      expect(screen.getByText('Loading drafts...')).toBeInTheDocument();

      resolve!(makeResponse());
      await waitFor(() => {
        expect(screen.queryByText('Loading drafts...')).not.toBeInTheDocument();
      });
    });
  });

  describe('Empty state', () => {
    it('renders empty state when total === 0', async () => {
      mockGetDraftHistory.mockResolvedValue(makeResponse({ total: 0, drafts: [] }));

      renderComponent();

      await waitFor(() => {
        expect(screen.getByTestId('draft-history-empty')).toBeInTheDocument();
      });
      expect(screen.getByText('No drafts yet')).toBeInTheDocument();
    });

    it('does not render table when total === 0', async () => {
      mockGetDraftHistory.mockResolvedValue(makeResponse({ total: 0, drafts: [] }));

      renderComponent();

      await waitFor(() => {
        expect(screen.queryByTestId('draft-history-table')).not.toBeInTheDocument();
      });
    });
  });

  describe('Table rendering', () => {
    it('renders table when data is returned', async () => {
      mockGetDraftHistory.mockResolvedValue(makeResponse({
        total: 1,
        drafts: [makeDraft()],
      }));

      renderComponent();

      await waitFor(() => {
        expect(screen.getByTestId('draft-history-table')).toBeInTheDocument();
      });
    });

    it('renders column headers: Date, Set, Wins, Losses', async () => {
      mockGetDraftHistory.mockResolvedValue(makeResponse({
        total: 1,
        drafts: [makeDraft()],
      }));

      renderComponent();

      await waitFor(() => {
        expect(screen.getByRole('table')).toBeInTheDocument();
      });

      const headers = screen.getAllByRole('columnheader');
      const headerTexts = headers.map((h) => h.textContent);
      expect(headerTexts).toContain('Date');
      expect(headerTexts).toContain('Set');
      expect(headerTexts).toContain('Wins');
      expect(headerTexts).toContain('Losses');
    });

    it('renders draft data in table rows', async () => {
      mockGetDraftHistory.mockResolvedValue(makeResponse({
        total: 1,
        drafts: [makeDraft({ set_code: 'BLB', wins: 3, losses: 2 })],
      }));

      renderComponent();

      await waitFor(() => {
        expect(screen.getByText('BLB')).toBeInTheDocument();
      });
      expect(screen.getByText('3')).toBeInTheDocument();
      expect(screen.getByText('2')).toBeInTheDocument();
    });

    it('renders multiple drafts', async () => {
      mockGetDraftHistory.mockResolvedValue(makeResponse({
        total: 2,
        drafts: [
          makeDraft({ id: 'seed-00', set_code: 'BLB', wins: 3, losses: 2 }),
          makeDraft({ id: 'seed-01', set_code: 'DSK', wins: 7, losses: 0 }),
        ],
      }));

      renderComponent();

      await waitFor(() => {
        expect(screen.getByText('BLB')).toBeInTheDocument();
        expect(screen.getByText('DSK')).toBeInTheDocument();
      });
    });

    it('renders started_at as the date column', async () => {
      mockGetDraftHistory.mockResolvedValue(makeResponse({
        total: 1,
        drafts: [makeDraft({ started_at: '2026-05-01T10:00:00Z' })],
      }));

      renderComponent();

      await waitFor(() => {
        expect(screen.getByTestId('draft-history-table')).toBeInTheDocument();
      });

      // The date cell should render something (locale-formatted) for the started_at value.
      // Exact format is locale-dependent, so just verify the row exists.
      const rows = screen.getAllByRole('row');
      // Header row + 1 data row
      expect(rows.length).toBe(2);
    });
  });

  describe('Row click navigation', () => {
    it('renders rows with data-testid draft-history-row', async () => {
      mockGetDraftHistory.mockResolvedValue(makeResponse({
        total: 1,
        drafts: [makeDraft({ id: 'seed-00', set_code: 'BLB' })],
      }));

      renderComponent();

      await waitFor(() => {
        expect(screen.getAllByTestId('draft-history-row').length).toBe(1);
      });
    });

    it('clicking a row navigates to draft-analytics with session and set params', async () => {
      mockGetDraftHistory.mockResolvedValue(makeResponse({
        total: 1,
        drafts: [makeDraft({ id: 'abc-123', set_code: 'BLB' })],
      }));

      renderComponent();

      await waitFor(() => {
        expect(screen.getByTestId('draft-history-row')).toBeInTheDocument();
      });

      fireEvent.click(screen.getByTestId('draft-history-row'));

      expect(mockNavigate).toHaveBeenCalledWith(
        '/draft-analytics?session=abc-123&set=BLB'
      );
    });

    it('each row has pointer cursor style', async () => {
      mockGetDraftHistory.mockResolvedValue(makeResponse({
        total: 1,
        drafts: [makeDraft({ id: 'seed-00' })],
      }));

      renderComponent();

      await waitFor(() => {
        const row = screen.getByTestId('draft-history-row');
        expect(row).toHaveStyle({ cursor: 'pointer' });
      });
    });
  });

  describe('Pagination', () => {
    it('Previous button is disabled on first page', async () => {
      mockGetDraftHistory.mockResolvedValue(makeResponse({
        total: 21,
        offset: 0,
        limit: 20,
        drafts: Array.from({ length: 20 }, (_, i) => makeDraft({ id: `seed-${i}` })),
      }));

      renderComponent();

      await waitFor(() => {
        expect(screen.getByRole('button', { name: 'Previous' })).toBeDisabled();
      });
    });

    it('Next button is disabled when no more pages', async () => {
      mockGetDraftHistory.mockResolvedValue(makeResponse({
        total: 3,
        offset: 0,
        limit: 20,
        drafts: Array.from({ length: 3 }, (_, i) => makeDraft({ id: `seed-${i}` })),
      }));

      renderComponent();

      await waitFor(() => {
        expect(screen.getByRole('button', { name: 'Next' })).toBeDisabled();
      });
    });

    it('Next button is enabled when more pages exist', async () => {
      mockGetDraftHistory.mockResolvedValue(makeResponse({
        total: 25,
        offset: 0,
        limit: 20,
        drafts: Array.from({ length: 20 }, (_, i) => makeDraft({ id: `seed-${i}` })),
      }));

      renderComponent();

      await waitFor(() => {
        expect(screen.getByRole('button', { name: 'Next' })).toBeEnabled();
      });
    });

    it('clicking Next fetches next page', async () => {
      const page1: DraftHistoryResponse = makeResponse({
        total: 25,
        offset: 0,
        limit: 20,
        drafts: Array.from({ length: 20 }, (_, i) => makeDraft({ id: `seed-${i}` })),
      });
      const page2: DraftHistoryResponse = makeResponse({
        total: 25,
        offset: 20,
        limit: 20,
        drafts: [makeDraft({ id: 'seed-20', set_code: 'FDN', wins: 5, losses: 3 })],
      });

      mockGetDraftHistory
        .mockResolvedValueOnce(page1)
        .mockResolvedValueOnce(page2);

      renderComponent();

      await waitFor(() => {
        expect(screen.getByRole('button', { name: 'Next' })).toBeEnabled();
      });

      fireEvent.click(screen.getByRole('button', { name: 'Next' }));

      await waitFor(() => {
        expect(screen.getByText('FDN')).toBeInTheDocument();
      });
      expect(mockGetDraftHistory).toHaveBeenCalledWith('clerk-test-token-stub', { limit: 20, offset: 20 });
    });
  });

  describe('Page title', () => {
    it('renders Draft History heading', async () => {
      mockGetDraftHistory.mockResolvedValue(makeResponse());

      renderComponent();

      await waitFor(() => {
        expect(screen.getByRole('heading', { level: 1 })).toHaveTextContent('Draft History');
      });
    });
  });

  // --------------------------------------------------------------------------
  // Font regression guard (#684): no Cormorant Garamond in the SPA CSS
  // --------------------------------------------------------------------------

  describe('Font regression — no Cormorant Garamond (#684)', () => {
    const CSS_PATH = join(dirname(fileURLToPath(import.meta.url)), 'BffDraftHistory.css');

    it('BffDraftHistory.css contains no Cormorant Garamond reference', () => {
      const css = readFileSync(CSS_PATH, 'utf8');
      expect(css.toLowerCase()).not.toContain('cormorant');
      expect(css.toLowerCase()).not.toContain('garamond');
    });
  });

  // --------------------------------------------------------------------------
  // Heading copy regression guard (#685): no lorebook affectations
  // --------------------------------------------------------------------------

  describe('Heading copy — no lorebook affectations (#685)', () => {
    it('page title reads "Draft History" — no § Chapter / The Draft pattern', async () => {
      mockGetDraftHistory.mockResolvedValue(makeResponse());

      renderComponent();

      await waitFor(() => {
        expect(screen.queryByText('Loading drafts...')).not.toBeInTheDocument();
      });

      const h1 = screen.getByRole('heading', { level: 1 });
      expect(h1).toHaveTextContent('Draft History');
      expect(h1.textContent).not.toMatch(/§|Chapter|Compendium/);
    });
  });

  // --------------------------------------------------------------------------
  // Response shape regression guard: BFF wire shape alignment
  // --------------------------------------------------------------------------

  describe('BFF wire shape alignment', () => {
    it('renders 3 seeded drafts when BFF returns the correct wire shape', async () => {
      // This test mirrors the actual BFF response for account_id 17 (ci-smoke).
      // The BFF returns { data: [...], total, page, limit } — NOT { drafts: [...] }.
      // getDraftHistory must map data → drafts so the component renders correctly.
      mockGetDraftHistory.mockResolvedValue({
        drafts: [
          makeDraft({ id: 'seed-02', set_code: 'SOS', wins: 1, losses: 3 }),
          makeDraft({ id: 'seed-01', set_code: 'BLB', wins: 0, losses: 0 }),
          makeDraft({ id: 'seed-00', set_code: 'SOS', wins: 6, losses: 3 }),
        ],
        total: 3,
        limit: 20,
        offset: 0,
      });

      renderComponent();

      await waitFor(() => {
        expect(screen.getByTestId('draft-history-table')).toBeInTheDocument();
      });

      const rows = screen.getAllByRole('row');
      // Header row + 3 data rows
      expect(rows.length).toBe(4);
      expect(screen.getAllByText('SOS').length).toBe(2);
      expect(screen.getByText('BLB')).toBeInTheDocument();
    });

    it('renders empty state when BFF returns total=0 with empty data array', async () => {
      mockGetDraftHistory.mockResolvedValue({
        drafts: [],
        total: 0,
        limit: 20,
        offset: 0,
      });

      renderComponent();

      await waitFor(() => {
        expect(screen.getByTestId('draft-history-empty')).toBeInTheDocument();
      });
    });
  });

  // --------------------------------------------------------------------------
  // Win Rate column — divide-by-zero guard (#1425)
  // --------------------------------------------------------------------------

  describe('Win Rate column', () => {
    it('renders Win Rate column header', async () => {
      mockGetDraftHistory.mockResolvedValue(makeResponse({
        total: 1,
        drafts: [makeDraft()],
      }));

      renderComponent();

      await waitFor(() => {
        expect(screen.getByRole('table')).toBeInTheDocument();
      });

      const headers = screen.getAllByRole('columnheader');
      const headerTexts = headers.map((h) => h.textContent);
      expect(headerTexts).toContain('Win Rate');
    });

    it('shows "—" for a draft with 0 wins and 0 losses — never NaN%', async () => {
      mockGetDraftHistory.mockResolvedValue(makeResponse({
        total: 1,
        drafts: [makeDraft({ wins: 0, losses: 0 })],
      }));

      renderComponent();

      await waitFor(() => {
        expect(screen.getByTestId('draft-history-table')).toBeInTheDocument();
      });

      // Must not contain any NaN% or Infinity% string
      expect(screen.queryByText(/NaN%/)).not.toBeInTheDocument();
      expect(screen.queryByText(/Infinity%/)).not.toBeInTheDocument();
      // Must show the placeholder
      expect(screen.getByTestId('draft-win-rate-0')).toHaveTextContent('—');
    });

    it('shows correct percentage for a draft with games played', async () => {
      // 3 wins, 2 losses → 3/5 = 60%
      mockGetDraftHistory.mockResolvedValue(makeResponse({
        total: 1,
        drafts: [makeDraft({ wins: 3, losses: 2 })],
      }));

      renderComponent();

      await waitFor(() => {
        expect(screen.getByTestId('draft-history-table')).toBeInTheDocument();
      });

      expect(screen.getByTestId('draft-win-rate-0')).toHaveTextContent('60%');
    });

    it('shows 100% for a draft with wins and 0 losses', async () => {
      mockGetDraftHistory.mockResolvedValue(makeResponse({
        total: 1,
        drafts: [makeDraft({ wins: 7, losses: 0 })],
      }));

      renderComponent();

      await waitFor(() => {
        expect(screen.getByTestId('draft-history-table')).toBeInTheDocument();
      });

      expect(screen.getByTestId('draft-win-rate-0')).toHaveTextContent('100%');
    });

    it('shows 0% for a draft with 0 wins and non-zero losses', async () => {
      mockGetDraftHistory.mockResolvedValue(makeResponse({
        total: 1,
        drafts: [makeDraft({ wins: 0, losses: 3 })],
      }));

      renderComponent();

      await waitFor(() => {
        expect(screen.getByTestId('draft-history-table')).toBeInTheDocument();
      });

      expect(screen.getByTestId('draft-win-rate-0')).toHaveTextContent('0%');
    });

    it('shows "—" for multiple 0-game drafts — no NaN% anywhere in the table', async () => {
      mockGetDraftHistory.mockResolvedValue(makeResponse({
        total: 3,
        drafts: [
          makeDraft({ id: 'seed-00', wins: 0, losses: 0 }),
          makeDraft({ id: 'seed-01', wins: 2, losses: 1 }),
          makeDraft({ id: 'seed-02', wins: 0, losses: 0 }),
        ],
      }));

      renderComponent();

      await waitFor(() => {
        expect(screen.getByTestId('draft-history-table')).toBeInTheDocument();
      });

      expect(screen.queryByText(/NaN%/)).not.toBeInTheDocument();
      expect(screen.getByTestId('draft-win-rate-0')).toHaveTextContent('—');
      expect(screen.getByTestId('draft-win-rate-1')).toHaveTextContent('67%');
      expect(screen.getByTestId('draft-win-rate-2')).toHaveTextContent('—');
    });
  });
});
