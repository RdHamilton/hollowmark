import type { Meta, StoryObj } from '@storybook/react';
import './Decks.css';

/**
 * Decks source badge.
 *
 * Token migration (#328): the `.source-badge.import` chip moved off the raw
 * `#9c27b0` onto the `--vault-decorative-purple` token.
 */
const meta: Meta = {
  title: 'Pages/Decks/SourceBadge',
  parameters: { layout: 'centered' },
};

export default meta;
type Story = StoryObj;

export const SourceBadges: Story = {
  render: () => (
    <div style={{ display: 'flex', gap: 8 }}>
      <span className="source-badge import" style={{ padding: '2px 8px', borderRadius: 4 }}>
        Imported
      </span>
    </div>
  ),
};
