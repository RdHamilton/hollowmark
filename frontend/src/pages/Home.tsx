/**
 * Home — returning-player command center ("Command Strip").
 *
 * Concept C: vertical stack of single-purpose data strips:
 *   WEEKLY RECORD → conditional ACTIVE DRAFT → LAST DECK → QUICK NAV quadrant
 *
 * ManaWheel is used only as the loading/empty-state visual.
 *
 * BFF contract for /api/v1/history/summary is being implemented by Bob (#689).
 * While the endpoint is not live we call the adapter which falls back to the
 * mock stub; a TODO in the adapter marks the swap point.
 *
 * Rules applied:
 *  - All BFF calls go through the REST adapter (bffHomeSummary + drafts + decks)
 *  - Clerk auth via useAuth() at the call site — no local session state
 *  - Design tokens only: Vault Sapphire #4A90D9, Space Grotesk display, Inter body,
 *    JetBrains Mono stats. ZERO Cormorant Garamond.
 */

import { useEffect, useState, useCallback } from 'react';
import { useNavigate } from 'react-router-dom';
import { useAuth, useUser } from '@clerk/react';
import {
  FireIcon,
  ArrowTrendingDownIcon,
  CalendarDaysIcon,
  ChartBarIcon,
} from '@heroicons/react/24/outline';
import {
  LedgerGlyph,
  FanCardsGlyph,
  DeckStackGlyph,
  BinderGlyph,
} from '../components/MagicGlyphs';
import ManaWheel from '../components/ManaWheel';
import {
  getHomeSummary,
  makeMockHomeSummary,
  type HomeSummaryResponse,
} from '../services/api/bffHomeSummary';
import { getActiveDraftSessions } from '../services/api/drafts';
import { getDecks } from '../services/api/decks';
import type { DraftSession } from '../services/api/drafts';
import type { DeckListItem } from '../services/api/decks';
import { winRateColor, formatElapsed, isLimitedFormat } from './homeUtils';
import './Home.css';

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

type LoadingState = 'loading' | 'loaded' | 'error';

interface HomeData {
  summary: HomeSummaryResponse;
  activeDraft: DraftSession | null;
  lastDeck: DeckListItem | null;
}
/** Format breakdown entry — optional BFF field (planned, not yet live). */
interface FormatBreakdownEntry {
  format: string;
  win_rate: number;
  matches: number;
}

// ---------------------------------------------------------------------------
// WhatsNextNudge
// ---------------------------------------------------------------------------

interface WhatsNextNudgeProps {
  data: HomeData;
  navigate: (path: string) => void;
}

