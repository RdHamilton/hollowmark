import type { Meta, StoryObj } from '@storybook/react';
import './CurrentPackPicker.css';

/**
 * CurrentPackPicker visual stories.
 *
 * Three story groups:
 *   1. ColorIndicators — mana-pip token migration (#328). Standalone CSS-class
 *      snapshots; deterministic for Chromatic.
 *   2. TierBadges — §7.3 inline tier badge design. Shows every tier variant
 *      (A/B/C/D/F + S backward-compat) at the fixed 28×22px size, bottom-right
 *      corner positioning, with design-token color classes (#686).
 *   3. PackGrid — realistic 5-card pack-grid snapshot with tier badges in situ.
 *      Covers the happy path (all grades), a no-grade card (badge absent), and
 *      the recommended card with "Best Pick" bar overlapping the badge (#686).
 *   4. MissingImageFallback — /back.png shown when image_url is absent or broken.
 */
const meta: Meta = {
  title: 'Components/CurrentPackPicker/ColorIndicators',
  parameters: { layout: 'centered' },
};

export default meta;
type Story = StoryObj;

// ── 1. Color Indicators ────────────────────────────────────────────────────

const COLORS: Array<[string, string]> = [
  ['color-w', 'W'],
  ['color-u', 'U'],
  ['color-b', 'B'],
  ['color-r', 'R'],
  ['color-g', 'G'],
  ['colorless', 'C'],
];

export const ColorIndicators: Story = {
  render: () => (
    <div style={{ display: 'flex', gap: 8 }}>
      {COLORS.map(([cls, label]) => (
        <span key={cls} className={`color-indicator ${cls}`}>
          {label}
        </span>
      ))}
    </div>
  ),
};

// ── 2. Tier Badges ─────────────────────────────────────────────────────────
// Each badge sits in a simulated card-image-container so position:absolute
// resolves correctly (bottom-right, 28×22px per §7.3).

const TIERS: Array<[string, string]> = [
  ['a', 'A'],
  ['b', 'B'],
  ['c', 'C'],
  ['d', 'D'],
  ['f', 'F'],
  ['s', 'S'],
];

export const TierBadges: Story = {
  name: 'Tier Badges (§7.3)',
  render: () => (
    <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap' }}>
      {TIERS.map(([cls, label]) => (
        <div
          key={cls}
          style={{
            position: 'relative',
            width: 80,
            height: 60,
            background: 'var(--vault-bg-raised, #161C26)',
            borderRadius: 4,
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
          }}
        >
          <span style={{ fontSize: 10, color: 'var(--vault-fg-secondary, #94A3B8)' }}>
            {label}-tier card
          </span>
          <div
            className={`tier-badge tier-badge--${cls}`}
            data-testid={`tier-badge-story-${cls}`}
            aria-label={`Tier ${label}`}
          >
            {label}
          </div>
        </div>
      ))}
    </div>
  ),
};

// ── 3. Pack Grid ────────────────────────────────────────────────────────────

interface MockCard {
  id: string;
  name: string;
  tier: string;
  isRecommended?: boolean;
  noGrade?: boolean;
}

const MOCK_CARDS: MockCard[] = [
  { id: '1', name: 'Lightning Bolt', tier: 'A', isRecommended: true },
  { id: '2', name: 'Counterspell', tier: 'B' },
  { id: '3', name: 'Llanowar Elves', tier: 'C' },
  { id: '4', name: 'Grey Ogre', tier: 'D' },
  { id: '5', name: 'Ponder', tier: 'F' },
  { id: '6', name: 'Forest', tier: '', noGrade: true },
];

