import type { Meta, StoryObj } from '@storybook/react';
import './SuggestDecksModal.css';

/**
 * SuggestDecksModal mana pips.
 *
 * Token migration (#328): the `.mana-<x>` pips moved onto the MTG-canonical
 * `--mana-*-bg` tokens with dark `--mana-pip-fg` text. Rendered as standalone
 * `.mana-pip` swatches for a deterministic Chromatic snapshot (the full modal
 * is API/state-bound).
 */
const meta: Meta = {
  title: 'Components/SuggestDecksModal/ManaPips',
  parameters: { layout: 'centered' },
};

export default meta;
type Story = StoryObj;

const PIPS: Array<[string, string]> = [
  ['mana-w', 'W'],
  ['mana-u', 'U'],
  ['mana-b', 'B'],
  ['mana-r', 'R'],
  ['mana-g', 'G'],
];

export const ManaPips: Story = {
  render: () => (
    <div style={{ display: 'flex', gap: 8 }}>
      {PIPS.map(([cls, label]) => (
        <span key={cls} className={`mana-pip ${cls}`}>
          {label}
        </span>
      ))}
    </div>
  ),
};
