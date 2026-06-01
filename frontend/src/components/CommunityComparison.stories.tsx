import type { Meta, StoryObj } from '@storybook/react';
import './CommunityComparison.css';

/**
 * CommunityComparison color swatches + rank gradient.
 *
 * Token migration (#328): the `.community-comparison__color--<x>` mana swatches
 * moved onto the MTG-canonical `--mana-*-bg` tokens, and the "high rank"
 * gradient moved onto the brand `--win` / `--vault-sapphire-dark` tokens.
 */
const meta: Meta = {
  title: 'Components/CommunityComparison/Swatches',
  parameters: { layout: 'centered' },
};

export default meta;
type Story = StoryObj;

const COLORS: Array<[string, string]> = [
  ['community-comparison__color--w', 'W'],
  ['community-comparison__color--u', 'U'],
  ['community-comparison__color--b', 'B'],
  ['community-comparison__color--r', 'R'],
  ['community-comparison__color--g', 'G'],
];

export const ColorSwatches: Story = {
  render: () => (
    <div style={{ display: 'flex', gap: 8 }}>
      {COLORS.map(([cls, label]) => (
        <span key={cls} className={`community-comparison__color ${cls}`}>
          {label}
        </span>
      ))}
    </div>
  ),
};

export const HighRankGradient: Story = {
  render: () => (
    <span
      className="community-comparison__rank community-comparison__rank--high"
      style={{ padding: '4px 12px', borderRadius: 6 }}
    >
      High
    </span>
  ),
};
