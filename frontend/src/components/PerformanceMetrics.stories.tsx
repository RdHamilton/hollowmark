import type { Meta, StoryObj } from '@storybook/react';
import './PerformanceMetrics.css';

/**
 * PerformanceMetrics success/error metric values.
 *
 * Token migration (#328): `.metric-value.success-value` moved off `#7dff7d`
 * onto `--win` (= `--vault-sapphire`); `.metric-value.error-value` off
 * `#ff7d7d` onto `--loss` (= `--vault-danger`).
 */
const meta: Meta = {
  title: 'Components/PerformanceMetrics/Values',
  parameters: { layout: 'centered' },
};

export default meta;
type Story = StoryObj;

export const MetricValues: Story = {
  render: () => (
    <div style={{ display: 'flex', gap: 24, fontWeight: 700 }}>
      <span className="metric-value success-value">+12%</span>
      <span className="metric-value error-value">-8%</span>
    </div>
  ),
};