function MockCardTile({ card }: { card: MockCard }) {
  const tierClass = card.tier
    ? `tier-badge--${card.tier.toLowerCase()}`
    : '';

  return (
    <div
      className={`pack-card${card.isRecommended ? ' recommended' : ''}`}
      data-testid={`pack-card-${card.id}`}
      style={{ width: 120 }}
    >
      <div className="card-image-container">
        {/* Placeholder image area */}
        <div
          style={{
            width: '100%',
            height: '100%',
            background: 'var(--vault-bg-overlay, #1E2636)',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            color: 'var(--vault-fg-muted, #7890AA)',
            fontSize: 10,
          }}
        >
          [art]
        </div>
        {/* Tier badge — absent when no grade (noGrade / empty tier) */}
        {card.tier && (
          <div
            className={`tier-badge ${tierClass}`}
            data-testid={`tier-badge-${card.id}`}
            aria-label={`Tier ${card.tier}`}
          >
            {card.tier}
          </div>
        )}
        {card.isRecommended && (
          <div className="recommended-indicator" data-testid="best-pick-indicator">
            Best Pick
          </div>
        )}
      </div>
      <div className="card-info">
        <div className="card-name">{card.name}</div>
        <div className="card-stats">
          <span className="gihwr">62.5%</span>
          <span className="alsa">ALSA: 2.1</span>
        </div>
      </div>
    </div>
  );
}

export const PackGrid: Story = {
  name: 'Pack Grid with Tier Badges (#686)',
  parameters: { layout: 'padded' },
  render: () => (
    <div style={{ maxWidth: 800 }}>
      {/* Simulated recommended banner */}
      <div className="recommended-banner" style={{ marginBottom: 12 }}>
        <span className="rec-label">Recommended Pick:</span>
        <span className="rec-card-name">Lightning Bolt</span>
        <span className="rec-tier rec-tier--a">A</span>
        <span className="rec-reason">High win rate and synergistic with your colors.</span>
      </div>
      <div className="pack-cards-grid" data-testid="pack-cards-grid">
        {MOCK_CARDS.map((card) => (
          <MockCardTile key={card.id} card={card} />
        ))}
      </div>
      <p style={{ marginTop: 8, fontSize: 11, color: 'var(--vault-fg-muted, #7890AA)' }}>
        Forest (last card) has no grade — badge is absent.
      </p>
    </div>
  ),
};

// ── 4. Missing Image Fallback ──────────────────────────────────────────────

/**
 * MissingImageFallback — shows the /back.png placeholder used when a card's
 * image_url is absent or returns a 4xx/5xx.  Previously this rendered a broken-
 * image icon because the onError handler pointed at a dead Scryfall CDN URL
 * (backs.scryfall.io — 404).  The fix routes both the primary src (null
 * image_url) and the onError handler to the local /back.png asset.
 */
export const MissingImageFallback: Story = {
  name: 'Card Image — missing/broken URL shows /back.png',
  render: () => (
    <div style={{ display: 'flex', gap: 16, alignItems: 'flex-start' }}>
      {/* Card whose image_url is absent — renders /back.png directly */}
      <div className="pack-card" style={{ width: 120 }}>
        <div className="card-image-container">
          <img
            src="/back.png"
            alt="Card back (no image_url)"
            className="card-image"
          />
          <div
            className="tier-badge"
            style={{ backgroundColor: '#4a9eff' }}
          >
            C
          </div>
        </div>
        <div className="card-info">
          <div className="card-name">Missing Image Card</div>
          <div className="card-stats">
            <span className="color-indicator colorless">C</span>
            <span className="gihwr">50.0%</span>
            <span className="alsa">ALSA: 4.5</span>
          </div>
        </div>
      </div>

      {/* Card that simulates the old broken-image state for comparison */}
      <div className="pack-card" style={{ width: 120 }}>
        <div className="card-image-container">
          <img
            src="/back.png"
            alt="Card back (onError fallback)"
            className="card-image"
          />
          <div
            className="tier-badge"
            style={{ backgroundColor: '#ffd700' }}
          >
            S
          </div>
        </div>
        <div className="card-info">
          <div className="card-name">Broken Image Card</div>
          <div className="card-stats">
            <span className="color-indicator color-r">R</span>
            <span className="gihwr">62.3%</span>
            <span className="alsa">ALSA: 2.1</span>
          </div>
        </div>
      </div>
    </div>
  ),
};
