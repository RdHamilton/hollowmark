import type { Meta, StoryObj } from '@storybook/react';
import './StatusStrip.css';
import type { DaemonHealthState } from './DaemonHealthIndicator';

/**
 * StatusStrip — persistent bottom status bar.
 *
 * Replaces Footer.tsx (retired in #1019). Renders on every authenticated route;
 * never appears on pre-auth routes (it is mounted inside Layout which is
 * structurally unreachable from unauthenticated routes).
 *
 * Accepts `daemonStatus` as a prop from Layout — no internal daemon polling.
 * This keeps daemon-health state as a single source of truth in Layout.
 *
 * States covered:
 *  - Healthy       — daemon connected, green "Synced: HH:MM" label
 *  - DaemonOffline — daemon not connected, red "Daemon offline" label
 *  - ZeroMatches   — no matches played yet; strip still renders (AC1)
 *  - Loading       — stats fetch in flight
 *
 * Static-HTML render pattern (same as retired Footer.stories.tsx):
 * These stories use the component's CSS classes directly for stable Chromatic
 * snapshots without triggering real API calls.
 */

const meta: Meta = {
  title: 'Organisms/StatusStrip',
  parameters: {
    layout: 'fullscreen',
  },
  tags: ['autodocs'],
};

export default meta;
type Story = StoryObj;

const connectedStatus: DaemonHealthState = 'connected';
const disconnectedStatus: DaemonHealthState = 'disconnected';

/** Healthy state — daemon connected, green Synced indicator, populated stats. */
export const Healthy: Story = {
  render: () => (
    <footer
      className="status-strip"
      data-testid="status-strip"
      data-daemon-status={connectedStatus}
    >
      <div className="status-strip-content">
        <span className="status-strip-label">All Time</span>
        <span className="status-strip-sep">·</span>
        <span className="status-strip-stat">
          <strong>Matches:</strong>{' '}
          <span className="status-strip-num">248</span>
        </span>
        <span className="status-strip-sep">·</span>
        <span className="status-strip-stat">
          <strong>Win Rate:</strong>{' '}
          <span className="status-strip-num">58.4% (145-103)</span>
        </span>
        <span className="status-strip-sep">·</span>
        <span className="status-strip-stat status-strip-streak-w">
          <strong>Streak:</strong>{' '}
          <span className="status-strip-num">W3</span>
        </span>
        <span className="status-strip-sep status-strip-sep-push">·</span>
        <span className="status-strip-stat">
          <strong>Last Played:</strong>{' '}
          <span className="status-strip-num">5/4/2026, 10:14 PM</span>
        </span>
        <span className="status-strip-sep">·</span>
        <span className="status-strip-synced status-strip-synced-ok">
          <strong>Synced:</strong>{' '}
          <span className="status-strip-num">10:18 PM</span>
        </span>
      </div>
    </footer>
  ),
};

/** Daemon offline — red "Daemon offline" label; synced time is hidden. (AC4) */
export const DaemonOffline: Story = {
  render: () => (
    <footer
      className="status-strip"
      data-testid="status-strip"
      data-daemon-status={disconnectedStatus}
    >
      <div className="status-strip-content">
        <span className="status-strip-label">All Time</span>
        <span className="status-strip-sep">·</span>
        <span className="status-strip-stat">
          <strong>Matches:</strong>{' '}
          <span className="status-strip-num">248</span>
        </span>
        <span className="status-strip-sep">·</span>
        <span className="status-strip-stat">
          <strong>Win Rate:</strong>{' '}
          <span className="status-strip-num">58.4% (145-103)</span>
        </span>
        <span className="status-strip-sep">·</span>
        <span className="status-strip-stat status-strip-streak-w">
          <strong>Streak:</strong>{' '}
          <span className="status-strip-num">W3</span>
        </span>
        <span className="status-strip-sep status-strip-sep-push">·</span>
        <span className="status-strip-stat">
          <strong>Last Played:</strong>{' '}
          <span className="status-strip-num">5/4/2026, 10:14 PM</span>
        </span>
        <span className="status-strip-sep">·</span>
        <span className="status-strip-synced status-strip-offline">
          Daemon offline
        </span>
      </div>
    </footer>
  ),
};

/**
 * Zero matches — strip still renders (AC1), shows Matches: 0, Win Rate: --,
 * and daemon-offline indicator because daemonStatus is disconnected.
 * This story documents the critical AC1+AC4 intersection: the strip must
 * always render regardless of match count.
 */
export const ZeroMatchesDaemonOffline: Story = {
  render: () => (
    <footer
      className="status-strip"
      data-testid="status-strip"
      data-daemon-status={disconnectedStatus}
    >
      <div className="status-strip-content">
        <span className="status-strip-label">All Time</span>
        <span className="status-strip-sep">·</span>
        <span className="status-strip-stat">
          <strong>Matches:</strong>{' '}
          <span className="status-strip-num">0</span>
        </span>
        <span className="status-strip-sep">·</span>
        <span className="status-strip-stat">
          <strong>Win Rate:</strong>{' '}
          <span className="status-strip-num">--</span>
        </span>
        <span className="status-strip-sep status-strip-sep-push">·</span>
        <span className="status-strip-synced status-strip-offline">
          Daemon offline
        </span>
      </div>
    </footer>
  ),
};

/** Loading state — stats fetch in flight. */
export const Loading: Story = {
  render: () => (
    <footer className="status-strip" data-testid="status-strip">
      <div className="status-strip-content">
        <span className="status-strip-loading">Loading stats...</span>
      </div>
    </footer>
  ),
};

/** Loss streak variant. */
export const LossStreak: Story = {
  render: () => (
    <footer
      className="status-strip"
      data-testid="status-strip"
      data-daemon-status={connectedStatus}
    >
      <div className="status-strip-content">
        <span className="status-strip-label">All Time</span>
        <span className="status-strip-sep">·</span>
        <span className="status-strip-stat">
          <strong>Matches:</strong>{' '}
          <span className="status-strip-num">37</span>
        </span>
        <span className="status-strip-sep">·</span>
        <span className="status-strip-stat">
          <strong>Win Rate:</strong>{' '}
          <span className="status-strip-num">43.2% (16-21)</span>
        </span>
        <span className="status-strip-sep">·</span>
        <span className="status-strip-stat status-strip-streak-l">
          <strong>Streak:</strong>{' '}
          <span className="status-strip-num">L3</span>
        </span>
        <span className="status-strip-sep status-strip-sep-push">·</span>
        <span className="status-strip-stat">
          <strong>Last Played:</strong>{' '}
          <span className="status-strip-num">5/31/2026, 8:22 PM</span>
        </span>
        <span className="status-strip-sep">·</span>
        <span className="status-strip-synced status-strip-synced-ok">
          <strong>Synced:</strong>{' '}
          <span className="status-strip-num">8:25 PM</span>
        </span>
      </div>
    </footer>
  ),
};
