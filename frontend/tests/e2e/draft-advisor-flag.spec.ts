/**
 * Draft advisor feature-flag gate E2E tests — vmt-t#628
 *
 * Tests that `live_draft_advisor_enabled` correctly gates:
 *   1. Draft.tsx / CurrentPackPicker (polling-based advisor surface)
 *   2. DraftLive.tsx — top-pick highlighting + feature_draft_advisor_pick_viewed
 *      telemetry (SSE-based; stream stays alive regardless of flag state)
 *
 * Flag injection: window.__POSTHOG_TEST_FLAGS__ injected via page.addInitScript()
 * (the same mechanism the useFeatureFlag hook checks first before PostHog).
 *
 * Auth: window.__CLERK_TEST_STATE__ injected so ProtectedRoute passes through
 * (requires VITE_CLERK_TEST_MODE=true, set in playwright.config.ts webServer).
 *
 * BFF endpoints intercepted via Playwright route mocking — no live BFF required.
 */

import { test, expect, type Page } from '@playwright/test';

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function sseData(payload: object): string {
  return `data: ${JSON.stringify(payload)}\n\n`;
}

/**
 * Intercept the SSE endpoint and serve the given event bodies one per
 * connection (in order). After the list is exhausted, reconnections are
 * aborted so the EventSource backs off.
 */
async function mockSse(page: Page, bodies: string[]): Promise<void> {
  let index = 0;
  await page.route('**/api/v1/events*', async (route) => {
    if (index >= bodies.length) {
      await route.abort();
      return;
    }
    const body = bodies[index];
    index += 1;
    await route.fulfill({
      status: 200,
      headers: {
        'Content-Type': 'text/event-stream',
        'Cache-Control': 'no-cache',
      },
      body,
    });
  });
}

function injectSignedIn(page: Page) {
  return page.addInitScript(() => {
    (window as unknown as Record<string, unknown>).__CLERK_TEST_STATE__ = { isSignedIn: true };
  });
}

function injectFlag(page: Page, flagValue: boolean | undefined) {
  return page.addInitScript((val: boolean | undefined) => {
    if (val !== undefined) {
      (window as unknown as Record<string, unknown>).__POSTHOG_TEST_FLAGS__ = {
        live_draft_advisor_enabled: val,
      };
    }
    // undefined → no override → hook defaults to optimistic-show (true)
  }, flagValue);
}

// ---------------------------------------------------------------------------
// Draft.tsx / CurrentPackPicker surface
// ---------------------------------------------------------------------------

