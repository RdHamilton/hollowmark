/**
 * WildcardAdvisorPanel
 *
 * Displays ranked wildcard craft targets for the authenticated user's
 * collection, filtered by MTGA format.
 *
 * States:
 *  1. loading         — skeleton placeholders while fetching.
 *  2. affordable-split "Craft Tonight" / "Saving Toward" — 200 OK with recs.
 *  3. 409 sync-CTA    — collection not yet synced; prompt to run sync.
 *  4. 200 empty       — 200 OK but recommendations list is empty.
 *  5. stale-warning   — 200 OK but X-Cache-Degraded + >24h old.
 *  6. 503 error-retry — BFF degraded; show retry button.
 *
 * PostHog telemetry: #422 — feature_ml_suggestions_viewed on load with recs,
 * wildcard_recommendation_clicked on row expand.
 * Coupling note: format values must match the BFF contract in ADR-045 / #420.
 */

import { useState, useEffect, useRef } from 'react';
import { useAuth } from '@clerk/react';
import { ArrowPathIcon, ChevronRightIcon, ChevronDownIcon } from '@heroicons/react/24/outline';
import { trackEvent } from '@/services/analytics';
import { bffWildcardAdvisor } from '@/services/api';
import { ApiRequestError } from '@/services/apiClient';
import type {
  WildcardAdvisorFormat,
  WildcardRecommendation,
  WildcardAdvisorResult,
} from '@/services/api/bffWildcardAdvisor';
import './WildcardAdvisorPanel.css';

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const FORMATS: WildcardAdvisorFormat[] = ['Standard', 'Historic', 'Explorer', 'Alchemy'];

/** Hours threshold above which the stale-warning banner is shown. */
const STALE_HOURS_THRESHOLD = 24;

// ---------------------------------------------------------------------------
// Rarity helpers
// ---------------------------------------------------------------------------

/** CSS variable name for each rarity's gem color token. */
function rarityColorVar(rarity: WildcardRecommendation['rarity']): string {
  return `var(--vault-rarity-${rarity})`;
}

/** Short gem-count label: "4x Rare" etc. */
function rarityLabel(rarity: WildcardRecommendation['rarity']): string {
  return rarity.charAt(0).toUpperCase() + rarity.slice(1);
}

// ---------------------------------------------------------------------------
// Collapsed-row helpers (#420 archetype-level fields)
// ---------------------------------------------------------------------------

/**
 * Build a compact wildcard-cost string for the collapsed row.
 *
 * Uses `wildcards_required` when present (ADR-045 / #420 full BFF response).
 * Falls back to `missing_copies` + `rarity` for the scaffold response shape.
 *
 * Examples:
 *   "4 Rare · 2 Mythic away"   (archetype-level, multiple rarities)
 *   "2 Rare away"              (archetype-level, single rarity)
 *   "2 Rare away"              (scaffold fallback)
 */
function buildCostSummary(rec: WildcardRecommendation): string {
  const { wildcards_required, missing_copies, rarity } = rec;

  if (wildcards_required) {
    const RARITY_ORDER: Array<keyof typeof wildcards_required> = ['mythic', 'rare', 'uncommon', 'common'];
    const parts = RARITY_ORDER
      .filter((r) => (wildcards_required[r] ?? 0) > 0)
      .map((r) => {
        const count = wildcards_required[r] as number;
        return `${count} ${r.charAt(0).toUpperCase() + r.slice(1)}`;
      });
    return parts.length > 0 ? `${parts.join(' · ')} away` : 'Complete!';
  }

  // Scaffold fallback: single-rarity summary from the card-level fields
  if (missing_copies > 0) {
    return `${missing_copies} ${rarityLabel(rarity)} away`;
  }
  return 'Complete!';
}

/**
 * Render a tier badge string from the BFF `tier` field.
 * Handles both integer (1, 2, 3) and string ("S", "1") tier values.
 * Returns null when tier is absent so callers can guard rendering.
 */
