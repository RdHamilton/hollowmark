import { test, expect, Page } from '@playwright/test';

/**
 * History Pages E2E Tests (#1461, #2178)
 *
 * Smoke and functional coverage for the cloud match-history page (/match-history,
 * served by BffMatchHistory) and the draft-history page (/history/drafts, served
 * by BffDraftHistory).
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
 *   BffMatchHistory / BffDraftHistory render and call the BFF API.
 *
 * BFF-data mocking (#2178): in CI the BFF runs with a Clerk secret that does not
 * accept the Clerk mock's stub token, so the real Clerk-protected
 * /api/v1/history/* endpoints reject every request. To keep these tests
 * independent of a live authenticated BFF, every authenticated test installs a
 * page.route() interceptor that fulfils /api/v1/history/matches and
 * /api/v1/history/drafts with deterministic fixture data before navigation.
 *
 * Route note (#2178): there is no /history/matches route — the cloud match
 * history page lives at /match-history (App.tsx). These tests target
 * /match-history directly.
 *
 * vmt-t#625 follow-up: mockMatchHistory previously returned the stale
 * { matches, total, limit, offset } shape. BffMatchHistory reads resp.data
 * (cursor-paginated), so the old mock produced an empty table. Updated to
 * { data, has_more, next_cursor_ts, next_cursor_id, limit } — matching the
 * reference in match-history.spec.ts and BffMatchHistory.test.tsx.
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

/**
 * MatchItem matches the cursor-paginated BFF shape for
 * GET /api/v1/history/matches (same contract as match-history.spec.ts).
 * The old MatchRow type used opponent_deck/played_at which the component
 * never reads; the component reads id, format, result, timestamp,
 * player_wins, opponent_wins. (vmt-t#625 follow-up)
 */
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

type DraftRow = {
  id: number;
  set_code: string;
  wins: number;
  losses: number;
  drafted_at: string;
};

/**
 * Mock GET /api/v1/history/matches with the ACTUAL BFF cursor-paginated shape.
 *
 * BffMatchHistory reads resp.data (not resp.matches). The old mock used
 * { matches, total, limit, offset } which was always wrong — the component
 * read resp.data (undefined from old shape) → empty state.
 *
 * Must be registered before page.goto(). (vmt-t#625 follow-up)
 */
async function mockMatchHistory(
  page: Page,
  items: MatchItem[],
  hasMore = false,
  nextCursorTS?: string,
  nextCursorID?: string,
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

/**
 * Mock GET /api/v1/history/drafts with fixture rows so BffDraftHistory does not
 * depend on a live authenticated BFF. Must be registered before page.goto().
 */
async function mockDraftHistory(page: Page, rows: DraftRow[], total = rows.length): Promise<void> {
  await page.route('**/api/v1/history/drafts**', (route) => {
    void route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({ drafts: rows, total, limit: 20, offset: 0 }),
    });
  });
}

// A page of 20 match items using the real MatchItem shape that BffMatchHistory
// renders. Each item has the fields the component reads: id, format, result,
// timestamp, player_wins, opponent_wins. (vmt-t#625 follow-up)
const MATCH_ROWS: MatchItem[] = Array.from({ length: 20 }, (_, i) => ({
  id: String(i + 1),
  format: 'Standard',
  result: i % 2 === 0 ? 'win' : 'loss',
  timestamp: '2026-05-01T12:00:00Z',
  player_wins: i % 2 === 0 ? 2 : 0,
  opponent_wins: i % 2 === 0 ? 0 : 2,
  duration_seconds: null,
  deck_id: null,
  rank_before: null,
  rank_after: null,
  opponent_rank: null,
}));

const DRAFT_ROWS: DraftRow[] = Array.from({ length: 20 }, (_, i) => ({
  id: i + 1,
  set_code: 'TDM',
  wins: 7,
  losses: 2,
  drafted_at: '2026-05-01T12:00:00Z',
}));

// ---------------------------------------------------------------------------
// Match history — /match-history (BffMatchHistory)
// ---------------------------------------------------------------------------

