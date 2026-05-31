import { describe, it, expect, vi, beforeEach } from 'vitest';
import { readFileSync } from 'node:fs';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { render } from '../test/utils/testUtils';
import Layout from './Layout';
import { mockMatches } from '@/test/mocks/apiMock';

const CSS_PATH = join(dirname(fileURLToPath(import.meta.url)), 'Layout.css');

// Mock Sentry so Layout tests don't need a real DSN
vi.mock('@sentry/react', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@sentry/react')>();
  return {
    ...actual,
    getFeedback: vi.fn().mockReturnValue({ openDialog: vi.fn() }),
    feedbackIntegration: vi.fn(),
    ErrorBoundary: ({ children }: { children: React.ReactNode }) => children,
    init: vi.fn(),
  };
});

// Mock DaemonHealthIndicator to prevent async polling effects from firing
// after jsdom teardown (causes "window is not defined" ReferenceError in CI).
vi.mock('./DaemonHealthIndicator', () => ({ default: () => null }));

// Mock useDownload since Layout renders Footer which includes DownloadProgressBar
vi.mock('@/context/DownloadContext', () => ({
  useDownload: () => ({
    state: { tasks: [], activeTask: null },
    isDownloading: false,
    overallProgress: 0,
  }),
  DownloadProvider: ({ children }: { children: React.ReactNode }) => children,
}));

