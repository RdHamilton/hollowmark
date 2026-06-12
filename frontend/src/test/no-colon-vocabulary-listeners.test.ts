/**
 * CI Fitness Function — colon-vocabulary SSE listener guard.
 *
 * Fails if any source file in frontend/src introduces (or re-introduces)
 * a colon-vocabulary SSE event listener.  The dead vocabulary
 * (stats:updated, quest:updated, draft:updated, rank:updated,
 * collection:updated, download:*, task:*) had zero server-side emitters
 * since the Wails→REST migration (ADR-084 G1 sweep).
 *
 * This test is the CI enforcement gate referenced in ADR-084 §Fitness Functions.
 * It deliberately runs at the node level (filesystem grep) rather than in jsdom.
 */

import { describe, it, expect } from 'vitest';
import { readdirSync, readFileSync, statSync } from 'fs';
import { join } from 'path';

// Dead colon-vocabulary patterns that must never appear in EventsOn calls.
// Replay events (replay:*) and task:progress/download:progress are ALSO dead
// server-side (no BFF emitter), but they are being removed in this ticket so
// the guard covers the domain-data vocabulary that is definitely dead.
const FORBIDDEN_PATTERNS = [
  /EventsOn\(['"`]stats:updated['"`]/,
  /EventsOn\(['"`]quest:updated['"`]/,
  /EventsOn\(['"`]draft:updated['"`]/,
  /EventsOn\(['"`]rank:updated['"`]/,
  /EventsOn\(['"`]collection:updated['"`]/,
  /EventsOn\(['"`]download:progress['"`]/,
  /EventsOn\(['"`]download:complete['"`]/,
  /EventsOn\(['"`]download:error['"`]/,
  /EventsOn\(['"`]task:progress['"`]/,
  /EventsOn\(['"`]task:complete['"`]/,
  /EventsOn\(['"`]task:error['"`]/,
  /EventsOn\(['"`]match\.completed['"`]/,   // dot-vocabulary race — replaced by readmodel.updated
  /EventsEmit\(['"`]stats:updated['"`]/,    // keyboard shortcut shim must also go
];

// Allowed-list: test files themselves are excluded (they test the pattern)
// and the test mocks directory legitimately exercises these strings.
const EXCLUDED_PATH_FRAGMENTS = [
  '/test/',
  '/__tests__/',
  '.test.ts',
  '.test.tsx',
  '.spec.ts',
  '.spec.tsx',
];

function walkSrc(dir: string): string[] {
  const entries = readdirSync(dir);
  const files: string[] = [];
  for (const entry of entries) {
    const full = join(dir, entry);
    const stat = statSync(full);
    if (stat.isDirectory()) {
      files.push(...walkSrc(full));
    } else if (full.endsWith('.ts') || full.endsWith('.tsx')) {
      files.push(full);
    }
  }
  return files;
}

describe('CI lint: no colon-vocabulary SSE listeners in production source', () => {
  // Resolve relative to this test file: src/test/ → src/
  const srcDir = join(__dirname, '..');
  const allFiles = walkSrc(srcDir);

  const productionFiles = allFiles.filter(
    (f) => !EXCLUDED_PATH_FRAGMENTS.some((fragment) => f.includes(fragment))
  );

  for (const pattern of FORBIDDEN_PATTERNS) {
    it(`no file uses ${pattern.source}`, () => {
      const violations: string[] = [];
      for (const file of productionFiles) {
        const content = readFileSync(file, 'utf-8');
        if (pattern.test(content)) {
          violations.push(file.replace(srcDir, 'src'));
        }
      }
      expect(violations).toEqual([]);
    });
  }
});
