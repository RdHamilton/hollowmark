/**
 * Gilt token alias regression test — hollowmark-tickets#1047
 *
 * Asserts that:
 *  1. `--hollowmark-gilt` is defined in index.css (the canonical gilt token).
 *  2. `--vault-gilt` is defined as a legacy alias pointing to `var(--hollowmark-gilt)`.
 *
 * Uses the same source-level approach as typography.test.ts — jsdom does not
 * resolve var() chains from injected stylesheets, so reading the raw CSS file
 * is the reliable contract lock.
 */

import { describe, it, expect } from 'vitest';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

const indexCss = readFileSync(resolve(__dirname, './index.css'), 'utf8');

describe('gilt design tokens — hollowmark-tickets#1047', () => {
  it('defines --hollowmark-gilt as the canonical aged-brass value', () => {
    expect(indexCss).toMatch(/--hollowmark-gilt\s*:\s*#B87D32/);
  });

  it('defines --vault-gilt as a legacy alias pointing to var(--hollowmark-gilt)', () => {
    expect(indexCss).toMatch(/--vault-gilt\s*:\s*var\(--hollowmark-gilt\)/);
  });
});
