import { describe, it, expect, vi, beforeEach } from 'vitest';
import { screen, waitFor } from '@testing-library/react';
import { render } from '../test/utils/testUtils';
import StatusStrip from './StatusStrip';
import { mockMatches, mockSystem } from '@/test/mocks/apiMock';
import { mockEventEmitter } from '@/test/mocks/websocketMock';
import { models } from '@/types/models';
import type { DaemonHealthState } from './DaemonHealthIndicator';

// Mock useDownload since StatusStrip includes DownloadProgressBar
vi.mock('@/context/DownloadContext', () => ({
  useDownload: () => ({
    state: { tasks: [], activeTask: null },
    isDownloading: false,
    overallProgress: 0,
  }),
  DownloadProvider: ({ children }: { children: React.ReactNode }) => children,
}));

function createMockStatistics(overrides: Partial<models.Statistics> = {}): models.Statistics {
  return new models.Statistics({
    TotalMatches: 100,
    MatchesWon: 60,
    MatchesLost: 40,
    TotalGames: 250,
    GamesWon: 150,
    GamesLost: 100,
    WinRate: 0.6,
    ...overrides,
  });
}

function createMockMatch(overrides: Partial<models.Match> = {}): models.Match {
  return new models.Match({
    ID: 'match-1',
    EventID: 'event-1',
    MatchID: 'match-123',
    Timestamp: new Date('2025-11-20T10:00:00Z'),
    Result: 'win',
    OpponentScreenName: 'Opponent1',
    Format: 'Standard',
    DeckColors: ['W', 'U'],
    OpponentColors: ['B', 'R'],
    OnPlay: true,
    TotalTurns: 10,
    DurationSeconds: 600,
    RankTier: 'Gold',
    RankClass: '4',
    ...overrides,
  });
}

const connectedStatus: DaemonHealthState = 'connected';
const disconnectedStatus: DaemonHealthState = 'disconnected';

