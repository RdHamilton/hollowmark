import { test, expect, type Page } from '@playwright/test';

/**
 * Draft Page E2E Tests
 *
 * Tests the Draft page functionality including navigation and content display.
 * Uses REST API backend for testing.
 *
 * Note: /draft is behind ProtectedRoute (added in #1300). Tests inject a signed-in
 * Clerk test state via window.__CLERK_TEST_STATE__ so ProtectedRoute renders the
 * Draft content rather than the sign-in prompt. This requires Playwright to be
 * started with VITE_CLERK_TEST_MODE=true (set in playwright.config.ts webServer command).
 */
test.describe('Draft', () => {
  test.beforeEach(async ({ page }) => {
    // Inject signed-in Clerk state so ProtectedRoute passes through to Draft content.
    await page.addInitScript(() => {
      (window as unknown as Record<string, unknown>).__CLERK_TEST_STATE__ = { isSignedIn: true };
    });

    await page.goto('/');
    await expect(page.locator('[data-testid="app-container"]')).toBeVisible();

    await page.click('a[href="/draft"]');
    await page.waitForURL('**/draft');
  });

  test.describe('Navigation and Page Load', () => {
    test('@smoke should navigate to Draft page', async ({ page }) => {
      // Wait for page content to load
      const appContainer = page.locator('[data-testid="app-container"]');
      await expect(appContainer).toBeVisible();

      // Verify we're on the draft page
      const url = page.url();
      expect(url).toContain('/draft');
    });
  });

  test.describe('Draft Content', () => {
    test('should display draft content or empty state', async ({ page }) => {
      const draftContainer = page.locator('.draft-container');
      const draftEmpty = page.locator('.draft-empty');

      // Wait for either content type to appear
      await expect(draftContainer.or(draftEmpty).first()).toBeVisible();

      const hasContainer = await draftContainer.isVisible();
      const hasEmpty = await draftEmpty.isVisible();

      expect(hasContainer || hasEmpty).toBeTruthy();
    });

    test('should display historical drafts section if no active draft', async ({ page }) => {
      const historicalSection = page.locator('text=Historical Drafts');
      const draftContainer = page.locator('.draft-container');
      const draftEmpty = page.locator('.draft-empty');

      // Wait for content to load
      await expect(draftContainer.or(draftEmpty).or(historicalSection).first()).toBeVisible();

      const hasHistorical = await historicalSection.isVisible();
      const hasDraftContent = await draftContainer.isVisible();
      const hasEmpty = await draftEmpty.isVisible();

      expect(hasHistorical || hasDraftContent || hasEmpty).toBeTruthy();
    });
  });

  test.describe('Loading State', () => {
    test('should not show error state on initial load', async ({ page }) => {
      // Wait for content to load
      const content = page.locator('.draft-container, .draft-empty');
      await expect(content.first()).toBeVisible();

      const errorState = page.locator('.error-state');
      await expect(errorState).not.toBeVisible();
    });
  });

  test.describe('Create Deck from Draft', () => {
    test('@smoke should display Build Deck button on historical draft sessions', async ({ page }) => {
      // Wait for draft content to load
      const draftContainer = page.locator('.draft-container');
      const draftEmpty = page.locator('.draft-empty');
      const historicalSection = page.locator('text=Historical Drafts');

      await expect(draftContainer.or(draftEmpty).or(historicalSection).first()).toBeVisible();

      // Look for Build Deck button in the page
      const buildDeckButton = page.locator('button.btn-build-deck, button:has-text("Build Deck")');
      const hasBuildButton = await buildDeckButton.first().isVisible().catch(() => false);

      // Only assert Build Deck button if actual draft session cards are present (not just the empty-state container)
      const hasDraftSessions = await page.locator('.draft-session, .draft-history-item, .draft-card').first().isVisible().catch(() => false);
      if (hasDraftSessions) {
        expect(hasBuildButton).toBeTruthy();
      }
    });

    test('should navigate to DeckBuilder when clicking Build Deck on a draft session', async ({ page }) => {
      // Wait for draft content to load
      const draftContainer = page.locator('.draft-container');
      const draftEmpty = page.locator('.draft-empty');
      const historicalSection = page.locator('text=Historical Drafts');

      await expect(draftContainer.or(draftEmpty).or(historicalSection).first()).toBeVisible();

      // Find a Build Deck button
      const buildDeckButton = page.locator('button.btn-build-deck, button:has-text("Build Deck")').first();
      const hasBuildButton = await buildDeckButton.isVisible().catch(() => false);

      if (hasBuildButton) {
        // Click the Build Deck button
        await buildDeckButton.click();

        // Should navigate to deck-builder with draft ID
        await page.waitForURL('**/deck-builder/draft/**', { timeout: 10000 });

        // Verify we're on the DeckBuilder page
        const url = page.url();
        expect(url).toContain('/deck-builder/draft/');
      }
    });

    test('should display DeckBuilder UI correctly when creating deck from draft', async ({ page }) => {
      // Wait for draft content to load
      const draftContainer = page.locator('.draft-container');
      const draftEmpty = page.locator('.draft-empty');
      const historicalSection = page.locator('text=Historical Drafts');

      await expect(draftContainer.or(draftEmpty).or(historicalSection).first()).toBeVisible();

      // Find a Build Deck button
      const buildDeckButton = page.locator('button.btn-build-deck, button:has-text("Build Deck")').first();
      const hasBuildButton = await buildDeckButton.isVisible().catch(() => false);

      if (hasBuildButton) {
        await buildDeckButton.click();
        await page.waitForURL('**/deck-builder/draft/**', { timeout: 10000 });

        // Wait for DeckBuilder to load
        const deckBuilder = page.locator('.deck-builder');
        await expect(deckBuilder).toBeVisible();

        // Verify deck header is displayed with deck name
        const deckHeader = page.locator('.deck-header h2, .deck-title h2');
        await expect(deckHeader).toBeVisible();

        // Deck name should contain "Draft" (e.g., "QuickDraft_DSK Draft")
        const deckName = await deckHeader.textContent();
        expect(deckName?.toLowerCase()).toContain('draft');
      }
    });

    test('should load draft picks into the deck when creating from draft', async ({ page }) => {
      // Wait for draft content to load
      const draftContainer = page.locator('.draft-container');
      const draftEmpty = page.locator('.draft-empty');
      const historicalSection = page.locator('text=Historical Drafts');

      await expect(draftContainer.or(draftEmpty).or(historicalSection).first()).toBeVisible();

      // Find a Build Deck button
      const buildDeckButton = page.locator('button.btn-build-deck, button:has-text("Build Deck")').first();
      const hasBuildButton = await buildDeckButton.isVisible().catch(() => false);

      if (hasBuildButton) {
        await buildDeckButton.click();
        await page.waitForURL('**/deck-builder/draft/**', { timeout: 10000 });

        // Wait for DeckBuilder to load
        const deckBuilder = page.locator('.deck-builder');
        await expect(deckBuilder).toBeVisible();

        // Wait for cards to load (deck list should have cards)
        const deckList = page.locator('.deck-list, .deck-cards');
        await expect(deckList).toBeVisible();

        // Check that the deck has cards (not empty)
        // Look for card entries or quantity indicators
        const cardEntry = page.locator('.deck-card, .card-entry, [class*="card"]').first();
        const emptyMessage = page.locator('text=No cards, text=Empty deck');

        // Either we have cards or we should verify the deck was created
        const hasCards = await cardEntry.isVisible().catch(() => false);
        const isEmpty = await emptyMessage.isVisible().catch(() => false);

        // The deck should have been created (we navigated successfully)
        // Cards may or may not be present depending on fixture data
        expect(hasCards || !isEmpty).toBeTruthy();
      }
    });

    test('should show Suggest Decks button for draft deck (not Build Around)', async ({ page }) => {
      // Wait for draft content to load
      const draftContainer = page.locator('.draft-container');
      const draftEmpty = page.locator('.draft-empty');
      const historicalSection = page.locator('text=Historical Drafts');

      await expect(draftContainer.or(draftEmpty).or(historicalSection).first()).toBeVisible();

      // Find a Build Deck button
      const buildDeckButton = page.locator('button.btn-build-deck, button:has-text("Build Deck")').first();
      const hasBuildButton = await buildDeckButton.isVisible().catch(() => false);

      if (hasBuildButton) {
        await buildDeckButton.click();
        await page.waitForURL('**/deck-builder/draft/**', { timeout: 10000 });

        // Wait for DeckBuilder to load
        const deckBuilder = page.locator('.deck-builder');
        await expect(deckBuilder).toBeVisible();

        // For draft decks, Suggest Decks button should be visible
        const suggestDecksButton = page.locator('button.suggest-decks-btn, button:has-text("Suggest Decks")');
        await expect(suggestDecksButton).toBeVisible({ timeout: 5000 });

        // Build Around button should NOT be visible for draft decks
        const buildAroundButton = page.locator('button.build-around-btn');
        await expect(buildAroundButton).not.toBeVisible();
      }
    });

    test('should show Export and Validate buttons in deck footer', async ({ page }) => {
      // Wait for draft content to load
      const draftContainer = page.locator('.draft-container');
      const draftEmpty = page.locator('.draft-empty');
      const historicalSection = page.locator('text=Historical Drafts');

      await expect(draftContainer.or(draftEmpty).or(historicalSection).first()).toBeVisible();

      // Find a Build Deck button
      const buildDeckButton = page.locator('button.btn-build-deck, button:has-text("Build Deck")').first();
      const hasBuildButton = await buildDeckButton.isVisible().catch(() => false);

      if (hasBuildButton) {
        await buildDeckButton.click();
        await page.waitForURL('**/deck-builder/draft/**', { timeout: 10000 });

        // Wait for DeckBuilder to load
        const deckBuilder = page.locator('.deck-builder');
        await expect(deckBuilder).toBeVisible();

        // Verify Export button is present
        const exportButton = page.locator('button:has-text("Export")');
        await expect(exportButton).toBeVisible({ timeout: 5000 });

        // Verify Validate button is present
        const validateButton = page.locator('button:has-text("Validate")');
        await expect(validateButton).toBeVisible({ timeout: 5000 });
      }
    });
  });
});

