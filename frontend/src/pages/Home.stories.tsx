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
            30m
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
        <div className="home-quick-nav" data-testid="home-quick-nav">
          <button className="home-nav-tile" aria-label="Match History">
            <svg className="home-nav-icon" aria-hidden="true" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={1.5}>
              <path strokeLinecap="round" strokeLinejoin="round" d="M12 6v6h4.5m4.5 0a9 9 0 1 1-18 0 9 9 0 0 1 18 0Z" />
            </svg>
            <span className="home-nav-label">Match History</span>
          </button>
          <button className="home-nav-tile" aria-label="Draft">
            <svg className="home-nav-icon" aria-hidden="true" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={1.5}>
              <path strokeLinecap="round" strokeLinejoin="round" d="M6 6.878V6a2.25 2.25 0 0 1 2.25-2.25h7.5A2.25 2.25 0 0 1 18 6v.878m-12 0c.235-.083.487-.128.75-.128h10.5c.263 0 .515.045.75.128m-12 0A2.25 2.25 0 0 0 4.5 9v.878m13.5-3A2.25 2.25 0 0 1 19.5 9v.878m0 0a2.246 2.246 0 0 0-.75-.128H5.25c-.263 0-.515.045-.75.128m15 0A2.25 2.25 0 0 1 21 12v6a2.25 2.25 0 0 1-2.25 2.25H5.25A2.25 2.25 0 0 1 3 18v-6c0-.98.626-1.813 1.5-2.122" />
            </svg>
            <span className="home-nav-label">Draft</span>
          </button>
          <button className="home-nav-tile" aria-label="Decks">
            <svg className="home-nav-icon" aria-hidden="true" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={1.5}>
              <path strokeLinecap="round" strokeLinejoin="round" d="M3.75 6A2.25 2.25 0 0 1 6 3.75h2.25A2.25 2.25 0 0 1 10.5 6v2.25a2.25 2.25 0 0 1-2.25 2.25H6a2.25 2.25 0 0 1-2.25-2.25V6ZM3.75 15.75A2.25 2.25 0 0 1 6 13.5h2.25a2.25 2.25 0 0 1 2.25 2.25V18a2.25 2.25 0 0 1-2.25 2.25H6A2.25 2.25 0 0 1 3.75 18v-2.25ZM13.5 6a2.25 2.25 0 0 1 2.25-2.25H18A2.25 2.25 0 0 1 20.25 6v2.25A2.25 2.25 0 0 1 18 10.5h-2.25a2.25 2.25 0 0 1-2.25-2.25V6ZM13.5 15.75a2.25 2.25 0 0 1 2.25-2.25H18a2.25 2.25 0 0 1 2.25 2.25V18A2.25 2.25 0 0 1 18 20.25h-2.25A2.25 2.25 0 0 1 13.5 18v-2.25Z" />
            </svg>
            <span className="home-nav-label">Decks</span>
          </button>
          <button className="home-nav-tile" aria-label="Collection">
            <svg className="home-nav-icon" aria-hidden="true" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={1.5}>
              <path strokeLinecap="round" strokeLinejoin="round" d="m20.25 7.5-.625 10.632a2.25 2.25 0 0 1-2.247 2.118H6.622a2.25 2.25 0 0 1-2.247-2.118L3.75 7.5M10 11.25h4M3.375 7.5h17.25c.621 0 1.125-.504 1.125-1.125v-1.5c0-.621-.504-1.125-1.125-1.125H3.375c-.621 0-1.125.504-1.125 1.125v1.5c0 .621.504 1.125 1.125 1.125Z" />
            </svg>
            <span className="home-nav-label">Collection</span>
          </button>
        </div>

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
              style={{ color: 'var(--vault-fg-muted)' }}
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
            12m
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
        <div className="home-quick-nav" data-testid="home-quick-nav">
          <button className="home-nav-tile" aria-label="Match History">
            <svg className="home-nav-icon" aria-hidden="true" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={1.5}>
              <path strokeLinecap="round" strokeLinejoin="round" d="M12 6v6h4.5m4.5 0a9 9 0 1 1-18 0 9 9 0 0 1 18 0Z" />
            </svg>
            <span className="home-nav-label">Match History</span>
          </button>
          <button className="home-nav-tile" aria-label="Draft">
            <svg className="home-nav-icon" aria-hidden="true" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={1.5}>
              <path strokeLinecap="round" strokeLinejoin="round" d="M6 6.878V6a2.25 2.25 0 0 1 2.25-2.25h7.5A2.25 2.25 0 0 1 18 6v.878m-12 0c.235-.083.487-.128.75-.128h10.5c.263 0 .515.045.75.128m-12 0A2.25 2.25 0 0 0 4.5 9v.878m13.5-3A2.25 2.25 0 0 1 19.5 9v.878m0 0a2.246 2.246 0 0 0-.75-.128H5.25c-.263 0-.515.045-.75.128m15 0A2.25 2.25 0 0 1 21 12v6a2.25 2.25 0 0 1-2.25 2.25H5.25A2.25 2.25 0 0 1 3 18v-6c0-.98.626-1.813 1.5-2.122" />
            </svg>
            <span className="home-nav-label">Draft</span>
          </button>
          <button className="home-nav-tile" aria-label="Decks">
            <svg className="home-nav-icon" aria-hidden="true" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={1.5}>
              <path strokeLinecap="round" strokeLinejoin="round" d="M3.75 6A2.25 2.25 0 0 1 6 3.75h2.25A2.25 2.25 0 0 1 10.5 6v2.25a2.25 2.25 0 0 1-2.25 2.25H6a2.25 2.25 0 0 1-2.25-2.25V6ZM3.75 15.75A2.25 2.25 0 0 1 6 13.5h2.25a2.25 2.25 0 0 1 2.25 2.25V18a2.25 2.25 0 0 1-2.25 2.25H6A2.25 2.25 0 0 1 3.75 18v-2.25ZM13.5 6a2.25 2.25 0 0 1 2.25-2.25H18A2.25 2.25 0 0 1 20.25 6v2.25A2.25 2.25 0 0 1 18 10.5h-2.25a2.25 2.25 0 0 1-2.25-2.25V6ZM13.5 15.75a2.25 2.25 0 0 1 2.25-2.25H18a2.25 2.25 0 0 1 2.25 2.25V18A2.25 2.25 0 0 1 18 20.25h-2.25A2.25 2.25 0 0 1 13.5 18v-2.25Z" />
            </svg>
            <span className="home-nav-label">Decks</span>
          </button>
          <button className="home-nav-tile" aria-label="Collection">
            <svg className="home-nav-icon" aria-hidden="true" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={1.5}>
              <path strokeLinecap="round" strokeLinejoin="round" d="m20.25 7.5-.625 10.632a2.25 2.25 0 0 1-2.247 2.118H6.622a2.25 2.25 0 0 1-2.247-2.118L3.75 7.5M10 11.25h4M3.375 7.5h17.25c.621 0 1.125-.504 1.125-1.125v-1.5c0-.621-.504-1.125-1.125-1.125H3.375c-.621 0-1.125.504-1.125 1.125v1.5c0 .621.504 1.125 1.125 1.125Z" />
            </svg>
            <span className="home-nav-label">Collection</span>
          </button>
        </div>

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
        <div className="home-quick-nav" data-testid="home-quick-nav">
          <button className="home-nav-tile" aria-label="Match History">
            <svg className="home-nav-icon" aria-hidden="true" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={1.5}>
              <path strokeLinecap="round" strokeLinejoin="round" d="M12 6v6h4.5m4.5 0a9 9 0 1 1-18 0 9 9 0 0 1 18 0Z" />
            </svg>
            <span className="home-nav-label">Match History</span>
          </button>
          <button className="home-nav-tile" aria-label="Draft">
            <svg className="home-nav-icon" aria-hidden="true" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={1.5}>
              <path strokeLinecap="round" strokeLinejoin="round" d="M6 6.878V6a2.25 2.25 0 0 1 2.25-2.25h7.5A2.25 2.25 0 0 1 18 6v.878m-12 0c.235-.083.487-.128.75-.128h10.5c.263 0 .515.045.75.128m-12 0A2.25 2.25 0 0 0 4.5 9v.878m13.5-3A2.25 2.25 0 0 1 19.5 9v.878m0 0a2.246 2.246 0 0 0-.75-.128H5.25c-.263 0-.515.045-.75.128m15 0A2.25 2.25 0 0 1 21 12v6a2.25 2.25 0 0 1-2.25 2.25H5.25A2.25 2.25 0 0 1 3 18v-6c0-.98.626-1.813 1.5-2.122" />
            </svg>
            <span className="home-nav-label">Draft</span>
          </button>
          <button className="home-nav-tile" aria-label="Decks">
            <svg className="home-nav-icon" aria-hidden="true" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={1.5}>
              <path strokeLinecap="round" strokeLinejoin="round" d="M3.75 6A2.25 2.25 0 0 1 6 3.75h2.25A2.25 2.25 0 0 1 10.5 6v2.25a2.25 2.25 0 0 1-2.25 2.25H6a2.25 2.25 0 0 1-2.25-2.25V6ZM3.75 15.75A2.25 2.25 0 0 1 6 13.5h2.25a2.25 2.25 0 0 1 2.25 2.25V18a2.25 2.25 0 0 1-2.25 2.25H6A2.25 2.25 0 0 1 3.75 18v-2.25ZM13.5 6a2.25 2.25 0 0 1 2.25-2.25H18A2.25 2.25 0 0 1 20.25 6v2.25A2.25 2.25 0 0 1 18 10.5h-2.25a2.25 2.25 0 0 1-2.25-2.25V6ZM13.5 15.75a2.25 2.25 0 0 1 2.25-2.25H18a2.25 2.25 0 0 1 2.25 2.25V18A2.25 2.25 0 0 1 18 20.25h-2.25A2.25 2.25 0 0 1 13.5 18v-2.25Z" />
            </svg>
            <span className="home-nav-label">Decks</span>
          </button>
          <button className="home-nav-tile" aria-label="Collection">
            <svg className="home-nav-icon" aria-hidden="true" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth={1.5}>
              <path strokeLinecap="round" strokeLinejoin="round" d="m20.25 7.5-.625 10.632a2.25 2.25 0 0 1-2.247 2.118H6.622a2.25 2.25 0 0 1-2.247-2.118L3.75 7.5M10 11.25h4M3.375 7.5h17.25c.621 0 1.125-.504 1.125-1.125v-1.5c0-.621-.504-1.125-1.125-1.125H3.375c-.621 0-1.125.504-1.125 1.125v1.5c0 .621.504 1.125 1.125 1.125Z" />
            </svg>
            <span className="home-nav-label">Collection</span>
          </button>
        </div>
      </div>
    </MemoryRouter>
  ),
};
