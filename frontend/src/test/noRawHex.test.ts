/**
 * Raw-hex regression guard — #309 Pass B, completed by #328.
 *
 * Pass B migrated the 2,300+ raw-hex sites across the component CSS files onto
 * the design-system tokens (the --vault-* primitives + the canonical semantic
 * layer Pass A added), leaving a documented allowlist of genuinely categorical
 * / data-viz values (Ray ruling #2 / Option B).
 *
 * #328 (Should-Have Epic B long-tail migration) closed out that remaining
 * allowlist: with Ramone's 2026-05-31 brand decisions (win → --vault-sapphire,
 * loss → --vault-danger; MTG-canonical mana-pip backgrounds promoted to the
 * --vault-mtg-* token contract) every former categorical hue now resolves to a
 * named token in index.css. The CATEGORICAL_HEX allowlist is therefore EMPTY:
 * component CSS contains zero raw hex. Any new raw hex trips this test.
 *
 *   - index.css is the ONE file permitted to define raw hex (the token source).
 *
 * Companion to designTokenBridge.test.ts (Pass A), which guarantees every
 * var() reference resolves. The stylelint `color-no-hex` rule (#340, hard-fail)
 * is the lint-layer mirror of this guard.
 */
import { describe, it, expect } from 'vitest';
import { readFileSync, readdirSync, statSync } from 'node:fs';
import { join, dirname, relative } from 'node:path';
import { fileURLToPath } from 'node:url';

const SRC_DIR = join(dirname(fileURLToPath(import.meta.url)), '..');
const INDEX_CSS = join(SRC_DIR, 'index.css');

/**
 * Categorical / data-viz hex values intentionally left raw.
 *
 * EMPTY as of #328: every former categorical hue (MTG mana pips, win/loss,
 * rarity/grade badges, Twitch/decorative gradients, urgency banners, EnvBadge,
 * Catppuccin win-rate scale) now resolves to a named --vault-* / semantic token
 * in index.css per Ramone's 2026-05-31 brand decisions. Component CSS holds zero
 * raw hex. Re-add an entry here ONLY with an explicit design ruling.
 */
const CATEGORICAL_HEX = new Set<string>(
  ([] as string[]).map((h) => h.toLowerCase()),
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

describe('#309 Pass B / #328 — no raw hex outside index.css (allowlist now empty)', () => {
  it('every component CSS file uses tokens, not raw hex', () => {
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