test.describe('History: /match-history', () => {
  test.describe('Unauthenticated', () => {
    test('unauthenticated access does not show match history content @smoke', async ({ page }) => {
      await setClerkSignedOut(page);
      await page.goto('/match-history');

      // Give the page time to settle — auth guard must have resolved.
      await page.waitForLoadState('networkidle', { timeout: 10_000 }).catch(() => {/* ignore timeout */});

      // Match history content must NOT be rendered for an unauthenticated user.
      const table = page.locator('[data-testid="match-history-table"]');
      const empty = page.locator('[data-testid="match-history-empty"]');

      await expect(table).not.toBeVisible();
      await expect(empty).not.toBeVisible();

      // ProtectedRoute must show the sign-in prompt instead.
      await expect(page.locator('[data-testid="protected-route-prompt"]')).toBeVisible();
    });
  });

  test.describe('Authenticated — with data', () => {
    test.beforeEach(async ({ page }) => {
      await setClerkSignedIn(page);
      await mockMatchHistory(page, MATCH_ROWS);
    });

    test('page loads without error and shows the match table @smoke', async ({ page }) => {
      await page.goto('/match-history');

      await expect(page.locator('[data-testid="match-history-table"]')).toBeVisible();
    });

    test('page title is "Match History"', async ({ page }) => {
      await page.goto('/match-history');

      await expect(page.locator('[data-testid="match-history-table"]')).toBeVisible();
      await expect(page.locator('h1.page-title')).toHaveText('Match History');
    });

    test('no error state is shown on initial load', async ({ page }) => {
      await page.goto('/match-history');

      await expect(page.locator('[data-testid="match-history-table"]')).toBeVisible();
      await expect(page.locator('.error-state')).not.toBeVisible();
    });

    test('table renders the expected column headers', async ({ page }) => {
      await page.goto('/match-history');

      const table = page.locator('[data-testid="match-history-table"]');
      await expect(table).toBeVisible();

      // BffMatchHistory renders four columns: Date, Format, Result, Score.
      await expect(table.locator('thead th').nth(0)).toHaveText('Date');
      await expect(table.locator('thead th').nth(1)).toHaveText('Format');
      await expect(table.locator('thead th').nth(2)).toHaveText('Result');
      await expect(table.locator('thead th').nth(3)).toHaveText('Score');
    });

    test('pagination controls render when there are more pages', async ({ page }) => {
      // has_more: true + cursor values → the BFF signals a next page exists.
      // The pagination footer renders with enabled Next when hasMore is true.
      await mockMatchHistory(
        page,
        MATCH_ROWS,
        true,
        '2026-05-01T00:00:00Z',
        'cursor-20',
      );
      await page.goto('/match-history');

      await expect(page.locator('[data-testid="match-history-table"]')).toBeVisible();

      const prevBtn = page.locator('.pagination-btn', { hasText: 'Previous' });
      const nextBtn = page.locator('.pagination-btn', { hasText: 'Next' });
      const pageInfo = page.locator('.pagination-info');

      await expect(prevBtn).toBeVisible({ timeout: 5_000 });
      await expect(nextBtn).toBeVisible({ timeout: 5_000 });
      await expect(pageInfo).toContainText('Page');
    });
  });

  test.describe('Authenticated — empty', () => {
    test('empty state renders when the BFF returns no matches', async ({ page }) => {
      await setClerkSignedIn(page);
      await mockMatchHistory(page, [], false);

      await page.goto('/match-history');

      await expect(page.locator('[data-testid="match-history-empty"]')).toBeVisible();
      await expect(page.locator('[data-testid="match-history-table"]')).not.toBeVisible();
    });
  });

  test.describe('Authenticated — API error', () => {
    test('error state is shown when the API returns an error', async ({ page }) => {
      await setClerkSignedIn(page);
      // Intercept the BFF match-history endpoint and return a 500 before load.
      await page.route('**/api/v1/history/matches**', (route) => {
        void route.fulfill({
          status: 500,
          contentType: 'application/json',
          body: JSON.stringify({ error: 'internal server error' }),
        });
      });

      await page.goto('/match-history');

      // After the failed request the error state div must be visible.
      await expect(page.locator('.error-state')).toBeVisible();

      // The match table and empty state must NOT appear alongside the error.
      await expect(page.locator('[data-testid="match-history-table"]')).not.toBeVisible();
      await expect(page.locator('[data-testid="match-history-empty"]')).not.toBeVisible();
    });
  });
});

// ---------------------------------------------------------------------------
// Match history — detail drill-down (row click → MatchDetailsModal)
//
// These tests mock the detail endpoints so they do not require a live,
// authenticated BFF session. Real-data verification (clicking a real match
// and seeing real turn-by-turn data) is gated on AWS / a live Clerk session
// and is covered by Tim's staging-verify pass after this PR merges.
// ---------------------------------------------------------------------------

