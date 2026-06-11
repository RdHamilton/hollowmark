/**
 * Home — returning-player command center ("Command Strip") (#689).
 *
 * Rendered at the `/home` route after sign-in. Fetches `useAuth().getToken()`,
 * then calls three BFF adapters in parallel to build the command strip.
 *
 * Because the live component is async (loading → loaded/empty/error states),
 * each story uses a static `render` override that reproduces the component's
 * own HTML structure and CSS classes. This produces deterministic Chromatic
 * snapshots without any BFF or network dependency — the same technique used
 * by OnboardingModal.stories.tsx for its async polling step.
 *
 * States covered:
 *   - Loading        — ManaWheel spinner shown while BFF calls are in flight
 *   - Loaded         — Full command strip: weekly record + last match + active
 *                      draft + last deck + QUICK NAV quadrant (realistic data)
 *   - LoadedNoDraft  — Loaded strip without the optional active-draft row
 *   - Empty          — First-run: no match history, no drafts, no decks;
 *                      ManaWheel + "get started" prompt + QUICK NAV
 *
 * Decorators:
 *   - withRouter (per-story) — required because the component uses useNavigate()
 *     internally; the static render overrides call noop navigate fns directly.
 *   - withClerkSession (global, registered in preview.ts) — supplies useAuth()
 *     and useUser() via the @clerk/react alias without a live Clerk key.
 */
import type { Meta, StoryObj } from '@storybook/react';
import { MemoryRouter } from 'react-router-dom';
import {
  LedgerGlyph,
  FanCardsGlyph,
  DeckStackGlyph,
  BinderGlyph,
} from '../components/MagicGlyphs';
import ManaWheel from '../components/ManaWheel';
import Home from './Home';
import './Home.css';

const meta: Meta<typeof Home> = {
  title: 'Organisms/Home',
  component: Home,
  parameters: {
    layout: 'fullscreen',
    clerk: { signedIn: true },
  },
  tags: ['autodocs'],
};

export default meta;
type Story = StoryObj<typeof Home>;

// ---------------------------------------------------------------------------
// Shared QUICK NAV block — uses the same MagicGlyphs components as Home.tsx
// ---------------------------------------------------------------------------