function formatTier(tier: number | string | undefined): string | null {
  if (tier === undefined || tier === null) return null;
  const str = String(tier).trim();
  // Already prefixed (e.g. "Tier 1") — pass through
  if (str.toLowerCase().startsWith('tier')) return str;
  return `Tier ${str}`;
}

// ---------------------------------------------------------------------------
// Sub-components
// ---------------------------------------------------------------------------

interface BudgetGemProps {
  rarity: WildcardRecommendation['rarity'];
  count: number;
}

function BudgetGem({ rarity, count }: BudgetGemProps) {
  return (
    <span
      className="wildcard-advisor__budget-gem"
      style={{ '--gem-color': rarityColorVar(rarity) } as React.CSSProperties}
      data-testid={`wildcard-advisor-gem-${rarity}`}
      aria-label={`${count} ${rarityLabel(rarity)} wildcard${count !== 1 ? 's' : ''}`}
    >
      <span className="wildcard-advisor__budget-gem-icon" aria-hidden="true" />
      <span className="wildcard-advisor__budget-gem-count">{count}</span>
      <span className="wildcard-advisor__budget-gem-label">{rarityLabel(rarity)}</span>
    </span>
  );
}

interface RecommendationCardProps {
  rec: WildcardRecommendation;
  affordable: boolean;
  totalCount: number;
}

