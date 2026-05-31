/**
 * Raw-hex regression guard — #309 Pass B.
 *
 * Pass B migrated the 2,300+ raw-hex sites across the component CSS files onto
 * the design-system tokens (the --vault-* primitives + the canonical semantic
 * layer Pass A added). This test is the executable regression floor that keeps
 * the tree clean: it asserts that NO raw hex color appears in component CSS
 * except a documented allowlist of genuinely categorical / data-viz values.
 *
 * Per Ray's APPROVE-WITH-CHANGES (#309) ruling #2 (Option B), categorical
 * accents — MTG mana (WUBRG) swatches, win/loss & ML chart series, rarity
 * badges, and the decorative urgency-banner gradients — keep their distinct
 * hues because flattening them into the brand palette destroys legibility.
 * Those values are enumerated in CATEGORICAL_HEX below so the omission is
 * explicit, not accidental: any NEW non-categorical raw hex trips this test.
 *
 *   - index.css is the ONE file permitted to define raw hex (the token source).
 *   - var(--token, #fallback) fallbacks for the categorical allowlist tokens
 *     (--bar-bg, --ml-*, --meta-*, --personal-*) are likewise permitted.
 *
 * Companion to designTokenBridge.test.ts (Pass A), which guarantees every
 * var() reference resolves. The stylelint `color-no-hex` rule (added warn-first
 * in this PR; follow-up #340 flips it to hard-fail) is the lint-layer mirror of
 * this guard.
 */
import { describe, it, expect } from 'vitest';
import { readFileSync, readdirSync, statSync } from 'node:fs';
import { join, dirname, relative } from 'node:path';
import { fileURLToPath } from 'node:url';

const SRC_DIR = join(dirname(fileURLToPath(import.meta.url)), '..');
const INDEX_CSS = join(SRC_DIR, 'index.css');

/**
 * Genuinely categorical / data-viz hex values that intentionally stay raw
 * (Ray ruling #2 / Option B). Grouped by purpose so the rationale is legible.
 * Lowercase, no shorthand-vs-longhand ambiguity — compared case-insensitively.
 */
const CATEGORICAL_HEX = new Set<string>(
  [
    // — MTG mana / card-color (WUBRG) swatches (Scryfall pip palette) —
    '#f9faf4', '#f8f6d8', '#fffbd5', // white
    '#0e68ab', // blue
    '#150b00', // black
    '#00733e', // green
    // — Win/loss & ML/score chart series (must read as distinct hues) —
    '#7dff7d', '#ff7d7d', '#44ff88', '#66ffaa', '#ff6666', '#ff8844',
    '#ff9966', '#ffbb66', '#ffed4e', '#ffff7d', '#90ee90', '#00ff00',
    '#7cfc00', '#4aff4a', '#6bff6b', '#82ca9d', '#50c878', '#7dcfff',
    // — Rarity / wildcard badges (bronze / gold / mythic / common) —
    '#cd7f32', '#e69559', '#d4af37', '#c9a840', '#d4c24a', '#dc7e0e', '#4b5563',
    // — Premium / Twitch / decorative-gradient accents —
    '#9147ff', '#7b2fff', '#8e24aa', '#ab47bc', '#a855f7', '#9c27b0',
    '#ba68c8', '#ec4899', '#f472b6', '#667eea', '#764ba2',
    // — Urgency-banner gradient stops (RotationBanner / LegalityBanner) —
    '#1a3a5c', '#0d2137', '#2d5a8a', '#5c4a1a', '#372d0d', '#8a7a2d',
    '#5c1a1a', '#370d0d', '#8a2d2d',
    // — EnvBadge / brewing chips & misc categorical tints —
    '#2a1a4a', '#4a2a6a', '#4a4a1a', '#6a6a2a', '#2a2a18', '#5a5a2d',
    '#3a2520', '#3a3520', '#ff6b00',
    // — Catppuccin syntax palette (ColorRatingsPanel data-viz) —
    '#a6e3a1', '#cdd6f4', '#f38ba8', '#f9e2af',
  ].map((h) => h.toLowerCase()),
);

/** Custom-property tokens whose var(--x, #fallback) fallback is allowed to be hex. */
const CATEGORICAL_TOKENS = new Set([
  '--bar-bg',
  '--ml-bg',
  '--ml-color',
  '--meta-bg',
  '--meta-color',
  '--personal-bg',
  '--personal-color',
]);

function listCssFiles(dir: string): string[] {
  const out: string[] = [];
  for (const entry of readdirSync(dir)) {
    if (entry === 'node_modules' || entry === 'dist') continue;
    const full = join(dir, entry);
    if (statSync(full).isDirectory()) out.push(...listCssFiles(full));
    else if (entry.endsWith('.css')) out.push(full);
  }
  return out;
}

const COMMENT_RE = /\/\*[\s\S]*?\*\//g;
const CATEGORICAL_FALLBACK_RE = /var\(\s*(--[a-zA-Z0-9-]+)\s*,\s*#[0-9a-fA-F]{3,8}\s*\)/g;
const HEX_RE = /#[0-9a-fA-F]{3,8}\b/g;

/** Raw hex values in a CSS string that are NOT permitted (comments + categorical fallbacks removed). */
function disallowedHex(css: string): string[] {
  // Strip comments (so issue refs like `(#2016)` are not mistaken for colors).
  let stripped = css.replace(COMMENT_RE, '');
  // Strip categorical-token var() fallbacks (e.g. var(--ml-color, #ce93d8)).
  stripped = stripped.replace(CATEGORICAL_FALLBACK_RE, (m, token) =>
    CATEGORICAL_TOKENS.has(token) ? '' : m,
  );
  const found = stripped.match(HEX_RE) ?? [];
  return found.filter((h) => !CATEGORICAL_HEX.has(h.toLowerCase()));
}

describe('#309 Pass B — no raw hex outside index.css + categorical allowlist', () => {
  it('every component CSS file uses tokens, not raw hex (except the documented categorical allowlist)', () => {
    const componentFiles = listCssFiles(SRC_DIR).filter((f) => f !== INDEX_CSS);
    const offenders: string[] = [];
    for (const f of componentFiles) {
      const bad = disallowedHex(readFileSync(f, 'utf8'));
      if (bad.length) {
        offenders.push(`${relative(SRC_DIR, f)}: ${[...new Set(bad)].join(', ')}`);
      }
    }
    expect(
      offenders,
      `raw hex found in component CSS (migrate to a design-system token, or add to ` +
        `CATEGORICAL_HEX if genuinely categorical per Option B):\n  ${offenders.join('\n  ')}`,
    ).toEqual([]);
  });

  it('does not allowlist any hex that no longer appears in the tree (keeps the allowlist honest)', () => {
    const all = listCssFiles(SRC_DIR)
      .map((f) => readFileSync(f, 'utf8').replace(COMMENT_RE, ''))
      .join('\n')
      .toLowerCase();
    const stale = [...CATEGORICAL_HEX].filter((h) => !all.includes(h));
    expect(stale, `categorical allowlist entries no longer used (remove them): ${stale.join(', ')}`).toEqual(
      [],
    );
  });
});
