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
 *
 * Heading copy (#685): page headings are plain "Draft History" and
 * "Draft Assistant" — no § Chapter / The Draft lorebook framing.
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

/**
 * PageHeader — plain "Draft History" heading in Space Grotesk.
 *
 * Chromatic gate for #684 (no Cormorant Garamond) and #685 (no lorebook copy).
 * The heading must read "Draft History" when no active draft is in progress,
 * and "Draft Assistant" once a draft session is loaded.
 */
export const PageHeaderHistory: Story = {
  render: () => (
    <div
      className="draft-header"
      style={{ background: 'var(--vault-bg, #0D1117)', padding: '1rem 1.5rem' }}
    >
      <h1>Draft History</h1>
      <p>Start a Quick Draft in MTG Arena to begin a new draft session</p>
    </div>
  ),
};

/**
 * PageHeaderActive — plain "Draft Assistant" heading shown during a live draft.
 */
export const PageHeaderActive: Story = {
  render: () => (
    <div
      className="draft-header"
      style={{ background: 'var(--vault-bg, #0D1117)', padding: '1rem 1.5rem' }}
    >
      <h1>Draft Assistant</h1>
      <div className="draft-info">
        <span className="draft-event">QuickDraft</span>
        <span className="draft-set">Set: BLB</span>
        <span className="draft-picks">Picks: 12/45</span>
      </div>
    </div>
  ),
};
