import type { Meta, StoryObj } from '@storybook/react';
import './AboutDialog.css';

/**
 * AboutDialog icon placeholder gradient.
 *
 * Token migration (#328): the icon-placeholder gradient moved off the raw
 * `#667eea → #764ba2` onto the `--vault-decorative-indigo-grad-*` tokens.
 */
const meta: Meta = {
  title: 'Components/AboutDialog/IconPlaceholder',
  parameters: { layout: 'centered' },
};

export default meta;
type Story = StoryObj;

export const IconPlaceholder: Story = {
  render: () => (
    <div className="about-dialog">
      <div className="icon-placeholder">V</div>
    </div>
  ),
};
