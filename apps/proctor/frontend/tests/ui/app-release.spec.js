import { test, expect } from '@playwright/test';
import { connect as connectTo, connectionDialog, nav } from './helpers.js';

// Each student's application reports its release with every heartbeat, and the server reports its own: the roster says which
// students run another one, and the Server connection dialog says which release the server is.
async function monitoring(page, prepare) {
  await page.goto('about:blank');
  await page.goto('/#/monitoring');
  if (prepare) await page.evaluate(prepare);
  await expect(page.getByRole('table', { name: /All sessions/ })).toBeVisible();
}
const rows = page => page.locator('tbody tr[data-session-id]');
const harness = (page, call, ...args) => page.evaluate(([call, args]) => window.ProctorHarness[call](...args), [call, args]);
const callout = page => page.locator('.callout.release');

test('every row names its application release, and the server policy names the server release', async ({ page }) => {
  await monitoring(page);
  await expect(rows(page).first()).toContainText('· app v0.16.0-test');
  await expect(page.getByText('· release v0.16.0-test.', { exact: false })).toBeVisible();
  // The same release everywhere: nothing to warn about.
  await expect(callout(page)).toHaveCount(0);
  await expect(page.locator('.release').filter({ hasText: 'the server is' })).toHaveCount(0);
});

test('a student on another release, or on an application too old to say, is pointed out on the row and above the roster', async ({ page }) => {
  await monitoring(page);
  const first = await rows(page).nth(1).getAttribute('data-session-id');
  await harness(page, 'rowPatch', 1, { presence: { app_version: 'v0.15.0' } });
  const row = page.locator(`tbody tr[data-session-id="${first}"]`);
  await expect(row).toContainText('Application v0.15.0 · the server is v0.16.0-test');
  await expect(callout(page)).toContainText('1 student still working runs another release of the application than the server (v0.16.0-test)');
  // An application older than the release field says nothing; once it has checked in, that is an older application too.
  const second = await rows(page).nth(2).getAttribute('data-session-id');
  await harness(page, 'rowPatch', 2, { presence: { app_version: '', last_seen: '2035-01-01T00:00:00Z' } });
  await expect(page.locator(`tbody tr[data-session-id="${second}"]`)).toContainText('Application older than v0.16.0-test: it does not report its release');
  await expect(callout(page)).toContainText('2 students still working run another release');
  // Updated, the warnings go.
  await harness(page, 'rowPatch', 1, { presence: { app_version: 'v0.16.0-test' } });
  await harness(page, 'rowPatch', 2, { presence: { app_version: 'v0.16.0-test' } });
  await expect(callout(page)).toHaveCount(0);
  await expect(row).not.toContainText('the server is');
});

test('a server too old to report its release compares nothing', async ({ page }) => {
  await monitoring(page, () => window.ProctorHarness.serverVersion(''));
  await harness(page, 'rowPatch', 1, { presence: { app_version: '' , last_seen: '2035-01-01T00:00:00Z' } });
  await expect(page.locator('.callout.release')).toHaveCount(0);
  await expect(page.getByText('· release ', { exact: false })).toHaveCount(0);
});

test('the Server connection dialog says which release the server is, or that it does not say', async ({ page }) => {
  await connectTo(page);
  await nav(page).getByRole('button', { name: 'Server connection' }).click();
  const dialog = connectionDialog(page);
  await expect(dialog.getByRole('status')).toContainText('Server release v0.16.0-test');
  await dialog.getByRole('button', { name: 'Cancel', exact: true }).click();
  await page.evaluate(() => window.ProctorHarness.serverVersion(''));
  await nav(page).getByRole('button', { name: 'Server connection' }).click();
  await expect(dialog.getByRole('status')).toContainText('The server does not report its release (older than v0.16)');
});

test('a long list of students on another release names the first five and counts the rest', async ({ page }) => {
  await monitoring(page);
  for (let i = 1; i <= 7; i++) await harness(page, 'rowPatch', i, { presence: { app_version: 'v0.15.0' } });
  await expect(callout(page)).toContainText('7 students still working run another release');
  await expect(callout(page)).toContainText('and 2 more. Their rows say which.');
  await expect(rows(page).first()).toContainText('10.0.0.1 · app v0.16.0-test');
  await expect(page.getByText('beyond 30.0s · release v0.16.0-test.', { exact: false })).toBeVisible();
});
