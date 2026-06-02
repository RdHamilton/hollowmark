import type { Meta, StoryObj } from '@storybook/react';
import './BffMatchHistory.css';

/**
 * BffMatchHistory — cloud-synced match history page (cursor-paginated BFF).
 *
 * Served at the `/matches` route for authenticated users. Data is fetched from
 * the BFF GET /api/v1/history/matches endpoint (ADR-039 cursor-paginated shape).
 *
 * Font contract (#684): headings use Space Grotesk (--font-display-vault),
 * table data uses Inter (--font-body). No Cormorant Garamond.
 *
 * Heading copy (#685): the page title is the plain string "Match History".
 * No § Chapter / The Ledger lorebook framing.
 *
 * These stories render the header + skeleton states using Chromatic-stable
 * static fixtures. The full live component requires a Clerk auth token and a
 * running BFF — use the Playwright staging smoke for that surface.
 */
const meta: Meta = {
  title: 'Pages/BffMatchHistory',
  parameters: {
    layout: 'fullscreen',
  },
};

export default meta;
type Story = StoryObj;

/**
 * PageHeader — renders the "Match History" h1 in isolation to give Chromatic
 * a stable snapshot of the heading font, size, and copy.
 *
 * This story is the primary Chromatic gate for #684 (no Cormorant) and
 * #685 (no lorebook affectations).
 */
export const PageHeader: Story = {
  render: () => (
    <div
      style={{
        background: 'var(--vault-bg, #0D1117)',
        padding: '1.5rem',
      }}
    >
      <div className="bff-match-history-header">
        <h1 className="page-title">Match History</h1>
      </div>
    </div>
  ),
};

/**
 * EmptyState — no matches returned from the BFF.
 * Shows the processing-aware empty message (the one Prof approved).
 */
export const EmptyState: Story = {
  render: () => (
    <div
      style={{
        background: 'var(--vault-bg, #0D1117)',
        padding: '1.5rem',
        minHeight: '300px',
        display: 'flex',
        flexDirection: 'column',
        gap: '1rem',
      }}
    >
      <div className="bff-match-history-header">
        <h1 className="page-title">Match History</h1>
      </div>
      <div
        data-testid="match-history-empty"
        style={{
          color: 'var(--vault-fg-secondary, #94A3B8)',
          textAlign: 'center',
          padding: '3rem 1rem',
        }}
      >
        <p style={{ fontSize: '2rem', marginBottom: '0.5rem' }}>🎮</p>
        <h2 style={{ fontFamily: 'var(--font-display-vault)', marginBottom: '0.5rem' }}>
          No recent matches
        </h2>
        <p style={{ fontFamily: 'var(--font-body)' }}>
          Your recent matches are loading — new matches usually appear within a minute.
        </p>
      </div>
    </div>
  ),
};

/**
 * TableSkeleton — shows the full 6-column table schema introduced in vmt-t#687:
 * Date, Format, Result, Score, P/D (play/draw), Opponent.
 * Win/loss row colorings and play/draw badges validate semantic tokens.
 * Third row demonstrates null/absent fields (pre-release match).
 */
export const TableSkeleton: Story = {
  render: () => (
    <div
      style={{
        background: 'var(--vault-bg, #0D1117)',
        padding: '1.5rem',
      }}
    >
      <div className="bff-match-history-header">
        <h1 className="page-title">Match History</h1>
      </div>
      <div className="bff-match-history-table-wrapper">
        <table data-testid="match-history-table">
          <thead>
            <tr>
              <th>Date</th>
              <th>Format</th>
              <th>Result</th>
              <th>Score</th>
              <th title="On the Play (P) or On the Draw (D)">P/D</th>
              <th>Opponent</th>
            </tr>
          </thead>
          <tbody>
            <tr className="result-win clickable-row" data-testid="match-row">
              <td>Jun 1, 2026</td>
              <td>Standard</td>
              <td>
                <span className="result-badge win">WIN</span>
              </td>
              <td>2–0</td>
              <td>
                <span className="play-draw-badge on-play">P</span>
              </td>
              <td>
                <span className="opponent-name">Esper Reanimator</span>
              </td>
            </tr>
            <tr className="result-loss clickable-row" data-testid="match-row">
              <td>Jun 1, 2026</td>
              <td>Quick Draft · BLB</td>
              <td>
                <span className="result-badge loss">LOSS</span>
              </td>
              <td>1–2</td>
              <td>
                <span className="play-draw-badge on-draw">D</span>
              </td>
              <td>
                <span className="opponent-name">Sultai Ramp</span>
              </td>
            </tr>
            <tr className="result-win clickable-row" data-testid="match-row">
              <td>May 31, 2026</td>
              <td>Ranked</td>
              <td>
                <span className="result-badge win">WIN</span>
              </td>
              <td>2–1</td>
              {/* Pre-release row: player_on_play and opponent_name absent — blank cells */}
              <td />
              <td />
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  ),
};

/**
 * PlayDrawBadges — isolated view of the on-the-play / on-the-draw pip badges.
 * Chromatic gate for vmt-t#687: validates badge appearance, font weight,
 * and semantic token colors (--info for on-play; --fg-secondary for on-draw).
 */
export const PlayDrawBadges: Story = {
  render: () => (
    <div
      style={{
        background: 'var(--vault-bg, #0D1117)',
        padding: '1.5rem',
        display: 'flex',
        gap: '1rem',
        alignItems: 'center',
      }}
    >
      <span className="play-draw-badge on-play">P</span>
      <span style={{ color: 'var(--vault-fg-secondary, #94A3B8)', fontSize: '0.8rem' }}>
        On the Play
      </span>
      <span className="play-draw-badge on-draw" style={{ marginLeft: '1.5rem' }}>D</span>
      <span style={{ color: 'var(--vault-fg-secondary, #94A3B8)', fontSize: '0.8rem' }}>
        On the Draw
      </span>
    </div>
  ),
};
