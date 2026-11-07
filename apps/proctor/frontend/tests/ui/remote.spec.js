import { test, expect } from '@playwright/test';

// What the student application says it found on a student's computer (a Remote Desktop session, remote-control software) shows on
// the student's row and needs attention, and the history records each change.
async function connect(page) {
  await page.goto('about:blank');
  await page.goto('/#/monitoring');
  await expect(page.getByRole('table', { name: /All sessions/ })).toBeVisible();
}
const rows = page => page.locator('tbody tr[data-session-id]');
const attention = page => page.locator('[data-stat="attention"] .num');
const harness = (page, call, ...args) => page.evaluate(([call, args]) => window.ProctorHarness[call](...args), [call, args]);

test('a student with nothing found shows nothing', async ({ page }) => {
  await connect(page);
  await expect(page.getByText('Remote access:', { exact: false })).toHaveCount(0);
});

test('what was found shows on the row, in the application\'s words as text, and needs attention until it is gone', async ({ page }) => {
  await connect(page);
  const before = Number(await attention(page).innerText());
  await harness(page, 'remoteFound', 5, [{ kind: 'remote_session', name: 'Remote Desktop', detail: 'LAPTOP-1' }, { kind: 'remote_tool', name: '<img src=x onerror="window.injected=true">' }]);
  const row = rows(page).filter({ hasText: 'Student 005' });
  await expect(row).toContainText('Remote access: Remote Desktop from LAPTOP-1 · <img src=x');
  await expect(attention(page)).toHaveText(String(before + 1));
  expect(await page.evaluate(() => window.injected)).toBeUndefined();
  await harness(page, 'remoteFound', 5, []);
  await expect(row).not.toContainText('Remote access');
  await expect(attention(page)).toHaveText(String(before));
});

test('the history says what was found, as plain text', async ({ page }) => {
  await page.goto('/?remote=found#/review/22222222-2222-4222-8222-222222222222');
  const review = page.getByRole('region', { name: 'Session review' });
  const row = review.getByRole('table').getByRole('row').filter({ hasText: 'Remote access' });
  await expect(row).toContainText("Found on the student's computer: Remote Desktop session from LAPTOP-1, <b>AnyDesk.exe</b>.");
});

test('the history says when nothing is found any more', async ({ page }) => {
  await page.goto('/?remote=gone#/review/22222222-2222-4222-8222-222222222222');
  const review = page.getByRole('region', { name: 'Session review' });
  await expect(review.getByRole('table').getByRole('row').filter({ hasText: 'Remote access' })).toContainText('Nothing found any more');
});
