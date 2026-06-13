import { useState, useEffect, useRef } from 'react';
import { trackEvent } from '@/services/analytics';
import { LineChart, Line, BarChart, Bar, XAxis, YAxis, CartesianGrid, Tooltip, Legend, ResponsiveContainer, ReferenceLine } from 'recharts';
import { ChartBarIcon } from '@heroicons/react/24/outline';
import { matches } from '@/services/api';
import { storage } from '@/types/models';
import LoadingSpinner from '../components/LoadingSpinner';
import EmptyState from '../components/EmptyState';
import ErrorState from '../components/ErrorState';
import { useAppContext } from '../context/AppContext';
import { getVisibleSetAnnotations, type ChartPeriodMeta } from '@/utils/setReleaseAnnotations';
import { toLocalDateString } from '@/utils/dateHelpers';
import './WinRateTrend.css';

const WinRateTrend = () => {
  const { filters, updateFilters } = useAppContext();
  const { dateRange, format, chartType } = filters.winRateTrend;

  const [analysis, setAnalysis] = useState<storage.TrendAnalysis | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // Analytics: 300ms trailing debounce for feature_chart_interacted (Ray Q1)
  const chartInteractedTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const fireChartInteracted = (interaction: 'filter_applied' | 'time_range_changed' | 'format_changed') => {
    if (chartInteractedTimerRef.current) clearTimeout(chartInteractedTimerRef.current);
    chartInteractedTimerRef.current = setTimeout(() => {
      trackEvent({ name: 'feature_chart_interacted', properties: { chart: 'win_rate_trend', interaction } });
    }, 300);
  };

  useEffect(() => {
    const loadTrendData = async () => {
    try {
      setLoading(true);
      setError(null);

      // Calculate date range
      const now = new Date();
      const start = new Date();
      let periodType = 'day';

      switch (dateRange) {
        case '7days':
          start.setDate(now.getDate() - 7);
          periodType = 'day';
          break;
        case '30days':
          start.setDate(now.getDate() - 30);
          periodType = 'week';
          break;
        case '90days':
          start.setDate(now.getDate() - 90);
          periodType = 'week';
          break;
        case 'all':
          start.setFullYear(now.getFullYear() - 1);
          periodType = 'month';
          break;
      }

      // Build formats array
      let formats: string[] | null = null;
      if (format === 'constructed') {
        formats = ['Ladder', 'Play'];
      } else if (format !== 'all') {
        formats = [format];
      }

      const data = await matches.getTrendAnalysis({
        startDate: toLocalDateString(start),
        endDate: toLocalDateString(now),
        periodType: periodType,
        formats: formats || undefined,
      });
      setAnalysis(data as storage.TrendAnalysis);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load trend data');
      console.error('Error loading trend data:', err);
    } finally {
      setLoading(false);
    }
  };

    loadTrendData();
  }, [dateRange, format]);

  // Transform data for Recharts
  const chartData = analysis?.Trends?.map(period => ({
    name: period.Period.Label,
    winRate: Math.round(period.WinRate * 100 * 10) / 10, // Convert to percentage with 1 decimal
    matches: period.Stats?.TotalMatches || 0
  })) || [];

  // Build period metadata (label + startDate) for annotation matching.
  // StartDate may be a Date object or ISO string depending on the BFF class
  // conversion; normalise to YYYY-MM-DD for consistent string comparison.
  const periodMeta: ChartPeriodMeta[] = analysis?.Trends?.map(period => {
    const raw = period.Period.StartDate;
    const startDate = raw
      ? String(raw).slice(0, 10)  // "2024-09-24T00:00:00Z" → "2024-09-24"
      : '';
    return { name: period.Period.Label, startDate };
  }) || [];

  const setAnnotations = getVisibleSetAnnotations(periodMeta);

  return (
    <div className="page-container">
      {/* Filters */}
      <div className="filter-row">
        <div className="filter-group">
          <label className="filter-label">Date Range</label>
          <select value={dateRange} onChange={(e) => { updateFilters('winRateTrend', { dateRange: e.target.value }); fireChartInteracted('time_range_changed'); }}>
            <option value="7days">Last 7 Days</option>
            <option value="30days">Last 30 Days</option>
            <option value="90days">Last 90 Days</option>
            <option value="all">All Time</option>
          </select>
        </div>

        <div className="filter-group">
          <label className="filter-label">Format</label>
          <select value={format} onChange={(e) => { updateFilters('winRateTrend', { format: e.target.value }); fireChartInteracted('format_changed'); }}>
            <option value="all">All Formats</option>
            <option value="constructed">Constructed</option>
            <option value="Ladder">Ranked (Ladder)</option>
            <option value="Play">Play Queue</option>
          </select>
        </div>

        <div className="filter-group">
          <label className="filter-label">Chart Type</label>
          <select value={chartType} onChange={(e) => { updateFilters('winRateTrend', { chartType: e.target.value as 'line' | 'bar' }); fireChartInteracted('filter_applied'); }}>
            <option value="line">Line Chart</option>
            <option value="bar">Bar Chart</option>
          </select>
        </div>
      </div>

      {/* Content */}
      {loading && <LoadingSpinner message="Loading trend data..." />}

      {error && (
        <ErrorState
          message="Failed to load trend data"
          error={error}
          helpText="Check that the database is accessible and try refreshing the page."
        />
      )}

      {!loading && !error && (!analysis || chartData.length === 0) && (
        <div data-testid="win-rate-trend-empty">
          <EmptyState
            icon={<ChartBarIcon className="w-12 h-12" aria-hidden="true" style={{ color: 'var(--vault-fg-muted)' }} />}
            heading="Not enough data"
            subtext="Play at least 5 matches to see your win rate trends over time."
            variant="no-data"
          />
        </div>
      )}

      {!loading && !error && analysis && chartData.length > 0 && (
        <>
          {/* Chart */}
          <div className="chart-container" data-testid="win-rate-trend-chart">
            <ResponsiveContainer width="100%" height={500}>
              {chartType === 'line' ? (
                <LineChart data={chartData}>
                  <CartesianGrid strokeDasharray="3 3" stroke="#3d3d3d" />
                  <XAxis dataKey="name" stroke="#ffffff" />
                  <YAxis stroke="#ffffff" domain={[0, 100]} />
                  <Tooltip
                    contentStyle={{ backgroundColor: '#2d2d2d', border: '1px solid #3d3d3d' }}
                    labelStyle={{ color: '#ffffff' }}
                  />
                  <Legend />
                  {/* 50% baseline — always visible, placed before data so data renders on top */}
                  <ReferenceLine
                    y={50}
                    stroke="var(--vault-fg-muted)"
                    strokeDasharray="6 3"
                    strokeWidth={1}
                    label={{
                      value: '50%',
                      position: 'insideTopRight',
                      fill: 'var(--vault-fg-muted)',
                      fontSize: 11,
                      fontFamily: 'var(--font-mono)',
                      dx: -4,
                      dy: 4,
                    }}
                    data-testid="winrate-baseline"
                  />
                  <Line
                    type="monotone"
                    dataKey="winRate"
                    stroke="#4a9eff"
                    name="Win Rate (%)"
                    strokeWidth={2}
                    dot={{ fill: '#4a9eff', r: 4 }}
                  />
                  {setAnnotations.map((annotation) => (
                    <ReferenceLine
                      key={annotation.code}
                      x={annotation.xLabel}
                      stroke="var(--fg-muted)"
                      strokeDasharray="4 3"
                      strokeWidth={1}
                      label={{
                        value: annotation.code,
                        position: 'top',
                        fill: 'var(--fg-muted)',
                        fontSize: 10,
                      }}
                      data-testid={`set-annotation-${annotation.code}`}
                    />
                  ))}
                </LineChart>
              ) : (
                <BarChart data={chartData}>
                  <CartesianGrid strokeDasharray="3 3" stroke="#3d3d3d" />
                  <XAxis dataKey="name" stroke="#ffffff" />
                  <YAxis stroke="#ffffff" domain={[0, 100]} />
                  <Tooltip
                    contentStyle={{ backgroundColor: '#2d2d2d', border: '1px solid #3d3d3d' }}
                    labelStyle={{ color: '#ffffff' }}
                  />
                  <Legend />
                  {/* 50% baseline — always visible, placed before data so data renders on top */}
                  <ReferenceLine
                    y={50}
                    stroke="var(--vault-fg-muted)"
                    strokeDasharray="6 3"
                    strokeWidth={1}
                    label={{
                      value: '50%',
                      position: 'insideTopRight',
                      fill: 'var(--vault-fg-muted)',
                      fontSize: 11,
                      fontFamily: 'var(--font-mono)',
                      dx: -4,
                      dy: 4,
                    }}
                    data-testid="winrate-baseline"
                  />
                  <Bar dataKey="winRate" fill="#4a9eff" name="Win Rate (%)" />
                  {setAnnotations.map((annotation) => (
                    <ReferenceLine
                      key={annotation.code}
                      x={annotation.xLabel}
                      stroke="var(--fg-muted)"
                      strokeDasharray="4 3"
                      strokeWidth={1}
                      label={{
                        value: annotation.code,
                        position: 'top',
                        fill: 'var(--fg-muted)',
                        fontSize: 10,
                      }}
                      data-testid={`set-annotation-${annotation.code}`}
                    />
                  ))}
                </BarChart>
              )}
            </ResponsiveContainer>
          </div>

          {/* Set-release annotation legend */}
          {setAnnotations.length > 0 && (
            <div className="set-annotation-legend" data-testid="set-annotation-legend">
              {setAnnotations.map((annotation) => (
                <span key={annotation.code} className="set-annotation-legend-item">
                  <span className="set-annotation-legend-swatch" aria-hidden="true" />
                  <span>{annotation.code} — {annotation.name}</span>
                </span>
              ))}
            </div>
          )}

          {/* Summary */}
          <div className="summary">
            <h3>Win Rate Trend Analysis</h3>
            <div className="summary-content">
              <div className="summary-grid">
                <div className="summary-item">
                  <span className="summary-label">Period:</span>
                  <span className="summary-value">
                    {analysis.Trends[0]?.Period.StartDate?.toString().split('T')[0]} to {analysis.Trends[analysis.Trends.length - 1]?.Period.EndDate?.toString().split('T')[0]}
                  </span>
                </div>
                <div className="summary-item">
                  <span className="summary-label">Format:</span>
                  <span className="summary-value">{format === 'all' ? 'All Formats' : format}</span>
                </div>
                <div className="summary-item">
                  <span className="summary-label">Trend:</span>
                  <span
                    className={`summary-value trend-${analysis.Trend ?? 'unknown'}`}
                    data-testid="trend-summary-value"
                  >
                    {(() => {
                      const tv = analysis.TrendValue;
                      const trendLabel = analysis.Trend ?? '';
                      const trendValueValid = typeof tv === 'number' && isFinite(tv) && tv !== 0;
                      if (!trendLabel && !trendValueValid) return '—';
                      return (
                        <>
                          {trendLabel}
                          {trendValueValid && ` (${tv > 0 ? '+' : ''}${Math.round(tv * 100 * 10) / 10}%)`}
                        </>
                      );
                    })()}
                  </span>
                </div>
                {analysis.Overall && (
                  <div className="summary-item">
                    <span className="summary-label">Overall Win Rate:</span>
                    <span className="summary-value">
                      {Math.round(analysis.Overall.WinRate * 100 * 10) / 10}% ({analysis.Overall.TotalMatches} matches)
                    </span>
                  </div>
                )}
              </div>
              <button className="export-button" onClick={() => alert('Export functionality coming soon!')}>Export as PNG</button>
            </div>
          </div>
        </>
      )}
    </div>
  );
};

export default WinRateTrend;
