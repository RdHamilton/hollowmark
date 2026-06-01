import type { Meta, StoryObj } from '@storybook/react';
import './CurrentPackPicker.css';

/**
 * CurrentPackPicker color-identity indicators.
 *
 * Token migration (#328): the `.color-indicator.color-<x>` mana swatches moved
 * onto the MTG-canonical `--mana-*-bg` tokens with dark `--mana-pip-fg` text.
 * Rendered as standalone classes for a deterministic Chromatic snapshot.
 */
const meta: Meta = {
  title: 'Components/CurrentPackPicker/ColorIndicators',
  parameters: { layout: 'centered' },
};

export default meta;
type Story = StoryObj;

const COLORS: Array<[string, string]> = [
  ['color-w', 'W'],
  ['color-u', 'U'],
  ['color-b', 'B'],
  ['color-r', 'R'],
  ['color-g', 'G'],
  ['colorless', 'C'],
];

export const ColorIndicators: Story = {
  render: () => (
    <div style={{ display: 'flex', gap: 8 }}>
      {COLORS.map(([cls, label]) => (
        <span key={cls} className={`color-indicator ${cls}`}>
          {label}
        </span>
      ))}
    </div>
  ),
};
