import type { Meta, StoryObj } from '@storybook/react';
import './GamePlayTimelinePanel.css';

/**
 * GamePlayTimelinePanel player/opponent + life-delta accents.
 *
 * Token migration (#328): the player-side positive accents moved off the raw
 * bright-green `#7dff7d` onto `--win` (= `--vault-sapphire`); the opponent-side
 * accents off `#ff7d7d` onto `--loss` (= `--vault-danger`).
 */
const meta: Meta = {
  title: 'Components/GamePlayTimelinePanel/Accents',
  parameters: { layout: 'centered' },
};

export default meta;
type Story = StoryObj;

export const PlayItems: Story = {
  render: () => (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 8, width: 260 }}>
      <div className="play-item player" style={{ padding: 8 }}>
        Player play
      </div>
      <div className="play-item opponent" style={{ padding: 8 }}>
        Opponent play
      </div>
    </div>
  ),
};

export const SnapshotValues: Story = {
  render: () => (
    <div className="snapshot-value" style={{ display: 'flex', gap: 24, fontWeight: 700 }}>
      <span className="player-lands">Player: 5</span>
      <span className="opponent-lands">Opponent: 4</span>
    </div>
  ),
};
