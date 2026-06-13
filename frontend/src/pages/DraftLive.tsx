/**
 * DraftLive — /draft/live
 *
 * Real-time draft assistant page. Consumes the SSE event stream via
 * useDraftEventStream, feeds events into useDraftSession state machine,
 * and fetches card ratings from the BFF for top-pick highlighting.
 *
 * Ticket: #1390
 *
 * #1421 — Mount-fetch cold-start hydration:
 * On mount (once Clerk is loaded), fetch the active draft session from the
 * BFF and hydrate useDraftSession so the page shows the active state
 * immediately without waiting for an SSE event replay that will never come.
 * ADR-084 = ingest-time broadcast only; the BFF SSE broker does NOT replay
 * missed events.  SSE remains the live-update path once the session is
 * hydrated.
 */

import { useEffect, useMemo, useRef, useState } from 'react';
import { ViewfinderCircleIcon } from '@heroicons/react/24/outline';
import { CheckCircleIcon } from '@heroicons/react/24/solid';
import { useAuth } from '@clerk/react';
import { useDraftEventStream, useDraftSession } from '@/hooks';
import type { DraftPackPayload } from '@/hooks';
import { getDraftRatings } from '@/services/api/bffDraftRatings';
import type { BffCardRating } from '@/services/api/bffDraftRatings';
import { getSetCards } from '@/services/api/cards';
import { getActiveDraftSessions, getDraftPicks } from '@/services/api/drafts';
import { trackEvent } from '@/services/analytics';
import { useFeatureFlag } from '@/hooks/useFeatureFlag';
import { gradeFromGihwr } from './draftGrade';
import LoadingSpinner from '../components/LoadingSpinner';
import EmptyState from '../components/EmptyState';
import './DraftLive.css';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/**
 * Map draft_type / CourseName strings to the canonical draft_format key.
 *
 * The returned string is sent verbatim to the BFF draft-ratings endpoint and
 * MUST match the `draft_format` value the sync lambda writes to the DB exactly
 * — `"PremierDraft"`, `"QuickDraft"`, `"Sealed"` (no spaces, exact casing).
 * Any drift (e.g. `"Quick Draft"` with a space) produces a 404 with no rows.
 *
 * MTGA's CurrentModule names QuickDraft internally as `"BotDraft"` (see
 * classify.go) and `draft.started` carries `draft_type: "BotDraft"`, so BotDraft
 * must map to the canonical `"QuickDraft"` key — not passed through unchanged.
 *
 * Handles both the flat `draft_type` field and the nested `CourseName` field
 * from the daemon pack payload.
 */
function formatLabel(raw: string | undefined): string {
  if (!raw) return 'Draft';
  const upper = raw.toUpperCase();
  if (upper.includes('PREMIER') || upper.includes('TRADITIONAL') || upper.includes('TRAD')) {
    return 'PremierDraft';
  }
  // "BOTDRAFT" is MTGA's internal CurrentModule name for QuickDraft (classify.go).
  if (upper.includes('QUICK') || upper.includes('BOTDRAFT') || upper.includes('BOT')) {
    return 'QuickDraft';
  }
  if (upper.includes('SEALED')) return 'Sealed';
  return raw;
}

/** Best-effort set code extractor from a CourseName like "Quilinor_QuickDraft". */
function setCodeFromCourseName(courseName: string | undefined): string | null {
  if (!courseName) return null;
  // CourseName format: "<SetCode>_<DraftType>", e.g. "ONE_PremierDraft"
  const parts = courseName.split('_');
  if (parts.length >= 2) return parts[0].toUpperCase();
  return null;
}

function gradeClass(grade: string): string {
  const letter = grade.charAt(0);
  switch (letter) {
    case 'A': return 'grade-a';
    case 'B': return 'grade-b';
    case 'C': return 'grade-c';
    case 'D': return 'grade-d';
    case 'F': return 'grade-f';
    default:  return 'grade-unknown';
  }
}

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

interface RatingsState {
  ratings: BffCardRating[];
  loading: boolean;
  error: string | null;
}

// ---------------------------------------------------------------------------
// Component
// ---------------------------------------------------------------------------

