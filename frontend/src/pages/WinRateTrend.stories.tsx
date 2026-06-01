import type { Meta, StoryObj } from '@storybook/react';
import './WinRateTrend.css';

/**
 * WinRateTrend trend indicators.
 *
 * Token migration (#328): `.trend-improving` moved off `#7dff7d` onto `--win`
 * (= `--vault-sapphire`); `.trend-declining` off `#ff7d7d` onto `--loss`
 * (= `--vault-danger`).
 */
const meta: Meta = {
  title: 'Pages/WinRateTrend/Trends',
  parameters: { layout: 'centered' },
};

export default meta;
type Story = StoryObj;

export const TrendIndicators: Story = {
  render: () => (
    <div style={{ display: 'flex', gap: 24, fontWeight: 700 }}>
      <span className="trend-improving">▲ Improving</span>
      <span className="trend-declining">▼ Declining</span>
      <span className="trend-stable">— Stable</span>
    </div>
  ),
};
