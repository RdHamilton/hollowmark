import type { Meta, StoryObj } from '@storybook/react';
import './Quests.css';

/**
 * Quests status badges + progress fills.
 *
 * Token migration (#328): the weekly progress fill moved off the raw
 * `#ab47bc → #8e24aa` onto `--vault-decorative-quest-*`; the quest progress fill
 * onto `--accent` / `--vault-sapphire-light`; the incomplete status badge onto
 * `--vault-warning-dim` / `--warning`; completed/rerolled badge text onto
 * `--fg-inverse`.
 */
const meta: Meta = {
  title: 'Pages/Quests/StatusBadges',
  parameters: { layout: 'centered' },
};

export default meta;
type Story = StoryObj;

export const StatusBadges: Story = {
  render: () => (
    <div style={{ display: 'flex', gap: 8 }}>
      <span className="status-badge completed">Completed</span>
      <span className="status-badge incomplete">Incomplete</span>
      <span className="status-badge rerolled">Rerolled</span>
    </div>
  ),
};

export const ProgressFills: Story = {
  render: () => (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 12, width: 280 }}>
      <div className="quest-progress-bar">
        <div className="quest-progress-fill" style={{ width: '60%' }} />
      </div>
      <div className="daily-wins-bar">
        <div className="daily-wins-fill weekly" style={{ width: '80%' }} />
      </div>
    </div>
  ),
};