function RecommendationCard({ rec, affordable, totalCount }: RecommendationCardProps) {
  const [expanded, setExpanded] = useState(false);

  const tierLabel = formatTier(rec.tier);
  const costSummary = buildCostSummary(rec);
  // Prefer archetype_name from the #420 response; fall back to card name
  const displayName = rec.archetype_name ?? rec.name;

  return (
    <div
      className={`wildcard-advisor__rec-card ${affordable ? 'wildcard-advisor__rec-card--affordable' : 'wildcard-advisor__rec-card--aspirational'}`}
      data-testid="wildcard-advisor-rec-card"
    >
      <button
        className="wildcard-advisor__rec-main"
        onClick={() => {
          const wasExpanded = expanded;
          setExpanded((prev) => !prev);
          // #422 telemetry: fire on expand only, not on collapse.
          if (!wasExpanded) {
            trackEvent({
              name: 'wildcard_recommendation_clicked',
              properties: {
                rarity: rec.rarity,
                suggestion_count: totalCount,
              },
            });
          }
        }}
        aria-expanded={expanded}
      >
        <span
          className="wildcard-advisor__rec-rarity-pip"
          style={{ background: rarityColorVar(rec.rarity) }}
          aria-hidden="true"
        />

        {/* Left cluster: name + optional tier badge */}
        <span className="wildcard-advisor__rec-identity">
          <span
            className="wildcard-advisor__rec-name"
            data-testid="wildcard-advisor-rec-name"
          >
            {displayName}
          </span>
          {tierLabel !== null && (
            <span
              className="wildcard-advisor__rec-tier-badge"
              data-testid="wildcard-advisor-rec-tier"
            >
              {tierLabel}
            </span>
          )}
        </span>

        {/* Right cluster: GIHWR signal + wildcard cost summary */}
        {rec.gihwr !== undefined && (
          <span
            className={`wildcard-advisor__rec-gihwr ${rec.gihwr >= 57 ? 'wildcard-advisor__rec-gihwr--positive' : rec.gihwr < 50 ? 'wildcard-advisor__rec-gihwr--negative' : ''}`}
            aria-label={`${rec.gihwr.toFixed(1)}% game-in-hand win rate`}
            title="GIHWR: game-in-hand win rate — win % when this card is in your opening hand or drawn"
          >
            <span className="wildcard-advisor__rec-gihwr-label">GIHWR</span>
            {rec.gihwr.toFixed(1)}%
          </span>
        )}
        <span
          className="wildcard-advisor__rec-cost"
          data-testid="wildcard-advisor-rec-cost"
          aria-label={costSummary}
        >
          {costSummary}
        </span>

        <span className="wildcard-advisor__rec-expand-icon" aria-hidden="true">
          {expanded
            ? <ChevronDownIcon className="wildcard-advisor__chevron-icon" />
            : <ChevronRightIcon className="wildcard-advisor__chevron-icon" />
          }
        </span>
      </button>

      {expanded && (
        <div className="wildcard-advisor__rec-drill-down" data-testid="wildcard-advisor-drill-down">
          <dl className="wildcard-advisor__rec-details">
            <div className="wildcard-advisor__rec-detail-row">
              <dt>Owned</dt>
              <dd>{rec.owned_copies} / 4</dd>
            </div>
            <div className="wildcard-advisor__rec-detail-row">
              <dt>Missing</dt>
              <dd>{rec.missing_copies}</dd>
            </div>
            {rec.gihwr !== undefined && (
              <div className="wildcard-advisor__rec-detail-row">
                <dt>GIHWR</dt>
                <dd>{rec.gihwr.toFixed(1)}%</dd>
              </div>
            )}
            {rec.archetype_count !== undefined && (
              <div className="wildcard-advisor__rec-detail-row">
                <dt>Archetypes</dt>
                <dd>{rec.archetype_count}</dd>
              </div>
            )}
            {rec.format_context && (
              <div className="wildcard-advisor__rec-detail-row wildcard-advisor__rec-detail-row--full">
                <dt>Context</dt>
                <dd>{rec.format_context}</dd>
              </div>
            )}
            {rec.set_code && (
              <div className="wildcard-advisor__rec-detail-row">
                <dt>Set</dt>
                <dd>{rec.set_code.toUpperCase()}</dd>
              </div>
            )}
          </dl>
        </div>
      )}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Main panel
// ---------------------------------------------------------------------------

interface WildcardAdvisorPanelProps {
  onClose?: () => void;
}

type PanelState =
  | { kind: 'loading' }
  | { kind: 'data'; result: WildcardAdvisorResult }
  | { kind: 'sync-cta' }
  | { kind: 'error' };

export default function WildcardAdvisorPanel({ onClose }: WildcardAdvisorPanelProps) {
  const { getToken } = useAuth();
  const [format, setFormat] = useState<WildcardAdvisorFormat>('Standard');
  const [retryCount, setRetryCount] = useState(0);
  const [state, setState] = useState<PanelState>({ kind: 'loading' });

  useEffect(() => {
    let cancelled = false;

    const loadData = async () => {
      setState({ kind: 'loading' });
      try {
        const token = await getToken();
        const result = await bffWildcardAdvisor.getWildcardRecommendations(format, token);
        if (!cancelled) setState({ kind: 'data', result });
      } catch (err) {
        if (cancelled) return;
        if (err instanceof ApiRequestError) {
          // Ray's note: detect by HTTP STATUS CODE, not body string.
          if (err.status === 409) {
            setState({ kind: 'sync-cta' });
            return;
          }
          setState({ kind: 'error' });
          return;
        }
        setState({ kind: 'error' });
      }
    };

    void loadData();

    return () => { cancelled = true; };
  }, [format, getToken, retryCount]);

  // #422 telemetry: fire feature_ml_suggestions_viewed once per (format, result)
  // when recommendations load with count > 0.
  // Keyed on format+count to avoid re-firing on re-renders while guarding
  // against the same result triggering twice. Uses a ref (not state) so the
  // comparison does NOT trigger another render cycle — prevents the infinite
  // render loop that caused the OOM in #2996.
  const lastViewedKeyRef = useRef<string | null>(null);
  useEffect(() => {
    if (state.kind !== 'data') return;
    const recs = state.result.data.recommendations;
    if (recs.length === 0) return;
    const key = `${format}/${recs.length}`;
    if (lastViewedKeyRef.current === key) return;
    lastViewedKeyRef.current = key;
    trackEvent({
      name: 'feature_ml_suggestions_viewed',
      properties: {
        suggestion_count: recs.length,
        context: 'collection',
      },
    });
  }, [format, state]);

  const handleFormatChange = (newFormat: WildcardAdvisorFormat) => {
    setFormat(newFormat);
  };

  const handleRetry = () => {
    // Cycle format to trigger the effect; since format hasn't changed,
    // we force a re-run by resetting state and re-running the effect via a key.
    // Use a dedicated retry counter state instead.
    setRetryCount((n) => n + 1);
  };

  // Split recommendations into affordable vs aspirational based on wildcard budget.
  const splitRecommendations = (result: WildcardAdvisorResult) => {
    const budget = result.data.wildcard_budget;
    const affordable: WildcardRecommendation[] = [];
    const aspirational: WildcardRecommendation[] = [];

    for (const rec of result.data.recommendations) {
      const budgetForRarity = budget[rec.rarity];
      if (budgetForRarity >= rec.missing_copies) {
        affordable.push(rec);
      } else {
        aspirational.push(rec);
      }
    }

    return { affordable, aspirational };
  };

  const isStale = (result: WildcardAdvisorResult): boolean =>
    result.cacheDegraded &&
    result.cacheAgeHours !== undefined &&
    result.cacheAgeHours > STALE_HOURS_THRESHOLD;

  return (
    <div className="wildcard-advisor" data-testid="wildcard-advisor-panel">
      {/* Panel header */}
      <div className="wildcard-advisor__header">
        <h2 className="wildcard-advisor__title">Wildcard Advisor</h2>
        {onClose && (
          <button
            className="wildcard-advisor__close"
            onClick={onClose}
            aria-label="Close Wildcard Advisor"
            data-testid="wildcard-advisor-close"
          >
            ×
          </button>
        )}
      </div>

      {/* Format toggle */}
      <div
        className="wildcard-advisor__format-toggle"
        role="group"
        aria-label="Select format"
        data-testid="wildcard-advisor-format-toggle"
      >
        {FORMATS.map((f) => (
          <button
            key={f}
            className={`wildcard-advisor__format-btn ${format === f ? 'wildcard-advisor__format-btn--active' : ''}`}
            onClick={() => handleFormatChange(f)}
            aria-pressed={format === f}
            data-testid={`wildcard-advisor-format-${f.toLowerCase()}`}
          >
            {f}
          </button>
        ))}
      </div>

      {/* Panel body */}
      {state.kind === 'loading' && (
        <div
          className="wildcard-advisor__loading"
          data-testid="wildcard-advisor-loading"
          aria-busy="true"
          aria-label="Loading wildcard recommendations"
        >
          {[1, 2, 3, 4].map((i) => (
            <div key={i} className="wildcard-advisor__skeleton-row" />
          ))}
        </div>
      )}

      {state.kind === 'sync-cta' && (
        <div
          className="wildcard-advisor__sync-cta"
          data-testid="wildcard-advisor-sync-cta"
          role="status"
        >
          <ArrowPathIcon
            className="wildcard-advisor__sync-cta-icon"
            aria-hidden="true"
          />
          <h3 className="wildcard-advisor__sync-cta-title">Collection Not Synced</h3>
          <p className="wildcard-advisor__sync-cta-body">
            Run the VaultMTG daemon to sync your MTGA collection, then come back
            for personalized craft recommendations.
          </p>
          <p className="wildcard-advisor__sync-cta-hint">
            Make sure the daemon is running and has finished its first sync.
          </p>
        </div>
      )}

      {state.kind === 'error' && (
        <div
          className="wildcard-advisor__error"
          data-testid="wildcard-advisor-error"
          role="status"
        >
          <p className="wildcard-advisor__error-msg">
            Recommendations are temporarily unavailable. Please try again.
          </p>
          <button
            className="wildcard-advisor__retry-btn"
            onClick={handleRetry}
            data-testid="wildcard-advisor-retry"
          >
            Retry
          </button>
        </div>
      )}

      {state.kind === 'data' && (() => {
        const { result } = state;
        const { affordable, aspirational } = splitRecommendations(result);
        const recs = result.data.recommendations;

        return (
          <>
            {/* Stale-data warning banner */}
            {isStale(result) && (
              <div
                className="wildcard-advisor__stale-banner"
                role="status"
                data-testid="wildcard-advisor-stale-banner"
              >
                Ratings data is over {result.cacheAgeHours?.toFixed(0)} hours old — crafting
                advice may be slightly outdated.
              </div>
            )}

            {/* Wildcard budget header */}
            <div
              className="wildcard-advisor__budget"
              data-testid="wildcard-advisor-budget"
              aria-label="Your wildcard budget"
            >
              <span className="wildcard-advisor__budget-label">Your wildcards:</span>
              <div className="wildcard-advisor__budget-gems">
                <BudgetGem rarity="mythic" count={result.data.wildcard_budget.mythic} />
                <BudgetGem rarity="rare" count={result.data.wildcard_budget.rare} />
                <BudgetGem rarity="uncommon" count={result.data.wildcard_budget.uncommon} />
                <BudgetGem rarity="common" count={result.data.wildcard_budget.common} />
              </div>
            </div>

            {recs.length === 0 ? (
              // Distinguish complete-collection from no-data using ratings_cached_at.
              // When ratings_cached_at is absent the BFF has no meta signal yet — show
              // the no-data message. When it is present but recs are empty the collection
              // is likely complete for the selected format.
              //
              // NOTE: The BFF currently does not expose a dedicated discriminator field.
              // ratings_cached_at is the best available proxy. A proper `empty_reason`
              // field on the BFF response (e.g. "collection_complete" | "no_data") would
              // make this unambiguous — route to Bob/Bianca via a follow-up ticket if
              // fill-rate analysis shows the heuristic is wrong.
              result.data.ratings_cached_at !== undefined ? (
                <div
                  className="wildcard-advisor__empty"
                  data-testid="wildcard-advisor-empty-complete"
                  role="status"
                >
                  <p className="wildcard-advisor__empty-title">Collection looks complete!</p>
                  <p className="wildcard-advisor__empty-body">
                    Your {format} collection looks complete — nothing left to craft!
                  </p>
                </div>
              ) : (
                <div
                  className="wildcard-advisor__empty"
                  data-testid="wildcard-advisor-empty-no-data"
                  role="status"
                >
                  <p className="wildcard-advisor__empty-title">No recommendations yet</p>
                  <p className="wildcard-advisor__empty-body">
                    Not enough data yet to make recommendations — keep playing.
                  </p>
                </div>
              )
            ) : (
              <>
                {affordable.length > 0 && (
                  <section
                    className="wildcard-advisor__section"
                    data-testid="wildcard-advisor-craft-tonight"
                  >
                    <h3 className="wildcard-advisor__section-title wildcard-advisor__section-title--affordable">
                      Craft Tonight
                    </h3>
                    <p className="wildcard-advisor__section-subtitle">
                      You have enough wildcards to complete these right now.
                    </p>
                    <div className="wildcard-advisor__rec-list">
                      {affordable.map((rec) => (
                        <RecommendationCard
                          key={`${rec.arena_id}-${rec.rarity}`}
                          rec={rec}
                          affordable
                          totalCount={recs.length}
                        />
                      ))}
                    </div>
                  </section>
                )}

                {aspirational.length > 0 && (
                  <section
                    className="wildcard-advisor__section"
                    data-testid="wildcard-advisor-saving-toward"
                  >
                    <h3 className="wildcard-advisor__section-title wildcard-advisor__section-title--aspirational">
                      Saving Toward
                    </h3>
                    <div className="wildcard-advisor__rec-list">
                      {aspirational.map((rec) => (
                        <RecommendationCard
                          key={`${rec.arena_id}-${rec.rarity}`}
                          rec={rec}
                          affordable={false}
                          totalCount={recs.length}
                        />
                      ))}
                    </div>
                  </section>
                )}
              </>
            )}
          </>
        );
      })()}
    </div>
  );
}
