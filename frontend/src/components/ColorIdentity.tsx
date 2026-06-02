/**
 * ColorIdentity — renders a row of ManaPip symbols for a color identity or
 * color array.  Replaces the per-surface inline renderColorPips / renderColorIdentity
 * helpers that previously returned plain CSS circles.
 *
 * Accepts two input shapes:
 *   - string[]  e.g. ["W","U","B"]  (most call sites)
 *   - string    e.g. "WUB"          (OpponentAnalysisPanel's concatenated form)
 *
 * Colorless (empty / undefined) renders a single 'C' pip.
 * Multicolor (length > 1) renders all individual pips (no gold pip merge).
 *
 * @example
 *   <ColorIdentity colors={["W","U"]} size="md" />
 *   <ColorIdentity colors="WUB" />
 *   <ColorIdentity colors={[]} />   // → colorless C pip
 */
import ManaPip, { type ManaColor, type ManaPipSize } from './ManaPip';

interface ColorIdentityProps {
  /** WUBRG array (e.g. ["W","U"]) or concatenated string (e.g. "WU"). */
  colors: string[] | string | undefined | null;
  size?: ManaPipSize;
  /** Additional class applied to the wrapper span. */
  className?: string;
}

const VALID_COLORS = new Set<string>(['W', 'U', 'B', 'R', 'G', 'C', 'M']);

function normalizeColors(raw: string[] | string | undefined | null): ManaColor[] {
  if (!raw) return ['C'];
  const arr = Array.isArray(raw) ? raw : raw.split('');
  const mapped = arr
    .map((c) => c.toUpperCase())
    .filter((c) => VALID_COLORS.has(c)) as ManaColor[];
  return mapped.length > 0 ? mapped : ['C'];
}

export default function ColorIdentity({ colors, size = 'md', className = '' }: ColorIdentityProps) {
  const pips = normalizeColors(colors);

  return (
    <span
      className={`color-identity-row ${className}`.trim()}
      style={{ display: 'inline-flex', gap: 'var(--space-1, 4px)', alignItems: 'center' }}
      data-testid="color-identity"
    >
      {pips.map((color, i) => (
        <ManaPip key={`${color}-${i}`} color={color} size={size} />
      ))}
    </span>
  );
}
