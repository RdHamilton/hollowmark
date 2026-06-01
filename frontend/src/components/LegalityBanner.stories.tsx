import type { Meta, StoryObj } from '@storybook/react';
import './LegalityBanner.css';

/**
 * LegalityBanner urgency variants.
 *
 * Token migration (#328): the info / warning / critical gradient stops + borders
 * moved off raw hex onto the shared `--vault-banner-*` token set.
 */
const meta: Meta = {
  title: 'Components/LegalityBanner/Urgency',
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj;

const Banner = ({ variant, label }: { variant: string; label: string }) => (
  <div className={`legality-banner legality-banner--${variant}`} style={{ padding: 16, marginBottom: 12 }}>
    <strong>{label}</strong>
  </div>
);

export const Variants: Story = {
  render: () => (
    <div style={{ width: 420 }}>
      <Banner variant="info" label="Deck is Standard-legal" />
      <Banner variant="warning" label="Some cards rotate soon" />
      <Banner variant="critical" label="Deck contains illegal cards" />
    </div>
  ),
};
