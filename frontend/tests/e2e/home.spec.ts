import { test, expect, Page } from '@playwright/test';

/**
 * Home Page — Command Strip E2E Tests (#689)
 *
 * Tests the /home route with the new returning-player command-center layout.
 *
 * Auth approach: VITE_CLERK_TEST_MODE=true aliases @clerk/react to clerkMock.tsx.
 * Auth state is injected via window.__CLERK_TEST_STATE__ before each navigation.
 *
 * API mocking: Playwright route intercept on BFF API paths.
 *   - /api/v1/history/summary → HomeSummaryResponse
 *   - /api/v1/drafts (POST, status=active) → DraftSession[]
 *   - /api/v1/decks (GET) → DeckListItem[]
 *
 * The /home route is inside ProtectedRoute; without signed-in state the mock
 * renders the sign-in prompt instead of Home content.
 */

// ---------------------------------------------------------------------------
// Auth helpers
// ---------------------------------------------------------------------------

type ClerkTestState = {
  isSignedIn: boolean;
  firstName?: string;
  lastName?: string;
};

async function setClerkSignedIn(page: Page, user?: Partial<ClerkTestState>): Promise<void> {
  const state: ClerkTestState = {
    isSignedIn: true,
    firstName: user?.firstName ?? 'Test',
    lastName: user?.lastName ?? 'User',
  };
  await page.addInitScript((s) => {
    (window as unknown as Record<string, unknown>).__CLERK_TEST_STATE__ = s;
  }, state);
}

async function setClerkSignedOut(page: Page): Promise<void> {
  await page.addInitScript((s) => {
    (window as unknown as Record<string, unknown>).__CLERK_TEST_STATE__ = s;
  }, { isSignedIn: false });
}

// ---------------------------------------------------------------------------
// API mock helpers
// ---------------------------------------------------------------------------

type SummaryOverride = {
  this_week?: { wins: number; losses: number; win_rate: number; matches: number };
  last_match?: {
    result: 'win' | 'loss';
    opponent_archetype: string | null;
    elapsed_seconds: number;
  } | null;
};

function makeSummaryResponse(overrides: SummaryOverride = {}) {
  return {
    today: { wins: 2, losses: 1, win_rate: 0.667 },
    this_week: overrides.this_week ?? { wins: 8, losses: 4, win_rate: 0.667, matches: 12 },
    all_time: {
      wins: 100,
      losses: 60,
      win_rate: 0.625,
      matches: 160,
      current_streak: 3,
      streak_type: 'W',
    },
    last_match: overrides.last_match !== undefined
      ? overrides.last_match
      : {
          result: 'win',
          opponent_archetype: 'Esper Midrange',
          elapsed_seconds: 1800,
        },
  };
}

async function mockHomeSummary(page: Page, overrides: SummaryOverride = {}): Promise<void> {
  await page.route('**/api/v1/history/summary', (route) => {
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify(makeSummaryResponse(overrides)),
    });
  });
}

async function mockNoActiveDraft(page: Page): Promise<void> {
  await page.route('**/api/v1/drafts', (route) => {
    if (route.request().method() === 'POST') {
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ data: [] }),
      });
    } else {
      route.continue();
    }
  });
}

async function mockActiveDraft(page: Page): Promise<void> {
  await page.route('**/api/v1/drafts', (route) => {
    if (route.request().method() === 'POST') {
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          data: [
            {
              ID: 'draft-1',
              EventName: 'BLB Booster Draft',
              SetCode: 'BLB',
              DraftType: 'PremierDraft',
              StartTime: new Date().toISOString(),
              Status: 'active',
              TotalPicks: 15,
              CreatedAt: new Date().toISOString(),
              UpdatedAt: new Date().toISOString(),
            },
          ],
        }),
      });
    } else {
      route.continue();
    }
  });
}

