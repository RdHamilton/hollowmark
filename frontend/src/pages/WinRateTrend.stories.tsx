import type { Meta, StoryObj } from '@storybook/react';
import './WinRateTrend.css';

/**
 * WinRateTrend visual stories.
 *
 * Token migration (#328): `.trend-improving` moved off `#7dff7d` onto `--win`
 * (= `--vault-sapphire`); `.trend-declining` off `#ff7d7d` onto `--loss`
 * (= `--vault-danger`).
 *
 * Set-release annotation legend (#691): the `.set-annotation-legend` block
 * shows a dashed-line swatch + "CODE — Name" for each visible set release
 * that falls within the chart window. Styled with --fg-muted tokens (subtle,
 * informational — not decorative). Chromatic captures the token rendering.
 */
const meta: Meta = {
  title: 'Pages/WinRateTrend/Trends',
  parameters: { layout: 'centered' },
};

export default meta;
type Story = StoryObj;

export const TrendIndicators: Story = {
  render: () => (
    <div style={{ display: 'flex', gap: 24, fontWeight: 700 }}>
      <span className="trend-improving">▲ Improving</span>
      <span className="trend-declining">▼ Declining</span>
      <span className="trend-stable">— Stable</span>
    </div>
  ),
};

/**
 * Annotation legend — rendered when one or more Arena set releases fall within
 * the chart's date range. Each entry pairs a dashed-line swatch with the set
 * code and full name.
 */
export const SetAnnotationLegend: Story = {
  render: () => (
    <div
      style={{
        background: 'var(--bg-overlay, #1E2636)',
        padding: '16px',
        borderRadius: '8px',
        minWidth: 340,
      }}
    >
      <div className="set-annotation-legend">
        <span className="set-annotation-legend-item">
          <span className="set-annotation-legend-swatch" aria-hidden="true" />
          <span>DSK — Duskmourn</span>
        </span>
        <span className="set-annotation-legend-item">
          <span className="set-annotation-legend-swatch" aria-hidden="true" />
          <span>BLB — Bloomburrow</span>
        </span>
        <span className="set-annotation-legend-item">
          <span className="set-annotation-legend-swatch" aria-hidden="true" />
          <span>OTJ — Outlaws of Thunder Junction</span>
        </span>
      </div>
    </div>
  ),
};
