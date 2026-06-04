/**
 * DraftAnalytics — session-scope tests (#58 / FT-3)
 *
 * Covers:
 *   - ?session=X&set=Y → session scope banner shown, set pre-selected
 *   - No params → generic view (no banner, default set)
 */
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import DraftAnalytics from './DraftAnalytics';
import { mockDrafts } from '@/test/mocks/apiMock';

vi.mock('@/hooks/useSettings', () => ({
  useSettings: vi.fn(() => ({ autoRefresh: false })),
}));

// Stub sub-components to avoid deep rendering noise in session-scope tests.
vi.mock('@/components/TemporalTrends', () => ({ default: () => <div data-testid="temporal-trends-stub" /> }));
vi.mock('@/components/CommunityComparison', () => ({ default: () => <div data-testid="community-comparison-stub" /> }));
vi.mock('@/components/FormatInsights', () => ({ default: () => <div data-testid="format-insights-stub" /> }));

function renderWithRoute(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <DraftAnalytics />
    </MemoryRouter>
  );
}

describe('DraftAnalytics — session scope (#58)', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockDrafts.getDraftFormats.mockResolvedValue(['DSK', 'FDN', 'BLB', 'SOS']);
  });

  describe('with ?session= and ?set= params', () => {
    it('renders the session-scope banner', async () => {
      renderWithRoute('/draft-analytics?session=abc-123&set=SOS');

      await waitFor(() => {
        expect(screen.getByTestId('draft-analytics-session-scope')).toBeInTheDocument();
      });
    });

    it('session-scope banner carries the session id as data-session-id attribute', async () => {
      renderWithRoute('/draft-analytics?session=abc-123&set=SOS');

      await waitFor(() => {
        const banner = screen.getByTestId('draft-analytics-session-scope');
        expect(banner).toHaveAttribute('data-session-id', 'abc-123');
      });
    });

    it('session-scope banner includes the set code', async () => {
      renderWithRoute('/draft-analytics?session=abc-123&set=SOS');

      await waitFor(() => {
        const banner = screen.getByTestId('draft-analytics-session-scope');
        expect(banner.textContent).toContain('SOS');
      });
    });

    it('pre-selects the set from ?set= in the Set dropdown', async () => {
      renderWithRoute('/draft-analytics?session=abc-123&set=SOS');

      await waitFor(() => {
        const setSelect = screen.getByLabelText('Set') as HTMLSelectElement;
        expect(setSelect.value).toBe('SOS');
      });
    });

    it('pre-selects the set even when it is not the first option', async () => {
      renderWithRoute('/draft-analytics?session=xyz-789&set=FDN');

      await waitFor(() => {
        const setSelect = screen.getByLabelText('Set') as HTMLSelectElement;
        expect(setSelect.value).toBe('FDN');
      });
    });

    it('still renders the page title', async () => {
      renderWithRoute('/draft-analytics?session=abc-123&set=SOS');

      await waitFor(() => {
        expect(screen.getByRole('heading', { name: 'Draft Analytics' })).toBeInTheDocument();
      });
    });
  });

  describe('without params (generic view)', () => {
    it('does NOT render the session-scope banner', async () => {
      renderWithRoute('/draft-analytics');

      await waitFor(() => {
        expect(screen.getByRole('heading', { name: 'Draft Analytics' })).toBeInTheDocument();
      });

      expect(screen.queryByTestId('draft-analytics-session-scope')).not.toBeInTheDocument();
    });

    it('defaults to the first available set', async () => {
      renderWithRoute('/draft-analytics');

      await waitFor(() => {
        const setSelect = screen.getByLabelText('Set') as HTMLSelectElement;
        expect(setSelect.value).toBe('DSK');
      });
    });
  });

  describe('with ?session= only (no ?set=)', () => {
    it('renders the session-scope banner without a set label', async () => {
      renderWithRoute('/draft-analytics?session=abc-123');

      await waitFor(() => {
        const banner = screen.getByTestId('draft-analytics-session-scope');
        expect(banner).toBeInTheDocument();
        expect(banner.textContent).not.toContain(' — ');
      });
    });

    it('defaults set dropdown to first available set', async () => {
      renderWithRoute('/draft-analytics?session=abc-123');

      await waitFor(() => {
        const setSelect = screen.getByLabelText('Set') as HTMLSelectElement;
        expect(setSelect.value).toBe('DSK');
      });
    });
  });
});