test.describe('History: /match-history — detail drill-down', () => {
  const DETAIL_MATCH_ID = 'match-detail-1';

  const SINGLE_MATCH: MatchItem[] = [
    {
      id: DETAIL_MATCH_ID,
      format: 'Ladder',
      result: 'win',
      timestamp: '2026-06-01T10:00:00Z',
      player_wins: 2,
      opponent_wins: 1,
      duration_seconds: null,
      deck_id: null,
      rank_before: null,
      rank_after: null,
      opponent_rank: null,
    },
  ];

  /**
   * Mock GET /api/v1/matches/{id} — returns a full models.Match (PascalCase).
   * This is the endpoint BffMatchHistory calls on row click.
   */
  async function mockMatchDetail(page: Page, matchId: string): Promise<void> {
    await page.route(`**/api/v1/matches/${matchId}`, (route) => {
      void route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          ID: matchId,
          AccountID: 1,
          EventID: 'Ladder',
          EventName: 'Standard Ranked',
          Timestamp: '2026-06-01T10:00:00Z',
          PlayerWins: 2,
          OpponentWins: 1,
          PlayerTeamID: 1,
          Format: 'Ladder',
          Result: 'win',
          CreatedAt: '2026-06-01T10:00:00Z',
        }),
      });
    });
  }

  /** Mock the games endpoint to return empty (detail modal loads cleanly).
   * The BFF now returns {data:{games:[],capturedResults:false}} (#1342). */
  async function mockMatchGames(page: Page, matchId: string): Promise<void> {
    await page.route(`**/api/v1/matches/${matchId}/games`, (route) => {
      void route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ data: { games: [], capturedResults: false } }),
      });
    });
  }

  /** Mock the timeline endpoint. */
  async function mockMatchTimeline(page: Page, matchId: string): Promise<void> {
    await page.route(`**/api/v1/matches/${matchId}/plays/timeline`, (route) => {
      void route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify([]),
      });
    });
  }

  /** Mock the opponent-cards endpoint. */
  async function mockOpponentCards(page: Page, matchId: string): Promise<void> {
    await page.route(`**/api/v1/matches/${matchId}/opponent-cards`, (route) => {
      void route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ data: [] }),
      });
    });
  }

  test.beforeEach(async ({ page }) => {
    await setClerkSignedIn(page);
    await mockMatchHistory(page, SINGLE_MATCH);
    await mockMatchDetail(page, DETAIL_MATCH_ID);
    await mockMatchGames(page, DETAIL_MATCH_ID);
    await mockMatchTimeline(page, DETAIL_MATCH_ID);
    await mockOpponentCards(page, DETAIL_MATCH_ID);
  });

  test('clicking a match row opens the detail modal', async ({ page }) => {
    await page.goto('/match-history');

    // Wait for the table row to render.
    await expect(page.locator('[data-testid="match-row"]').first()).toBeVisible();

    // The detail modal must not be open yet.
    await expect(page.locator('.modal-backdrop')).not.toBeVisible();

    // Click the row.
    await page.locator('[data-testid="match-row"]').first().click();

    // The modal must open.
    await expect(page.locator('.modal-backdrop')).toBeVisible({ timeout: 5_000 });
    await expect(page.locator('.match-details-modal')).toBeVisible({ timeout: 5_000 });
  });

  test('detail modal contains the Game Timeline panel header', async ({ page }) => {
    await page.goto('/match-history');

    await expect(page.locator('[data-testid="match-row"]').first()).toBeVisible();
    await page.locator('[data-testid="match-row"]').first().click();

    await expect(page.locator('.modal-backdrop')).toBeVisible({ timeout: 5_000 });

    // MatchDetailsModal always renders the GamePlayTimelinePanel toggle button.
    await expect(page.locator('button', { hasText: /Game Timeline/i })).toBeVisible({ timeout: 5_000 });
  });

  test('detail modal can be closed', async ({ page }) => {
    await page.goto('/match-history');

    await expect(page.locator('[data-testid="match-row"]').first()).toBeVisible();
    await page.locator('[data-testid="match-row"]').first().click();

    await expect(page.locator('.modal-backdrop')).toBeVisible({ timeout: 5_000 });

    // Close via the × button.
    await page.locator('.modal-close').click();

    await expect(page.locator('.modal-backdrop')).not.toBeVisible();
  });

  test('"Ranked" is displayed for format "Ladder" (format normalization)', async ({ page }) => {
    await page.goto('/match-history');
    await expect(page.locator('[data-testid="match-history-table"]')).toBeVisible();

    // SINGLE_MATCH has format: 'Ladder' → should display 'Ranked', not 'Ladder'
    await expect(page.locator('[data-testid="match-row"]').first().locator('td').nth(1)).toHaveText('Ranked');
  });
});

