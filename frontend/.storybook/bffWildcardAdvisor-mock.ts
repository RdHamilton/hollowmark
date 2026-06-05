/**
 * Storybook mock for `@/services/api/bffWildcardAdvisor`.
 *
 * Aliased via `viteFinal` in `.storybook/main.ts` so every story that
 * imports WildcardAdvisorPanel gets this mock instead of the real BFF
 * adapter. The real adapter calls `fetch` with a real URL — in Chromatic's
 * render environment there is no server, so that fetch throws and the story
 * crashes before it can be snapshot.
 *
 * `getWildcardRecommendations` is a `fn()` (Storybook's mock function, which
 * wraps `@vitest/spy`). Each story's `beforeEach` hook overrides the
 * implementation to control which render state the panel shows:
 *
 *   import { getWildcardRecommendations } from
 *     '!!@/services/api/bffWildcardAdvisor';   // the mock, not the real module
 *
 *   beforeEach: () => {
 *     getWildcardRecommendations.mockResolvedValue(myFixture);
 *   }
 *
 * Default implementation: never-resolving Promise (component stays in
 * loading/skeleton state). Overridden per story.
 *
 * Re-exports all types from the real module so type imports in stories and
 * components continue to work with no changes.
 */

import { fn } from 'storybook/test';
import type {
  WildcardAdvisorFormat,
  WildcardAdvisorResult,
} from '../src/services/api/bffWildcardAdvisor';

export type {
  WildcardAdvisorFormat,
  WildcardRecommendation,
  WildcardBudget,
  WildcardAdvisorResponse,
  WildcardAdvisorResult,
} from '../src/services/api/bffWildcardAdvisor';

/**
 * Spy-able mock for `getWildcardRecommendations`.
 *
 * Default: a never-resolving Promise so the panel shows the loading/skeleton
 * state unless a story's `beforeEach` overrides it.
 */
export const getWildcardRecommendations = fn<
  [WildcardAdvisorFormat, string | null],
  Promise<WildcardAdvisorResult>
>().mockImplementation(() => new Promise(() => {}));
