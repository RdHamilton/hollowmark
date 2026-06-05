import React, { useState, useEffect, useRef } from 'react';
import { useSearchParams } from 'react-router-dom';
import { apiAdapter } from '@/services/adapter';
import { useSettings } from '@/hooks/useSettings';
import { trackEvent } from '@/services/analytics';
import TemporalTrends from '@/components/TemporalTrends';
import CommunityComparison from '@/components/CommunityComparison';
import FormatInsights from '@/components/FormatInsights';
import { DraftGrade } from '@/components/DraftGrade';
import './DraftAnalytics.css';

const DraftAnalytics: React.FC = () => {
  const [searchParams] = useSearchParams();
  // ?session= and ?set= are emitted by BffDraftHistory row clicks.
  // When present, scope the view to that specific draft session.
  const sessionParam = searchParams.get('session');
  const setParam = searchParams.get('set');

  const [availableSets, setAvailableSets] = useState<string[]>([]);
  const [selectedSet, setSelectedSet] = useState<string>(setParam ?? '');
  const [draftFormat, setDraftFormat] = useState<string>('PremierDraft');
  const [loading, setLoading] = useState(true);
  // AC1/AC2: read from global settings — do not manage local auto-refresh state (#2023).
  const { autoRefresh } = useSettings();

  // Analytics: feature_draft_analytics_viewed — fires once per mount when data is non-empty
  const viewedFiredRef = useRef(false);

  useEffect(() => {
    async function fetchSets() {
      try {
        const formats = await apiAdapter.drafts.getDraftFormats();
        setAvailableSets(formats);
        if (formats.length > 0) {
          // If a ?set= param was provided, honour it (pre-select); otherwise default to first.
          setSelectedSet((currentSet) => currentSet || formats[0]);
          if (!viewedFiredRef.current) {
            viewedFiredRef.current = true;
            trackEvent({
              name: 'feature_draft_analytics_viewed',
              properties: { draft_count: formats.length },
            });
          }
        }
      } catch (err) {
        console.error('Failed to fetch draft formats:', err);
      } finally {
        setLoading(false);
      }
    }
    fetchSets();
  }, []);

  if (loading) {
    return (
      <div className="draft-analytics draft-analytics--loading">
        <div className="draft-analytics__spinner" />
        <span>Loading draft analytics...</span>
      </div>
    );
  }

  if (availableSets.length === 0) {
    return (
      <div className="draft-analytics draft-analytics--empty">
        <h2>No Draft Data Available</h2>
        <p>Complete some drafts to see your analytics and performance trends.</p>
      </div>
    );
  }

  return (
    <div className="draft-analytics">
      {sessionParam && (
        <div
          className="draft-analytics__session-scope"
          data-testid="draft-analytics-session-scope"
          data-session-id={sessionParam}
        >
          <span>Viewing draft session{setParam ? ` — ${setParam}` : ''}</span>
          {/* Grade pill: reads overall_grade from /api/v1/drafts/{id}/analysis
              so the Layer 5 corpus harness can assert the concrete grade value
              (data-testid="session-overall-grade", ADR-052 Surface 6, #829). */}
          <DraftGrade sessionID={sessionParam} compact={true} />
        </div>
      )}
      <div className="draft-analytics__header">
        <h1>Draft Analytics</h1>
        <div className="draft-analytics__filters">
          <div className="draft-analytics__filter">
            <label htmlFor="set-select">Set</label>
            <select
              id="set-select"
              value={selectedSet}
              onChange={(e) => setSelectedSet(e.target.value)}
              className="draft-analytics__select"
            >
              {availableSets.map((set) => (
                <option key={set} value={set}>
                  {set}
                </option>
              ))}
            </select>
          </div>
          <div className="draft-analytics__filter">
            <label htmlFor="format-select">Format</label>
            <select
              id="format-select"
              value={draftFormat}
              onChange={(e) => setDraftFormat(e.target.value)}
              className="draft-analytics__select"
            >
              <option value="PremierDraft">Premier Draft</option>
              <option value="QuickDraft">Quick Draft</option>
              <option value="TradDraft">Traditional Draft</option>
            </select>
          </div>
        </div>
      </div>

      <div className="draft-analytics__content">
        <div className="draft-analytics__section draft-analytics__section--full">
          <TemporalTrends
            setCode={selectedSet}
            periodType="week"
            numPeriods={12}
            showLearningCurve={true}
            autoRefresh={autoRefresh}
          />
        </div>

        <div className="draft-analytics__row">
          <div className="draft-analytics__section">
            <CommunityComparison
              setCode={selectedSet}
              draftFormat={draftFormat}
              autoRefresh={autoRefresh}
            />
          </div>
          <div className="draft-analytics__section">
            <FormatInsights
              setCode={selectedSet}
              draftFormat={draftFormat}
              autoRefresh={autoRefresh}
            />
          </div>
        </div>
      </div>
    </div>
  );
};

export default DraftAnalytics;
