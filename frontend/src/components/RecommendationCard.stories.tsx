import type { Meta, StoryObj } from '@storybook/react';
import './RecommendationCard.css';

/**
 * RecommendationCard source category chips.
 *
 * Token migration (#328): the ML / meta / personal source chip color fallbacks
 * moved off raw hex onto the `--vault-source-*` tokens (still overridable via
 * the inline `--ml-color` / `--meta-color` / `--personal-color` vars).
 */
const meta: Meta = {
  title: 'Components/RecommendationCard/SourceChips',
  parameters: { layout: 'centered' },
};

export default meta;
type Story = StoryObj;

const SOURCES = [
  ['source-ml', 'ML'],
  ['source-meta', 'Meta'],
  ['source-personal', 'Personal'],
];

export const SourceChips: Story = {
  render: () => (
    <div style={{ display: 'flex', gap: 8 }}>
      {SOURCES.map(([cls, label]) => (
        <span key={cls} className={`source-value ${cls}`}>
          {label}
        </span>
      ))}
    </div>
  ),
};
