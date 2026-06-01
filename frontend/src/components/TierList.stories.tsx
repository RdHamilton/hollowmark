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
