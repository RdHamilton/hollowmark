/**
 * ManaCurveBar — inline CSS bar chart for mana curve visualization.
 *
 * Intentionally avoids recharts to keep bundle weight low for this small
 * inline context. Pure CSS bars with proportional height relative to the
 * maximum CMC bucket count.
 *
 * Usage:
 *   <ManaCurveBar manaCurve={statistics.manaCurve} size="sm" />
 */

interface ManaCurveBarProps {
  /** Record<cmc, cardCount> — from statistics.manaCurve */
  manaCurve: Record<number, number> | undefined;
  /** Optional: override label shown in tooltip. Default "Mana Curve" */
  label?: string;
  /** 'sm' = footer inline (max-height 40px bars) | 'md' = panel (max-height 80px bars) */
  size?: 'sm' | 'md';
}

const CMC_BUCKETS = [1, 2, 3, 4, 5, 6, 7] as const;
const SPIKE_THRESHOLD = 12;

export default function ManaCurveBar({
  manaCurve,
  label = 'Mana Curve',
  size = 'sm',
}: ManaCurveBarProps) {
  if (!manaCurve || Object.keys(manaCurve).length === 0) {
    return null;
  }

  // Aggregate CMC ≥ 7 into the "7+" bucket
  const bucketCounts: Record<number, number> = {};
  for (const cmc of CMC_BUCKETS) {
    bucketCounts[cmc] = 0;
  }
  for (const [cmcStr, count] of Object.entries(manaCurve)) {
    const cmc = parseInt(cmcStr, 10);
    if (isNaN(cmc) || count <= 0) continue;
    if (cmc >= 7) {
      bucketCounts[7] = (bucketCounts[7] ?? 0) + count;
    } else if (cmc >= 1) {
      bucketCounts[cmc] = (bucketCounts[cmc] ?? 0) + count;
    }
  }

  // Max count across all buckets — used to scale bar heights
  const maxCount = Math.max(...Object.values(bucketCounts), 1);

  // Average CMC for tooltip (weighted mean, CMC 7+ counted as 7)
  const { totalWeighted, totalCards } = Object.entries(manaCurve).reduce(
    (acc, [cmcStr, count]) => {
      const cmc = Math.min(parseInt(cmcStr, 10), 7);
      if (!isNaN(cmc) && count > 0) {
        acc.totalWeighted += cmc * count;
        acc.totalCards += count;
      }
      return acc;
    },
    { totalWeighted: 0, totalCards: 0 }
  );
  const avgCMC = totalCards > 0 ? (totalWeighted / totalCards).toFixed(2) : '0.00';

  const containerHeight = size === 'sm' ? 40 : 80;
  const barWidth = size === 'sm' ? 12 : 20;
  const minBarHeight = 4; // px — ensures a single card is always visible

  return (
    <div
      className="mana-curve-bar"
      style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 2 }}
      title={`${label} — Avg CMC: ${avgCMC}`}
    >
      {/* Bars */}
      <div
        style={{
          display: 'flex',
          alignItems: 'flex-end',
          gap: 2,
          height: containerHeight,
        }}
        aria-label={`${label}: Avg CMC ${avgCMC}`}
        role="img"
      >
        {CMC_BUCKETS.map((cmc) => {
          const count = bucketCounts[cmc] ?? 0;
          const isSpike = count > SPIKE_THRESHOLD;
          const heightPx =
            count > 0
              ? Math.max(minBarHeight, Math.round((count / maxCount) * containerHeight))
              : 0;
          return (
            <div
              key={cmc}
              data-testid={`mana-curve-bar-${cmc}`}
              style={{
                width: barWidth,
                height: heightPx,
                backgroundColor: isSpike
                  ? 'var(--vault-warning)'
                  : 'var(--vault-sapphire)',
                borderRadius: '2px 2px 0 0',
                flexShrink: 0,
              }}
              aria-label={`CMC ${cmc === 7 ? '7+' : cmc}: ${count}`}
            />
          );
        })}
      </div>

      {/* X-axis labels */}
      <div style={{ display: 'flex', gap: 2 }}>
        {CMC_BUCKETS.map((cmc) => (
          <div
            key={cmc}
            style={{
              width: barWidth,
              textAlign: 'center',
              fontFamily: 'var(--font-mono)',
              fontSize: 'var(--text-xs)',
              color: 'var(--vault-fg-muted)',
              marginTop: 2,
              flexShrink: 0,
            }}
          >
            {cmc === 7 ? '7+' : cmc}
          </div>
        ))}
      </div>
    </div>
  );
}