test.describe('Draft.tsx — live_draft_advisor_enabled flag gate', () => {
  async function setupDraftPage(page: Page, flagValue: boolean | undefined) {
    await injectSignedIn(page);
    await injectFlag(page, flagValue);

    // POST /api/v1/drafts — getActiveDraftSessions() calls getDraftSessions({ status: 'active' })
    // which POSTs to /drafts. Mirror the working pattern from mockBffForActiveDraft in
    // current-pack-picker.spec.ts. The apiClient unwraps response.json().data, so all
    // BFF responses must be wrapped in { data: ... }.
    await page.route('**/api/v1/drafts', async (route) => {
      if (route.request().method() === 'POST') {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            data: [
              {
                ID: 'session-e2e-1',
                EventName: 'QuickDraft',
                SetCode: 'ONE',
                DraftType: 'PremierDraft',
                Status: 'active',
                TotalPicks: 45,
              },
            ],
          }),
        });
      } else {
        await route.continue();
      }
    });

    // Stub all other data endpoints Draft.tsx touches.
    // Wildcard session ID so the mock fires regardless of the ID returned above.
    await page.route('**/api/v1/drafts/*/picks', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ data: [] }),
      });
    });
    await page.route('**/api/v1/drafts/*/pool', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ data: [] }),
      });
    });
    await page.route('**/api/v1/cards/sets/*/cards', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ data: [] }),
      });
    });
    await page.route('**/api/v1/cards/ratings/**', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ data: [] }),
      });
    });
    // CurrentPackPicker polls this daemon endpoint — allow any session ID
    await page.route('**/api/v1/drafts/*/current-pack*', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ data: null }),
      });
    });
    // BFF color/card ratings — getDraftRatings issues a raw fetch (not through apiClient),
    // so the BFF returns the JSON envelope directly without a { data: ... } wrapper.
    await page.route('**/api/v1/draft-ratings/**', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          set_code: 'ONE',
          draft_format: 'PremierDraft',
          cached_at: '2026-01-01T00:00:00Z',
          card_ratings: [],
          color_ratings: [],
        }),
      });
    });
    // POST /api/v1/drafts/*/analyze-picks — auto-analysis fires if picks > 0 (won't here, but safe to stub)
    await page.route('**/api/v1/drafts/*/analyze-picks', async (route) => {
      await route.fulfill({ status: 204 });
    });

    await page.goto('/draft');
    // Wait until the active-draft view loads (shows "Draft Assistant" heading)
    await expect(page.getByText('Draft Assistant')).toBeVisible({ timeout: 10_000 });
  }

  test('flag ON — CurrentPackPicker (advisor surface) renders', async ({ page }) => {
    // Register the request spy BEFORE navigation so it captures the mount-time poll.
    const currentPackRequest = page.waitForRequest('**/current-pack*', { timeout: 15_000 });
    await setupDraftPage(page, true);

    // CurrentPackPicker mounts and polls the current-pack endpoint on load.
    const req = await currentPackRequest.catch(() => null);
    expect(req).not.toBeNull();
  });

  test('flag OFF — CurrentPackPicker (advisor surface) is hidden', async ({ page }) => {
    // Intercept the current-pack endpoint and fail the test if it is called.
    // Register before setupDraftPage so the monitor is in place before navigation.
    let currentPackCalled = false;
    await page.route('**/current-pack*', async (route) => {
      currentPackCalled = true;
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ data: null }),
      });
    });

    await setupDraftPage(page, false);

    // Give the page a moment to settle (CurrentPackPicker would have mounted by now if not gated)
    await page.waitForTimeout(500);

    expect(currentPackCalled).toBe(false);
  });

  test('flag undefined/loading — CurrentPackPicker renders (optimistic-show)', async ({ page }) => {
    // Register the request spy BEFORE navigation so it captures the mount-time poll.
    const currentPackRequest = page.waitForRequest('**/current-pack*', { timeout: 15_000 });
    // No flag injection = optimistic-show default
    await setupDraftPage(page, undefined);

    // CurrentPackPicker should mount (same as flag ON)
    const req = await currentPackRequest.catch(() => null);
    expect(req).not.toBeNull();
  });
});

// ---------------------------------------------------------------------------
// DraftLive.tsx surface — top-pick highlight and telemetry
// ---------------------------------------------------------------------------

