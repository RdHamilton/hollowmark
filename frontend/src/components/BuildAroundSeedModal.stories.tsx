import type { Meta, StoryObj } from '@storybook/react';
import './BuildAroundSeedModal.css';

/**
 * BuildAroundSeedModal mana pips + wildcard rarity badges.
 *
 * Token migration (#328): the `.mana-<x>` / `.color-filter-btn.mana-<x>` pips
 * moved onto the MTG-canonical `--mana-*-bg` tokens with dark `--mana-pip-fg`
 * text; the `.wildcard-badge.<rarity>` chips moved onto `--vault-rarity-*`
 * tokens; the quality-bar gradient onto `--vault-decorative-magenta*`.
 */
const meta: Meta = {
  title: 'Components/BuildAroundSeedModal/Tokens',
  parameters: { layout: 'centered' },
};

export default meta;
type Story = StoryObj;

const FILTERS: Array<[string, string]> = [
  ['mana-w', 'W'],
  ['mana-u', 'U'],
  ['mana-b', 'B'],
  ['mana-r', 'R'],
  ['mana-g', 'G'],
];

export const ColorFilterButtons: Story = {
  render: () => (
    <div style={{ display: 'flex', gap: 8 }}>
      {FILTERS.map(([cls, label]) => (
        <button key={cls} className={`color-filter-btn ${cls}`} type="button">
          {label}
        </button>
      ))}
    </div>
  ),
};

const RARITIES = ['common', 'uncommon', 'rare', 'mythic'];

export const WildcardBadges: Story = {
  render: () => (
    <div style={{ display: 'flex', gap: 8 }}>
      {RARITIES.map((r) => (
        <span
          key={r}
          className={`wildcard-badge ${r}`}
          style={{ padding: '2px 8px', borderRadius: 4 }}
        >
          {r}
        </span>
      ))}
    </div>
  ),
};

export const QualityBar: Story = {
  render: () => (
    <div className="score-bar quality-bar" style={{ width: 200, height: 12, borderRadius: 6 }} />
  ),
};
