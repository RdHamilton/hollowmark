import type { Meta, StoryObj } from '@storybook/react';
import './OpponentAnalysisPanel.css';

/**
 * OpponentAnalysisPanel color-identity mana symbols.
 *
 * Token migration (#328): the `.mana-<color>` swatches moved onto the
 * MTG-canonical `--mana-*-bg` tokens with dark `--mana-pip-fg` text. Rendered
 * inside the `.opponent-analysis-panel` scope so the scoped selectors apply.
 */
const meta: Meta = {
  title: 'Components/OpponentAnalysisPanel/ColorIdentity',
  parameters: { layout: 'centered' },
};

export default meta;
type Story = StoryObj;

const COLORS: Array<[string, string]> = [
  ['mana-white', 'W'],
  ['mana-blue', 'U'],
  ['mana-black', 'B'],
  ['mana-red', 'R'],
  ['mana-green', 'G'],
  ['mana-colorless', 'C'],
];

export const ColorIdentity: Story = {
  render: () => (
    <div className="opponent-analysis-panel">
      <div className="color-identity">
        {COLORS.map(([cls, label]) => (
          <span key={cls} className={`mana-symbol ${cls}`}>
            {label}
          </span>
        ))}
      </div>
    </div>
  ),
};
