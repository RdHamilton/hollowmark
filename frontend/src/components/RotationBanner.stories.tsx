import type { Meta, StoryObj } from '@storybook/react';
import './RotationBanner.css';

/**
 * RotationBanner urgency variants.
 *
 * Token migration (#328): the info / warning / critical gradient stops + borders
 * moved off raw hex onto the `--vault-banner-*` token set.
 */
const meta: Meta = {
  title: 'Components/RotationBanner/Urgency',
  parameters: { layout: 'padded' },
};

export default meta;
type Story = StoryObj;

const Banner = ({ variant, label }: { variant: string; label: string }) => (
  <div className={`rotation-banner rotation-banner--${variant}`} style={{ padding: 16, marginBottom: 12 }}>
    <strong>{label}</strong>
  </div>
);

export const Variants: Story = {
  render: () => (
    <div style={{ width: 420 }}>
      <Banner variant="info" label="Set rotation is months away" />
      <Banner variant="warning" label="Rotation approaching soon" />
      <Banner variant="critical" label="Rotation imminent" />
    </div>
  ),
};
