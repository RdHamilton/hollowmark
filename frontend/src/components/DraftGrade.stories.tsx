import type { Meta, StoryObj } from '@storybook/react';
import './DraftGrade.css';

/**
 * DraftGrade best/worst pick accents.
 *
 * Token migration (#328): `.picks-title.best` and `.pick-item.best` moved off
 * the raw bright-green `#44ff88` onto the on-brand `--win`
 * (= `--vault-sapphire`); the worst-pick accents already use `--danger`.
 */
const meta: Meta = {
  title: 'Components/DraftGrade/Picks',
  parameters: { layout: 'centered' },
};

export default meta;
type Story = StoryObj;

export const BestVsWorst: Story = {
  render: () => (
    <div style={{ width: 280 }}>
      <h4 className="picks-title best">Best picks</h4>
      <ul className="picks-list">
        <li className="pick-item best">Sheoldred, the Apocalypse</li>
      </ul>
      <h4 className="picks-title worst">Worst picks</h4>
      <ul className="picks-list">
        <li className="pick-item">Vanilla 2/2</li>
      </ul>
    </div>
  ),
};
