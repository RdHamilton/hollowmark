import { test, expect, Page } from '@playwright/test';

/**
 * Match History E2E Tests (#2000, #2061, #2178, vmt-t#625)
 *
 * Tests the cloud match-history page at /match-history, served by the
 * BffMatchHistory component. BffMatchHistory fetches the Clerk-protected
 * GET /api/v1/history/matches endpoint and renders a cursor-paginated table,
 * an empty state, or an error state.
 *
 * Auth approach: the Vite build is produced with VITE_CLERK_TEST_MODE=true which
 * aliases @clerk/react to src/test/mocks/clerkMock.tsx. That mock reads
 * window.__CLERK_TEST_STATE__ — injected via page.addInitScript() — so tests
 * control auth state without a real Clerk publishable key.
 *
 * Default state (no injection or { isSignedIn: false }): signed-out.
 *   ProtectedRoute renders the sign-in prompt instead of page content.
 *
 * Signed-in state ({ isSignedIn: true }): ProtectedRoute passes through and
 *   BffMatchHistory renders, calling the BFF API.
 *
 * vmt-t#625 fix: the mock now uses the ACTUAL BFF response shape
 * (cursor-paginated: { data: [...], has_more, next_cursor_ts, next_cursor_id, limit })
 * rather than the old broken shape ({ matches: [...], total, offset, limit }).
 * Tests that previously sent the old shape were silently passing with an empty
 * table because the component would read data.matches (undefined) → empty state,
 * and the test didn't assert on row count.
 */

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/** Inject signed-in Clerk state before page load. Must be called before page.goto(). */
async function setClerkSignedIn(page: Page): Promise<void> {
  await page.addInitScript((s) => {
    (window as unknown as Record<string, unknown>).__CLERK_TEST_STATE__ = s;
  }, { isSignedIn: true, firstName: 'Test', lastName: 'User' });
}

/** Inject signed-out Clerk state before page load. Must be called before page.goto(). */
async function setClerkSignedOut(page: Page): Promise<void> {
  await page.addInitScript((s) => {
    (window as unknown as Record<string, unknown>).__CLERK_TEST_STATE__ = s;
  }, { isSignedIn: false });
}

type MatchItem = {
  id: string;
  format: string;
  result: string;
  timestamp: string;
  player_wins: number;
  opponent_wins: number;
  duration_seconds: number | null;
  deck_id: string | null;
  rank_before: string | null;
  rank_after: string | null;
  opponent_rank: string | null;
};

function makeMatchItem(overrides: Partial<MatchItem> = {}): MatchItem {
  return {
    id: 'match-1',
    format: 'Standard',
    result: 'win',
    timestamp: '2026-05-01T12:00:00Z',
    player_wins: 2,
    opponent_wins: 0,
    duration_seconds: null,
    deck_id: null,
    rank_before: null,
    rank_after: null,
    opponent_rank: null,
    ...overrides,
  };
}

/**
 * Mock GET /api/v1/history/matches with the ACTUAL BFF cursor-paginated shape.
 *
 * IMPORTANT: The BFF returns { data: [...], has_more, next_cursor_ts, next_cursor_id, limit }.
 * The old mock used { matches: [...], total, offset, limit } which was always wrong —
 * the component read data.data (undefined from old shape) → empty state.
 *
 * Must be registered before page.goto().
 */
async function mockMatchHistory(
  page: Page,
  items: MatchItem[],
  hasMore = false,
  nextCursorTS?: string,
  nextCursorID?: string
): Promise<void> {
  await page.route('**/api/v1/history/matches**', (route) => {
    const body: Record<string, unknown> = {
      data: items,
      has_more: hasMore,
      limit: 20,
    };
    if (hasMore && nextCursorTS) body['next_cursor_ts'] = nextCursorTS;
    if (hasMore && nextCursorID) body['next_cursor_id'] = nextCursorID;

    void route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(body),
    });
  });
}

