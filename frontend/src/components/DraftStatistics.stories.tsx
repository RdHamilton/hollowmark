import type { Meta, StoryObj } from '@storybook/react';
import './DraftStatistics.css';

/**
 * DraftStatistics error accent.
 *
 * Token migration (#328): `.statistics-error` moved off the raw `#ff7d7d` onto
 * the `--loss` token (= `--vault-danger`).
 */
const meta: Meta = {
  title: 'Components/DraftStatistics/Error',
  parameters: { layout: 'centered' },
};

export default meta;
type Story = StoryObj;

export const ErrorMessage: Story = {
  render: () => <div className="statistics-error">Failed to load draft statistics.</div>,
};
