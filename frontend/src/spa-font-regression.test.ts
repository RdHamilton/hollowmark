/**
 * SPA font regression tests — vmt-t#684 + vmt-t#685
 *
 * Guard against re-introduction of:
 *   - Cormorant Garamond (marketing-site-only serif-italic) in any SPA resource
 *   - Lorebook affectations (§ Chapter N · The Ledger / The Draft) in any TSX page component
 *
 * These tests read file contents directly so they are CI-fast (no DOM / render
 * overhead) and catch regressions before they reach Chromatic snapshots.
 *
 * Strategy:
 *   - For the font: grep index.html and index.css — the only two places a new
 *     Google Fonts import or font-family token would land.
 *   - For the lorebook copy: grep every .tsx file under src/pages/ and
 *     src/components/ for the literal patterns Prof flagged.
 *
 * These tests are intentionally strict: a false negative (test passes when it
 * should fail) is much worse than a false positive. If you legitimately need to
 * add a new font, update the allowlist below.
 */

import { describe, it, expect } from 'vitest';
import { readFileSync, readdirSync, statSync } from 'node:fs';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';

const __dirname = dirname(fileURLToPath(import.meta.url));
const FRONTEND_ROOT = join(__dirname, '..');
const SRC_ROOT = __dirname;

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/** Recursively collect all .tsx files under a directory. */
function collectTsx(dir: string): string[] {
  const results: string[] = [];
  for (const entry of readdirSync(dir)) {
    const full = join(dir, entry);
    const stat = statSync(full);
    if (stat.isDirectory()) {
      results.push(...collectTsx(full));
    } else if (entry.endsWith('.tsx') || entry.endsWith('.ts')) {
      results.push(full);
    }
  }
  return results;
}

// ---------------------------------------------------------------------------
// #684 — No Cormorant Garamond anywhere in the SPA
// ---------------------------------------------------------------------------

describe('SPA font audit — no Cormorant Garamond (#684)', () => {
  it('index.html contains no Cormorant Garamond Google Fonts import', () => {
    const html = readFileSync(join(FRONTEND_ROOT, 'index.html'), 'utf8');
    expect(html.toLowerCase()).not.toContain('cormorant');
    expect(html.toLowerCase()).not.toContain('garamond');
  });

  it('index.css font tokens do not reference Cormorant Garamond', () => {
    const css = readFileSync(join(SRC_ROOT, 'index.css'), 'utf8');
    expect(css.toLowerCase()).not.toContain('cormorant');
    expect(css.toLowerCase()).not.toContain('garamond');
    // Verify the three approved fonts ARE present
    expect(css).toContain('Space Grotesk');
    expect(css).toContain('Inter');
    expect(css).toContain('JetBrains Mono');
  });

  it('index.css --font-display-vault token uses Space Grotesk', () => {
    const css = readFileSync(join(SRC_ROOT, 'index.css'), 'utf8');
    expect(css).toMatch(/--font-display-vault\s*:\s*"Space Grotesk"/);
  });

  it('no .css file under src/ references Cormorant Garamond', () => {
    const violations: string[] = [];
    function scanDir(dir: string) {
      for (const entry of readdirSync(dir)) {
        const full = join(dir, entry);
        const stat = statSync(full);
        if (stat.isDirectory()) {
          scanDir(full);
        } else if (entry.endsWith('.css')) {
          const content = readFileSync(full, 'utf8').toLowerCase();
          if (content.includes('cormorant') || content.includes('garamond')) {
            violations.push(full.replace(SRC_ROOT + '/', ''));
          }
        }
      }
    }
    scanDir(SRC_ROOT);
    expect(violations).toEqual([]);
  });
});

// ---------------------------------------------------------------------------
// #685 — No lorebook affectations in page/component TSX files
// ---------------------------------------------------------------------------

describe('SPA heading copy — no lorebook affectations (#685)', () => {
  /**
   * Patterns that indicate a lorebook affectation.
   *
   * The § character is legitimate in code comments (ADR references, spec
   * section cites). We only flag it when it appears inside JSX — i.e., after
   * a `>` or inside a template literal that is clearly display text.
   *
   * To keep the regex simple, we scan for the specific multi-word lorebook
   * patterns Prof flagged rather than all § occurrences.
   */
  const LOREBOOK_PATTERNS = [
    /§\s*Chapter\s+\d/,  // "§ Chapter N"
    /The\s+Ledger/,       // "The Ledger"
    /The\s+Draft\s*[·]/,  // "The Draft ·" (title variant)
    /Compendium/,         // lorebook framing word
  ];

  const allTsx = collectTsx(join(SRC_ROOT, 'pages'))
    .concat(collectTsx(join(SRC_ROOT, 'components')));

  // Filter to only .tsx (skip .ts utility files where these patterns are irrelevant)
  const tsxOnly = allTsx.filter((f) => f.endsWith('.tsx'));

  // Filter out test files — they may legitimately reference the old patterns as
  // string literals in assertions (e.g., expect(h1.textContent).not.toMatch(...))
  const nonTestTsx = tsxOnly.filter(
    (f) => !f.includes('.test.') && !f.includes('.stories.')
  );

  for (const file of nonTestTsx) {
    it(`${file.replace(SRC_ROOT + '/', '')} contains no lorebook affectation patterns`, () => {
      const content = readFileSync(file, 'utf8');
      for (const pattern of LOREBOOK_PATTERNS) {
        expect(content).not.toMatch(pattern);
      }
    });
  }
});
