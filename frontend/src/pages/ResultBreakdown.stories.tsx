import type { Meta, StoryObj } from '@storybook/react';
import './ResultBreakdown.css';

/**
 * ResultBreakdown win/loss metric values + bars.
 *
 * Token migration (#328, Ramone 2026-05-31): win indicators moved off the raw
 * bright-green `#7dff7d` onto the on-brand `--win` (= `--vault-sapphire`);
 * loss indicators off `#ff7d7d` onto `--loss` (= `--vault-danger`). Rendered as
 * the migrated `.metric-value.win|loss` + `.win-loss-bar` classes.
 */
const meta: Meta = {
  title: 'Pages/ResultBreakdown/WinLoss',
  parameters: { layout: 'centered' },
};

export default meta;
type Story = StoryObj;

export const MetricValues: Story = {
  render: () => (
    <div className="metric-grid" style={{ display: 'flex', gap: 24 }}>
      <div className="metric-card">
        <div className="metric-label">Wins</div>
        <div className="metric-value win">42</div>
      </div>
      <div className="metric-card">
        <div className="metric-label">Losses</div>
        <div className="metric-value loss">17</div>
      </div>
    </div>
  ),
};