const DraftLive: React.FC = () => {
  const { getToken, isLoaded, isSignedIn } = useAuth();
  const getTokenRef = useRef(getToken);
  useEffect(() => { getTokenRef.current = getToken; });

  // Feature flag gate: live_draft_advisor_enabled
  // Optimistic-show default: treat null (loading) as enabled (!== false).
  // SSE stream stays alive regardless of flag state — only highlighting and
  // recommendation telemetry are suppressed when the flag is OFF (ADR-047).
  const { enabled: advisorEnabled } = useFeatureFlag('live_draft_advisor_enabled');
  const advisorVisible = advisorEnabled !== false;

  // ── SSE stream ────────────────────────────────────────────────────────────
  const { latestEvent, status: streamStatus } = useDraftEventStream();

  // ── Session state machine ─────────────────────────────────────────────────
  const { state: session, dispatch, hydrate } = useDraftSession();

  // ── Draft metadata derived from events or mount-fetch ────────────────────
  const [setCode, setSetCode] = useState<string | null>(null);
  const [draftFormat, setDraftFormat] = useState<string | null>(null);

  // ── Mount-fetch cold-start hydration (#1421) ──────────────────────────────
  // On mount, once Clerk is loaded and the user is signed in, fetch the active
  // draft session from the BFF and hydrate the state machine.  This prevents
  // "No active draft" showing mid-draft when the user opens the page between
  // SSE pack events — the BFF SSE broker does NOT replay (ADR-084).
  //
  // Race handling: hydrate() is a no-op when sessionStatus is already 'active',
  // so if an SSE draft.pack arrives before this fetch resolves the live SSE
  // state is preserved.
  const mountFetchedRef = useRef(false);

  useEffect(() => {
    // Wait for Clerk to finish hydrating before touching the BFF — same guard
    // as useDraftEventStream uses before opening the EventSource.
    if (!isLoaded || !isSignedIn) return;
    // Only run once per mount.
    if (mountFetchedRef.current) return;
    mountFetchedRef.current = true;

    const fetchActiveSession = async () => {
      try {
        const sessions = await getActiveDraftSessions();
        if (!sessions || sessions.length === 0) return;

        const session = sessions[0];

        // Derive metadata from the session row.
        if (session.SetCode) setSetCode(session.SetCode.toUpperCase());
        if (session.DraftType) setDraftFormat(formatLabel(session.DraftType));

        // Fetch picks to reconstruct pickedCardIds and latest pack/pick position.
        const picks = await getDraftPicks(session.ID);
        const pickedCardIds = (picks ?? []).map((p) => Number(p.CardID)).filter(Number.isFinite);

        // Derive pack/pick from the most-recent pick row (picks are returned
        // newest-last by the BFF — take the last element).
        const lastPick = picks && picks.length > 0 ? picks[picks.length - 1] : null;
        // BFF PackNumber is 0-based; normalise to 1-based for the state machine.
        const packNumber = lastPick ? lastPick.PackNumber + 1 : 0;
        const pickNumber = lastPick ? lastPick.PickNumber + 1 : 0;

        hydrate({ pickedCardIds, packNumber, pickNumber });
      } catch {
        // Mount-fetch is best-effort: a network error here is non-fatal.
        // The SSE stream will activate the session on the next pack event.
      }
    };

    void fetchActiveSession();
  // eslint-disable-next-line react-hooks/exhaustive-deps -- hydrate is stable; setSetCode/setDraftFormat are stable setState dispatchers
  }, [isLoaded, isSignedIn]);

  // Feed latest SSE event into state machine + derive metadata.
  useEffect(() => {
    if (!latestEvent) return;

    dispatch({ type: latestEvent.type, payload: latestEvent.payload ?? undefined });

    // Extract set code and format from draft.started payload.
    if (latestEvent.type === 'draft.started') {
      const p = latestEvent.payload as { set_code?: string; draft_type?: string } | null;
      if (p?.set_code) setSetCode(p.set_code.toUpperCase());
      if (p?.draft_type) setDraftFormat(formatLabel(p.draft_type));
    }

    // Fallback: also try to extract set code from pack payload CourseName.
    if (latestEvent.type === 'draft.pack' && !setCode) {
      const p = latestEvent.payload as DraftPackPayload | null;
      const extracted = setCodeFromCourseName(p?.CourseName);
      if (extracted) setSetCode(extracted);
      if (p?.CourseName && !draftFormat) setDraftFormat(formatLabel(p.CourseName));
    }
  // eslint-disable-next-line react-hooks/exhaustive-deps -- dispatch is stable; setCode/draftFormat only needed as guards
  }, [latestEvent, dispatch]);

  // ── BFF ratings fetch ─────────────────────────────────────────────────────
  const [ratingsState, setRatingsState] = useState<RatingsState>({
    ratings: [],
    loading: false,
    error: null,
  });

  const lastFetchedRef = useRef<string | null>(null);

  useEffect(() => {
    if (!setCode || !draftFormat) return;
    const key = `${setCode}/${draftFormat}`;
    if (lastFetchedRef.current === key) return;
    lastFetchedRef.current = key;

    const fetchRatings = async () => {
      setRatingsState((prev) => ({ ...prev, loading: true, error: null }));
      try {
        const token = await getTokenRef.current();
        const result = await getDraftRatings(setCode, draftFormat, token);
        setRatingsState({
          ratings: result.data.card_ratings ?? [],
          loading: false,
          error: null,
        });
      } catch (err) {
        setRatingsState({
          ratings: [],
          loading: false,
          error: err instanceof Error ? err.message : 'Failed to load ratings',
        });
      }
    };

    void fetchRatings();
  }, [setCode, draftFormat]);

  // ── Base catalog name fallback ────────────────────────────────────────────
  // Names come from the ratings map only when ratings exist. When ratings are
  // unavailable for ANY reason (auth failure, 404, stale-cache miss), a player
  // must NEVER see a raw "#<arenaId>".  We resolve the card NAME from the base
  // catalog (/api/v1/cards — the same source the deck-builder uses) so names
  // always render; ratings supply GRADES only.
  const [catalogNames, setCatalogNames] = useState<Map<number, string>>(new Map());
  const lastCatalogSetRef = useRef<string | null>(null);

  useEffect(() => {
    if (!setCode) return;
    if (lastCatalogSetRef.current === setCode) return;
    lastCatalogSetRef.current = setCode;

    const fetchCatalog = async () => {
      try {
        const cards = await getSetCards(setCode);
        const map = new Map<number, string>();
        for (const c of cards) {
          const id = Number(c.ArenaID);
          if (Number.isFinite(id) && c.Name) {
            map.set(id, c.Name);
          }
        }
        setCatalogNames(map);
      } catch {
        // Catalog fetch is best-effort — leave names empty and fall back to
        // the ratings name when present (grade still renders as "—").
        setCatalogNames(new Map());
      }
    };

    void fetchCatalog();
  }, [setCode]);

  // ── Derived data: pack cards with ratings ─────────────────────────────────
  const ratingsMap = useMemo(() => {
    const map = new Map<number, BffCardRating>();
    for (const r of ratingsState.ratings) {
      map.set(r.arena_id, r);
    }
    return map;
  }, [ratingsState.ratings]);

  interface PackCard {
    arenaId: number;
    rating: BffCardRating | undefined;
    name: string | null;
    grade: string;
    gihwr: number | undefined;
  }

  const packCards: PackCard[] = useMemo(() => {
    return session.currentPackCards.map((id) => {
      const rating = ratingsMap.get(id);
      const grade = gradeFromGihwr(rating?.gihwr);
      // Prefer ratings name, fall back to base-catalog name. null only when
      // neither source has it (render shows "Card #<id>", never a bare "#id").
      const name = rating?.name ?? catalogNames.get(id) ?? null;
      return { arenaId: id, rating, name, grade, gihwr: rating?.gihwr };
    });
  }, [session.currentPackCards, ratingsMap, catalogNames]);

  // Analytics: feature_draft_advisor_pick_viewed — fires once per pack when cards are non-empty
  // Suppressed when live_draft_advisor_enabled flag is OFF (ADR-047).
  const lastPickKeyRef = useRef<string | null>(null);
  useEffect(() => {
    if (!advisorVisible) return;
    if (packCards.length === 0 || !setCode) return;
    const key = `${setCode}/${session.packNumber}/${session.pickNumber}`;
    if (lastPickKeyRef.current === key) return;
    lastPickKeyRef.current = key;
    trackEvent({
      name: 'feature_draft_advisor_pick_viewed',
      properties: {
        set_code: setCode,
        pack_number: session.packNumber,
        pick_number: session.pickNumber,
      },
    });
  }, [advisorVisible, packCards.length, setCode, session.packNumber, session.pickNumber]);

  // Top pick = highest GIHWR. Null when all are unrated OR when advisor flag is OFF.
  const topPickArenaId: number | null = useMemo(() => {
    if (!advisorVisible) return null;
    if (packCards.length === 0) return null;
    let best: PackCard | null = null;
    for (const card of packCards) {
      if (card.gihwr === undefined) continue;
      if (!best || card.gihwr > (best.gihwr ?? 0)) best = card;
    }
    return best?.arenaId ?? null;
  }, [advisorVisible, packCards]);

  // Picked cards with names: ratings name → catalog name → last-resort label.
  const pickedCardsInfo = useMemo(() => {
    return session.pickedCards.map((id) => {
      const rating = ratingsMap.get(id);
      const name = rating?.name ?? catalogNames.get(id) ?? `Card #${id}`;
      return { arenaId: id, name, grade: gradeFromGihwr(rating?.gihwr) };
    });
  }, [session.pickedCards, ratingsMap, catalogNames]);

  // ── Render ─────────────────────────────────────────────────────────────────

  // No active draft.
  // The stream-status badge is intentionally omitted here: when there is no active
  // draft the SSE connection may show 'error' simply because the daemon is not
  // connected or the staging-api cluster is unreachable. Showing "Error" next to
  // "No active draft" is confusing — the correct empty state already communicates
  // that nothing is happening. The badge is only meaningful during an active draft
  // when real-time pick data is expected.
  if (session.sessionStatus === 'idle') {
    return (
      <div className="draft-live-container" data-testid="draft-live-container">
        <div className="draft-live-header">
          <h1>Live Draft</h1>
        </div>
        <EmptyState
          icon={<ViewfinderCircleIcon className="w-12 h-12" aria-hidden="true" style={{ color: 'var(--vault-fg-muted)' }} />}
          heading="No active draft"
          subtext="Start a draft in Arena to see your live pick recommendations"
          variant="no-data"
        />
      </div>
    );
  }

  // Draft complete.
  if (session.sessionStatus === 'complete') {
    return (
      <div className="draft-live-container" data-testid="draft-live-container">
        <div className="draft-live-header">
          <h1>Live Draft</h1>
        </div>
        <EmptyState
          icon={<CheckCircleIcon className="w-12 h-12" aria-hidden="true" style={{ color: 'var(--vault-success)' }} />}
          heading="Draft complete"
          subtext="Your draft session has ended. View your picks in Draft History."
          variant="no-data"
        />
      </div>
    );
  }

  // Active draft.
  return (
    <div className="draft-live-container" data-testid="draft-live-container">
      {/* Header */}
      <div className="draft-live-header">
        <div className="draft-live-title-row">
          <h1>Live Draft</h1>
          <span
            className={`stream-status stream-status--${streamStatus}`}
            data-testid="stream-status"
          >
            {streamStatus}
          </span>
        </div>
        <div className="draft-live-meta" data-testid="draft-live-meta">
          {setCode && (
            <span className="draft-live-set" data-testid="draft-live-set">
              {setCode}
            </span>
          )}
          {draftFormat && (
            <span className="draft-live-format" data-testid="draft-live-format">
              {draftFormat}
            </span>
          )}
          <span className="draft-live-progress" data-testid="draft-live-progress">
            Pack {session.packNumber} · Pick {session.pickNumber}
          </span>
        </div>
      </div>

      <div className="draft-live-body">
        {/* Current Pack */}
        <section className="draft-live-pack-section" data-testid="draft-live-pack">
          <h2>Current Pack</h2>
          {ratingsState.loading && <LoadingSpinner message="Loading ratings..." />}
          {ratingsState.error && (
            <p className="draft-live-ratings-error" data-testid="ratings-error">
              {ratingsState.error}
            </p>
          )}
          {packCards.length === 0 && !ratingsState.loading && (
            <p className="draft-live-waiting" data-testid="pack-waiting">
              Waiting for next pack…
            </p>
          )}
          <div className="draft-live-pack-grid" data-testid="pack-grid">
            {packCards.map((card) => {
              const isTop = card.arenaId === topPickArenaId;
              return (
                <div
                  key={card.arenaId}
                  className={`draft-live-card${isTop ? ' draft-live-card--top' : ''}`}
                  data-testid={`pack-card-${card.arenaId}`}
                  data-top-pick={isTop ? 'true' : undefined}
                >
                  <span className="draft-live-card-name">
                    {card.name ?? `Card #${card.arenaId}`}
                  </span>
                  <span
                    className={`draft-live-grade ${gradeClass(card.grade)}`}
                    data-testid={`card-grade-${card.arenaId}`}
                  >
                    {card.grade}
                  </span>
                  {card.gihwr !== undefined && card.gihwr !== 0 && (
                    <span
                      className="draft-live-gihwr"
                      data-testid={`card-gihwr-${card.arenaId}`}
                    >
                      {(card.gihwr * 100).toFixed(1)}%
                    </span>
                  )}
                  {isTop && (
                    <span className="draft-live-top-badge" data-testid="top-pick-badge">
                      Top Pick
                    </span>
                  )}
                </div>
              );
            })}
          </div>
        </section>

        {/* Pick History */}
        <section className="draft-live-history-section" data-testid="draft-live-history">
          <h2>Picks ({pickedCardsInfo.length})</h2>
          {pickedCardsInfo.length === 0 ? (
            <p className="draft-live-no-picks">No picks yet</p>
          ) : (
            <div className="draft-live-history-grid">
              {pickedCardsInfo.map((card, idx) => (
                <div
                  key={`${card.arenaId}-${idx}`}
                  className="draft-live-history-item"
                  data-testid={`picked-card-${card.arenaId}`}
                >
                  <span className="draft-live-history-name">{card.name}</span>
                  <span className={`draft-live-grade ${gradeClass(card.grade)}`}>
                    {card.grade}
                  </span>
                </div>
              ))}
            </div>
          )}
        </section>
      </div>
    </div>
  );
};

export default DraftLive;
