import type { Meta, StoryObj } from '@storybook/react';
import './Quests.css';

/**
 * Quests status badges + progress fills.
 *
 * Token migration (#328): the weekly progress fill moved off the raw
 * `#ab47bc → #8e24aa` onto `--vault-decorative-quest-*`; the quest progress fill
 * onto `--accent` / `--vault-sapphire-light`; the incomplete status badge onto
 * `--vault-warning-dim` / `--warning`; completed/rerolled badge text onto
 * `--fg-inverse`.
 *
 * #1021 Compendium spec contrast states:
 *   in-progress  → sapphire gradient (var(--accent) → var(--vault-sapphire-light))
 *   done (100%)  → gilt gradient (var(--hollowmark-gilt) → var(--hollowmark-gilt-light))
 *   Both gradients pass WCAG AA non-text contrast ≥3:1 against --vault-bg-raised.
 */
const meta: Meta = {
  title: 'Pages/Quests/StatusBadges',
  parameters: { layout: 'centered' },
};

export default meta;
type Story = StoryObj;

export const StatusBadges: Story = {
  render: () => (
    <div style={{ display: 'flex', gap: 8 }}>
      <span className="status-badge completed">Completed</span>
      <span className="status-badge incomplete">Incomplete</span>
      <span className="status-badge rerolled">Rerolled</span>
    </div>
  ),
};

export const ProgressFills: Story = {
  render: () => (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 12, width: 280 }}>
      <div className="quest-progress-bar">
        <div className="quest-progress-fill" style={{ width: '60%' }} />
      </div>
      <div className="daily-wins-bar">
        <div className="daily-wins-fill weekly" style={{ width: '80%' }} />
      </div>
    </div>
  ),
};

// ── #1021 — Five progress states per Compendium spec (AC5) ──────────────────
// Each story shows the bar at one of the 5 canonical widths.
// At 100%, the `.quest-progress-fill--done` modifier switches the gradient
// from sapphire to gilt (#B87D32 → #C8913A) to mark the gold-economy
// completion moment.

function ProgressBarRow({
  pct,
  label,
  done = false,
}: {
  pct: number;
  label: string;
  done?: boolean;
}) {
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 4, width: 280 }}>
      <span style={{ fontSize: 11, color: 'var(--vault-fg-muted, #4E6080)', fontFamily: 'monospace' }}>
        {label}
      </span>
      <div className="quest-progress-bar">
        <div
          className={`quest-progress-fill${done ? ' quest-progress-fill--done' : ''}`}
          style={{ width: `${pct}%` }}
        />
      </div>
      <span style={{ fontSize: 11, color: 'var(--vault-fg-muted, #4E6080)', fontFamily: 'monospace', textAlign: 'right' }}>
        {pct}%
      </span>
    </div>
  );
}

export const ProgressAt0: Story = {
  name: 'Progress — 0%',
  render: () => <ProgressBarRow pct={0} label="0 / 20 — not started (sapphire, empty)" />,
};

export const ProgressAt25: Story = {
  name: 'Progress — 25%',
  render: () => <ProgressBarRow pct={25} label="5 / 20 — in progress (sapphire gradient)" />,
};

export const ProgressAt50: Story = {
  name: 'Progress — 50%',
  render: () => <ProgressBarRow pct={50} label="10 / 20 — halfway (sapphire gradient)" />,
};

export const ProgressAt75: Story = {
  name: 'Progress — 75%',
  render: () => <ProgressBarRow pct={75} label="15 / 20 — nearly done (sapphire gradient)" />,
};

export const ProgressAt100: Story = {
  name: 'Progress — 100% (done)',
  render: () => (
    <ProgressBarRow pct={100} label="20 / 20 — complete (gilt gradient #B87D32→#C8913A)" done />
  ),
};