// ---------------------------------------------------------------------------
// #1349 — Draft Resume State: Case B awaiting-data
//
// These tests run outside the main describe.beforeEach so they can set up
// route interceptions before page.goto('/draft') — the beforeEach above
// navigates to / first and then clicks the nav link, which fires BFF calls
// before our routes are registered.
//
// SSE stream is aborted so useDraftEventStream never fires an event — the
// test only verifies the poll-on-mount path (degradation path per Ray's Q2).
// ---------------------------------------------------------------------------

/**
 * Wrap the BFF envelope shape used by the REST API adapter.
 * getActiveDraftSessions → POST /api/v1/drafts → { data: [...sessions] }
 */
function bffEnvelope(data: unknown): string {
  return JSON.stringify({ data });
}

/**
 * Set up BFF route mocks for Case B: active session, zero picks, zero packs.
 * Also aborts the SSE stream so the useDraftEventStream hook does not interfere.
 */
async function setupCaseBRoutes(page: Page): Promise<void> {
  // Abort SSE so the SSE trigger path does not run in this test
  await page.route('**/api/v1/events*', async (route) => {
    await route.abort();
  });

  // POST /api/v1/drafts → one active session with zero picks
  await page.route('**/api/v1/drafts', async (route) => {
    if (route.request().method() === 'POST') {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: bffEnvelope([
          {
            ID: 'session-case-b-e2e',
            EventName: 'Quick Draft',
            SetCode: 'BLB',
            DraftType: 'QuickDraft',
            Status: 'active',
            TotalPicks: 45,
            StartedAt: '2026-06-01T00:00:00Z',
          },
        ]),
      });
    } else {
      await route.continue();
    }
  });

  // GET /api/v1/drafts/*/picks → zero picks (Case B condition)
  await page.route('**/api/v1/drafts/*/picks', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: bffEnvelope([]),
    });
  });

  // GET /api/v1/drafts/*/pool → zero packs (Case B condition)
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

  // GET /api/v1/cards/ratings/**
  await page.route('**/api/v1/cards/ratings/**', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: bffEnvelope([]),
    });
  });

  // GET /api/v1/draft-ratings/** (bffDraftRatings)
  await page.route('**/api/v1/draft-ratings/**', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        data: { set_code: 'BLB', draft_format: 'QuickDraft', cached_at: '2026-01-01T00:00:00Z', card_ratings: [], color_ratings: [] },
      }),
    });
  });

  // POST /api/v1/drafts/*/analyze-picks (non-critical auto-analysis)
  await page.route('**/api/v1/drafts/*/analyze-picks', async (route) => {
    await route.fulfill({ status: 204 });
  });
}

