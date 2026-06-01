import type { Meta, StoryObj } from '@storybook/react';
import './WinRatePrediction.css';

/**
 * WinRatePrediction gradient accents.
 *
 * Token migration (#328): the predicted win-rate gradient, premium-performer
 * accent, and curve-bar fill moved off the raw bright-green `#44ff88` onto the
 * on-brand `--win` token (= `--vault-sapphire`).
 */
const meta: Meta = {
  title: 'Components/WinRatePrediction/Accents',
  parameters: { layout: 'centered' },
};

export default meta;
type Story = StoryObj;

export const PredictedWinRate: Story = {
  render: () => <div className="predicted-win-rate-large">63%</div>,
};

export const PremiumPerformer: Story = {
  render: () => (
    <div className="performer-item premium" style={{ width: 240 }}>
      Top performer
    </div>
  ),
};

export const CurveBar: Story = {
  render: () => (
    <div style={{ display: 'flex', alignItems: 'flex-end', height: 180, width: 60 }}>
      <div className="curve-bar-fill" style={{ height: 140 }} />
    </div>
  ),
};
