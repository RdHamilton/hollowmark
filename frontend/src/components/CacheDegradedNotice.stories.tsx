import type { Meta, StoryObj } from '@storybook/react';
import CacheDegradedNotice from './CacheDegradedNotice';
import './CacheDegradedNotice.css';

/**
 * CacheDegradedNotice — a subtle warning-level banner shown when ratings data
 * may be stale (live sync unavailable).
 *
 * Token migration (#328): the notice text color moved off the raw `#c9a840`
 * gold to the semantic `--warning` token. These stories pin a stable Chromatic
 * snapshot of the migrated styling.
 */
const meta: Meta<typeof CacheDegradedNotice> = {
  title: 'Components/CacheDegradedNotice',
  component: CacheDegradedNotice,
  parameters: { layout: 'centered' },
  tags: ['autodocs'],
};

export default meta;
type Story = StoryObj<typeof CacheDegradedNotice>;

export const Default: Story = {
  args: { visible: true },
};

export const WithCacheAge: Story = {
  args: { visible: true, cacheAgeHours: 6 },
};
