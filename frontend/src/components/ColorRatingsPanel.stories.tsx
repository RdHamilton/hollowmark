import type { Meta, StoryObj } from '@storybook/react';
import './ColorRatingsPanel.css';

/**
 * ColorRatingsPanel win-rate quality scale.
 *
 * Token migration (#328): the Catppuccin data-viz hues moved onto semantic
 * tokens — excellent → `--success`, good → `--warning`, average →
 * `--fg-secondary`, below → `--danger`.
 */
const meta: Meta = {
  title: 'Components/ColorRatingsPanel/WinRateScale',
  parameters: { layout: 'centered' },
};

export default meta;
type Story = StoryObj;

export const WinRateScale: Story = {
  render: () => (
    <div style={{ display: 'flex', gap: 24, fontWeight: 700 }}>
      <span className="win-rate wr-excellent">62%</span>
      <span className="win-rate wr-good">54%</span>
      <span className="win-rate wr-average">50%</span>
      <span className="win-rate wr-below">43%</span>
    </div>
  ),
};
