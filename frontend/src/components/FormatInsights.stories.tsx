import type { Meta, StoryObj } from '@storybook/react';
import './FormatInsights.css';

/**
 * FormatInsights win/loss accents.
 *
 * Token migration (#328): `.gihwr-value` (games-in-hand win rate) moved off
 * `#7dff7d` onto `--win` (= `--vault-sapphire`); the error text and close
 * button off `#ff7d7d` onto `--loss` (= `--vault-danger`).
 */
const meta: Meta = {
  title: 'Components/FormatInsights/Accents',
  parameters: { layout: 'centered' },
};

export default meta;
type Story = StoryObj;

export const GihwrValue: Story = {
  render: () => <span className="gihwr-value">58.3% GIHWR</span>,
};

export const CloseButton: Story = {
  render: () => (
    <button className="btn-close-archetype" type="button">
      Close archetype
    </button>
  ),
};