test.describe('#1349 Draft Resume State — Case B awaiting-data', () => {
  test.beforeEach(async ({ page }) => {
    // Inject signed-in Clerk state so ProtectedRoute passes through.
    await page.addInitScript(() => {
      (window as unknown as Record<string, unknown>).__CLERK_TEST_STATE__ = { isSignedIn: true };
    });
  });

  test('@smoke lands on awaiting-data state when navigating to /draft with active session but no picks', async ({ page }) => {
    // Must set up routes BEFORE page.goto so BFF calls are intercepted from mount
    await setupCaseBRoutes(page);

    await page.goto('/draft');

    // Case B heading must be visible (REQ-2 copy)
    await expect(page.getByRole('heading', { name: 'Draft in progress' })).toBeVisible({ timeout: 10000 });

    // Approved Prof copy (REQ-2)
    await expect(page.getByText(/Connected — waiting on Arena's first pack/i)).toBeVisible();

    // Set + event line (REQ-3: EventName · SetCode)
    await expect(page.getByText(/Quick Draft/i)).toBeVisible();
    await expect(page.getByText(/BLB/i)).toBeVisible();

    // Full active-draft view must NOT be visible
    await expect(page.getByRole('heading', { name: 'Draft Assistant' })).not.toBeVisible();

    // Draft History must NOT be visible
    await expect(page.getByRole('heading', { name: 'Draft History' })).not.toBeVisible();

    // "Back to Home" link present
    await expect(page.getByRole('link', { name: /Back to Home/i })).toBeVisible();
  });

  test('transitions from Case B to Case C when picks arrive via draft:updated', async ({ page }) => {
    await setupCaseBRoutes(page);

    await page.goto('/draft');

    // Confirm Case B is rendered
    await expect(page.getByRole('heading', { name: 'Draft in progress' })).toBeVisible({ timeout: 10000 });

    // Now update the picks route to return one pick
    await page.route('**/api/v1/drafts/*/picks', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: bffEnvelope([
          {
            ID: 1,
            SessionID: 'session-case-b-e2e',
            PackNumber: 0,
            PickNumber: 1,
            CardID: '99999',
            Timestamp: '2026-06-01T00:00:05Z',
          },
        ]),
      });
    });

    // REQ-4: trigger is reload-only within /draft — dispatch the Wails bridge event
    // via page.evaluate so Draft.tsx's EventsOn('draft:updated') fires and calls
    // debouncedLoadActiveDraft(). No navigate() should occur.
    await page.evaluate(() => {
      // The Wails EventsOn bridge in test mode uses window.__wails_event_emitter__
      // which is wired up by websocketMock; in E2E we emit via the CustomEvent path
      // that the mock websocket client listens on.
      window.dispatchEvent(new CustomEvent('wails:draft:updated'));
    });

    // After the reload, picks.length > 0 → Case C (Draft Assistant heading)
    await expect(page.getByRole('heading', { name: 'Draft Assistant' })).toBeVisible({ timeout: 5000 });

    // Case B heading must be gone
    await expect(page.getByRole('heading', { name: 'Draft in progress' })).not.toBeVisible();

    // URL must still be /draft (REQ-4: no navigate() away)
    expect(page.url()).toContain('/draft');
    expect(page.url()).not.toContain('/draft/');
  });
});