// ---------------------------------------------------------------------------
// Draft history — /history/drafts (BffDraftHistory)
// ---------------------------------------------------------------------------

test.describe('History: /history/drafts', () => {
  test.describe('Unauthenticated', () => {
    test('unauthenticated access does not show draft history content @smoke', async ({ page }) => {
      await setClerkSignedOut(page);
      await page.goto('/history/drafts');

      await page.waitForLoadState('networkidle', { timeout: 10_000 }).catch(() => {/* ignore timeout */});

      // Draft history content must NOT be rendered for an unauthenticated user.
      const table = page.locator('[data-testid="draft-history-table"]');
      const empty = page.locator('[data-testid="draft-history-empty"]');

      await expect(table).not.toBeVisible();
      await expect(empty).not.toBeVisible();

      await expect(page.locator('[data-testid="protected-route-prompt"]')).toBeVisible();
    });
  });

  test.describe('Authenticated — with data', () => {
    test.beforeEach(async ({ page }) => {
      await setClerkSignedIn(page);
      await mockDraftHistory(page, DRAFT_ROWS);
    });

    test('page loads without error and shows the draft table @smoke', async ({ page }) => {
      await page.goto('/history/drafts');

      await expect(page.locator('[data-testid="draft-history-table"]')).toBeVisible();
    });

    test('page title is "Draft History"', async ({ page }) => {
      await page.goto('/history/drafts');

      await expect(page.locator('[data-testid="draft-history-table"]')).toBeVisible();
      await expect(page.locator('h1.page-title')).toHaveText('Draft History');
    });

    test('no error state is shown on initial load', async ({ page }) => {
      await page.goto('/history/drafts');

      await expect(page.locator('[data-testid="draft-history-table"]')).toBeVisible();
      await expect(page.locator('.error-state')).not.toBeVisible();
    });

    test('table renders the expected column headers', async ({ page }) => {
      await page.goto('/history/drafts');

      const table = page.locator('[data-testid="draft-history-table"]');
      await expect(table).toBeVisible();

      // BffDraftHistory renders four columns: Date, Set, Wins, Losses.
      await expect(table.locator('thead th').nth(0)).toHaveText('Date');
      await expect(table.locator('thead th').nth(1)).toHaveText('Set');
      await expect(table.locator('thead th').nth(2)).toHaveText('Wins');
      await expect(table.locator('thead th').nth(3)).toHaveText('Losses');
    });

    test('pagination controls render when total exceeds the page size', async ({ page }) => {
      await mockDraftHistory(page, DRAFT_ROWS, 41);
      await page.goto('/history/drafts');

      await expect(page.locator('[data-testid="draft-history-table"]')).toBeVisible();

      const prevBtn = page.locator('.pagination-btn', { hasText: 'Previous' });
      const nextBtn = page.locator('.pagination-btn', { hasText: 'Next' });
      const pageInfo = page.locator('.pagination-info');

      await expect(prevBtn).toBeVisible({ timeout: 5_000 });
      await expect(nextBtn).toBeVisible({ timeout: 5_000 });
      await expect(pageInfo).toContainText('Page');
    });
  });

  test.describe('Authenticated — empty', () => {
    test('empty state renders when the BFF returns no drafts', async ({ page }) => {
      await setClerkSignedIn(page);
      await mockDraftHistory(page, [], 0);

      await page.goto('/history/drafts');

      await expect(page.locator('[data-testid="draft-history-empty"]')).toBeVisible();
      await expect(page.locator('[data-testid="draft-history-table"]')).not.toBeVisible();
    });
  });

  test.describe('Authenticated — API error', () => {
    test('error state is shown when the API returns an error', async ({ page }) => {
      await setClerkSignedIn(page);
      await page.route('**/api/v1/history/drafts**', (route) => {
        void route.fulfill({
          status: 500,
          contentType: 'application/json',
          body: JSON.stringify({ error: 'internal server error' }),
        });
      });

      await page.goto('/history/drafts');

      await expect(page.locator('.error-state')).toBeVisible();

      await expect(page.locator('[data-testid="draft-history-table"]')).not.toBeVisible();
      await expect(page.locator('[data-testid="draft-history-empty"]')).not.toBeVisible();
    });
  });
});
