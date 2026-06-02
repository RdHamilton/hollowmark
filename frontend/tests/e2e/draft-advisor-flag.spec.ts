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

    // Stub active draft session with a session ID
    await page.route('**/api/v1/draft/sessions/active', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify([
          {
            ID: 'session-e2e-1',
            EventName: 'QuickDraft',
            SetCode: 'ONE',
            DraftType: 'PremierDraft',
            StartTime: '2026-06-01T00:00:00Z',
            Status: 'active',
            TotalPicks: 45,
            CreatedAt: '2026-06-01T00:00:00Z',
            UpdatedAt: '2026-06-01T00:00:00Z',
          },
        ]),
      });
    });

    // Stub all other data endpoints Draft.tsx touches
    await page.route('**/api/v1/draft/sessions/session-e2e-1/picks', async (route) => {
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([]) });
    });
    await page.route('**/api/v1/draft/sessions/session-e2e-1/pool', async (route) => {
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([]) });
    });
    await page.route('**/api/v1/cards/set/ONE', async (route) => {
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([]) });
    });
    await page.route('**/api/v1/cards/ratings/ONE/**', async (route) => {
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([]) });
    });
    await page.route('**/api/v1/cards/ratings/staleness/**', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ cachedAt: new Date().toISOString(), isStale: false, cardCount: 0 }),
      });
    });
    // CurrentPackPicker polls this endpoint
    await page.route('**/api/v1/draft/sessions/session-e2e-1/current-pack', async (route) => {
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(null) });
    });
    // BFF color ratings
    await page.route('**/api/v1/draft-ratings/**', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ card_ratings: [], color_ratings: [] }),
      });
    });

    await page.goto('/draft');
    // Wait until the active-draft view loads (shows "Draft Assistant" heading)
    await expect(page.getByText('Draft Assistant')).toBeVisible({ timeout: 10_000 });
  }

  test('flag ON — CurrentPackPicker (advisor surface) renders', async ({ page }) => {
    await setupDraftPage(page, true);

    // When flag is ON, the CurrentPackPicker component should mount.
    // It always hits the current-pack endpoint on mount — we detect it via network.
    const currentPackRequest = page.waitForRequest('**/current-pack**', { timeout: 5_000 }).catch(() => null);
    // Navigate to page (already done above) — if CurrentPackPicker mounted it should have fired
    const req = await currentPackRequest;
    expect(req).not.toBeNull();
  });

  test('flag OFF — CurrentPackPicker (advisor surface) is hidden', async ({ page }) => {
    // Intercept the current-pack endpoint and fail the test if it is called
    let currentPackCalled = false;
    await page.route('**/current-pack**', async (route) => {
      currentPackCalled = true;
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(null) });
    });

    await setupDraftPage(page, false);

    // Give the page a moment to settle (CurrentPackPicker would have mounted by now if not gated)
    await page.waitForTimeout(500);

    expect(currentPackCalled).toBe(false);
  });

  test('flag undefined/loading — CurrentPackPicker renders (optimistic-show)', async ({ page }) => {
    // No flag injection = optimistic-show default
    await setupDraftPage(page, undefined);

    // CurrentPackPicker should mount (same as flag ON)
    const currentPackRequest = page.waitForRequest('**/current-pack**', { timeout: 5_000 }).catch(() => null);
    const req = await currentPackRequest;
    expect(req).not.toBeNull();
  });

  test('low_confidence=true — "Limited data" pill renders on card (vmt-t#646)', async ({ page }) => {
    await injectSignedIn(page);
    await injectFlag(page, true);

    // Override active sessions stub
    await page.route('**/api/v1/draft/sessions/active', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify([
          {
            ID: 'session-lc-1',
            EventName: 'QuickDraft',
            SetCode: 'ONE',
            DraftType: 'PremierDraft',
            StartTime: '2026-06-01T00:00:00Z',
            Status: 'active',
            TotalPicks: 45,
            CreatedAt: '2026-06-01T00:00:00Z',
            UpdatedAt: '2026-06-01T00:00:00Z',
          },
        ]),
      });
    });

    // Other Draft.tsx endpoints
    await page.route('**/api/v1/draft/sessions/session-lc-1/picks', async (route) => {
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([]) });
    });
    await page.route('**/api/v1/draft/sessions/session-lc-1/pool', async (route) => {
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([]) });
    });
    await page.route('**/api/v1/cards/set/ONE', async (route) => {
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([]) });
    });
    await page.route('**/api/v1/cards/ratings/ONE/**', async (route) => {
      await route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([]) });
    });
    await page.route('**/api/v1/cards/ratings/staleness/**', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ cachedAt: new Date().toISOString(), isStale: false, cardCount: 0 }),
      });
    });
    await page.route('**/api/v1/draft-ratings/**', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ card_ratings: [], color_ratings: [] }),
      });
    });

    // Serve a pack that contains a low_confidence card
    await page.route('**/api/v1/draft/sessions/session-lc-1/current-pack', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          session_id: 'session-lc-1',
          pack_number: 0,
          pick_number: 0,
          pack_label: 'Pack 1, Pick 1',
          cards: [
            {
              arena_id: '999',
              name: 'Sparse Sample Card',
              image_url: '',
              rarity: 'common',
              colors: ['R'],
              mana_cost: '{R}',
              cmc: 1,
              type_line: 'Creature',
              gihwr: 52.1,
              alsa: 4.5,
              tier: 'C',
              is_recommended: false,
              score: 0.4,
              reasoning: 'Limited sample',
              low_confidence: true,
            },
          ],
          recommended_card: null,
          pool_colors: [],
          pool_size: 0,
        }),
      });
    });

    await page.goto('/draft');
    await expect(page.getByText('Draft Assistant')).toBeVisible({ timeout: 10_000 });

    // The "Limited data" pill must be visible for arena_id=999
    await expect(page.locator('[data-testid="low-confidence-999"]')).toBeVisible({ timeout: 8_000 });
    await expect(page.locator('[data-testid="low-confidence-999"]')).toHaveText('Limited data');
  });
});

// ---------------------------------------------------------------------------
// DraftLive.tsx surface — top-pick highlight and telemetry
// ---------------------------------------------------------------------------

test.describe('DraftLive.tsx — live_draft_advisor_enabled flag gate', () => {
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

    await mockSse(page, [sseData(startedEvent), sseData(packEvent)]);
    await page.goto('/draft/live');
    await expect(page.locator('[data-testid="draft-live-container"]')).toBeVisible();
    // Wait for pack cards to render
    await expect(page.locator('[data-testid="pack-card-601"]')).toBeVisible({ timeout: 10_000 });
  }

  test('flag ON — top-pick badge IS rendered on highest-GIHWR card', async ({ page }) => {
    await setupDraftLivePage(page, true);

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

    await expect(page.locator('[data-testid="top-pick-badge"]')).toBeVisible();
    await expect(page.locator('[data-testid="pack-card-601"]')).toHaveAttribute('data-top-pick', 'true');
  });
});
