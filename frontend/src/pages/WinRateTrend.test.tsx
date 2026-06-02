import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor, fireEvent } from '@testing-library/react';
import WinRateTrend from './WinRateTrend';
import { mockMatches } from '@/test/mocks/apiMock';
import { AppProvider } from '../context/AppContext';
// storage types imported but using any for mock flexibility

// Mock Recharts to avoid rendering issues in tests
vi.mock('recharts', () => ({
  ResponsiveContainer: ({ children }: { children: React.ReactNode }) => (
    <div data-testid="responsive-container">{children}</div>
  ),
  LineChart: ({ children, data }: { children: React.ReactNode; data: unknown[] }) => (
    <div data-testid="line-chart" data-chart-data={JSON.stringify(data)}>
      {children}
    </div>
  ),
  BarChart: ({ children, data }: { children: React.ReactNode; data: unknown[] }) => (
    <div data-testid="bar-chart" data-chart-data={JSON.stringify(data)}>
      {children}
    </div>
  ),
  Line: () => <div data-testid="line" />,
  Bar: () => <div data-testid="bar" />,
  XAxis: () => <div data-testid="x-axis" />,
  YAxis: () => <div data-testid="y-axis" />,
  CartesianGrid: () => <div data-testid="cartesian-grid" />,
  Tooltip: () => <div data-testid="tooltip" />,
  Legend: () => <div data-testid="legend" />,
  ReferenceLine: ({ 'data-testid': testId }: { 'data-testid'?: string }) => (
    <div data-testid={testId ?? 'reference-line'} />
  ),
}));

// Helper function to create mock trend analysis data.
// IMPORTANT: uses the real BFF wire-format key "Trends" (NOT "Periods").
// storage.TrendAnalysis.constructor reads source["Trends"]; if it regresses to
// source["Periods"] this fixture will produce an empty chart (contract test invariant).
// eslint-disable-next-line @typescript-eslint/no-explicit-any
function createMockTrendAnalysis(overrides: Record<string, unknown> = {}): any {
  return {
    Trends: [
      {
        Period: {
          Label: 'Day 1',
          StartDate: '2024-01-01',
          EndDate: '2024-01-01',
        },
        Stats: {
          TotalMatches: 10,
          MatchesWon: 6,
          MatchesLost: 4,
          TotalGames: 20,
          GamesWon: 12,
          GamesLost: 8,
          WinRate: 0.6,
        },
        WinRate: 0.6,
      },
      {
        Period: {
          Label: 'Day 2',
          StartDate: '2024-01-02',
          EndDate: '2024-01-02',
        },
        Stats: {
          TotalMatches: 8,
          MatchesWon: 5,
          MatchesLost: 3,
          TotalGames: 16,
          GamesWon: 10,
          GamesLost: 6,
          WinRate: 0.625,
        },
        WinRate: 0.625,
      },
    ],
    Overall: {
      TotalMatches: 18,
      MatchesWon: 11,
      MatchesLost: 7,
      TotalGames: 36,
      GamesWon: 22,
      GamesLost: 14,
      WinRate: 0.611,
    },
    Trend: 'improving',
    TrendValue: 0.025,
    ...overrides,
  };
}

// Wrapper component with AppProvider
function renderWithProvider(ui: React.ReactElement) {
  return render(<AppProvider>{ui}</AppProvider>);
}

