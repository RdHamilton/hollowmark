import type { Meta, StoryObj } from '@storybook/react';
import './CardSearch.css';

/**
 * CardSearch mana-color filter pips.
 *
 * Token migration (#328 / Ramone 2026-05-31): the WUBRG color-button
 * backgrounds moved off raw MTG-frame hex onto the MTG-canonical mana-pip
 * tokens (`--mana-w-bg` … `--mana-g-bg`) with dark `--mana-pip-fg` text. These
 * stories render the migrated `.color-button.<color>` classes directly so
 * Chromatic snapshots the exact recolored pips without mounting the (API-bound)
 * CardSearch component.
 */
const meta: Meta = {
  title: 'Components/CardSearch/ManaPips',
  parameters: { layout: 'centered' },
};

export default meta;
type Story = StoryObj;

const COLORS: Array<[string, string]> = [
  ['white', 'W'],
  ['blue', 'U'],
  ['black', 'B'],
  ['red', 'R'],
  ['green', 'G'],
  ['colorless', 'C'],
];

export const ColorFilterButtons: Story = {
  render: () => (
    <div className="color-filters">
      {COLORS.map(([cls, label]) => (
        <button key={cls} className={`color-button ${cls}`} type="button">
          {label}
        </button>
      ))}
    </div>
  ),
};

export const ColorFilterButtonsActive: Story = {
  render: () => (
    <div className="color-filters">
      {COLORS.map(([cls, label]) => (
        <button key={cls} className={`color-button ${cls} active`} type="button">
          {label}
        </button>
      ))}
    </div>
  ),
};

/** The multicolor swatch (full WUBRG token gradient). */
export const MulticolorSwatch: Story = {
  render: () => (
    <div className="color-filters">
      <button className="color-button multicolor" type="button">
        M
      </button>
    </div>
  ),
};