function WhatsNextNudge({ data, navigate }: WhatsNextNudgeProps) {
  const { summary, activeDraft, lastDeck } = data;

  // Nudge 1: active draft — handled by existing strip; skip here
  if (activeDraft !== null) return null;

  // Nudge 2: cold deck — below .500 this week AND has a last deck
  if (
    summary.this_week.losses > summary.this_week.wins &&
    lastDeck !== null
  ) {
    return (
      <div className="home-whats-next" data-testid="home-whats-next-cold-deck">
        <ArrowTrendingDownIcon className="home-whats-next-icon" aria-hidden="true" />
        <div className="home-whats-next-body">
          <span className="home-whats-next-headline">
            Your record is below .500 this week
          </span>
          <span className="home-whats-next-detail">
            Try tweaking <strong>{lastDeck.name}</strong> or switching decks.
          </span>
          <button
            className="home-whats-next-cta"
            onClick={() => navigate(`/deck-builder/${lastDeck.id}`)}
          >
            Edit deck &rarr;
          </button>
        </div>
      </div>
    );
  }

  // Nudge 3: hot streak — 5+ wins at 60%+ this week
  if (summary.this_week.wins >= 5 && summary.this_week.win_rate >= 0.60) {
    return (
      <div className="home-whats-next" data-testid="home-whats-next-hot-streak">
        <FireIcon className="home-whats-next-icon" aria-hidden="true" />
        <div className="home-whats-next-body">
          <span className="home-whats-next-headline">On a roll this week</span>
          <span className="home-whats-next-detail">
            {summary.this_week.wins} wins at{' '}
            {(summary.this_week.win_rate * 100).toFixed(1)}% — you&apos;re in the zone.
          </span>
          <button className="home-whats-next-cta" onClick={() => navigate('/draft')}>
            Start a draft &rarr;
          </button>
        </div>
      </div>
    );
  }

  // Nudge 4: stale — no matches today or this week
  if (
    summary.today.wins + summary.today.losses === 0 &&
    summary.this_week.wins + summary.this_week.losses === 0
  ) {
    return (
      <div className="home-whats-next" data-testid="home-whats-next-stale">
        <CalendarDaysIcon className="home-whats-next-icon" aria-hidden="true" />
        <div className="home-whats-next-body">
          <span className="home-whats-next-headline">No matches this week</span>
          <span className="home-whats-next-detail">
            Ready for a draft?
          </span>
          <button className="home-whats-next-cta" onClick={() => navigate('/draft')}>
            Start a draft &rarr;
          </button>
        </div>
      </div>
    );
  }

  // Nudge 5: most-played format from format_breakdown (future BFF field)
  const formatBreakdown = (summary as HomeSummaryResponse & { format_breakdown?: FormatBreakdownEntry[] }).format_breakdown;
  if (formatBreakdown && formatBreakdown.length > 0) {
    const topFormat = formatBreakdown.reduce((a, b) => (b.matches > a.matches ? b : a));
    return (
      <div className="home-whats-next" data-testid="home-whats-next-format">
        <ChartBarIcon className="home-whats-next-icon" aria-hidden="true" />
        <div className="home-whats-next-body">
          <span className="home-whats-next-headline">
            Most played format: {topFormat.format}
          </span>
          <span className="home-whats-next-detail">
            {topFormat.matches} matches at {(topFormat.win_rate * 100).toFixed(1)}% win rate
          </span>
          <button className="home-whats-next-cta" onClick={() => navigate('/format-distribution')}>
            View stats &rarr;
          </button>
        </div>
      </div>
    );
  }

  // No conditions matched — render nothing
  return null;
}



// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------


// ---------------------------------------------------------------------------
// Sub-components
// ---------------------------------------------------------------------------

/** CSS separator — a real border-left element, not a Unicode box-drawing char */
function Divider() {
  return (
    <span
      className="home-strip-divider"
      aria-hidden="true"
    />
  );
}

// ---------------------------------------------------------------------------
// QuickNavStrip — extracted so it renders in both loaded and empty states
// ---------------------------------------------------------------------------

interface QuickNavStripProps {
  navigate: (path: string) => void;
}

