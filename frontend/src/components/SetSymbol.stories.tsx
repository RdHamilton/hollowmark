import type { Meta, StoryObj } from '@storybook/react';
import './SetSymbol.css';

/**
 * SetSymbol text-fallback rarity chips.
 *
 * Token migration (#328): the rare/mythic text-fallback chips moved off raw hex
 * onto tokens — rare bg → `--vault-warning-dim`, mythic color →
 * `--vault-rarity-mythic`, mythic bg → `--vault-danger-dim`. Rendered as the
 * text-fallback classes (the live component fetches set art from the API).
 */
const meta: Meta = {
  title: 'Components/SetSymbol/RarityChips',
  parameters: { layout: 'centered' },
};

export default meta;
type Story = StoryObj;

const RARITIES = ['common', 'uncommon', 'rare', 'mythic'];

export const RarityChips: Story = {
  render: () => (
    <div style={{ display: 'flex', gap: 8 }}>
      {RARITIES.map((r) => (
        <span
          key={r}
          className={`set-symbol-text set-symbol-${r}`}
          style={{ padding: '4px 8px', borderRadius: 4, fontWeight: 700 }}
        >
          {r[0].toUpperCase()}
        </span>
      ))}
    </div>
  ),
};