const MATCH_ITEMS: MatchItem[] = Array.from({ length: 20 }, (_, i) =>
  makeMatchItem({
    id: String(i + 1),
    result: i % 2 === 0 ? 'win' : 'loss',
    format: 'Standard',
    timestamp: '2026-05-01T12:00:00Z',
    player_wins: i % 2 === 0 ? 2 : 0,
    opponent_wins: i % 2 === 0 ? 0 : 2,
  })
);

// ---------------------------------------------------------------------------
// Tests — signed-in, table rendered
// ---------------------------------------------------------------------------

test.describe('Match History', () => {
  test.beforeEach(async ({ page }) => {
    // Inject signed-in state so ProtectedRoute allows /match-history to render,
    // and mock the BFF response so the table renders deterministically.
    await setClerkSignedIn(page);
    await mockMatchHistory(page, MATCH_ITEMS);
  });

  test.describe('Navigation and Page Load', () => {
    test('@smoke should display the Match History page', async ({ page }) => {
      await page.goto('/match-history');
      await expect(page.locator('[data-testid="app-container"]')).toBeVisible({ timeout: 15_000 });
      await expect(page.locator('h1.page-title')).toHaveText('Match History');
    });

    test('should render the match history table container', async ({ page }) => {
      await page.goto('/match-history');
      await expect(page.locator('[data-testid="match-history-page"]')).toBeVisible();
      await expect(page.locator('[data-testid="match-history-table"]')).toBeVisible();
    });
  });

  test.describe('Match Table', () => {
    test('@smoke should display the expected column headers', async ({ page }) => {
      await page.goto('/match-history');

      const table = page.locator('[data-testid="match-history-table"]');
      await expect(table).toBeVisible();

      const headerTexts = await table.locator('thead th').allTextContents();
      expect(headerTexts).toContain('Date');
      expect(headerTexts).toContain('Format');
      expect(headerTexts).toContain('Result');
    });

    test('should render a row for every match returned by the BFF', async ({ page }) => {
      await page.goto('/match-history');

      const table = page.locator('[data-testid="match-history-table"]');
      await expect(table).toBeVisible();
      await expect(table.locator('tbody tr')).toHaveCount(MATCH_ITEMS.length);
    });

    test('@smoke renders actual BFF cursor-paginated shape — not empty state (vmt-t#625 regression guard)', async ({ page }) => {
      // This test is the E2E regression guard for vmt-t#625.
      // Before the fix, the mock sending { data: [...] } would result in "No matches yet"
      // because the component read data.matches (undefined). After the fix it reads
      // data.data and renders the table.
      await page.goto('/match-history');

      // Table must be visible — NOT the empty state.
      await expect(page.locator('[data-testid="match-history-table"]')).toBeVisible({ timeout: 15_000 });
      await expect(page.locator('[data-testid="match-history-empty"]')).not.toBeVisible();
    });
  });

  test.describe('Pagination', () => {
    test('should display pagination controls', async ({ page }) => {
      await mockMatchHistory(page, MATCH_ITEMS, true, '2026-05-01T00:00:00Z', 'cursor-20');
      await page.goto('/match-history');

      await expect(page.locator('[data-testid="match-history-table"]')).toBeVisible();

      const pageInfo = page.locator('.pagination-info');
      await expect(pageInfo).toBeVisible();
      await expect(pageInfo).toContainText('Page');
    });
  });

  test.describe('Loading State', () => {
    test('should not show error state on initial load', async ({ page }) => {
      await page.goto('/match-history');

      await expect(page.locator('[data-testid="match-history-table"]')).toBeVisible();
      await expect(page.locator('.error-state')).not.toBeVisible();
    });
  });

  test.describe('Daemon independence — no ERR_CONNECTION_REFUSED (#2061)', () => {
    test('should not emit ERR_CONNECTION_REFUSED errors when navigating to Match History page', async ({ page }) => {
      // Collect all console errors to detect failed network requests to port 9001
      // (the daemon port). Before the fix in PR #2058, getHealth() was called without
      // an isDesktopApp() guard and produced ERR_CONNECTION_REFUSED when the daemon
      // was offline. This test ensures the guard is in place.
      const daemonErrors: string[] = [];
      page.on('console', (msg) => {
        if (msg.type() === 'error') {
          const text = msg.text();
          if (
            text.includes('ERR_CONNECTION_REFUSED') ||
            text.includes('9001')
          ) {
            daemonErrors.push(text);
          }
        }
      });

      await page.goto('/match-history');
      await expect(page.locator('[data-testid="match-history-page"]')).toBeVisible();

      // Wait for the page to finish loading so all API calls have fired.
      await page.waitForLoadState('networkidle', { timeout: 15_000 }).catch(() => {/* ignore timeout */});

      expect(
        daemonErrors,
        `Match History page emitted daemon connection errors: ${daemonErrors.join('; ')}`,
      ).toHaveLength(0);
    });

    test('should not make network requests to port 9001 on initial load', async ({ page }) => {
      // Intercept all network requests and flag any directed to port 9001 (the daemon).
      // The match history page should only contact the BFF on port 8080.
      const daemonRequests: string[] = [];
      page.on('request', (request) => {
        const url = request.url();
        if (url.includes('9001')) {
          daemonRequests.push(url);
        }
      });

      await page.goto('/match-history');
      await expect(page.locator('[data-testid="match-history-page"]')).toBeVisible();

      await page.waitForLoadState('networkidle', { timeout: 15_000 }).catch(() => {/* ignore timeout */});

      expect(
        daemonRequests,
        `Match History page sent requests to the daemon (port 9001): ${daemonRequests.join(', ')}`,
      ).toHaveLength(0);
    });
  });
});

