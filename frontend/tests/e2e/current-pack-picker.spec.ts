/**
 * CurrentPackPicker E2E Tests — vmt-t#405
 *
 * Asserts the draft recommendation surface rendered by CurrentPackPicker
 * inside the Draft page (/draft). The component calls the daemon's
 * REST endpoint GET /api/v1/drafts/{sessionId}/current-pack to retrieve
 * pick recommendations. No live daemon is required — the endpoint is
 * mocked via Playwright route interception.
 *
 * The Draft page is behind ProtectedRoute and calls several BFF endpoints
 * before rendering CurrentPackPicker. This file mocks all required BFF
 * routes so the page reaches the active-session state, then mocks the
 * daemon current-pack route to inject a realistic gui.CurrentPackResponse.
 *
 * Coverage:
 *   1. @smoke  Happy path — recommended banner, "Best Pick" indicator, and
 *              plain-English reasoning all render correctly.
 *   2.         Graceful N/A degrade — empty ratings → no crash, no banner,
 *              card list still shown.
 *   3.         is_recommended CSS class applied to the correct card only.
 *   4.         Reason strings contain no raw GIHWR percentage (Prof gate).
 *   5.         Empty pack response — no crash, empty-state shown.
 *   6. @smoke  pack_label and pool_size rendered from daemon response.
 *
 * Daemon dependency note: the full signup → live draft → first pick →
 * telemetry flow described in vmt-t#405 AC1 requires an authentic daemon
 * connection (the daemon watches the MTGA Player.log on the local machine).
 * The SSE-and-pick-event portion of that flow is covered by draft-live.spec.ts
 * which mocks useDraftEventStream. What remains daemon-dependent:
 *   - The current-pack REST response being populated from live log state.
 *   - The `feature_ml_suggestions_viewed` PostHog event (not yet fired by
 *     CurrentPackPicker — gap filed as part of this ticket; see AC note below).
 *
 * Telemetry gap: analytics.FEATURE_ML_SUGGESTIONS_VIEWED is defined in
 * analytics.ts but CurrentPackPicker does not call trackEvent(). The E2E
 * cannot assert an event that is never fired. Frank should wire the
 * trackEvent call in CurrentPackPicker.tsx (per vmt-t#405 AC4).
 *
 * data-testid gap: CurrentPackPicker.tsx has no data-testid attributes.
 * The selectors below use stable CSS class names (.recommended-banner,
 * .pack-card.recommended, .recommended-indicator, .pack-cards-grid,
 * .current-pack-empty, .rec-reason, .card-reasoning, .rec-card-name).
 * Frank should add data-testid attributes to the key elements to make
 * these selectors more robust (tracked under vmt-t#405).
 */

import { test, expect, type Page } from '@playwright/test';

// ---------------------------------------------------------------------------
// Shared mock fixture — realistic daemon current-pack response shape.
// Uses snake_case to match the daemon contract established in MH-ML1 (PR #2856).
// ---------------------------------------------------------------------------

const SESSION_ID = 'e2e-session-001';

const MOCK_RECOMMENDED_CARD = {
  arena_id: '100',
  name: 'Lightning Bolt',
  image_url: '',
  rarity: 'common',
  colors: ['R'],
  mana_cost: '{R}',
  cmc: 1,
  type_line: 'Instant',
  gihwr: 72.0,
  alsa: 1.8,
  tier: 'S',
  is_recommended: true,
  score: 1.0,
  // Plain-English reason — no raw GIHWR % per Prof PLAYER_VERDICT gate
  reasoning: 'Best pick in the pack based on win rate and color synergy',
};

const MOCK_ALTERNATIVE_CARD = {
  arena_id: '200',
  name: 'Counterspell',
  image_url: '',
  rarity: 'uncommon',
  colors: ['U'],
  mana_cost: '{U}{U}',
  cmc: 2,
  type_line: 'Instant',
  gihwr: 61.5,
  alsa: 2.3,
  tier: 'A',
  is_recommended: false,
  score: 0.72,
  reasoning: 'Strong two-drop with late-game flexibility',
};

function buildCurrentPackResponse(overrides: Record<string, unknown> = {}): unknown {
  return {
    session_id: SESSION_ID,
    pack_number: 0,
    pick_number: 0,
    pack_label: 'Pack 1, Pick 1',
    cards: [MOCK_RECOMMENDED_CARD, MOCK_ALTERNATIVE_CARD],
    recommended_card: MOCK_RECOMMENDED_CARD,
    pool_colors: [],
    pool_size: 0,
    ...overrides,
  };
}

// Wrap in the standard { data: ... } envelope that daemonClient unwraps.
function daemonEnvelope(body: unknown): string {
  return JSON.stringify({ data: body });
}

// BFF response envelope.
function bffEnvelope(body: unknown): string {
  return JSON.stringify({ data: body });
}

