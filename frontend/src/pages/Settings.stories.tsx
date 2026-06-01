import type { Meta, StoryObj } from '@storybook/react';
import './Settings.css';

/**
 * Settings connection-status + replay accents.
 *
 * Token migration (#328): the connected-status text/dot and replay-complete
 * accents moved off the raw bright-green `#7dff7d` / `#7cfc00` onto the on-brand
 * `--win` token (= `--vault-sapphire`); success/error message text onto
 * `--fg-inverse`. Rendered as the migrated classes for a stable Chromatic
 * snapshot (the full Settings page is Clerk/API-bound).
 */
const meta: Meta = {
  title: 'Pages/Settings/StatusAccents',
  parameters: { layout: 'centered' },
};

export default meta;
type Story = StoryObj;

export const ConnectionStatus: Story = {
  render: () => (
    <div className="status-connected" style={{ display: 'inline-flex', alignItems: 'center', gap: 8, padding: '6px 12px', borderRadius: 6 }}>
      <span className="status-dot" style={{ width: 10, height: 10, borderRadius: '50%' }} />
      Connected
    </div>
  ),
};

export const ReplayComplete: Story = {
  render: () => <span className="replay-progress-title complete">Replay complete</span>,
};
