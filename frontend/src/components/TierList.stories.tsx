import type { Meta, StoryObj } from '@storybook/react';
import './TierList.css';

/**
 * TierList picked-marker + error accents.
 *
 * Token migration (#328): `.picked-marker` moved off the raw `#44ff88` onto the
 * on-brand `--win` token (= `--vault-sapphire`); the error heading off `#ff8844`
 * onto `--vault-grade-orange`.
 */
const meta: Meta = {
  title: 'Components/TierList/Accents',
  parameters: { layout: 'centered' },
};

export default meta;
type Story = StoryObj;

export const PickedMarker: Story = {
  render: () => <span className="picked-marker">✓ Picked</span>,
};

export const ErrorState: Story = {
  render: () => (
    <div className="tier-list-error">
      <p>Could not load tier list</p>
    </div>
  ),
};

/**
 * Tier badge color states — D17 / ADR-074 canonical severity ordering.
 *
 * F (deep oxblood #7F1414) is the most dire tier; D (red #EF4444) is below
 * average. The severity gradient runs F → D → C → B → A → S.
 * Any Chromatic diff on these stories requires Ramone acceptance — it means
 * the D17 token values have changed.
 */

const TIER_COLORS: Record<string, string> = {
  S: '#FFD700',
  A: '#4A90D9',
  B: '#22C55E',
  C: '#94A3B8',
  D: '#EF4444',
  F: '#7F1414',
};

const TierBadgeRow = () => (
  <div style={{ display: 'flex', gap: '8px', alignItems: 'center' }}>
    {(['S', 'A', 'B', 'C', 'D', 'F'] as const).map((tier) => (
      <span
        key={tier}
        className="tier-badge"
        style={{ backgroundColor: TIER_COLORS[tier] }}
      >
        {tier}
      </span>
    ))}
  </div>
);

export const TierBadges: Story = {
  render: () => <TierBadgeRow />,
};

export const TierBadgeF: Story = {
  name: 'Tier F — deep oxblood (var(--vault-tier-f) #7F1414)',
  render: () => (
    <span className="tier-badge" style={{ backgroundColor: TIER_COLORS['F'] }}>F</span>
  ),
};

export const TierBadgeD: Story = {
  name: 'Tier D — red (var(--vault-tier-d) #EF4444)',
  render: () => (
    <span className="tier-badge" style={{ backgroundColor: TIER_COLORS['D'] }}>D</span>
  ),
};