// ---------------------------------------------------------------------------
// Route helpers
// ---------------------------------------------------------------------------

/**
 * Mock all BFF routes the Draft page calls before rendering CurrentPackPicker.
 * Returns one active draft session so the page enters active-session state.
 */
async function mockBffForActiveDraft(page: Page): Promise<void> {
  // POST /api/v1/drafts — active sessions list (getActiveDraftSessions calls
  // getDraftSessions({ status: 'active' }) which POSTs to /drafts).
  await page.route('**/api/v1/drafts', async (route) => {
    if (route.request().method() === 'POST') {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: bffEnvelope([
          {
            ID: SESSION_ID,
            SetCode: 'ONE',
            DraftType: 'PremierDraft',
            Status: 'active',
            StartedAt: '2026-06-01T00:00:00Z',
          },
        ]),
      });
    } else {
      await route.continue();
    }
  });

  // GET /api/v1/drafts/*/picks
  await page.route('**/api/v1/drafts/*/picks', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: bffEnvelope([]),
    });
  });

  // GET /api/v1/drafts/*/pool (getDraftPacks via getDraftPool)
  await page.route('**/api/v1/drafts/*/pool', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: bffEnvelope([]),
    });
  });

  // GET /api/v1/cards/sets/*/cards
  await page.route('**/api/v1/cards/sets/*/cards', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: bffEnvelope([]),
    });
  });

  // GET /api/v1/cards/ratings/*/*
  await page.route('**/api/v1/cards/ratings/**', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: bffEnvelope([]),
    });
  });

  // GET /api/v1/draft-ratings/** (bffDraftRatings.getDraftRatings)
  await page.route('**/api/v1/draft-ratings/**', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        data: {
          set_code: 'ONE',
          draft_format: 'PremierDraft',
          cached_at: '2026-01-01T00:00:00Z',
          card_ratings: [],
          color_ratings: [],
        },
      }),
    });
  });

  // POST /api/v1/drafts/*/analyze-picks (auto-analysis, non-critical)
  await page.route('**/api/v1/drafts/*/analyze-picks', async (route) => {
    await route.fulfill({ status: 204 });
  });
}

/**
 * Mock the daemon current-pack endpoint.
 * The daemon client hits http://localhost:9001/api/v1 — the glob pattern
 * matches regardless of port so tests run without a live daemon.
 */
async function mockDaemonCurrentPack(
  page: Page,
  body: unknown
): Promise<void> {
  await page.route('**/api/v1/drafts/*/current-pack*', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: daemonEnvelope(body),
    });
  });
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

