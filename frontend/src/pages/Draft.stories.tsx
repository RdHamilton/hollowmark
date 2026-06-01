import type { Meta, StoryObj } from '@storybook/react';
import './Draft.css';

/**
 * Draft pick + grade accents.
 *
 * Token migration (#328): the picked-card / best-pick positive accents moved off
 * the raw bright-green `#44ff88` onto the on-brand `--win` token
 * (= `--vault-sapphire`); the draft-grade gradients moved onto the
 * `--vault-grade-*` token set. Rendered as the migrated classes for a stable
 * Chromatic snapshot (the full Draft page is API/router-bound).
 */
const meta: Meta = {
  title: 'Pages/Draft/Accents',
  parameters: { layout: 'centered' },
};

export default meta;
type Story = StoryObj;

const GRADES = ['a-plus', 'a', 'b', 'c', 'd', 'f'];

export const GradeBadges: Story = {
  render: () => (
    <div style={{ display: 'flex', gap: 8 }}>
      {GRADES.map((g) => (
        <span
          key={g}
          className={`quality-${g}`}
          style={{ padding: '6px 14px', borderRadius: 6, fontWeight: 700 }}
        >
          {g.replace('-plus', '+').toUpperCase()}
        </span>
      ))}
    </div>
  ),
};

export const PickedCard: Story = {
  render: () => (
    <div className="card-item picked" style={{ padding: 16, width: 200, borderRadius: 8 }}>
      Picked card
    </div>
  ),
};