// ---------------------------------------------------------------------------
// Empty state — signed-in, no matches
// ---------------------------------------------------------------------------

test.describe('Match History — empty state', () => {
  test('shows the empty state when the BFF returns no matches', async ({ page }) => {
    await setClerkSignedIn(page);
    await mockMatchHistory(page, [], false);

    await page.goto('/match-history');

    await expect(page.locator('[data-testid="match-history-empty"]')).toBeVisible();
    await expect(page.locator('[data-testid="match-history-table"]')).not.toBeVisible();
  });
});

// ---------------------------------------------------------------------------
// Error state — signed-in, BFF error
// ---------------------------------------------------------------------------

test.describe('Match History — error state', () => {
  test('shows the error state when the BFF returns a 500', async ({ page }) => {
    await setClerkSignedIn(page);
    await page.route('**/api/v1/history/matches**', (route) => {
      void route.fulfill({
        status: 500,
        contentType: 'application/json',
        body: JSON.stringify({ error: 'internal server error' }),
      });
    });

    await page.goto('/match-history');

    await expect(page.locator('.error-state')).toBeVisible();
    await expect(page.locator('[data-testid="match-history-table"]')).not.toBeVisible();
    await expect(page.locator('[data-testid="match-history-empty"]')).not.toBeVisible();
  });
});

// ---------------------------------------------------------------------------
// Unauthenticated access — protected route shows sign-in prompt (#2000)
//
// When { isSignedIn: false } is injected the Clerk mock reports signed-out.
// ProtectedRoute must show the sign-in prompt, NOT match-history content.
// ---------------------------------------------------------------------------

test.describe('Match History — unauthenticated access', () => {
  test('@smoke unauthenticated visit to /match-history shows sign-in prompt, not match content', async ({ page }) => {
    await setClerkSignedOut(page);
    await page.goto('/match-history');

    // Give the page time to resolve the auth guard.
    await page.waitForLoadState('networkidle', { timeout: 10_000 }).catch(() => {/* ignore timeout */});

    // ProtectedRoute must render the sign-in prompt for unauthenticated users.
    await expect(page.locator('[data-testid="protected-route-prompt"]'), {
      message: 'ProtectedRoute must show the sign-in prompt for unauthenticated users on /match-history',
    }).toBeVisible({ timeout: 10_000 });

    // The prompt must contain the sign-in action button.
    await expect(page.locator('[data-testid="protected-route-sign-in-btn"]')).toBeVisible();

    // Match History content must NOT be rendered without authentication.
    await expect(page.locator('[data-testid="match-history-table"]')).not.toBeVisible();
    await expect(page.locator('[data-testid="match-history-empty"]')).not.toBeVisible();
  });
});