async function mockDecks(page: Page, overrides: { format?: string; name?: string } = {}): Promise<void> {
  await page.route('**/api/v1/decks', (route) => {
    if (route.request().method() === 'GET') {
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          data: [
            {
              id: 'deck-1',
              name: overrides.name ?? 'Azorius Tempo',
              format: overrides.format ?? 'Standard',
              source: 'constructed',
              cardCount: 60,
              matchesPlayed: 20,
              matchWinRate: 0.65,
              modifiedAt: new Date().toISOString(),
              lastPlayed: new Date().toISOString(),
              currentStreak: 2,
            },
          ],
        }),
      });
    } else {
      route.continue();
    }
  });
}

async function mockNoDecks(page: Page): Promise<void> {
  await page.route('**/api/v1/decks', (route) => {
    if (route.request().method() === 'GET') {
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ data: [] }),
      });
    } else {
      route.continue();
    }
  });
}

async function mockSummary404(page: Page): Promise<void> {
  await page.route('**/api/v1/history/summary', (route) => {
    route.fulfill({ status: 404, contentType: 'application/json', body: '{"error":"not found"}' });
  });
}

// ---------------------------------------------------------------------------
// Shared setup
// ---------------------------------------------------------------------------

async function setupSignedInHome(
  page: Page,
  opts: {
    summaryOverrides?: SummaryOverride;
    activeDraft?: boolean;
    deckFormat?: string;
    deckName?: string;
    noDecks?: boolean;
  } = {}
): Promise<void> {
  await setClerkSignedIn(page);
  await mockHomeSummary(page, opts.summaryOverrides);
  if (opts.activeDraft) {
    await mockActiveDraft(page);
  } else {
    await mockNoActiveDraft(page);
  }
  if (opts.noDecks) {
    await mockNoDecks(page);
  } else {
    await mockDecks(page, { format: opts.deckFormat, name: opts.deckName });
  }
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

test.describe('Feature: Home Command Strip (#689)', () => {

  // ── Route and auth ─────────────────────────────────────────────
  test.describe('Route and auth', () => {
    test('AC @smoke: authenticated users see home-page on /home', async ({ page }) => {
      await setupSignedInHome(page);
      await page.goto('/home');
      await expect(page.locator('[data-testid="app-container"]')).toBeVisible();
      await expect(page.locator('[data-testid="home-page"]')).toBeVisible();
    });

    test('unauthenticated visit to /home shows sign-in prompt', async ({ page }) => {
      await setClerkSignedOut(page);
      await page.goto('/home');
      await expect(page.locator('[data-testid="app-container"]')).toBeVisible();
      await expect(page.locator('[data-testid="protected-route-prompt"]')).toBeVisible();
      await expect(page.locator('[data-testid="home-page"]')).not.toBeVisible();
    });
  });

  // ── WEEKLY RECORD strip ────────────────────────────────────────
  test.describe('WEEKLY RECORD strip', () => {
    test('@smoke weekly record strip is visible', async ({ page }) => {
      await setupSignedInHome(page);
      await page.goto('/home');
      await expect(page.locator('[data-testid="app-container"]')).toBeVisible();
      await expect(page.locator('[data-testid="home-strip-weekly"]')).toBeVisible();
    });

    test('weekly wins are shown', async ({ page }) => {
      await setupSignedInHome(page, { summaryOverrides: { this_week: { wins: 8, losses: 4, win_rate: 0.667, matches: 12 } } });
      await page.goto('/home');
      await expect(page.locator('[data-testid="home-weekly-wins"]')).toContainText('8W');
    });

    test('win rate shown as percentage', async ({ page }) => {
      await setupSignedInHome(page);
      await page.goto('/home');
      await expect(page.locator('[data-testid="home-weekly-winrate"]')).toContainText('%');
    });

    test('today record shown when today has matches', async ({ page }) => {
      await setupSignedInHome(page);
      await page.goto('/home');
      // Summary fixture has today: {wins:2, losses:1}
      await expect(page.locator('[data-testid="home-today-record"]')).toBeVisible();
      await expect(page.locator('[data-testid="home-today-record"]')).toContainText('Today: 2–1');
    });
  });

  // ── LAST MATCH micro-strip ─────────────────────────────────────
  test.describe('LAST MATCH micro-strip', () => {
    test('@smoke last match strip visible when last_match present', async ({ page }) => {
      await setupSignedInHome(page);
      await page.goto('/home');
      await expect(page.locator('[data-testid="home-strip-last-match"]')).toBeVisible();
    });

    test('shows "Won" for win result', async ({ page }) => {
      await setupSignedInHome(page, {
        summaryOverrides: { last_match: { result: 'win', opponent_archetype: 'Esper Midrange', elapsed_seconds: 1800 } },
      });
      await page.goto('/home');
      await expect(page.locator('[data-testid="home-last-match-result"]')).toContainText('Won');
    });

    test('shows "Lost" for loss result', async ({ page }) => {
      await setupSignedInHome(page, {
        summaryOverrides: { last_match: { result: 'loss', opponent_archetype: null, elapsed_seconds: 600 } },
      });
      await page.goto('/home');
      await expect(page.locator('[data-testid="home-last-match-result"]')).toContainText('Lost');
    });

    test('shows opponent archetype when present', async ({ page }) => {
      await setupSignedInHome(page, {
        summaryOverrides: { last_match: { result: 'win', opponent_archetype: 'Esper Midrange', elapsed_seconds: 1800 } },
      });
      await page.goto('/home');
      await expect(page.locator('[data-testid="home-last-match-archetype"]')).toContainText('vs. Esper Midrange');
    });

    test('last match strip not visible when last_match is null', async ({ page }) => {
      await setupSignedInHome(page, { summaryOverrides: { last_match: null } });
      await page.goto('/home');
      await expect(page.locator('[data-testid="home-strip-last-match"]')).not.toBeVisible();
    });
  });

  // ── ACTIVE DRAFT strip ─────────────────────────────────────────
  test.describe('ACTIVE DRAFT strip', () => {
    test('@smoke active draft strip visible when draft present', async ({ page }) => {
      await setupSignedInHome(page, { activeDraft: true });
      await page.goto('/home');
      await expect(page.locator('[data-testid="home-strip-active-draft"]')).toBeVisible();
    });

    test('shows draft event name', async ({ page }) => {
      await setupSignedInHome(page, { activeDraft: true });
      await page.goto('/home');
      await expect(page.locator('[data-testid="home-active-draft-name"]')).toContainText('BLB Booster Draft');
    });

    test('active draft strip not visible when no active draft', async ({ page }) => {
      await setupSignedInHome(page, { activeDraft: false });
      await page.goto('/home');
      await expect(page.locator('[data-testid="home-strip-active-draft"]')).not.toBeVisible();
    });

    test('@smoke clicking active draft strip navigates to /draft', async ({ page }) => {
      await setupSignedInHome(page, { activeDraft: true });
      await page.goto('/home');
      await expect(page.locator('[data-testid="home-strip-active-draft"]')).toBeVisible();
      await page.locator('[data-testid="home-strip-active-draft"]').click();
      await page.waitForURL('**/draft');
      await expect(page).toHaveURL(/\/draft$/);
    });
  });

  // ── LAST DECK strip ─────────────────────────────────────────────
  test.describe('LAST DECK strip', () => {
    test('@smoke last deck strip visible when decks exist', async ({ page }) => {
      await setupSignedInHome(page);
      await page.goto('/home');
      await expect(page.locator('[data-testid="home-strip-last-deck"]')).toBeVisible();
    });

    test('shows deck name', async ({ page }) => {
      await setupSignedInHome(page, { deckName: 'Azorius Tempo' });
      await page.goto('/home');
      await expect(page.locator('[data-testid="home-last-deck-name"]')).toContainText('Azorius Tempo');
    });

    test('shows "Play Again" CTA for Constructed (Standard) format', async ({ page }) => {
      await setupSignedInHome(page, { deckFormat: 'Standard' });
      await page.goto('/home');
      await expect(page.locator('[data-testid="home-last-deck-cta"]')).toContainText('Play Again');
    });

    test('shows "Open Log" CTA for Draft format', async ({ page }) => {
      await setupSignedInHome(page, { deckFormat: 'PremierDraft' });
      await page.goto('/home');
      await expect(page.locator('[data-testid="home-last-deck-cta"]')).toContainText('Open Log');
    });

    test('shows "Open Log" CTA for Sealed format', async ({ page }) => {
      await setupSignedInHome(page, { deckFormat: 'Traditional Sealed' });
      await page.goto('/home');
      await expect(page.locator('[data-testid="home-last-deck-cta"]')).toContainText('Open Log');
    });

    test('last deck strip not visible when no decks', async ({ page }) => {
      await setupSignedInHome(page, { noDecks: true });
      await page.goto('/home');
      await expect(page.locator('[data-testid="home-strip-last-deck"]')).not.toBeVisible();
    });

    test('@smoke clicking last deck strip navigates to /decks', async ({ page }) => {
      await setupSignedInHome(page);
      await page.goto('/home');
      await expect(page.locator('[data-testid="home-strip-last-deck"]')).toBeVisible();
      await page.locator('[data-testid="home-strip-last-deck"]').click();
      await page.waitForURL('**/decks');
      await expect(page).toHaveURL(/\/decks$/);
    });
  });

  // ── QUICK NAV quadrant ─────────────────────────────────────────
  test.describe('QUICK NAV quadrant', () => {
    test('@smoke all 4 quick-nav tiles are visible', async ({ page }) => {
      await setupSignedInHome(page);
      await page.goto('/home');
      await expect(page.locator('[data-testid="home-quick-nav"]')).toBeVisible();
      await expect(page.locator('[data-testid="home-nav-match-history"]')).toBeVisible();
      await expect(page.locator('[data-testid="home-nav-draft"]')).toBeVisible();
      await expect(page.locator('[data-testid="home-nav-decks"]')).toBeVisible();
      await expect(page.locator('[data-testid="home-nav-collection"]')).toBeVisible();
    });

    test('@smoke Match History tile navigates to /match-history', async ({ page }) => {
      await setupSignedInHome(page);
      await page.goto('/home');
      await page.locator('[data-testid="home-nav-match-history"]').click();
      await page.waitForURL('**/match-history');
      await expect(page).toHaveURL(/\/match-history$/);
    });

    test('Draft tile navigates to /draft', async ({ page }) => {
      await setupSignedInHome(page);
      await page.goto('/home');
      await page.locator('[data-testid="home-nav-draft"]').click();
      await page.waitForURL('**/draft');
      await expect(page).toHaveURL(/\/draft$/);
    });

    test('Decks tile navigates to /decks', async ({ page }) => {
      await setupSignedInHome(page);
      await page.goto('/home');
      await page.locator('[data-testid="home-nav-decks"]').click();
      await page.waitForURL('**/decks');
      await expect(page).toHaveURL(/\/decks$/);
    });

    test('Collection tile navigates to /collection', async ({ page }) => {
      await setupSignedInHome(page);
      await page.goto('/home');
      await page.locator('[data-testid="home-nav-collection"]').click();
      await page.waitForURL('**/collection');
      await expect(page).toHaveURL(/\/collection$/);
    });
  });

  // ── Empty / first-run state ────────────────────────────────────
  test.describe('Empty state (first-run)', () => {
    test('@smoke shows home-page and QUICK NAV when no data (summary 404)', async ({ page }) => {
      await setClerkSignedIn(page);
      await mockSummary404(page);
      await mockNoActiveDraft(page);
      await mockNoDecks(page);
      await page.goto('/home');
      await expect(page.locator('[data-testid="home-page"]')).toBeVisible();
      await expect(page.locator('[data-testid="home-quick-nav"]')).toBeVisible();
    });
  });
});
