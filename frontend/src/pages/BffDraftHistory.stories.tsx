import type { Meta, StoryObj } from '@storybook/react';
import './BffDraftHistory.css';

/**
 * BffDraftHistory — cloud-synced draft history page (offset-paginated BFF).
 *
 * Served at the `/draft-history` route for authenticated users. Data is fetched
 * from the BFF GET /api/v1/history/drafts endpoint.
 *
 * Font contract (#684): heading uses Space Grotesk (--font-display-vault),
 * table data uses Inter (--font-body). No Cormorant Garamond.
 *
 * Heading copy (#685): the page title is the plain string "Draft History".
 * No § Chapter / The Draft lorebook framing.
 */
const meta: Meta = {
  title: 'Pages/BffDraftHistory',
  parameters: {
    layout: 'fullscreen',
  },
};

export default meta;
type Story = StoryObj;

/**
 * PageHeader — renders the "Draft History" h1 in isolation for Chromatic.
 *
 * Primary Chromatic gate for #684 (no Cormorant) and #685 (no lorebook).
 */
export const PageHeader: Story = {
  render: () => (
    <div
      style={{
        background: 'var(--vault-bg, #0D1117)',
        padding: '1.5rem',
      }}
    >
      <div className="bff-draft-history-header">
        <h1 className="page-title">Draft History</h1>
      </div>
    </div>
  ),
};

/**
 * EmptyState — no drafts returned from the BFF.
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
      <div className="bff-draft-history-header">
        <h1 className="page-title">Draft History</h1>
      </div>
      <div
        data-testid="draft-history-empty"
        style={{
          color: 'var(--vault-fg-secondary, #94A3B8)',
          textAlign: 'center',
          padding: '3rem 1rem',
        }}
      >
        <p style={{ fontSize: '2rem', marginBottom: '0.5rem' }}>🃏</p>
        <h2 style={{ fontFamily: 'var(--font-display-vault)', marginBottom: '0.5rem' }}>
          No drafts yet
        </h2>
        <p style={{ fontFamily: 'var(--font-body)' }}>
          Your cloud draft history will appear here once synced.
        </p>
      </div>
    </div>
  ),
};

/**
 * TableSkeleton — sample draft history table with two rows.
 * Documents the column schema: Date, Set, Wins, Losses.
 */
export const TableSkeleton: Story = {
  render: () => (
    <div
      style={{
        background: 'var(--vault-bg, #0D1117)',
        padding: '1.5rem',
      }}
    >
      <div className="bff-draft-history-header">
        <h1 className="page-title">Draft History</h1>
      </div>
      <div className="bff-draft-history-table-wrapper">
        <table data-testid="draft-history-table">
          <thead>
            <tr>
              <th>Date</th>
              <th>Set</th>
              <th>Wins</th>
              <th>Losses</th>
            </tr>
          </thead>
          <tbody>
            <tr>
              <td>Jun 1, 2026</td>
              <td>BLB</td>
              <td>7</td>
              <td>2</td>
            </tr>
            <tr>
              <td>May 28, 2026</td>
              <td>DSK</td>
              <td>5</td>
              <td>3</td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  ),
};