describe('WinRateTrend', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    localStorage.clear();
  });

  describe('Loading State', () => {
    it('should show loading spinner while fetching data', async () => {
      // Create a promise that won't resolve immediately
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      let resolvePromise: (value: any) => void;
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const loadingPromise = new Promise<any>((resolve) => {
        resolvePromise = resolve;
      });
      mockMatches.getTrendAnalysis.mockReturnValue(loadingPromise);

      renderWithProvider(<WinRateTrend />);

      expect(screen.getByText('Loading trend data...')).toBeInTheDocument();

      // Resolve to clean up
      resolvePromise!(createMockTrendAnalysis());
      await waitFor(() => {
        expect(screen.queryByText('Loading trend data...')).not.toBeInTheDocument();
      });
    });
  });

  describe('Error State', () => {
    it('should show error state when API fails', async () => {
      mockMatches.getTrendAnalysis.mockRejectedValue(new Error('Network error'));

      renderWithProvider(<WinRateTrend />);

      await waitFor(() => {
        expect(screen.getByText('Failed to load trend data')).toBeInTheDocument();
      });
      expect(screen.getByText('Network error')).toBeInTheDocument();
    });

    it('should show generic error message for non-Error rejections', async () => {
      mockMatches.getTrendAnalysis.mockRejectedValue('Unknown error');

      renderWithProvider(<WinRateTrend />);

      await waitFor(() => {
        expect(screen.getByRole('heading', { name: 'Failed to load trend data' })).toBeInTheDocument();
      });
    });
  });

  describe('Empty State', () => {
    it('should show empty state when no analysis data', async () => {
      mockMatches.getTrendAnalysis.mockResolvedValue(null);

      renderWithProvider(<WinRateTrend />);

      await waitFor(() => {
        expect(screen.getByText('Not enough data')).toBeInTheDocument();
      });
      expect(
        screen.getByText('Play at least 5 matches to see your win rate trends over time.')
      ).toBeInTheDocument();
    });

    it('should show empty state when periods array is empty', async () => {
      mockMatches.getTrendAnalysis.mockResolvedValue({
        Trends: [],
        Overall: null,
        Trend: '',
        TrendValue: 0,
      });

      renderWithProvider(<WinRateTrend />);

      await waitFor(() => {
        expect(screen.getByText('Not enough data')).toBeInTheDocument();
      });
    });
  });

  describe('Data Display', () => {
    it('should render line chart by default', async () => {
      mockMatches.getTrendAnalysis.mockResolvedValue(createMockTrendAnalysis());

      renderWithProvider(<WinRateTrend />);

      await waitFor(() => {
        expect(screen.getByTestId('line-chart')).toBeInTheDocument();
      });
      expect(screen.queryByTestId('bar-chart')).not.toBeInTheDocument();
    });

    it('should display summary information', async () => {
      mockMatches.getTrendAnalysis.mockResolvedValue(createMockTrendAnalysis());

      renderWithProvider(<WinRateTrend />);

      await waitFor(() => {
        expect(screen.getByText('Win Rate Trend Analysis')).toBeInTheDocument();
      });
      expect(screen.getByText('Period:')).toBeInTheDocument();
      expect(screen.getByText('Format:')).toBeInTheDocument();
      expect(screen.getByText('Trend:')).toBeInTheDocument();
      expect(screen.getByText('Overall Win Rate:')).toBeInTheDocument();
    });

    it('should display trend with correct value', async () => {
      mockMatches.getTrendAnalysis.mockResolvedValue(createMockTrendAnalysis());

      renderWithProvider(<WinRateTrend />);

      await waitFor(() => {
        expect(screen.getByText(/improving/)).toBeInTheDocument();
      });
    });

    it('should display overall win rate', async () => {
      mockMatches.getTrendAnalysis.mockResolvedValue(createMockTrendAnalysis());

      renderWithProvider(<WinRateTrend />);

      await waitFor(() => {
        expect(screen.getByText(/61\.1%/)).toBeInTheDocument();
        expect(screen.getByText(/18 matches/)).toBeInTheDocument();
      });
    });

    it('should transform data correctly for chart', async () => {
      mockMatches.getTrendAnalysis.mockResolvedValue(createMockTrendAnalysis());

      renderWithProvider(<WinRateTrend />);

      await waitFor(() => {
        const lineChart = screen.getByTestId('line-chart');
        const chartData = JSON.parse(lineChart.getAttribute('data-chart-data') || '[]');
        expect(chartData).toHaveLength(2);
        expect(chartData[0].name).toBe('Day 1');
        expect(chartData[0].winRate).toBe(60);
        expect(chartData[0].matches).toBe(10);
      });
    });
  });

  describe('Filters', () => {
    // Helper to get select by finding the label then the next select sibling
    function getSelectByLabel(labelText: string): HTMLSelectElement {
      const label = screen.getByText(labelText);
      const filterGroup = label.closest('.filter-group');
      return filterGroup?.querySelector('select') as HTMLSelectElement;
    }

    it('should render date range filter with default value', async () => {
      mockMatches.getTrendAnalysis.mockResolvedValue(createMockTrendAnalysis());

      renderWithProvider(<WinRateTrend />);

      await waitFor(() => {
        expect(screen.getByTestId('line-chart')).toBeInTheDocument();
      });

      const dateRangeSelect = getSelectByLabel('Date Range');
      expect(dateRangeSelect.value).toBe('7days');
    });

    it('should render format filter with default value', async () => {
      mockMatches.getTrendAnalysis.mockResolvedValue(createMockTrendAnalysis());

      renderWithProvider(<WinRateTrend />);

      await waitFor(() => {
        expect(screen.getByTestId('line-chart')).toBeInTheDocument();
      });

      const formatSelect = getSelectByLabel('Format');
      expect(formatSelect.value).toBe('all');
    });

    it('should render chart type filter with default value', async () => {
      mockMatches.getTrendAnalysis.mockResolvedValue(createMockTrendAnalysis());

      renderWithProvider(<WinRateTrend />);

      await waitFor(() => {
        expect(screen.getByTestId('line-chart')).toBeInTheDocument();
      });

      const chartTypeSelect = getSelectByLabel('Chart Type');
      expect(chartTypeSelect.value).toBe('line');
    });

    it('should update date range when filter changes', async () => {
      mockMatches.getTrendAnalysis.mockResolvedValue(createMockTrendAnalysis());

      renderWithProvider(<WinRateTrend />);

      await waitFor(() => {
        expect(screen.getByTestId('line-chart')).toBeInTheDocument();
      });

      const dateRangeSelect = getSelectByLabel('Date Range');
      fireEvent.change(dateRangeSelect, { target: { value: '30days' } });

      await waitFor(() => {
        expect(dateRangeSelect.value).toBe('30days');
      });
    });

    it('should update format when filter changes', async () => {
      mockMatches.getTrendAnalysis.mockResolvedValue(createMockTrendAnalysis());

      renderWithProvider(<WinRateTrend />);

      await waitFor(() => {
        expect(screen.getByTestId('line-chart')).toBeInTheDocument();
      });

      const formatSelect = getSelectByLabel('Format');
      fireEvent.change(formatSelect, { target: { value: 'Ladder' } });

      await waitFor(() => {
        expect(formatSelect.value).toBe('Ladder');
      });
    });

    it('should switch to bar chart when chart type changes', async () => {
      mockMatches.getTrendAnalysis.mockResolvedValue(createMockTrendAnalysis());

      renderWithProvider(<WinRateTrend />);

      await waitFor(() => {
        expect(screen.getByTestId('line-chart')).toBeInTheDocument();
      });

      const chartTypeSelect = getSelectByLabel('Chart Type');
      fireEvent.change(chartTypeSelect, { target: { value: 'bar' } });

      await waitFor(() => {
        expect(screen.getByTestId('bar-chart')).toBeInTheDocument();
      });
      expect(screen.queryByTestId('line-chart')).not.toBeInTheDocument();
    });
  });

  describe('API Calls', () => {
    // Helper to get select by finding the label then the next select sibling
    function getSelectByLabel(labelText: string): HTMLSelectElement {
      const label = screen.getByText(labelText);
      const filterGroup = label.closest('.filter-group');
      return filterGroup?.querySelector('select') as HTMLSelectElement;
    }

    it('should call GetTrendAnalysis with correct parameters for 7days', async () => {
      mockMatches.getTrendAnalysis.mockResolvedValue(createMockTrendAnalysis());

      renderWithProvider(<WinRateTrend />);

      await waitFor(() => {
        expect(mockMatches.getTrendAnalysis).toHaveBeenCalled();
      });

      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const calls = mockMatches.getTrendAnalysis.mock.calls as any[][];
      const call = calls[0][0];
      expect(call.periodType).toBe('day'); // AC3: periodType for 7days must be 'day', not 'daily'
      expect(call.formats).toBeUndefined(); // formats for 'all' is undefined
    });

    it('should refetch data when date range changes', async () => {
      mockMatches.getTrendAnalysis.mockResolvedValue(createMockTrendAnalysis());

      renderWithProvider(<WinRateTrend />);

      await waitFor(() => {
        expect(screen.getByTestId('line-chart')).toBeInTheDocument();
      });

      const dateRangeSelect = getSelectByLabel('Date Range');
      fireEvent.change(dateRangeSelect, { target: { value: '30days' } });

      await waitFor(() => {
        expect(mockMatches.getTrendAnalysis).toHaveBeenCalledTimes(2);
      });
    });

    it('should refetch data when format changes', async () => {
      mockMatches.getTrendAnalysis.mockResolvedValue(createMockTrendAnalysis());

      renderWithProvider(<WinRateTrend />);

      await waitFor(() => {
        expect(screen.getByTestId('line-chart')).toBeInTheDocument();
      });

      const formatSelect = getSelectByLabel('Format');
      fireEvent.change(formatSelect, { target: { value: 'Ladder' } });

      await waitFor(() => {
        expect(mockMatches.getTrendAnalysis).toHaveBeenCalledTimes(2);
      });
    });

    it('should send period type "day" for 7days date range (AC3)', async () => {
      mockMatches.getTrendAnalysis.mockResolvedValue(createMockTrendAnalysis());
      renderWithProvider(<WinRateTrend />);
      await waitFor(() => expect(mockMatches.getTrendAnalysis).toHaveBeenCalled());
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const calls = mockMatches.getTrendAnalysis.mock.calls as any[][];
      expect(calls[0][0].periodType).toBe('day');
    });

    it('should send period type "week" for 30days date range (AC3)', async () => {
      mockMatches.getTrendAnalysis.mockResolvedValue(createMockTrendAnalysis());
      renderWithProvider(<WinRateTrend />);

      await waitFor(() => expect(screen.getByTestId('line-chart')).toBeInTheDocument());

      const dateRangeSelect = (screen.getByText('Date Range').closest('.filter-group') as HTMLElement)?.querySelector('select') as HTMLSelectElement;
      fireEvent.change(dateRangeSelect, { target: { value: '30days' } });

      await waitFor(() => expect(mockMatches.getTrendAnalysis).toHaveBeenCalledTimes(2));
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const calls = mockMatches.getTrendAnalysis.mock.calls as any[][];
      expect(calls[1][0].periodType).toBe('week');
    });

    it('should send period type "week" for 90days date range (AC3)', async () => {
      mockMatches.getTrendAnalysis.mockResolvedValue(createMockTrendAnalysis());
      renderWithProvider(<WinRateTrend />);

      await waitFor(() => expect(screen.getByTestId('line-chart')).toBeInTheDocument());

      const dateRangeSelect = (screen.getByText('Date Range').closest('.filter-group') as HTMLElement)?.querySelector('select') as HTMLSelectElement;
      fireEvent.change(dateRangeSelect, { target: { value: '90days' } });

      await waitFor(() => expect(mockMatches.getTrendAnalysis).toHaveBeenCalledTimes(2));
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const calls = mockMatches.getTrendAnalysis.mock.calls as any[][];
      expect(calls[1][0].periodType).toBe('week');
    });

    it('should send period type "month" for all-time date range (AC3)', async () => {
      mockMatches.getTrendAnalysis.mockResolvedValue(createMockTrendAnalysis());
      renderWithProvider(<WinRateTrend />);

      await waitFor(() => expect(screen.getByTestId('line-chart')).toBeInTheDocument());

      const dateRangeSelect = (screen.getByText('Date Range').closest('.filter-group') as HTMLElement)?.querySelector('select') as HTMLSelectElement;
      fireEvent.change(dateRangeSelect, { target: { value: 'all' } });

      await waitFor(() => expect(mockMatches.getTrendAnalysis).toHaveBeenCalledTimes(2));
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const calls = mockMatches.getTrendAnalysis.mock.calls as any[][];
      expect(calls[1][0].periodType).toBe('month');
    });

    it('should never send "daily", "weekly", or "monthly" as periodType (AC3)', async () => {
      mockMatches.getTrendAnalysis.mockResolvedValue(createMockTrendAnalysis());
      renderWithProvider(<WinRateTrend />);

      await waitFor(() => expect(screen.getByTestId('line-chart')).toBeInTheDocument());

      const dateRangeSelect = (screen.getByText('Date Range').closest('.filter-group') as HTMLElement)?.querySelector('select') as HTMLSelectElement;
      // Trigger 3 distinct range changes (skip '7days' since it's the default and won't re-fetch)
      for (const range of ['30days', '90days', 'all']) {
        fireEvent.change(dateRangeSelect, { target: { value: range } });
      }

      await waitFor(() => expect(mockMatches.getTrendAnalysis).toHaveBeenCalledTimes(4));
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      const calls = mockMatches.getTrendAnalysis.mock.calls as any[][];
      const forbidden = ['daily', 'weekly', 'monthly'];
      for (const [{ periodType }] of calls) {
        expect(forbidden).not.toContain(periodType);
      }
    });

    it('should pass constructed formats for constructed filter', async () => {
      mockMatches.getTrendAnalysis.mockResolvedValue(createMockTrendAnalysis());

      renderWithProvider(<WinRateTrend />);

      await waitFor(() => {
        expect(screen.getByTestId('line-chart')).toBeInTheDocument();
      });

      const formatSelect = getSelectByLabel('Format');
      fireEvent.change(formatSelect, { target: { value: 'constructed' } });

      await waitFor(() => {
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
        const calls = mockMatches.getTrendAnalysis.mock.calls as any[][];
        const lastCall = calls.slice(-1)[0][0];
        expect(lastCall.formats).toEqual(['Ladder', 'Play']);
      });
    });
  });

  describe('Export Button', () => {
    it('should render export button', async () => {
      mockMatches.getTrendAnalysis.mockResolvedValue(createMockTrendAnalysis());

      renderWithProvider(<WinRateTrend />);

      await waitFor(() => {
        expect(screen.getByText('Export as PNG')).toBeInTheDocument();
      });
    });

    it('should show alert when export button is clicked', async () => {
      mockMatches.getTrendAnalysis.mockResolvedValue(createMockTrendAnalysis());
      const alertMock = vi.spyOn(window, 'alert').mockImplementation(() => {});

      renderWithProvider(<WinRateTrend />);

      await waitFor(() => {
        expect(screen.getByText('Export as PNG')).toBeInTheDocument();
      });

      fireEvent.click(screen.getByText('Export as PNG'));
      expect(alertMock).toHaveBeenCalledWith('Export functionality coming soon!');

      alertMock.mockRestore();
    });
  });

  describe('Trend Display', () => {
    it('should display declining trend correctly', async () => {
      mockMatches.getTrendAnalysis.mockResolvedValue(
        createMockTrendAnalysis({
          Trend: 'declining',
          TrendValue: -0.05,
        })
      );

      renderWithProvider(<WinRateTrend />);

      await waitFor(() => {
        expect(screen.getByText(/declining/)).toBeInTheDocument();
        expect(screen.getByText(/-5%/)).toBeInTheDocument();
      });
    });

    it('should display stable trend correctly', async () => {
      mockMatches.getTrendAnalysis.mockResolvedValue(
        createMockTrendAnalysis({
          Trend: 'stable',
          TrendValue: 0,
        })
      );

      renderWithProvider(<WinRateTrend />);

      await waitFor(() => {
        expect(screen.getByText('stable')).toBeInTheDocument();
      });
    });
  });

  // ─── Set-Release Annotation Tests ─────────────────────────────────────────
  describe('Set-release annotations', () => {
    /**
     * Build a mock TrendAnalysis whose periods span a known set-release date so
     * we can assert annotation presence/absence without coupling to the real
     * ARENA_SET_RELEASES constant. We test the constant independently in
     * setReleaseAnnotations.test.ts; here we verify the component wires the
     * utility output into ReferenceLine elements and the legend.
     */
    function createAnalysisAroundDSK() {
      // DSK released 2024-09-24. Three weekly periods bridging that date.
      return {
        Trends: [
          {
            Period: { Label: 'Week of Sep 17', StartDate: '2024-09-17', EndDate: '2024-09-23' },
            Stats: { TotalMatches: 10, MatchesWon: 6, MatchesLost: 4, TotalGames: 20, GamesWon: 12, GamesLost: 8, WinRate: 0.6 },
            WinRate: 0.6,
          },
          {
            Period: { Label: 'Week of Sep 24', StartDate: '2024-09-24', EndDate: '2024-09-30' },
            Stats: { TotalMatches: 8, MatchesWon: 4, MatchesLost: 4, TotalGames: 16, GamesWon: 8, GamesLost: 8, WinRate: 0.5 },
            WinRate: 0.5,
          },
          {
            Period: { Label: 'Week of Oct 1', StartDate: '2024-10-01', EndDate: '2024-10-07' },
            Stats: { TotalMatches: 12, MatchesWon: 7, MatchesLost: 5, TotalGames: 24, GamesWon: 14, GamesLost: 10, WinRate: 0.583 },
            WinRate: 0.583,
          },
        ],
        Overall: { TotalMatches: 30, MatchesWon: 17, MatchesLost: 13, TotalGames: 60, GamesWon: 34, GamesLost: 26, WinRate: 0.567 },
        Trend: 'stable',
        TrendValue: 0,
      };
    }

    function createAnalysisOutsideSetWindow() {
      // A chart window entirely in 2025 — no Arena sets released in this window
      // from the ARENA_SET_RELEASES constant that have dates after 2025-05-01.
      return {
        Trends: [
          {
            Period: { Label: 'May 1', StartDate: '2025-05-01', EndDate: '2025-05-01' },
            Stats: { TotalMatches: 5, MatchesWon: 3, MatchesLost: 2, TotalGames: 10, GamesWon: 6, GamesLost: 4, WinRate: 0.6 },
            WinRate: 0.6,
          },
          {
            Period: { Label: 'May 2', StartDate: '2025-05-02', EndDate: '2025-05-02' },
            Stats: { TotalMatches: 5, MatchesWon: 3, MatchesLost: 2, TotalGames: 10, GamesWon: 6, GamesLost: 4, WinRate: 0.6 },
            WinRate: 0.6,
          },
        ],
        Overall: { TotalMatches: 10, MatchesWon: 6, MatchesLost: 4, TotalGames: 20, GamesWon: 12, GamesLost: 8, WinRate: 0.6 },
        Trend: 'stable',
        TrendValue: 0,
      };
    }

    it('renders a ReferenceLine for DSK when chart window spans the DSK release date', async () => {
      mockMatches.getTrendAnalysis.mockResolvedValue(createAnalysisAroundDSK());
      renderWithProvider(<WinRateTrend />);

      await waitFor(() => {
        expect(screen.getByTestId('line-chart')).toBeInTheDocument();
      });

      expect(screen.getByTestId('set-annotation-DSK')).toBeInTheDocument();
    });

    it('renders the annotation legend when annotations are present', async () => {
      mockMatches.getTrendAnalysis.mockResolvedValue(createAnalysisAroundDSK());
      renderWithProvider(<WinRateTrend />);

      await waitFor(() => {
        expect(screen.getByTestId('set-annotation-legend')).toBeInTheDocument();
      });

      expect(screen.getByText(/DSK/)).toBeInTheDocument();
      expect(screen.getByText(/Duskmourn/)).toBeInTheDocument();
    });

    it('does NOT render annotation legend when no set releases fall in the chart window', async () => {
      mockMatches.getTrendAnalysis.mockResolvedValue(createAnalysisOutsideSetWindow());
      renderWithProvider(<WinRateTrend />);

      await waitFor(() => {
        expect(screen.getByTestId('line-chart')).toBeInTheDocument();
      });

      expect(screen.queryByTestId('set-annotation-legend')).not.toBeInTheDocument();
    });

    it('does NOT render annotation legend when no analysis data', async () => {
      mockMatches.getTrendAnalysis.mockResolvedValue(null);
      renderWithProvider(<WinRateTrend />);

      await waitFor(() => {
        expect(screen.getByText('Not enough data')).toBeInTheDocument();
      });

      expect(screen.queryByTestId('set-annotation-legend')).not.toBeInTheDocument();
    });

    it('renders DSK annotation in bar chart mode as well', async () => {
      mockMatches.getTrendAnalysis.mockResolvedValue(createAnalysisAroundDSK());
      renderWithProvider(<WinRateTrend />);

      await waitFor(() => {
        expect(screen.getByTestId('line-chart')).toBeInTheDocument();
      });

      // Switch to bar chart.
      const chartTypeSelect = (
        screen.getByText('Chart Type').closest('.filter-group') as HTMLElement
      )?.querySelector('select') as HTMLSelectElement;
      fireEvent.change(chartTypeSelect, { target: { value: 'bar' } });

      await waitFor(() => {
        expect(screen.getByTestId('bar-chart')).toBeInTheDocument();
      });

      expect(screen.getByTestId('set-annotation-DSK')).toBeInTheDocument();
    });
  });

  // ─── BFF Contract Tests ────────────────────────────────────────────────────
  // Fixtures use the REAL BFF wire key "Trends". If TrendAnalysis.constructor
  // regresses to reading source["Periods"], chartData will be empty and the
  // chart will never render — catching the contract drift.
  describe('BFF field contract — Trends key (not Periods)', () => {
    it('chart renders when BFF response uses "Trends" key', async () => {
      // Raw fixture bypasses createMockTrendAnalysis to confirm the key name matters
      const bffResponse = {
        Trends: [
          {
            Period: { Label: 'Jan 1', StartDate: '2024-01-01', EndDate: '2024-01-01' },
            Stats: { TotalMatches: 5, WinRate: 0.6 },
            WinRate: 0.6,
          },
        ],
        Overall: { TotalMatches: 5, WinRate: 0.6 },
        Trend: 'improving',
        TrendValue: 0.05,
      };

      mockMatches.getTrendAnalysis.mockResolvedValue(bffResponse);

      renderWithProvider(<WinRateTrend />);

      await waitFor(() => {
        expect(screen.getByTestId('line-chart')).toBeInTheDocument();
      });

      // Chart receives data — not empty state
      expect(screen.queryByText('Not enough data')).not.toBeInTheDocument();
    });

    it('chart is EMPTY when response uses wrong key "Periods" (regression sentinel)', async () => {
      // This fixture mimics a BFF rollback that would re-emit "Periods" instead of "Trends".
      // TrendAnalysis reads "Trends", so Periods is ignored → empty chart.
      const wrongKeyResponse = {
        Periods: [
          {
            Period: { Label: 'Jan 1', StartDate: '2024-01-01', EndDate: '2024-01-01' },
            Stats: { TotalMatches: 5, WinRate: 0.6 },
            WinRate: 0.6,
          },
        ],
        Overall: { TotalMatches: 5, WinRate: 0.6 },
        Trend: 'improving',
        TrendValue: 0.05,
      };

      mockMatches.getTrendAnalysis.mockResolvedValue(wrongKeyResponse);

      renderWithProvider(<WinRateTrend />);

      await waitFor(() => {
        // Empty state because Trends array is missing
        expect(screen.getByText('Not enough data')).toBeInTheDocument();
      });
    });
  });
});
