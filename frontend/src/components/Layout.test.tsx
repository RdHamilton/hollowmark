import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { readFileSync } from 'node:fs';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';
import { screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { render } from '../test/utils/testUtils';
import Layout from './Layout';
import { mockMatches } from '@/test/mocks/apiMock';
import { SESSION_HAS_ACCOUNT_DATA_KEY } from '@/hooks/useDaemonOnboarding';

// Mock bffHomeSummary so Layout tests do not hit real fetch and do not pop
// the onboarding modal accidentally (getHomeSummary returns 19 matches = 'has-data').
// Tests that need the new-user path should override getHomeSummary per-test.
vi.mock('@/services/api/bffHomeSummary', () => ({
  getHomeSummary: vi.fn(() =>
    Promise.resolve({
      today: { wins: 2, losses: 1, win_rate: 0.667 },
      this_week: { wins: 10, losses: 9, win_rate: 0.526, matches: 19 },
      all_time: {
        wins: 10,
        losses: 9,
        win_rate: 0.526,
        matches: 19,
        current_streak: 1,
        streak_type: 'W',
      },
      last_match: { result: 'win', opponent_archetype: null, elapsed_seconds: 600 },
    })
  ),
  makeMockHomeSummary: vi.fn(() => ({
    today: { wins: 0, losses: 0, win_rate: 0 },
    this_week: { wins: 0, losses: 0, win_rate: 0, matches: 0 },
    all_time: { wins: 0, losses: 0, win_rate: 0, matches: 0, current_streak: 0, streak_type: 'W' },
    last_match: null,
  })),
}));

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

// Mock useDownload since Layout renders StatusStrip which includes DownloadProgressBar
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

    it('should render the Hollowmark brand lockup in the tab bar (#1020)', () => {
      render(
        <Layout>
          <div>Test Content</div>
        </Layout>,
        { initialRoute: '/' }
      );

      const brand = screen.getByTestId('nav-brand');
      expect(brand).toBeInTheDocument();
      // #1020: wordmark updated from VaultMTG → Hollowmark
      expect(brand).toHaveTextContent('Hollowmark');
      // Brand lockup links back to home
      expect(brand).toHaveAttribute('href', '/home');
      // aria-label updated to match Hollowmark
      expect(brand).toHaveAttribute('aria-label', 'Hollowmark home');
      // Orb mark rendered at ≥32px per design spec
      const mark = brand.querySelector('img');
      expect(mark).toHaveAttribute('width', '32');
      expect(mark).toHaveAttribute('height', '32');
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

    it('should highlight Draft tab when on /draft/live route', () => {
      render(
        <Layout>
          <div>Test Content</div>
        </Layout>,
        { initialRoute: '/draft/live' }
      );

      const draftTab = screen.getByTestId('nav-tab-draft');
      expect(draftTab).toHaveClass('active');
      // Match History must NOT be highlighted -- that was the bug
      expect(screen.getByTestId('nav-tab-match-history')).not.toHaveClass('active');
    });

    it('should highlight Draft tab when on /history/drafts route', () => {
      render(
        <Layout>
          <div>Test Content</div>
        </Layout>,
        { initialRoute: '/history/drafts' }
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

    it('should render StatusStrip component when signed in', () => {
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

      // StatusStrip should be present (replaces Footer per #1019)
      expect(screen.getByTestId('status-strip')).toBeInTheDocument();
    });
  });

  describe('AC5: StatusStrip auth guard — signed-in user on public routes (regression fix)', () => {
    // Regression: isSignedIn guard alone is insufficient — a signed-in user navigating
    // to /download or /setup must NOT see the StatusStrip.  This describe block adds
    // the missing "signed-in AND on a public route" coverage that Tim's staging smoke caught.

    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    let useAuthSpy: ReturnType<typeof vi.spyOn<any, any>> | undefined;

    afterEach(() => {
      useAuthSpy?.mockRestore();
      useAuthSpy = undefined;
    });

    it('hides StatusStrip when user is not signed in', async () => {
      const clerkModule = await import('@clerk/react');
      useAuthSpy = vi.spyOn(clerkModule, 'useAuth').mockReturnValue({
        isLoaded: true,
        isSignedIn: false,
        getToken: () => Promise.resolve(null),
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
      } as any);

      render(
        <Layout>
          <div>Public Content</div>
        </Layout>,
        { initialRoute: '/download' }
      );

      expect(screen.queryByTestId('status-strip')).not.toBeInTheDocument();
    });

    it('shows StatusStrip when user is signed in on an app route', () => {
      // Default mock has isSignedIn: true (no spy override needed)
      render(
        <Layout>
          <div>Authenticated Content</div>
        </Layout>,
        { initialRoute: '/home' }
      );

      expect(screen.getByTestId('status-strip')).toBeInTheDocument();
    });

    it('hides StatusStrip when signed-in user is on /download', async () => {
      // Regression: the isSignedIn-only guard lets the strip appear here.
      // isSignedIn: true is the default test mock — no spy override needed.
      render(
        <Layout>
          <div>Download Page</div>
        </Layout>,
        { initialRoute: '/download' }
      );

      expect(screen.queryByTestId('status-strip')).not.toBeInTheDocument();
    });

    it('hides StatusStrip when signed-in user is on /setup', async () => {
      // Regression: /setup is also a public route that should never show the strip.
      render(
        <Layout>
          <div>Setup Page</div>
        </Layout>,
        { initialRoute: '/setup' }
      );

      expect(screen.queryByTestId('status-strip')).not.toBeInTheDocument();
    });

    it('shows StatusStrip on /match-history (app route, signed in)', () => {
      render(
        <Layout>
          <div>Match History</div>
        </Layout>,
        { initialRoute: '/match-history' }
      );

      expect(screen.getByTestId('status-strip')).toBeInTheDocument();
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

  describe('Sign-out reset (#715 — Ray required addition)', () => {
    // The sign-out useEffect in Layout must clear the sessionStorage entry and
    // reset dataCheckDoneRef so a subsequent sign-in re-fetches the summary
    // instead of inheriting the prior session's 'has-data' state.

    afterEach(() => {
      sessionStorage.removeItem(SESSION_HAS_ACCOUNT_DATA_KEY);
    });

    it('clears sessionStorage when isSignedIn transitions to false', async () => {
      // This test verifies that the sign-out useEffect in Layout.tsx calls
      // sessionStorage.removeItem(SESSION_HAS_ACCOUNT_DATA_KEY) when isSignedIn is false.
      //
      // Approach: spy on sessionStorage.removeItem, render Layout with a signed-out
      // Clerk mock. The global vi.mock('@clerk/react') factory is hoisted, but we can
      // spy on the module's exports after import.

      const clerkModule = await import('@clerk/react');
      const useAuthSpy = vi.spyOn(clerkModule, 'useAuth').mockReturnValue({
        isLoaded: true,
        isSignedIn: false,
        getToken: () => Promise.resolve(null),
        // eslint-disable-next-line @typescript-eslint/no-explicit-any
      } as any);

      // Seed sessionStorage — the sign-out effect should clear this.
      sessionStorage.setItem(SESSION_HAS_ACCOUNT_DATA_KEY, 'true');

      render(
        <Layout>
          <div>Test Content</div>
        </Layout>
      );

      // The sign-out effect (useEffect([isSignedIn])) should fire on mount
      // because isSignedIn is false, clearing the sessionStorage entry.
      await waitFor(() => {
        expect(sessionStorage.getItem(SESSION_HAS_ACCOUNT_DATA_KEY)).toBeNull();
      });

      useAuthSpy.mockRestore();
    });

    it('does NOT clear sessionStorage while still signed in', async () => {
      // Seed sessionStorage — should persist while signed in.
      sessionStorage.setItem(SESSION_HAS_ACCOUNT_DATA_KEY, 'true');

      // Default mock returns isSignedIn: true — no override needed.
      render(
        <Layout>
          <div>Test Content</div>
        </Layout>
      );

      // Give the effect time to run — it should not clear the entry.
      await waitFor(() => {
        expect(screen.getByTestId('app-container')).toBeInTheDocument();
      });

      expect(sessionStorage.getItem(SESSION_HAS_ACCOUNT_DATA_KEY)).toBe('true');
    });
  });
});