test.describe('CurrentPackPicker — recommendation surface', () => {
  test.beforeEach(async ({ page }) => {
    // Inject signed-in Clerk test state so ProtectedRoute passes through.
    await page.addInitScript(() => {
      (window as unknown as Record<string, unknown>).__CLERK_TEST_STATE__ = {
        isSignedIn: true,
      };
    });
  });

  // ── 1. Happy path: recommendation banner, Best Pick, reasoning ────────────

  test(
    'shows Recommended Pick banner and Best Pick indicator for top card @smoke',
    async ({ page }) => {
      await mockBffForActiveDraft(page);
      await mockDaemonCurrentPack(page, buildCurrentPackResponse());

      await page.goto('/draft');
      await expect(page.locator('[data-testid="app-container"]')).toBeVisible();

      // Wait for CurrentPackPicker to resolve its fetch and render the pack.
      // The component renders inside the draft page when showCurrentPack=true
      // and state.session is non-null.
      const recommendedBanner = page.locator('.recommended-banner');
      await expect(recommendedBanner).toBeVisible();

      // "Recommended Pick:" label must appear in the banner.
      await expect(page.getByText('Recommended Pick:')).toBeVisible();

      // The recommended card name must appear in the banner's rec-card-name span.
      await expect(page.locator('.rec-card-name')).toHaveText('Lightning Bolt');

      // The recommended card in the grid must show "Best Pick" indicator.
      await expect(page.locator('.recommended-indicator')).toBeVisible();
      await expect(page.locator('.recommended-indicator')).toHaveText('Best Pick');

      // The rec-reason span in the banner must contain the plain-English reason.
      const recReason = page.locator('.rec-reason');
      await expect(recReason).toBeVisible();
      const reasonText = await recReason.textContent();
      expect(reasonText).toBeTruthy();
      // Prof gate: reason must not contain a raw GIHWR percentage.
      expect(reasonText).not.toMatch(/%/);
    }
  );

  // ── 2. is_recommended CSS class applied to the correct card only ──────────

  test('applies .pack-card.recommended class only to the recommended card', async ({
    page,
  }) => {
    await mockBffForActiveDraft(page);
    await mockDaemonCurrentPack(page, buildCurrentPackResponse());

    await page.goto('/draft');
    await expect(page.locator('[data-testid="app-container"]')).toBeVisible();

    // Wait for the pack grid to appear.
    await expect(page.locator('.pack-cards-grid')).toBeVisible();

    // Exactly one card should carry the recommended class.
    const recommendedCards = page.locator('.pack-card.recommended');
    await expect(recommendedCards).toHaveCount(1);

    // Total pack cards rendered = 2 (one recommended + one alternative).
    const allCards = page.locator('.pack-card');
    await expect(allCards).toHaveCount(2);
  });

  // ── 3. Prof gate: no raw GIHWR percentage in card-reasoning text ──────────

  test('card reasoning contains no raw GIHWR percentage (Prof gate)', async ({
    page,
  }) => {
    await mockBffForActiveDraft(page);
    await mockDaemonCurrentPack(page, buildCurrentPackResponse());

    await page.goto('/draft');
    await expect(page.locator('[data-testid="app-container"]')).toBeVisible();
    await expect(page.locator('.pack-cards-grid')).toBeVisible();

    // Collect all .card-reasoning divs and assert none contain a bare %.
    const reasonDivs = page.locator('.card-reasoning');
    const reasonCount = await reasonDivs.count();
    // Both mock cards have reasoning, so at least one div must exist.
    expect(reasonCount).toBeGreaterThan(0);

    for (let i = 0; i < reasonCount; i++) {
      const text = await reasonDivs.nth(i).textContent();
      expect(text ?? '').not.toMatch(/%/);
    }
  });

  // ── 4. N/A degrade: no recommended_card — no crash, no banner, cards shown ─

  test('degrades gracefully when no recommended_card is present (N/A state)', async ({
    page,
  }) => {
    await mockBffForActiveDraft(page);

    // Daemon returns cards but no recommended_card (new set, no rating data).
    const degradedResponse = buildCurrentPackResponse({
      cards: [
        {
          ...MOCK_RECOMMENDED_CARD,
          is_recommended: false,
          reasoning: 'No rating data available for this set',
        },
        {
          ...MOCK_ALTERNATIVE_CARD,
          is_recommended: false,
          reasoning: 'No rating data available for this set',
        },
      ],
      recommended_card: null,
    });
    await mockDaemonCurrentPack(page, degradedResponse);

    await page.goto('/draft');
    await expect(page.locator('[data-testid="app-container"]')).toBeVisible();

    // Pack grid must render — no crash.
    await expect(page.locator('.pack-cards-grid')).toBeVisible();

    // Banner must NOT appear when recommended_card is absent.
    await expect(page.locator('.recommended-banner')).not.toBeVisible();

    // "Best Pick" must not appear — no recommended card.
    await expect(page.locator('.recommended-indicator')).not.toBeVisible();

    // Pack cards must be present in the grid — 2 cards with no recommended class.
    // Image elements may be off-viewport (lazy loading) so we assert the card
    // container count rather than image visibility.
    const allPackCards = page.locator('.pack-card');
    await expect(allPackCards).toHaveCount(2);
  });

  // ── 5. Empty pack list — no crash, empty-state shown ─────────────────────

  test('handles empty pack cards without crashing', async ({ page }) => {
    await mockBffForActiveDraft(page);

    // Daemon returns empty cards list (pack not yet received from Arena).
    const emptyResponse = buildCurrentPackResponse({
      cards: [],
      recommended_card: null,
    });
    await mockDaemonCurrentPack(page, emptyResponse);

    await page.goto('/draft');
    await expect(page.locator('[data-testid="app-container"]')).toBeVisible();

    // The component must show its empty state without crashing.
    const emptyEl = page.locator('.current-pack-empty');
    await expect(emptyEl).toBeVisible();
    await expect(
      page.getByText('Pack data will appear when you start a draft pick')
    ).toBeVisible();

    // No recommended banner should appear in the empty state.
    await expect(page.locator('.recommended-banner')).not.toBeVisible();
  });

  // ── 6. Pack label and pool size from daemon response ─────────────────────

  test('displays pack_label and pool_size from daemon response @smoke', async ({
    page,
  }) => {
    await mockBffForActiveDraft(page);
    await mockDaemonCurrentPack(
      page,
      buildCurrentPackResponse({
        pack_label: 'Pack 2, Pick 5',
        pool_size: 12,
      })
    );

    await page.goto('/draft');
    await expect(page.locator('[data-testid="app-container"]')).toBeVisible();

    // Pack label heading (rendered inside an <h2> inside current-pack-header).
    await expect(page.getByRole('heading', { name: 'Pack 2, Pick 5' })).toBeVisible();

    // Pool size info string.
    await expect(page.getByText('Pool: 12 cards')).toBeVisible();
  });
});
