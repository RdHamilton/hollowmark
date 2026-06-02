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
  ClockIcon,
  RectangleStackIcon,
  Squares2X2Icon,
  ArchiveBoxIcon,
} from '@heroicons/react/24/outline';
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
// Shared QUICK NAV block — uses the same Heroicon components as Home.tsx
// ---------------------------------------------------------------------------

function QuickNavTiles() {
  return (
    <div className="home-quick-nav" data-testid="home-quick-nav">
      <button className="home-nav-tile" aria-label="Match History">
        <ClockIcon className="home-nav-icon" aria-hidden="true" />
        <span className="home-nav-label">Match History</span>
      </button>
      <button className="home-nav-tile" aria-label="Draft">
        <RectangleStackIcon className="home-nav-icon" aria-hidden="true" />
        <span className="home-nav-label">Draft</span>
      </button>
      <button className="home-nav-tile" aria-label="Decks">
        <Squares2X2Icon className="home-nav-icon" aria-hidden="true" />
        <span className="home-nav-label">Decks</span>
      </button>
      <button className="home-nav-tile" aria-label="Collection">
        <ArchiveBoxIcon className="home-nav-icon" aria-hidden="true" />
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
