import { test, expect } from '@playwright/test';

async function login(page) {
  await page.goto('/');
  await page.getByLabel('I have read these rules').check();
  await page.getByLabel('Account number').fill('00123');
  await page.getByLabel('Exam PIN').fill('PIN');
  await page.getByRole('button', { name: 'Login / resume' }).click();
}

test('ticking warnings fire once and added time re-arms a dismissed threshold', async ({ page }) => {
  await login(page);
  const report = seconds => page.evaluate(value => window.__studentHarness.status({ status: 'active', seconds_remaining: value }), seconds);
  const toast = page.locator('.toast').filter({ hasText: '10 minutes left before the grace period' });
  await report(601);
  await expect(page.getByLabel('Remaining time')).toContainText('00:10:01');
  await expect(toast).toHaveCount(1);
  await toast.getByRole('button').click();
  await page.waitForTimeout(1200);
  await expect(toast).toHaveCount(0);
  await report(660);
  await expect(page.getByLabel('Remaining time')).toContainText('00:11:00');
  await report(600);
  await expect(toast).toHaveCount(1);
});

test('sequenced grace and end-time notices are shown once, including clearing an end time', async ({ page }) => {
  await login(page);
  await page.evaluate(() => window.__studentHarness.timeNotices([{ sequence: 1, kind: 'grace', added_seconds: 300 }]));
  const grace = page.locator('.toast').filter({ hasText: 'Your instructor extended the grace period by 5 minutes.' });
  await expect(grace).toHaveCount(1);
  await grace.getByRole('button').click();
  await page.waitForTimeout(1200);
  await expect(grace).toHaveCount(0);
  await page.evaluate(() => window.__studentHarness.timeNotices([{ sequence: 2, kind: 'end_time', ends_at: '2035-01-02T17:30:00Z' }]));
  await expect(page.locator('.toast').filter({ hasText: 'The exam now ends at' })).toHaveCount(1);
  await page.evaluate(() => window.__studentHarness.timeNotices([{ sequence: 3, kind: 'end_time', ends_at: null }]));
  await expect(page.locator('.toast').filter({ hasText: 'The exam no longer has an end time.' })).toHaveCount(1);
});
