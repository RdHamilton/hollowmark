/**
 * MagicGlyphs — Magic-native SVG glyphs for Home nav tiles (#1026).
 *
 * Hand-drawn at Heroicon 1.5px stroke weight. Evoke the MTG card-game
 * domain (ledger, fan of cards, deck stack, binder) without lifting
 * WOTC-owned art. Source: final design kit
 * (`vault-mtg-docs/engineering/design/rebranding/Ray Hamilton Engineering
 *  Design System/ui_kits/vaultmtg-app-redesign/Icons.jsx` ~lines 36–39).
 *
 * Each component accepts a `size` prop (default 24) and forwards all
 * remaining SVG props. The `data-glyph` attribute enables test selectors
 * without requiring text content or aria roles.
 *
 * Trademark note: these paths are bespoke — they evoke MTG domain
 * without reproducing WOTC's official iconography. Pre-vetted in D17.
 */

import type { SVGAttributes } from 'react';

interface GlyphProps extends SVGAttributes<SVGElement> {
  size?: number;
}

function GlyphBase({
  children,
  size = 24,
  ...rest
}: GlyphProps & { children: React.ReactNode }) {
  return (
    <svg
      xmlns="http://www.w3.org/2000/svg"
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={1.5}
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      {...rest}
    >
      {children}
    </svg>
  );
}

/**
 * LedgerGlyph — open book / scroll representing Match History.
 * Use on the Match History nav tile.
 */
export function LedgerGlyph({ size, ...rest }: GlyphProps) {
  return (
    <GlyphBase size={size} data-glyph="ledger" {...rest}>
      <path d="M12 6C10 4.6 6.5 4.6 4.5 5.5V19c2-.9 5.5-.9 7.5.5 2-1.4 5.5-1.4 7.5-.5V5.5C17.5 4.6 14 4.6 12 6Z" />
      <path d="M12 6v13.5" />
    </GlyphBase>
  );
}

/**
 * FanCardsGlyph — fanned set of three cards representing Draft.
 * Use on the Draft nav tile.
 */
export function FanCardsGlyph({ size, ...rest }: GlyphProps) {
  return (
    <GlyphBase size={size} data-glyph="fan-cards" {...rest}>
      <rect x="3.5" y="8" width="8.5" height="12" rx="1.3" transform="rotate(-15 7.75 14)" />
      <rect x="12" y="8" width="8.5" height="12" rx="1.3" transform="rotate(15 16.25 14)" />
      <rect x="7.75" y="5.5" width="8.5" height="13" rx="1.3" />
    </GlyphBase>
  );
}

/**
 * DeckStackGlyph — stacked cards with a lip representing Decks.
 * Use on the Decks nav tile.
 */
export function DeckStackGlyph({ size, ...rest }: GlyphProps) {
  return (
    <GlyphBase size={size} data-glyph="deck-stack" {...rest}>
      <path d="M8 5.5h7.5A1.5 1.5 0 0 1 17 7v9.5" />
      <rect x="5.5" y="7.5" width="11" height="12" rx="1.5" />
    </GlyphBase>
  );
}

/**
 * BinderGlyph — 2×2 grid of card slots representing Collection.
 * Use on the Collection nav tile.
 */
export function BinderGlyph({ size, ...rest }: GlyphProps) {
  return (
    <GlyphBase size={size} data-glyph="binder" {...rest}>
      <rect x="3.5" y="3.5" width="7" height="8" rx="1" />
      <rect x="13.5" y="3.5" width="7" height="8" rx="1" />
      <rect x="3.5" y="12.5" width="7" height="8" rx="1" />
      <rect x="13.5" y="12.5" width="7" height="8" rx="1" />
    </GlyphBase>
  );
}