test.describe('DraftLive.tsx — live_draft_advisor_enabled flag gate', () => {
  // Warm up Vite's module graph for DraftLive and bffDraftRatings so that the
  // first real test does not have to wait for on-demand transpilation. Without
  // this, the ratings fetch (which happens after setSetCode fires) may race
  // against Vite's initial module compile and arrive too late for the badge.
  test.beforeAll(async ({ browser }) => {
    const page = await browser.newPage();
    await page.route('**/api/v1/events*', (r) => r.fulfill({ status: 200, headers: { 'Content-Type': 'text/event-stream' }, body: '' }));
    await page.route('**/api/v1/draft-ratings/**', (r) => r.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ set_code: 'ONE', draft_format: 'PremierDraft', cached_at: '2026-01-01T00:00:00Z', card_ratings: [], color_ratings: [] }) }));
    await page.addInitScript(() => {
      (window as unknown as Record<string, unknown>).__CLERK_TEST_STATE__ = { isSignedIn: true };
    });
    await page.goto('/draft/live');
    await page.locator('[data-testid="draft-live-container"]').waitFor({ timeout: 30_000 });
    await page.close();
  });
  const startedEvent = {
    type: 'draft.started',
    account_id: 'acc1',
    event_id: 'e0',
    session_id: 's1',
    sequence: 0,
    occurred_at: '2026-05-08T00:00:00Z',
    payload: { set_code: 'ONE', draft_type: 'PremierDraft' },
  };
  const packEvent = {
    type: 'draft.pack',
    account_id: 'acc1',
    event_id: 'e1',
    session_id: 's1',
    sequence: 1,
    occurred_at: '2026-05-08T00:00:01Z',
    payload: { card_ids: [601, 602], pack_number: 0, pick_number: 0 },
  };

  async function setupDraftLivePage(page: Page, flagValue: boolean | undefined) {
    await injectSignedIn(page);
    await injectFlag(page, flagValue);

    await page.route('**/api/v1/draft-ratings/**', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          set_code: 'ONE',
          draft_format: 'PremierDraft',
          cached_at: '2026-01-01T00:00:00Z',
          card_ratings: [
            { arena_id: 601, name: 'Bomb Rare', gihwr: 69 },
            { arena_id: 602, name: 'Filler', gihwr: 47 },
          ],
          color_ratings: [],
        }),
      });
    });

    // SSE routing for DraftLive tests.
    //
    // Two SSE consumers connect to /api/v1/events:
    //   1. websocketClient (adapter.ts) — fetch-based, no ?token= param
    //   2. useDraftEventStream — EventSource, appends ?token=<clerk-jwt>
    //
    // DraftLive.tsx processes events only from useDraftEventStream (its own
    // EventSource); the websocketClient feeds a separate EventsOn bus.
    //
    // Strategy:
    //   - Token-bearing connections are from useDraftEventStream.
    //   - First token connection → draft.started (so setSetCode/setDraftFormat fire).
    //   - Subsequent token connections → draft.pack (so currentPackCards populates).
    //   - Tokenless connections (websocketClient) → draft.started (no-op for DraftLive).
    //
    // This guarantees React processes draft.started and draft.pack as separate
    // renders (React 18 batches simultaneous setLatestEvent calls, collapsing
    // both events into one if they arrive in the same connection response).
    let tokenConnections = 0;
    await page.route('**/api/v1/events*', async (route) => {
      const hasToken = route.request().url().includes('token=');
      if (hasToken) {
        tokenConnections += 1;
        if (tokenConnections === 1) {
          // First token connection: deliver draft.started so React can call
          // setSetCode/setDraftFormat in its own render cycle.
          await route.fulfill({
            status: 200,
            headers: { 'Content-Type': 'text/event-stream', 'Cache-Control': 'no-cache' },
            body: sseData(startedEvent),
          });
        } else {
          // Subsequent token connections: deliver draft.pack. Wait 300ms before
          // responding so that React has had time to process the draft.started
          // render cycle (setSetCode → useEffect → getDraftRatings) before the
          // pack event triggers a second render.
          await new Promise((r) => setTimeout(r, 300));
          await route.fulfill({
            status: 200,
            headers: { 'Content-Type': 'text/event-stream', 'Cache-Control': 'no-cache' },
            body: sseData(packEvent),
          });
        }
      } else {
        // websocketClient tokenless connection — deliver started so adapter is happy
        await route.fulfill({
          status: 200,
          headers: { 'Content-Type': 'text/event-stream', 'Cache-Control': 'no-cache' },
          body: sseData(startedEvent),
        });
      }
    });
    await page.goto('/draft/live');
    await expect(page.locator('[data-testid="draft-live-container"]')).toBeVisible();
    // Wait for pack cards to render
    await expect(page.locator('[data-testid="pack-card-601"]')).toBeVisible({ timeout: 10_000 });
  }

  test('flag ON — top-pick badge IS rendered on highest-GIHWR card', async ({ page }) => {
    await setupDraftLivePage(page, true);

    // Wait for ratings to load: the card name changes from '#601' (no rating) to
    // 'Bomb Rare' (from mock ratings) once getDraftRatings resolves.
    await expect(page.locator('[data-testid="pack-card-601"]')).not.toContainText('#601', { timeout: 10_000 });

    await expect(page.locator('[data-testid="top-pick-badge"]')).toBeVisible();
    await expect(page.locator('[data-testid="pack-card-601"]')).toHaveAttribute('data-top-pick', 'true');
  });

  test('flag OFF — top-pick badge is NOT rendered and stream stays alive', async ({ page }) => {
    await setupDraftLivePage(page, false);

    // Pack cards should still render (stream alive)
    await expect(page.locator('[data-testid="pack-card-601"]')).toBeVisible();
    await expect(page.locator('[data-testid="pack-card-602"]')).toBeVisible();

    // Top-pick badge must be absent
    await expect(page.locator('[data-testid="top-pick-badge"]')).not.toBeVisible();

    // No card should have data-top-pick
    const card601 = page.locator('[data-testid="pack-card-601"]');
    await expect(card601).not.toHaveAttribute('data-top-pick', 'true');
  });

  test('flag undefined/loading — top-pick IS shown (optimistic-show)', async ({ page }) => {
    await setupDraftLivePage(page, undefined);

    // Wait for ratings to load: the card name changes from '#601' (no rating) to
    // 'Bomb Rare' (from mock ratings) once getDraftRatings resolves.
    // This guards against asserting the badge before the ratings fetch completes.
    await expect(page.locator('[data-testid="pack-card-601"]')).not.toContainText('#601', { timeout: 10_000 });

    await expect(page.locator('[data-testid="top-pick-badge"]')).toBeVisible();
    await expect(page.locator('[data-testid="pack-card-601"]')).toHaveAttribute('data-top-pick', 'true');
  });
});