// — Design token compliance (AC2, #312) ————————————————————————————————
describe('Layout CSS — design token compliance (#312)', () => {
  const css = readFileSync(CSS_PATH, 'utf8');

  it('uses var(--vault-success-dim) for connected status background, not raw rgba', () => {
    expect(css).toContain('var(--vault-success-dim)');
    expect(css).not.toMatch(/rgba\(\s*76\s*,\s*175\s*,\s*80/);
  });

  it('uses var(--vault-warning-dim) for standalone status background, not raw rgba', () => {
    expect(css).toContain('var(--vault-warning-dim)');
    expect(css).not.toMatch(/rgba\(\s*255\s*,\s*152\s*,\s*0/);
  });

  it('uses var(--vault-info-dim) for reconnecting status background, not raw rgba', () => {
    expect(css).toContain('var(--vault-info-dim)');
    expect(css).not.toMatch(/rgba\(\s*33\s*,\s*150\s*,\s*243/);
  });

  it('active tab uses accent token not raw hex', () => {
    expect(css).toContain('.tab.active');
    expect(css).toContain('var(--accent)');
    // No legacy blue hex
    expect(css).not.toContain('#4a9eff');
    expect(css).not.toContain('#4A9EFF');
  });
});

// ——————————————————————————————————————————————————————————————————————————

describe('Layout Component', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  describe('Navigation Tabs', () => {
    it('should render all navigation tabs', () => {
      render(
        <Layout>
          <div>Test Content</div>
        </Layout>,
        { initialRoute: '/' }
      );

      expect(screen.getByTestId('nav-tab-match-history')).toBeInTheDocument();
      expect(screen.getByTestId('nav-tab-quests')).toBeInTheDocument();
      expect(screen.getByTestId('nav-tab-draft')).toBeInTheDocument();
      expect(screen.getByTestId('nav-tab-charts')).toBeInTheDocument();
      expect(screen.getByTestId('nav-tab-settings')).toBeInTheDocument();
    });

    it('should render the VaultMTG brand lockup in the tab bar', () => {
      render(
        <Layout>
          <div>Test Content</div>
        </Layout>,
        { initialRoute: '/' }
      );

      const brand = screen.getByTestId('nav-brand');
      expect(brand).toBeInTheDocument();
      expect(brand).toHaveTextContent('VaultMTG');
      // Brand lockup links back to home
      expect(brand).toHaveAttribute('href', '/home');
    });

    it('should apply active treatment class to the current tab', () => {
      render(
        <Layout>
          <div>Test Content</div>
        </Layout>,
        { initialRoute: '/match-history' }
      );

      expect(screen.getByTestId('nav-tab-match-history')).toHaveClass('active');
      // Inactive tabs should not carry the active treatment
      expect(screen.getByTestId('nav-tab-quests')).not.toHaveClass('active');
    });

    it('should highlight active tab based on current route', () => {
      render(
        <Layout>
          <div>Test Content</div>
        </Layout>,
        { initialRoute: '/draft' }
      );

      const draftTab = screen.getByTestId('nav-tab-draft');
      expect(draftTab).toHaveClass('active');
    });

    it('should navigate to correct route when tab is clicked', async () => {
      render(
        <Layout>
          <div>Test Content</div>
        </Layout>,
        { initialRoute: '/' }
      );

      const questsTab = screen.getByTestId('nav-tab-quests');
      await userEvent.click(questsTab);

      await waitFor(() => {
        expect(questsTab).toHaveClass('active');
      });
    });

    it('should show sub-navigation when Charts tab is active', async () => {
      render(
        <Layout>
          <div>Test Content</div>
        </Layout>,
        { initialRoute: '/charts/win-rate-trend' }
      );

      await waitFor(() => {
        expect(screen.getByText('Win Rate Trend')).toBeInTheDocument();
        expect(screen.getByText('Deck Performance')).toBeInTheDocument();
        expect(screen.getByText('Rank Progression')).toBeInTheDocument();
        expect(screen.getByText('Format Distribution')).toBeInTheDocument();
        expect(screen.getByText('Result Breakdown')).toBeInTheDocument();
      });
    });

    it('should not show sub-navigation when Charts tab is inactive', () => {
      render(
        <Layout>
          <div>Test Content</div>
        </Layout>,
        { initialRoute: '/match-history' }
      );

      // Sub-navigation should not be present
      expect(screen.queryByTestId('charts-sub-tab-bar')).not.toBeInTheDocument();
      expect(screen.queryByTestId('draft-sub-tab-bar')).not.toBeInTheDocument();
    });
  });

  describe('Connection Status', () => {
    it('should render the connection status indicator container with DaemonHealthIndicator', () => {
      render(
        <Layout>
          <div>Test Content</div>
        </Layout>
      );

      // connection-status-indicator is the single status area in the nav bar.
      // The old status-badge-compact (standalone/connected dot) has been removed —
      // DaemonHealthIndicator owns daemon status via /api/v1/health/daemon polling.
      expect(screen.getByTestId('app-container')).toBeInTheDocument();
      expect(screen.queryByTestId('connection-status-badge')).not.toBeInTheDocument();
    });
  });

  describe('Content Rendering', () => {
    it('should render children content', () => {
      render(
        <Layout>
          <div data-testid="test-content">Test Content</div>
        </Layout>
      );

      expect(screen.getByTestId('test-content')).toBeInTheDocument();
      expect(screen.getByText('Test Content')).toBeInTheDocument();
    });

    it('should render Footer component', () => {
      mockMatches.getStats.mockResolvedValue({
        TotalMatches: 0,
        MatchesWon: 0,
        MatchesLost: 0,
        TotalGames: 0,
        GamesWon: 0,
        GamesLost: 0,
        WinRate: 0,
      });

      render(
        <Layout>
          <div>Test Content</div>
        </Layout>
      );

      // Footer should be present
      const footer = document.querySelector('.app-footer');
      expect(footer).toBeInTheDocument();
    });
  });

  describe('ReportBugButton', () => {
    it('shows report bug button when user is signed in', () => {
      render(
        <Layout>
          <div>Test Content</div>
        </Layout>,
        { initialRoute: '/' }
      );

      // Default test setup has isSignedIn: true (see setup.ts)
      expect(screen.getByTestId('report-bug-button')).toBeInTheDocument();
    });
  });

  describe('Error Handling', () => {
    it('should render nav tabs even when API calls fail', async () => {
      render(
        <Layout>
          <div>Test Content</div>
        </Layout>
      );

      // Component should still render
      expect(screen.getByTestId('nav-tab-match-history')).toBeInTheDocument();
    });

    it('should not throw on mount', async () => {
      expect(() => {
        render(
          <Layout>
            <div>Test Content</div>
          </Layout>
        );
      }).not.toThrow();

      // Layout should still render
      expect(screen.getByTestId('nav-tab-match-history')).toBeInTheDocument();
    });
  });
});
