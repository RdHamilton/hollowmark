import type { Meta, StoryObj } from '@storybook/react';
import './DeckList.css';

/**
 * DeckList sideboard accents.
 *
 * Token migration (#328): the sideboard count badge + headers moved off the raw
 * `#ba68c8` onto the `--vault-decorative-purple-light` token.
 */
const meta: Meta = {
  title: 'Components/DeckList/Sideboard',
  parameters: { layout: 'centered' },
};

export default meta;
type Story = StoryObj;

export const SideboardBadge: Story = {
  render: () => (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 12, width: 240 }}>
      <span className="count-badge sideboard" style={{ padding: '2px 8px', borderRadius: 4, width: 'fit-content' }}>
        15 cards
      </span>
      <div className="sideboard-header">
        <h3>Sideboard</h3>
      </div>
      <button className="toggle-sideboard" type="button">
        ▾ Toggle
      </button>
    </div>
  ),
};
