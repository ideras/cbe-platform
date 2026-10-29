import { test, expect } from '@playwright/test';

async function login(page) {
  await page.getByLabel('I have read these rules').check();
  await page.getByLabel('Account number').fill('00123');
  await page.getByLabel('Exam PIN').fill('synthetic PIN');
  await page.getByRole('button', { name: 'Login / resume' }).click();
  await expect(page.getByText('Fixture exam')).toBeVisible();
}

// The lock times the exam was published with, as the monitoring view carries them.
async function lockTimes(page) {
  await page.evaluate(() => window.__studentHarness.monitoring({ active: true, policy: { lock_away_after_ms: 300000, lock_no_contact_after_ms: 420000 } }));
}

test('the lock screen replaces the questions and says why', async ({ page }) => {
  await page.goto('/');
  await login(page);
  await lockTimes(page);
  await page.evaluate(() => window.__studentHarness.lock('away'));
  await expect(page.getByRole('heading', { name: 'Your exam is locked' })).toBeVisible();
  await expect(page.getByText('You were away from the exam window for 5 minutes.')).toBeVisible();
  await expect(page.getByText('Call your instructor. The exam time keeps running.')).toBeVisible();
  // No questions and no answer fields are shown.
  await expect(page.getByRole('textbox')).toHaveCount(0);
  await expect(page.getByText('Ejercicio 1', { exact: false })).toHaveCount(0);
});

test('the lock screen names the reason of a lost connection', async ({ page }) => {
  await page.goto('/');
  await login(page);
  await lockTimes(page);
  await page.evaluate(() => window.__studentHarness.lock('no_contact'));
  await expect(page.getByRole('heading', { name: 'Your exam is locked' })).toBeVisible();
  await expect(page.getByText('The app could not reach the server for 7 minutes.')).toBeVisible();
  await expect(page.getByText('You were away', { exact: false })).toHaveCount(0);
});

test('the exam comes back after the unlock, saying so and how much time was given back', async ({ page }) => {
  await page.goto('/');
  await login(page);
  await page.evaluate(() => window.__studentHarness.lock('away'));
  await expect(page.getByRole('heading', { name: 'Your exam is locked' })).toBeVisible();
  await page.evaluate(() => window.__studentHarness.unlock(420));
  await expect(page.getByRole('heading', { name: 'Your exam is locked' })).toHaveCount(0);
  await expect(page.getByRole('status').filter({ hasText: 'Unlocked by your instructor — 7 minutes were added.' })).toBeVisible();
  await expect(page.getByRole('textbox').first()).toBeVisible();
});

test('an unlock without time back says only that the exam was unlocked', async ({ page }) => {
  await page.goto('/');
  await login(page);
  await page.evaluate(() => window.__studentHarness.lock('no_contact'));
  await page.evaluate(() => window.__studentHarness.unlock(0));
  await expect(page.getByRole('status').filter({ hasText: 'Unlocked by your instructor' })).toBeVisible();
  await expect(page.getByText('were added', { exact: false })).toHaveCount(0);
});

test('the rules show the real times the server sent', async ({ page }) => {
  await page.goto('/');
  const rules = page.getByRole('region', { name: 'Participation rules' });
  await expect(rules.getByText('more than 5 seconds', { exact: false })).toBeVisible();
  await expect(rules.getByText('your exam locks', { exact: false })).toBeVisible();
  await expect(rules.getByText('for 5 minutes', { exact: false })).toBeVisible();
});

test('the rules fall back to plain words when the server does not answer', async ({ page }) => {
  await page.goto('/');
  const rules = page.getByRole('region', { name: 'Participation rules' });
  await expect(rules.getByText('for 5 minutes', { exact: false })).toBeVisible();
  await page.evaluate(() => window.__studentHarness.rulesUnavailable(true));
  await page.getByLabel('Server address').fill('http://192.0.2.10:8080');
  await expect(rules.getByText('a few seconds', { exact: false })).toBeVisible();
  await expect(rules.getByText('for several minutes', { exact: false })).toBeVisible();
});

test('the screenshot retest block never says the exam is locked', async ({ page }) => {
  await page.goto('/');
  await login(page);
  await page.evaluate(() => window.__studentHarness.capture({ state: 'failed' }));
  await expect(page.getByRole('alert').filter({ hasText: 'Screenshot retest required' })).toBeVisible();
  await expect(page.getByText('Editing is locked', { exact: false })).toHaveCount(0);
});
