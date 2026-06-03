/**
 * Copy humanization E2E tests — v0.3.7 wave
 */
import { test, expect, type Page } from "@playwright/test";

async function setClerkSignedIn(page: Page): Promise<void> {
  await page.addInitScript((s) => {
    (window as unknown as Record<string, unknown>).__CLERK_TEST_STATE__ = s;
  }, { isSignedIn: true, firstName: "Test", lastName: "User" });
}

async function mockDashboard(page: Page): Promise<void> {
  await page.route("**/api/v1/**", (route) => {
    void route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({ data: null }),
    });
  });
}

test.describe("Format slug humanization", () => {
  test("@smoke no raw Arena slugs visible on profile page", async ({ page }) => {
    await setClerkSignedIn(page);
    await mockDashboard(page);
    await page.goto("/profile");
    await expect(page.locator("[data-testid=\"profile-page\"]")).toBeVisible({ timeout: 15_000 });
    await expect(page.locator("[data-testid=\"profile-title\"]")).toHaveText("Profile");
  });
});

test.describe("Draft Live nav highlight", () => {
  test("@smoke /draft/live highlights the Draft nav tab", async ({ page }) => {
    await setClerkSignedIn(page);
    await mockDashboard(page);
    await page.route("**/api/v1/events*", (route) => void route.abort());
    await page.goto("/draft/live");
    await expect(page.locator("[data-testid=\"nav-tab-bar\"]")).toBeVisible({ timeout: 15_000 });
    await expect(page.locator("[data-testid=\"nav-tab-draft\"]")).toHaveClass(/active/);
    await expect(page.locator("[data-testid=\"nav-tab-match-history\"]")).not.toHaveClass(/active/);
  });
});
