import { chromium } from '@playwright/test';

async function globalSetup() {
  const browser = await chromium.launch();
  const page = await browser.newPage();
  await page.goto('http://localhost:3000');
  await page.waitForSelector('[data-testid="app-container"]', { timeout: 60000 });
  await browser.close();
}

export default globalSetup;
