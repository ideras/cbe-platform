import { test, expect } from '@playwright/test';

async function login(page) {
  await page.goto('/');
  await page.getByLabel('I have read these rules').check();
  await page.getByLabel('Account number').fill('00123');
  await page.getByLabel('Exam PIN').fill('PIN');
  await page.getByRole('button', { name: 'Login / resume' }).click();
}

test('toolbar ticks, freezes when stale and stays visible while the content scales', async ({ page }) => {
  await page.setViewportSize({ width: 1000, height: 700 });
  await login(page);
  const timer = page.getByLabel('Remaining time');
  await expect(timer).toContainText('00:01:00');
  await expect(timer).toContainText('00:00:59');
  await page.evaluate(() => window.__studentHarness.offline(true));
  await expect(timer).toContainText('Timing stale');
  const frozen = await timer.textContent();
  await page.waitForTimeout(1300);
  await expect(timer).toHaveText(frozen);
  for (let i = 0; i < 3; i++) await page.getByRole('button', { name: 'Larger text' }).click();
  await expect(page.getByRole('button', { name: 'Larger text' })).toBeDisabled();
  expect(await page.locator('.exam-content').evaluate(el => getComputedStyle(el).getPropertyValue('--exam-scale'))).toBe('1.5');
  await page.evaluate(() => window.scrollTo(0, document.body.scrollHeight));
  expect((await timer.boundingBox()).y).toBeGreaterThanOrEqual(0);
  expect((await timer.boundingBox()).y).toBeLessThan(180);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await page.setViewportSize({ width: 390, height: 700 });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
});

test('navigation checks follow the answers and submit names the remaining blanks', async ({ page }) => {
  await login(page);
  const pill = page.getByRole('button', { name: /^Exercise 2:/ });
  await pill.click();
  const check = pill.locator('svg');
  await expect(check).toHaveCount(0);
  await page.getByRole('textbox', { name: 'Answer for q2' }).fill('18');
  await page.getByRole('textbox', { name: 'Answer for q3' }).fill('answer');
  await expect(check).toHaveCount(1);
  await page.getByRole('textbox', { name: 'Answer for q2' }).fill('');
  await expect(check).toHaveCount(0);
  await page.getByRole('button', { name: 'Submit current answers — final' }).click();
  await expect(page.getByRole('dialog')).toContainText('Exercise 2 (1 of 2)');
  await expect(page.getByRole('dialog')).toContainText('Exercise 3 (0 of 1)');
});

test('dismissed grace toast does not return on repeated snapshots', async ({ page }) => {
  await login(page);
  await page.evaluate(() => window.__studentHarness.status({ status: 'grace', seconds_remaining: 0 }));
  const toast = page.locator('.toast').filter({ hasText: 'Time is up. You are in the grace period' });
  await expect(toast).toHaveCount(1);
  await toast.getByRole('button').click();
  await page.waitForTimeout(1200);
  await expect(toast).toHaveCount(0);
});
