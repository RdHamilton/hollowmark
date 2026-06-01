import type { Meta, StoryObj } from '@storybook/react';
import './RankProgression.css';

/**
 * RankProgression trend indicators.
 *
 * Token migration (#328): `.trend-up` moved off `#7dff7d` onto `--win`
 * (= `--vault-sapphire`); `.trend-down` off `#ff7d7d` onto `--loss`
 * (= `--vault-danger`).
 */
const meta: Meta = {
  title: 'Pages/RankProgression/Trends',
  parameters: { layout: 'centered' },
};

export default meta;
type Story = StoryObj;

export const TrendIndicators: Story = {
  render: () => (
    <div style={{ display: 'flex', gap: 24, fontWeight: 700 }}>
      <span className="trend-up">▲ Up</span>
      <span className="trend-down">▼ Down</span>
      <span className="trend-stable">— Stable</span>
    </div>
  ),
};