describe('StatusStrip Component', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockEventEmitter.clear();
  });

  describe('AC1: Always renders when mounted — no zero-match early return', () => {
    it('renders with data-testid="status-strip" in loading state', () => {
      mockMatches.getStats.mockImplementation(() => new Promise(() => {}));
      mockMatches.getMatches.mockImplementation(() => new Promise(() => {}));

      render(<StatusStrip daemonStatus={connectedStatus} />);

      expect(screen.getByTestId('status-strip')).toBeInTheDocument();
    });

    it('renders at zero matches — strip is present, not null', async () => {
      const emptyStats = createMockStatistics({ TotalMatches: 0 });
      mockMatches.getStats.mockResolvedValue(emptyStats);
      mockMatches.getMatches.mockResolvedValue([]);

      render(<StatusStrip daemonStatus={connectedStatus} />);

      await waitFor(() => {
        expect(screen.getByTestId('status-strip')).toBeInTheDocument();
      });
    });

    it('renders at zero matches — shows Matches: 0', async () => {
      const emptyStats = createMockStatistics({ TotalMatches: 0 });
      mockMatches.getStats.mockResolvedValue(emptyStats);
      mockMatches.getMatches.mockResolvedValue([]);

      render(<StatusStrip daemonStatus={connectedStatus} />);

      await waitFor(() => {
        expect(screen.getByText('Matches:')).toBeInTheDocument();
        expect(screen.getByText('0')).toBeInTheDocument();
      });
    });

    it('renders at zero matches — shows Win Rate: --', async () => {
      const emptyStats = createMockStatistics({ TotalMatches: 0, MatchesWon: 0, MatchesLost: 0, WinRate: 0 });
      mockMatches.getStats.mockResolvedValue(emptyStats);
      mockMatches.getMatches.mockResolvedValue([]);

      render(<StatusStrip daemonStatus={connectedStatus} />);

      await waitFor(() => {
        expect(screen.getByText('Win Rate:')).toBeInTheDocument();
        expect(screen.getByText('--')).toBeInTheDocument();
      });
    });
  });

  describe('AC3: 5 value labels rendered', () => {
    it('renders all 5 value labels: Matches, Win Rate, Streak, Last Played, Synced', async () => {
      const stats = createMockStatistics();
      const matchList = [createMockMatch()];
      mockMatches.getStats.mockResolvedValue(stats);
      mockMatches.getMatches.mockResolvedValue(matchList);

      render(<StatusStrip daemonStatus={connectedStatus} />);

      await waitFor(() => {
        expect(screen.getByText('Matches:')).toBeInTheDocument();
        expect(screen.getByText('Win Rate:')).toBeInTheDocument();
        expect(screen.getByText('Streak:')).toBeInTheDocument();
        expect(screen.getByText('Last Played:')).toBeInTheDocument();
        expect(screen.getByText('Synced:')).toBeInTheDocument();
      });
    });
  });

  describe('AC4: Daemon health coloring', () => {
    it('shows green Synced label when daemon is connected', async () => {
      const stats = createMockStatistics();
      mockMatches.getStats.mockResolvedValue(stats);
      mockMatches.getMatches.mockResolvedValue([]);

      render(<StatusStrip daemonStatus={connectedStatus} />);

      await waitFor(() => {
        // Should show Synced: label (not daemon offline)
        expect(screen.getByText('Synced:')).toBeInTheDocument();
        expect(screen.queryByText(/Daemon offline/i)).not.toBeInTheDocument();
      });
    });

    it('shows red Daemon offline label when daemon is disconnected', async () => {
      const stats = createMockStatistics();
      mockMatches.getStats.mockResolvedValue(stats);
      mockMatches.getMatches.mockResolvedValue([]);

      render(<StatusStrip daemonStatus={disconnectedStatus} />);

      await waitFor(() => {
        expect(screen.getByText(/Daemon offline/i)).toBeInTheDocument();
        expect(screen.queryByText('Synced:')).not.toBeInTheDocument();
      });
    });

    it('shows red Daemon offline even at zero matches (AC1+AC4 combined)', async () => {
      const emptyStats = createMockStatistics({ TotalMatches: 0 });
      mockMatches.getStats.mockResolvedValue(emptyStats);
      mockMatches.getMatches.mockResolvedValue([]);

      render(<StatusStrip daemonStatus={disconnectedStatus} />);

      await waitFor(() => {
        expect(screen.getByTestId('status-strip')).toBeInTheDocument();
        expect(screen.getByText(/Daemon offline/i)).toBeInTheDocument();
      });
    });

    it('daemon offline label has the offline CSS class', async () => {
      const stats = createMockStatistics();
      mockMatches.getStats.mockResolvedValue(stats);
      mockMatches.getMatches.mockResolvedValue([]);

      render(<StatusStrip daemonStatus={disconnectedStatus} />);

      await waitFor(() => {
        const offlineEl = screen.getByText(/Daemon offline/i).closest('.status-strip-synced');
        expect(offlineEl).toHaveClass('status-strip-offline');
      });
    });

    it('connected synced label has the synced-ok CSS class', async () => {
      const stats = createMockStatistics();
      mockMatches.getStats.mockResolvedValue(stats);
      mockMatches.getMatches.mockResolvedValue([]);

      render(<StatusStrip daemonStatus={connectedStatus} />);

      await waitFor(() => {
        // The synced container should have the ok class
        const syncedLabel = screen.getByText('Synced:');
        const syncedContainer = syncedLabel.closest('.status-strip-synced');
        expect(syncedContainer).toHaveClass('status-strip-synced-ok');
      });
    });
  });

  describe('Statistics display', () => {
    it('displays total matches count', async () => {
      const stats = createMockStatistics({ TotalMatches: 100 });
      mockMatches.getStats.mockResolvedValue(stats);
      mockMatches.getMatches.mockResolvedValue([]);

      render(<StatusStrip daemonStatus={connectedStatus} />);

      await waitFor(() => {
        expect(screen.getByText('100')).toBeInTheDocument();
      });
    });

    it('displays win rate', async () => {
      const stats = createMockStatistics({ WinRate: 0.6, MatchesWon: 60, MatchesLost: 40 });
      mockMatches.getStats.mockResolvedValue(stats);
      mockMatches.getMatches.mockResolvedValue([]);

      render(<StatusStrip daemonStatus={connectedStatus} />);

      await waitFor(() => {
        expect(screen.getByText(/60%/)).toBeInTheDocument();
      });
    });

    it('displays win streak', async () => {
      const stats = createMockStatistics();
      const matchList = [
        createMockMatch({ Result: 'win' }),
        createMockMatch({ Result: 'win' }),
        createMockMatch({ Result: 'win' }),
        createMockMatch({ Result: 'loss' }),
      ];
      mockMatches.getStats.mockResolvedValue(stats);
      mockMatches.getMatches.mockResolvedValue(matchList);

      render(<StatusStrip daemonStatus={connectedStatus} />);

      await waitFor(() => {
        expect(screen.getByText('W3')).toBeInTheDocument();
      });
    });

    it('displays loss streak', async () => {
      const stats = createMockStatistics();
      const matchList = [
        createMockMatch({ Result: 'loss' }),
        createMockMatch({ Result: 'loss' }),
        createMockMatch({ Result: 'win' }),
      ];
      mockMatches.getStats.mockResolvedValue(stats);
      mockMatches.getMatches.mockResolvedValue(matchList);

      render(<StatusStrip daemonStatus={connectedStatus} />);

      await waitFor(() => {
        expect(screen.getByText('L2')).toBeInTheDocument();
      });
    });

    it('numeric values use the mono class', async () => {
      const stats = createMockStatistics({ TotalMatches: 42 });
      mockMatches.getStats.mockResolvedValue(stats);
      mockMatches.getMatches.mockResolvedValue([]);

      render(<StatusStrip daemonStatus={connectedStatus} />);

      await waitFor(() => {
        const matchNum = screen.getByText('42');
        expect(matchNum).toHaveClass('status-strip-num');
      });
    });
  });

  describe('Real-time updates', () => {
    it('reloads stats on stats:updated event', async () => {
      const initialStats = createMockStatistics({ TotalMatches: 10 });
      const updatedStats = createMockStatistics({ TotalMatches: 11 });

      mockMatches.getStats.mockResolvedValueOnce(initialStats);
      mockMatches.getMatches.mockResolvedValue([]);

      render(<StatusStrip daemonStatus={connectedStatus} />);

      await waitFor(() => {
        expect(screen.getByText('10')).toBeInTheDocument();
      });

      mockMatches.getStats.mockResolvedValueOnce(updatedStats);
      mockEventEmitter.emit('stats:updated');

      await waitFor(() => {
        expect(screen.getByText('11')).toBeInTheDocument();
      });
    });
  });

  describe('AC8: data-testid present on all render paths', () => {
    it('has data-testid="status-strip" in loading state', () => {
      mockMatches.getStats.mockImplementation(() => new Promise(() => {}));
      mockMatches.getMatches.mockImplementation(() => new Promise(() => {}));

      render(<StatusStrip daemonStatus={connectedStatus} />);

      expect(screen.getByTestId('status-strip')).toBeInTheDocument();
    });

    it('has data-testid="status-strip" with populated stats', async () => {
      const stats = createMockStatistics({ TotalMatches: 50 });
      mockMatches.getStats.mockResolvedValue(stats);
      mockMatches.getMatches.mockResolvedValue([]);

      render(<StatusStrip daemonStatus={connectedStatus} />);

      await waitFor(() => {
        expect(screen.getByTestId('status-strip')).toBeInTheDocument();
      });
    });
  });

  describe('Synced time display', () => {
    it('shows last-sync time from health status when daemon is connected', async () => {
      const stats = createMockStatistics();
      mockMatches.getStats.mockResolvedValue(stats);
      mockMatches.getMatches.mockResolvedValue([]);
      mockSystem.getHealth.mockResolvedValue({
        status: 'healthy',
        version: '1.4.0',
        uptime: 3600,
        database: { status: 'ok', lastWrite: '2025-12-28T15:30:00Z' },
        logMonitor: { status: 'ok' },
        websocket: { status: 'ok', connectedClients: 1 },
        metrics: { totalProcessed: 100, totalErrors: 0 },
      });

      render(<StatusStrip daemonStatus={connectedStatus} />);

      await waitFor(() => {
        expect(screen.getByText('Synced:')).toBeInTheDocument();
        const syncedText = screen.getByText('Synced:').nextSibling;
        expect(syncedText).toBeTruthy();
      });
    });

    it('hides Synced time when daemon is offline, shows Daemon offline instead', async () => {
      const stats = createMockStatistics();
      mockMatches.getStats.mockResolvedValue(stats);
      mockMatches.getMatches.mockResolvedValue([]);

      render(<StatusStrip daemonStatus={disconnectedStatus} />);

      await waitFor(() => {
        expect(screen.queryByText('Synced:')).not.toBeInTheDocument();
        expect(screen.getByText(/Daemon offline/i)).toBeInTheDocument();
      });
    });
  });
});
