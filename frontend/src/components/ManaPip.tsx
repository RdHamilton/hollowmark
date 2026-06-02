/**
 * ManaPip — a single MTG mana symbol rendered via the open-source mana-font
 * (Andrew Gioia, MIT license — https://mana.andrewgioia.com,
 * npm: mana-font@1.18.0).
 *
 * Usage:
 *   <ManaPip color="W" />          — White pip
 *   <ManaPip color="C" />          — Colorless pip
 *   <ManaPip color="M" size="lg"/> — Multicolor (gold) pip, large
 *
 * The `color` prop accepts the canonical WUBRG single-letter values plus:
 *   - "C" for colorless
 *   - "M" for multicolor / gold (rendered as the gold {X} symbol)
 *
 * Sizing tokens map to the design system spacing scale:
 *   - "sm"  → 14px  (pip-spacing, badge context)
 *   - "md"  → 18px  (default, inline in card/row)
 *   - "lg"  → 24px  (emphasis, draft card header)
 */
import 'mana-font/css/mana.css';

export type ManaColor = 'W' | 'U' | 'B' | 'R' | 'G' | 'C' | 'M';
export type ManaPipSize = 'sm' | 'md' | 'lg';

interface ManaPipProps {
  color: ManaColor;
  size?: ManaPipSize;
  className?: string;
}

/** Maps our canonical color keys to mana-font's CSS modifier classes */
const COLOR_CLASS: Record<ManaColor, string> = {
  W: 'ms-w',
  U: 'ms-u',
  B: 'ms-b',
  R: 'ms-r',
  G: 'ms-g',
  C: 'ms-c',
  M: 'ms-gold',
};

/** Maps our size tokens to inline font-size so we integrate with the design
 *  token grid (4px base unit).  */
const SIZE_PX: Record<ManaPipSize, number> = {
  sm: 14,
  md: 18,
  lg: 24,
};

const COLOR_LABEL: Record<ManaColor, string> = {
  W: 'White',
  U: 'Blue',
  B: 'Black',
  R: 'Red',
  G: 'Green',
  C: 'Colorless',
  M: 'Multicolor',
};

export default function ManaPip({ color, size = 'md', className = '' }: ManaPipProps) {
  const colorClass = COLOR_CLASS[color] ?? 'ms-c';
  const fontSize = SIZE_PX[size];
  return (
    <i
      className={`ms ms-cost ms-shadow ${colorClass} ${className}`.trim()}
      style={{ fontSize }}
      aria-label={COLOR_LABEL[color] ?? color}
      role="img"
      data-testid={`mana-pip-${color.toLowerCase()}`}
    />
  );
}
