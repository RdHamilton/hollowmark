import { useState, useEffect, useCallback } from 'react';
import { matches, system } from '@/services/api';
import { useReadModelUpdates } from '@/hooks/useReadModelUpdates';
import { models } from '@/types/models';
import type { DaemonHealthState } from './DaemonHealthIndicator';
import DownloadProgressBar from './DownloadProgressBar';
import EnvBadge from './EnvBadge';
import './StatusStrip.css';

interface StatusStripProps {
  /** Daemon health state passed from Layout — no internal polling. */
  daemonStatus: DaemonHealthState;
}

const StatusStrip = ({ daemonStatus }: StatusStripProps) => {
  const [stats, setStats] = useState<models.Statistics | null>(null);
  const [streak, setStreak] = useState<{ type: string; count: number }>({ type: '', count: 0 });
  const [lastMatch, setLastMatch] = useState<string>('');
  const [lastSynced, setLastSynced] = useState<string>('');
  const [loading, setLoading] = useState(true);

  const loadStats = useCallback(async () => {
    try {
      const filter = new models.StatsFilter();
      const statsData = await matches.getStats(matches.statsFilterToRequest(filter));
      setStats(statsData);

      const matchData = await matches.getMatches(matches.statsFilterToRequest(filter));

      if (matchData && matchData.length > 0) {
        const lastResult = matchData[0].Result;
        let streakCount = 1;

        for (let i = 1; i < matchData.length; i++) {
          if (matchData[i].Result === lastResult) {
            streakCount++;
          } else {
            break;
          }
        }

        setStreak({
          type: lastResult === 'win' ? 'W' : 'L',
          count: streakCount,
        });

        const lastMatchDate = new Date(matchData[0].Timestamp as string);
        setLastMatch(lastMatchDate.toLocaleString());
      }

      try {
        const health = await system.getHealth();
        if (health.database.lastWrite) {
          const syncDate = new Date(health.database.lastWrite);
          setLastSynced(syncDate.toLocaleTimeString());
        } else {
          setLastSynced(new Date().toLocaleTimeString());
        }
      } catch {
        setLastSynced(new Date().toLocaleTimeString());
      }
    } catch (err) {
      console.error('Error loading status strip stats:', err);
    } finally {
      setLoading(false);
    }
  }, []); // no external deps — only stable setters and imported API fns

  useEffect(() => {
    void loadStats();
  }, [loadStats]);

  // Rewired per ADR-084: readmodel.updated matches/decks domain replaces
  // the dead stats:updated colon-vocabulary listener (no server emitter).
  useReadModelUpdates({
    onMatches: () => { void loadStats(); },
    onDecks: () => { void loadStats(); },
  });

  const isDaemonOffline = daemonStatus !== 'connected';

  const renderMatches = () => {
    if (!stats) return '0';
    return String(stats.TotalMatches);
  };

  const renderWinRate = () => {
    if (!stats || stats.TotalMatches === 0) return '--';
    return `${Math.round(stats.WinRate * 1000) / 10}% (${stats.MatchesWon}-${stats.MatchesLost})`;
  };

  if (loading) {
    return (
      <footer className="status-strip" data-testid="status-strip">
        <div className="status-strip-content">
          <span className="status-strip-loading">Loading stats...</span>
          <EnvBadge />
        </div>
      </footer>
    );
  }

  return (
    <footer className="status-strip" data-testid="status-strip">
      <div className="status-strip-content">
        <span className="status-strip-label">All Time</span>
        <span className="status-strip-sep">·</span>
        <span className="status-strip-stat">
          <strong>Matches:</strong>{' '}
          <span className="status-strip-num">{renderMatches()}</span>
        </span>
        <span className="status-strip-sep">·</span>
        <span className="status-strip-stat">
          <strong>Win Rate:</strong>{' '}
          <span className="status-strip-num">{renderWinRate()}</span>
        </span>
        {streak.count > 0 && (
          <>
            <span className="status-strip-sep">·</span>
            <span className={`status-strip-stat status-strip-streak-${streak.type.toLowerCase()}`}>
              <strong>Streak:</strong>{' '}
              <span className="status-strip-num">{streak.type}{streak.count}</span>
            </span>
          </>
        )}
        {lastMatch && (
          <>
            <span className="status-strip-sep status-strip-sep-push">·</span>
            <span className="status-strip-stat">
              <strong>Last Played:</strong>{' '}
              <span className="status-strip-num">{lastMatch}</span>
            </span>
          </>
        )}
        <span className="status-strip-sep">·</span>
        {isDaemonOffline ? (
          <span className="status-strip-synced status-strip-offline">
            Daemon offline
          </span>
        ) : (
          <span className="status-strip-synced status-strip-synced-ok">
            <strong>Synced:</strong>{' '}
            <span className="status-strip-num">{lastSynced}</span>
          </span>
        )}
        <DownloadProgressBar />
        <EnvBadge />
      </div>
    </footer>
  );
};

export default StatusStrip;