function QuickNavTiles() {
  return (
    <div className="home-quick-nav" data-testid="home-quick-nav">
      <button className="home-nav-tile" aria-label="Match History">
        <LedgerGlyph className="home-nav-icon" size={20} />
        <span className="home-nav-label">Match History</span>
      </button>
      <button className="home-nav-tile" aria-label="Draft">
        <FanCardsGlyph className="home-nav-icon" size={20} />
        <span className="home-nav-label">Draft</span>
      </button>
      <button className="home-nav-tile" aria-label="Decks">
        <DeckStackGlyph className="home-nav-icon" size={20} />
        <span className="home-nav-label">Decks</span>
      </button>
      <button className="home-nav-tile" aria-label="Collection">
        <BinderGlyph className="home-nav-icon" size={20} />
        <span className="home-nav-label">Collection</span>
      </button>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Loading
// ---------------------------------------------------------------------------

/**
 * Loading — the component is waiting for BFF calls to resolve.
 * ManaWheel spinner + loading text is the only content.
 */
export const Loading: Story = {
  render: () => (
    <MemoryRouter>
      <div className="home-page" data-testid="home-page">
        <div className="home-loading" data-testid="home-loading">
          <ManaWheel size={120} ariaLabel="Loading your stats" />
          <p className="home-loading-text">Loading your command center…</p>
        </div>
      </div>
    </MemoryRouter>
  ),
};

// ---------------------------------------------------------------------------
// Loaded — full command strip with realistic data
// ---------------------------------------------------------------------------

/**
 * Loaded — full command strip: weekly record with today's split, last match
 * context, an active draft (visually dominant sapphire strip), last deck with
 * "Play Again" CTA, and the QUICK NAV quadrant.
 *
 * This is the primary Chromatic target — all seven Prof gates visible in one
 * snapshot (semantic W/L color, CSS divider, active-draft dominance, spacing
 * tokens, "Play Again" CTA, last-match micro-strip, four nav icons).
 *
 * Elapsed time "30 min ago" matches formatElapsed(1800) from homeUtils.ts.
 */
export const Loaded: Story = {
  render: () => (
    <MemoryRouter>
      <div className="home-page" data-testid="home-page">

        {/* ── WEEKLY RECORD strip ───────────────────────── */}
        <div className="home-strip home-strip-weekly" data-testid="home-strip-weekly">
          <span className="home-strip-label">THIS WEEK</span>

          <span className="home-strip-stat-group">
            <span
              className="home-stat-w"
              style={{ color: 'var(--vault-success)' }}
              data-testid="home-weekly-wins"
            >
              8W
            </span>
            <span className="home-strip-divider" aria-hidden="true" />
            <span
              className="home-stat-l"
              style={{ color: 'var(--vault-danger)' }}
              data-testid="home-weekly-losses"
            >
              4L
            </span>
            <span className="home-strip-divider" aria-hidden="true" />
            <span
              className="home-stat-wr"
              style={{ color: 'var(--vault-success)' }}
              data-testid="home-weekly-winrate"
            >
              66.7%
            </span>
          </span>

          <span className="home-strip-today" data-testid="home-today-record">
            Today: 2–1
          </span>
        </div>

        {/* ── LAST MATCH micro-strip ─────────────────────── */}
        <div className="home-strip home-strip-last-match" data-testid="home-strip-last-match">
          <span
            className="home-last-match-result"
            style={{ color: 'var(--vault-success)' }}
            data-testid="home-last-match-result"
          >
            Won
          </span>
          <span className="home-strip-divider" aria-hidden="true" />
          <span className="home-last-match-archetype" data-testid="home-last-match-archetype">
            vs. Esper Midrange
          </span>
          <span className="home-strip-divider" aria-hidden="true" />
          <span className="home-last-match-elapsed" data-testid="home-last-match-elapsed">
            30 min ago
          </span>
        </div>

        {/* ── ACTIVE DRAFT strip (dominant) ─────────────── */}
        <button
          className="home-strip home-strip-active-draft"
          data-testid="home-strip-active-draft"
          onClick={() => {}}
          aria-label="Resume active draft: BLB Booster Draft"
        >
          <span className="home-strip-label home-strip-label-accent">ACTIVE DRAFT</span>
          <span className="home-draft-name" data-testid="home-active-draft-name">
            BLB Booster Draft
          </span>
          <span className="home-draft-cta" aria-hidden="true">Resume →</span>
        </button>

        {/* ── LAST DECK strip ────────────────────────────── */}
        <button
          className="home-strip home-strip-last-deck"
          data-testid="home-strip-last-deck"
          onClick={() => {}}
          aria-label="Continue playing with Azorius Tempo"
        >
          <span className="home-strip-label">LAST DECK</span>
          <span className="home-deck-name" data-testid="home-last-deck-name">
            Azorius Tempo
          </span>
          <span className="home-deck-cta" data-testid="home-last-deck-cta">
            Play Again →
          </span>
        </button>

        {/* ── QUICK NAV quadrant ─────────────────────────── */}
        <QuickNavTiles />

      </div>
    </MemoryRouter>
  ),
};

// ---------------------------------------------------------------------------
// LoadedNoDraft — loaded strip without an active draft row
// ---------------------------------------------------------------------------

/**
 * LoadedNoDraft — same as Loaded but without an active-draft session.
 * The active-draft strip is conditionally rendered; this story documents the
 * layout when no draft is in progress (deck strip sits directly below last match).
 *
 * Win rate 50.0% uses var(--vault-fg-secondary) — matches winRateColor(0.5)
 * which returns 'var(--vault-fg-secondary)' for rate >= 0.5 (but < 0.57).
 * Elapsed time "12 min ago" matches formatElapsed(720) from homeUtils.ts.
 */
export const LoadedNoDraft: Story = {
  render: () => (
    <MemoryRouter>
      <div className="home-page" data-testid="home-page">

        {/* ── WEEKLY RECORD strip ───────────────────────── */}
        <div className="home-strip home-strip-weekly" data-testid="home-strip-weekly">
          <span className="home-strip-label">THIS WEEK</span>

          <span className="home-strip-stat-group">
            <span
              className="home-stat-w"
              style={{ color: 'var(--vault-success)' }}
              data-testid="home-weekly-wins"
            >
              3W
            </span>
            <span className="home-strip-divider" aria-hidden="true" />
            <span
              className="home-stat-l"
              style={{ color: 'var(--vault-danger)' }}
              data-testid="home-weekly-losses"
            >
              3L
            </span>
            <span className="home-strip-divider" aria-hidden="true" />
            <span
              className="home-stat-wr"
              style={{ color: 'var(--vault-fg-secondary)' }}
              data-testid="home-weekly-winrate"
            >
              50.0%
            </span>
          </span>
        </div>

        {/* ── LAST MATCH micro-strip ─────────────────────── */}
        <div className="home-strip home-strip-last-match" data-testid="home-strip-last-match">
          <span
            className="home-last-match-result"
            style={{ color: 'var(--vault-danger)' }}
            data-testid="home-last-match-result"
          >
            Lost
          </span>
          <span className="home-strip-divider" aria-hidden="true" />
          <span className="home-last-match-archetype" data-testid="home-last-match-archetype">
            vs. Mono-Red Aggro
          </span>
          <span className="home-strip-divider" aria-hidden="true" />
          <span className="home-last-match-elapsed" data-testid="home-last-match-elapsed">
            12 min ago
          </span>
        </div>

        {/* ── LAST DECK strip (no draft — Open Log CTA for limited format) */}
        <button
          className="home-strip home-strip-last-deck"
          data-testid="home-strip-last-deck"
          onClick={() => {}}
          aria-label="Continue playing with BLB Draft Deck"
        >
          <span className="home-strip-label">LAST DECK</span>
          <span className="home-deck-name" data-testid="home-last-deck-name">
            BLB Draft Deck
          </span>
          <span className="home-deck-cta" data-testid="home-last-deck-cta">
            Open Log →
          </span>
        </button>

        {/* ── QUICK NAV quadrant ─────────────────────────── */}
        <QuickNavTiles />

      </div>
    </MemoryRouter>
  ),
};

// ---------------------------------------------------------------------------
// Empty / first-run
// ---------------------------------------------------------------------------

/**
 * Empty — first-run state: no match history, no active draft, no decks.
 * Shows ManaWheel + "get started" prompt + QUICK NAV quadrant only.
 * The welcome name falls back to "Planeswalker" (the Clerk mock default).
 */
export const Empty: Story = {
  render: () => (
    <MemoryRouter>
      <div className="home-page" data-testid="home-page">
        <div className="home-empty" data-testid="home-empty">
          <ManaWheel size={120} ariaLabel="Get started with VaultMTG" />
          <p className="home-empty-title">Welcome, Planeswalker</p>
          <p className="home-empty-body">
            Connect your daemon and play a game to get started — your command center
            will fill in as you play.
          </p>
        </div>

        {/* QUICK NAV is present even in the empty state */}
        <QuickNavTiles />
      </div>
    </MemoryRouter>
  ),
};

// ─── What's Next nudge variants (v0.3.7 anti-slop) ─────────────────────────

export const NudgeColdDeck: Story = {
  name: "What's Next — Cold Deck",
  render: () => (
    <div className="home-page" style={{ background: '#0D1117', padding: 16, minHeight: '50vh' }}>
      <p style={{ color: '#94A3B8', fontSize: 12, marginBottom: 8 }}>
        Condition: this_week 2W 5L, lastDeck present. Shows "below .500" nudge.
      </p>
      <div className="home-whats-next" data-testid="home-whats-next-cold-deck">
        <svg className="home-whats-next-icon" aria-hidden="true" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" strokeWidth={1.5} stroke="currentColor">
          <path strokeLinecap="round" strokeLinejoin="round" d="M2.25 6L9 12.75l4.286-4.286a11.948 11.948 0 014.306 6.43l.776 2.898m0 0l3.182-5.511m-3.182 5.51l-5.511-3.181" />
        </svg>
        <div className="home-whats-next-body">
          <span className="home-whats-next-headline">Your record is below .500 this week</span>
          <span className="home-whats-next-detail">Try tweaking <strong>Azorius Tempo</strong> or switching decks.</span>
          <button className="home-whats-next-cta">Edit deck &rarr;</button>
        </div>
      </div>
    </div>
  ),
  decorators: [(Story) => <MemoryRouter><Story /></MemoryRouter>],
};

export const NudgeHotStreak: Story = {
  name: "What's Next — Hot Streak",
  render: () => (
    <div className="home-page" style={{ background: '#0D1117', padding: 16, minHeight: '50vh' }}>
      <p style={{ color: '#94A3B8', fontSize: 12, marginBottom: 8 }}>
        Condition: 6W 2L, 75% win rate. Shows "on a roll" nudge.
      </p>
      <div className="home-whats-next" data-testid="home-whats-next-hot-streak">
        <svg className="home-whats-next-icon" aria-hidden="true" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" strokeWidth={1.5} stroke="currentColor">
          <path strokeLinecap="round" strokeLinejoin="round" d="M15.362 5.214A8.252 8.252 0 0112 21 8.25 8.25 0 016.038 7.048 8.287 8.287 0 009 9.6a8.983 8.983 0 013.361-6.867 8.21 8.21 0 003 2.48z" />
          <path strokeLinecap="round" strokeLinejoin="round" d="M12 18a3.75 3.75 0 00.495-7.467 5.99 5.99 0 00-1.925 3.546 5.974 5.974 0 01-2.133-1A3.75 3.75 0 0012 18z" />
        </svg>
        <div className="home-whats-next-body">
          <span className="home-whats-next-headline">On a roll this week</span>
          <span className="home-whats-next-detail">6 wins at 75.0% — you&apos;re in the zone.</span>
          <button className="home-whats-next-cta">Start a draft &rarr;</button>
        </div>
      </div>
    </div>
  ),
  decorators: [(Story) => <MemoryRouter><Story /></MemoryRouter>],
};

export const NudgeStale: Story = {
  name: "What's Next — Stale (no matches)",
  render: () => (
    <div className="home-page" style={{ background: '#0D1117', padding: 16, minHeight: '50vh' }}>
      <p style={{ color: '#94A3B8', fontSize: 12, marginBottom: 8 }}>
        Condition: all-zero this_week. Shows "no matches this week" nudge.
      </p>
      <div className="home-whats-next" data-testid="home-whats-next-stale">
        <svg className="home-whats-next-icon" aria-hidden="true" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" strokeWidth={1.5} stroke="currentColor">
          <path strokeLinecap="round" strokeLinejoin="round" d="M6.75 3v2.25M17.25 3v2.25M3 18.75V7.5a2.25 2.25 0 012.25-2.25h13.5A2.25 2.25 0 0121 7.5v11.25m-18 0A2.25 2.25 0 005.25 21h13.5A2.25 2.25 0 0021 18.75m-18 0v-7.5A2.25 2.25 0 015.25 9h13.5A2.25 2.25 0 0121 11.25v7.5m-9-6h.008v.008H12v-.008zM12 15h.008v.008H12V15zm0 2.25h.008v.008H12v-.008zM9.75 15h.008v.008H9.75V15zm0 2.25h.008v.008H9.75v-.008zM7.5 15h.008v.008H7.5V15zm0 2.25h.008v.008H7.5v-.008zm6.75-4.5h.008v.008h-.008v-.008zm0 2.25h.008v.008h-.008V15zm0 2.25h.008v.008h-.008v-.008zm2.25-4.5h.008v.008H16.5v-.008zm0 2.25h.008v.008H16.5V15z" />
        </svg>
        <div className="home-whats-next-body">
          <span className="home-whats-next-headline">No matches this week</span>
          <span className="home-whats-next-detail">Ready for a draft?</span>
          <button className="home-whats-next-cta">Start a draft &rarr;</button>
        </div>
      </div>
    </div>
  ),
  decorators: [(Story) => <MemoryRouter><Story /></MemoryRouter>],
};

export const NudgeFormatBreakdown: Story = {
  name: "What's Next — Most Played Format",
  render: () => (
    <div className="home-page" style={{ background: '#0D1117', padding: 16, minHeight: '50vh' }}>
      <p style={{ color: '#94A3B8', fontSize: 12, marginBottom: 8 }}>
        Condition: format_breakdown present, QuickDraft dominant (62 matches).
      </p>
      <div className="home-whats-next" data-testid="home-whats-next-format">
        <svg className="home-whats-next-icon" aria-hidden="true" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" strokeWidth={1.5} stroke="currentColor">
          <path strokeLinecap="round" strokeLinejoin="round" d="M3 13.125C3 12.504 3.504 12 4.125 12h2.25c.621 0 1.125.504 1.125 1.125v6.75C7.5 20.496 6.996 21 6.375 21h-2.25A1.125 1.125 0 013 19.875v-6.75zM9.75 8.625c0-.621.504-1.125 1.125-1.125h2.25c.621 0 1.125.504 1.125 1.125v11.25c0 .621-.504 1.125-1.125 1.125h-2.25a1.125 1.125 0 01-1.125-1.125V8.625zM16.5 4.125c0-.621.504-1.125 1.125-1.125h2.25C20.496 3 21 3.504 21 4.125v15.75c0 .621-.504 1.125-1.125 1.125h-2.25a1.125 1.125 0 01-1.125-1.125V4.125z" />
        </svg>
        <div className="home-whats-next-body">
          <span className="home-whats-next-headline">Most played format: QuickDraft</span>
          <span className="home-whats-next-detail">62 matches at 58.1% win rate</span>
          <button className="home-whats-next-cta">View stats &rarr;</button>
        </div>
      </div>
    </div>
  ),
  decorators: [(Story) => <MemoryRouter><Story /></MemoryRouter>],
};

export const NudgeEmpty: Story = {
  name: "What's Next — Empty (no conditions match)",
  render: () => (
    <div className="home-page" style={{ background: '#0D1117', padding: 16, minHeight: '50vh' }}>
      <p style={{ color: '#94A3B8', fontSize: 12, marginBottom: 8 }}>
        Condition: no nudge conditions match and no format_breakdown. Module is absent.
      </p>
      <p style={{ color: '#7890AA', fontSize: 11 }}>
        (Nothing rendered here — that&apos;s correct. The What&apos;s Next module is absent.)
      </p>
    </div>
  ),
  decorators: [(Story) => <MemoryRouter><Story /></MemoryRouter>],
};
