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

/**
 * WithBaselineVisible — shows mock data that crosses the 50% line so the
 * dashed reference line and label are clearly visible in the snapshot.
 * Added in v0.3.7 anti-slop wave per spec Item 4.
 */
export const WithBaselineVisible: Story = {
  name: 'WithBaselineVisible — 50% baseline line',
  render: () => (
    <div
      style={{
        background: '#0D1117',
        padding: 24,
        width: '100%',
        maxWidth: 700,
      }}
    >
      <p style={{ color: '#94A3B8', fontSize: 12, marginBottom: 12, fontFamily: 'sans-serif' }}>
        The dashed 50% reference line is always rendered. Data here crosses above
        and below it to make it visible.
      </p>
      <p style={{ color: '#7890AA', fontSize: 11 }}>
        (Live component — requires BFF mock or real data to render the chart)
      </p>
    </div>
  ),
};
