/**
 * ManaCurveBar stories — v0.3.7 anti-slop wave.
 *
 * Covers all 6 variants specified in the design spec.
 */
import type { Meta, StoryObj } from '@storybook/react';
import ManaCurveBar from './ManaCurveBar';

const meta: Meta<typeof ManaCurveBar> = {
  title: 'Components/ManaCurveBar',
  component: ManaCurveBar,
  parameters: {
    layout: 'centered',
    backgrounds: {
      default: 'dark',
      values: [{ name: 'dark', value: '#0D1117' }],
    },
  },
  tags: ['autodocs'],
};

export default meta;
type Story = StoryObj<typeof ManaCurveBar>;

export const AggressiveCurve: Story = {
  name: 'Aggressive Curve',
  args: {
    manaCurve: { 1: 4, 2: 8, 3: 7, 4: 4, 5: 2, 6: 1 },
    size: 'sm',
  },
};

export const MidrangeCurve: Story = {
  name: 'Midrange Curve',
  args: {
    manaCurve: { 2: 4, 3: 8, 4: 7, 5: 4, 6: 2 },
    size: 'sm',
  },
};

export const ControlCurve: Story = {
  name: 'Control Curve',
  args: {
    manaCurve: { 1: 2, 2: 4, 3: 6, 4: 4, 5: 3, 6: 3, 7: 2 },
    size: 'sm',
  },
};

export const EmptyCurve: Story = {
  name: 'Empty Curve (no-data)',
  args: {
    manaCurve: undefined,
    size: 'sm',
  },
};

export const SpikedBucket: Story = {
  name: 'Spiked Bucket (warning color)',
  args: {
    manaCurve: { 3: 14 },
    size: 'sm',
  },
};

export const BothSizes: Story = {
  name: 'Both Sizes (sm + md)',
  render: () => (
    <div style={{ display: 'flex', gap: 32, alignItems: 'flex-end', background: '#0D1117', padding: 24 }}>
      <div>
        <p style={{ color: '#94A3B8', fontSize: 12, marginBottom: 8 }}>size="sm"</p>
        <ManaCurveBar manaCurve={{ 1: 4, 2: 8, 3: 7, 4: 4, 5: 2, 6: 1 }} size="sm" />
      </div>
      <div>
        <p style={{ color: '#94A3B8', fontSize: 12, marginBottom: 8 }}>size="md"</p>
        <ManaCurveBar manaCurve={{ 1: 4, 2: 8, 3: 7, 4: 4, 5: 2, 6: 1 }} size="md" />
      </div>
    </div>
  ),
};