function QuickNavStrip({ navigate }: QuickNavStripProps) {
  return (
    <div className="home-quick-nav" data-testid="home-quick-nav">
      <button
        className="home-nav-tile"
        data-testid="home-nav-match-history"
        onClick={() => navigate('/match-history')}
        aria-label="Match History"
      >
        <LedgerGlyph className="home-nav-icon" size={20} />
        <span className="home-nav-label">Match History</span>
      </button>
      <button
        className="home-nav-tile"
        data-testid="home-nav-draft"
        onClick={() => navigate('/draft')}
        aria-label="Draft"
      >
        <FanCardsGlyph className="home-nav-icon" size={20} />
        <span className="home-nav-label">Draft</span>
      </button>
      <button
        className="home-nav-tile"
        data-testid="home-nav-decks"
        onClick={() => navigate('/decks')}
        aria-label="Decks"
      >
        <DeckStackGlyph className="home-nav-icon" size={20} />
        <span className="home-nav-label">Decks</span>
      </button>
      <button
        className="home-nav-tile"
        data-testid="home-nav-collection"
        onClick={() => navigate('/collection')}
        aria-label="Collection"
      >
        <BinderGlyph className="home-nav-icon" size={20} />
        <span className="home-nav-label">Collection</span>
      </button>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Main component
// ---------------------------------------------------------------------------

export default function Home() {
  const navigate = useNavigate();
  const { getToken } = useAuth();
  const { user } = useUser();

  const [loadingState, setLoadingState] = useState<LoadingState>('loading');
  const [data, setData] = useState<HomeData | null>(null);

  const displayName = user?.firstName || user?.username || 'Planeswalker';

  const loadData = useCallback(async () => {
    setLoadingState('loading');
    try {
      const token = await getToken();
      if (!token) throw new Error('No auth token');

      // Fetch in parallel: summary (with 404 fallback), active drafts, last-played deck
      const [summaryResult, draftsResult, decksResult] = await Promise.allSettled([
        getHomeSummary(token),
        getActiveDraftSessions(),
        getDecks(),
      ]);

      // Summary: fall back to mock when BFF endpoint not yet live (404 / any error)
      const summary =
        summaryResult.status === 'fulfilled'
          ? summaryResult.value
          : makeMockHomeSummary();

      // Active draft: first result from the sorted list, or null
      const activeDraft =
        draftsResult.status === 'fulfilled' && draftsResult.value.length > 0
          ? draftsResult.value[0]
          : null;

      // Last deck: sort by lastPlayed descending, take first
      let lastDeck: DeckListItem | null = null;
      if (decksResult.status === 'fulfilled' && decksResult.value.length > 0) {
        const sorted = [...decksResult.value].sort((a, b) => {
          const aTime = a.lastPlayed ? new Date(String(a.lastPlayed)).getTime() : 0;
          const bTime = b.lastPlayed ? new Date(String(b.lastPlayed)).getTime() : 0;
          return bTime - aTime;
        });
        lastDeck = sorted[0] ?? null;
      }

      setData({ summary, activeDraft, lastDeck });
      setLoadingState('loaded');
    } catch {
      setLoadingState('error');
    }
  }, [getToken]);

  useEffect(() => {
    void loadData();
  }, [loadData]);

  // ---------------------------------------------------------------------------
  // Empty / first-run state
  // ---------------------------------------------------------------------------
  const isEmpty =
    loadingState === 'loaded' &&
    data !== null &&
    data.summary.all_time.matches === 0 &&
    data.activeDraft === null &&
    data.lastDeck === null;

  // ---------------------------------------------------------------------------
  // Render: loading
  // ---------------------------------------------------------------------------
  if (loadingState === 'loading') {
    return (
      <div className="home-page" data-testid="home-page">
        <div className="home-loading" data-testid="home-loading">
          <ManaWheel size={120} ariaLabel="Loading your stats" />
          <p className="home-loading-text">Loading your command center…</p>
        </div>
      </div>
    );
  }

  // ---------------------------------------------------------------------------
  // Render: error
  // ---------------------------------------------------------------------------
  if (loadingState === 'error') {
    return (
      <div className="home-page" data-testid="home-page">
        <div className="home-error" data-testid="home-error">
          <p className="home-error-text">Unable to load your stats. Please try again.</p>
          <button className="home-retry-btn" onClick={() => void loadData()}>
            Retry
          </button>
        </div>
      </div>
    );
  }

  // ---------------------------------------------------------------------------
  // Render: empty / first-run state
  // ---------------------------------------------------------------------------
  if (isEmpty) {
    return (
      <div className="home-page" data-testid="home-page">
        <div className="home-empty" data-testid="home-empty">
          <ManaWheel size={120} ariaLabel="Get started with VaultMTG" />
          <p className="home-empty-title">Welcome, {displayName}</p>
          <p className="home-empty-body">
            Connect your daemon and play a game to get started — your command center
            will fill in as you play.
          </p>
        </div>
        <QuickNavStrip navigate={navigate} />
      </div>
    );
  }

  // ---------------------------------------------------------------------------
  // Render: loaded
  // ---------------------------------------------------------------------------
  const { summary, activeDraft, lastDeck } = data!;
  const { today, this_week, last_match } = summary;

  return (
    <div className="home-page" data-testid="home-page">

      {/* ── WEEKLY RECORD strip ─────────────────────────────────── */}
      <div className="home-strip home-strip-weekly" data-testid="home-strip-weekly">
        <span className="home-strip-label">THIS WEEK</span>

        <span className="home-strip-stat-group">
          <span
            className="home-stat-w"
            style={{ color: 'var(--vault-success)' }}
            data-testid="home-weekly-wins"
          >
            {this_week.wins}W
          </span>
          <Divider />
          <span
            className="home-stat-l"
            style={{ color: 'var(--vault-danger)' }}
            data-testid="home-weekly-losses"
          >
            {this_week.losses}L
          </span>
          <Divider />
          <span
            className="home-stat-wr"
            style={{ color: winRateColor(this_week.win_rate) }}
            data-testid="home-weekly-winrate"
          >
            {(this_week.win_rate * 100).toFixed(1)}%
          </span>
        </span>

        {today.wins + today.losses > 0 && (
          <span className="home-strip-today" data-testid="home-today-record">
            Today: {today.wins}–{today.losses}
          </span>
        )}
      </div>

      {/* ── LAST MATCH micro-strip (if available) ───────────────── */}
      {last_match && (
        <div className="home-strip home-strip-last-match" data-testid="home-strip-last-match">
          <span
            className="home-last-match-result"
            style={{
              color:
                last_match.result === 'win'
                  ? 'var(--vault-success)'
                  : 'var(--vault-danger)',
            }}
            data-testid="home-last-match-result"
          >
            {last_match.result === 'win' ? 'Won' : 'Lost'}
          </span>
          {last_match.opponent_archetype && (
            <>
              <Divider />
              <span
                className="home-last-match-archetype"
                data-testid="home-last-match-archetype"
              >
                vs. {last_match.opponent_archetype}
              </span>
            </>
          )}
          <Divider />
          <span
            className="home-last-match-elapsed"
            data-testid="home-last-match-elapsed"
          >
            {formatElapsed(last_match.elapsed_seconds)}
          </span>
        </div>
      )}

      {/* ── ACTIVE DRAFT strip (dominant — shown only when present) */}
      {activeDraft && (
        <button
          className="home-strip home-strip-active-draft"
          data-testid="home-strip-active-draft"
          onClick={() => navigate('/draft')}
          aria-label={`Resume active draft: ${activeDraft.EventName}`}
        >
          <span className="home-strip-label home-strip-label-accent">ACTIVE DRAFT</span>
          <span className="home-draft-name" data-testid="home-active-draft-name">
            {activeDraft.EventName}
          </span>
          <span className="home-draft-cta" aria-hidden="true">Resume →</span>
        </button>
      )}

      {/* ── LAST DECK strip ─────────────────────────────────────── */}
      {lastDeck && (
        <button
          className="home-strip home-strip-last-deck"
          data-testid="home-strip-last-deck"
          onClick={() => navigate('/decks')}
          aria-label={`Continue playing with ${lastDeck.name}`}
        >
          <span className="home-strip-label">LAST DECK</span>
          <span className="home-deck-name" data-testid="home-last-deck-name">
            {lastDeck.name}
          </span>
          <span className="home-deck-cta" data-testid="home-last-deck-cta">
            {isLimitedFormat(lastDeck.format) ? 'Open Log' : 'Play Again'} →
          </span>
        </button>
      )}

      {/* ── WHAT'S NEXT nudge module ────────────────────────────── */}
      <WhatsNextNudge data={data!} navigate={navigate} />
    </div>
  );
}
