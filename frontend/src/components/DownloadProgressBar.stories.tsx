import type { Meta, StoryObj } from '@storybook/react';
import './DownloadProgressBar.css';

/**
 * DownloadProgressBar fill states.
 *
 * Token migration (#328): the "complete" fill gradient moved off the raw
 * bright-green `#4aff4a → #6bff6b` onto the on-brand `--win` /
 * `--vault-sapphire-light` tokens.
 */
const meta: Meta = {
  title: 'Components/DownloadProgressBar/States',
  parameters: { layout: 'centered' },
};

export default meta;
type Story = StoryObj;

const Bar = ({ state, pct }: { state: string; pct: number }) => (
  <div className="download-progress-bar" style={{ width: 280 }}>
    <div className={`download-progress-fill ${state}`} style={{ width: `${pct}%` }} />
  </div>
);

export const States: Story = {
  render: () => (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
      <Bar state="downloading" pct={45} />
      <Bar state="complete" pct={100} />
      <Bar state="error" pct={70} />
    </div>
  ),
};
