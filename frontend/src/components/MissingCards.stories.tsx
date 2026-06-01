import type { Meta, StoryObj } from '@storybook/react';
import './MissingCards.css';

/**
 * MissingCards tier badges.
 *
 * Token migration (#328): the tier-badge grade gradients moved off raw hex onto
 * tokens — tier-A green → `--win` / `--vault-sapphire-light`, tier-D orange →
 * `--vault-grade-orange*`, tier-F red → `--danger` / `--loss`, gold companions
 * → `--vault-grade-gold-light` / `--vault-grade-amber-light`.
 */
const meta: Meta = {
  title: 'Components/MissingCards/TierBadges',
  parameters: { layout: 'centered' },
};

export default meta;
type Story = StoryObj;

const TIERS = ['tier-s', 'tier-a', 'tier-b', 'tier-c', 'tier-d', 'tier-f'];

export const TierBadges: Story = {
  render: () => (
    <div style={{ display: 'flex', gap: 8 }}>
      {TIERS.map((t) => (
        <span key={t} className={`tier-badge ${t}`}>
          {t.replace('tier-', '').toUpperCase()}
        </span>
      ))}
    </div>
  ),
};
