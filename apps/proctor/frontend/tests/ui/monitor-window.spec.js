import { test, expect } from '@playwright/test';

const notConnected = page => page.getByText('Not connected to the server', { exact: false });
const noExam = page => page.getByText('No exam is being monitored', { exact: false });
const table = page => page.getByRole('table', { name: /All sessions/ });
const spinner = (page, text) => page.getByRole('status').filter({ hasText: text });
const DASHBOARD = 'Loading the dashboard…';
const ROSTER = 'Loading the roster from the server…';
const WARNINGS = ['Not connected to the server', 'No exam is being monitored', 'STALE / UNCONFIRMED'];

// Before the page loads: tells the fake native side how to treat the window's first questions, and writes down when each message
// first appeared, which looking afterwards cannot tell. Times are milliseconds since this script ran, i.e. since the page began.
const before = (page, fake) => page.addInitScript(fake => {
  window.__proctorFake = fake;
  window.__seen = {};
  const started = performance.now();
  new MutationObserver(() => {
    const text = document.body?.innerText ?? '';
    for (const message of ['Not connected to the server', 'No exam is being monitored', 'STALE / UNCONFIRMED', 'Loading the dashboard…', 'Loading the roster from the server…']) {
      if (text.includes(message) && !(message in window.__seen)) window.__seen[message] = Math.round(performance.now() - started);
    }
  }).observe(document, { subtree: true, childList: true, characterData: true });
}, fake);
const seen = page => page.evaluate(() => window.__seen);
const ringTurns = (page, text) => spinner(page, text).evaluate(element => getComputedStyle(element, '::before').animationName);

test('while the window waits for the first answer it shows a spinner and no warning', async ({ page }) => {
  await before(page, { holdSnapshot: true });
  await page.goto('/#/monitoring');
  await expect(page.locator('main.window')).toHaveAttribute('aria-busy', 'true');
  // The dashboard is loading, and the window says so from the first moment, with the theme's turning ring.
  await expect(spinner(page, DASHBOARD)).toBeVisible();
  expect(await ringTurns(page, DASHBOARD)).toBe('theme-spin');
  // Several polling turns go by with the native side silent: still loading, and never disconnected or without an exam.
  await page.waitForTimeout(1300);
  await expect(spinner(page, DASHBOARD)).toBeVisible();
  await expect(notConnected(page)).toHaveCount(0);
  await expect(noExam(page)).toHaveCount(0);
  await expect(table(page)).toHaveCount(0);
  await page.evaluate(() => window.ProctorHarness.releaseSnapshot());
  await expect(table(page)).toBeVisible();
  await expect(spinner(page, DASHBOARD)).toHaveCount(0);
  await expect(page.locator('main.window')).toHaveAttribute('aria-busy', 'false');
  for (const warning of WARNINGS) expect(await seen(page)).not.toHaveProperty([warning]); // at no moment, not just now
});

test('even a short wait shows the spinner, never a warning, and the spinner goes when the roster is there', async ({ page }) => {
  await before(page, { snapshotDelay: 150 }); // as a real call takes: far less than a second
  await page.goto('/#/monitoring');
  await expect(table(page)).toBeVisible();
  const log = await seen(page);
  expect(log).toHaveProperty([DASHBOARD]); // it was on screen while the window waited
  for (const warning of WARNINGS) expect(log).not.toHaveProperty([warning]);
  await expect(page.getByRole('status')).toHaveCount(0); // and is gone now
});

test('a roster that is still coming from the server shows a spinner and no stale warning', async ({ page }) => {
  await before(page, { rosterLoading: true });
  await page.goto('/#/monitoring');
  await expect(page.getByRole('heading', { name: /Live roster/ })).toBeVisible(); // the window knows its exam; the roster is not here yet
  await expect(spinner(page, ROSTER)).toBeVisible();
  expect(await ringTurns(page, ROSTER)).toBe('theme-spin');
  await expect(page.getByText('STALE / UNCONFIRMED', { exact: false })).toHaveCount(0); // nothing displayed can be out of date
  await expect(table(page)).toHaveCount(0);
  await page.evaluate(() => window.ProctorHarness.rosterLoading(false));
  await expect(table(page)).toBeVisible();
  await expect(spinner(page, ROSTER)).toHaveCount(0);
  const log = await seen(page);
  expect(log).toHaveProperty([ROSTER]);
  for (const warning of WARNINGS) expect(log).not.toHaveProperty([warning]);
});

test('a roster that fails to load is still reported while the window keeps trying', async ({ page }) => {
  await before(page, { rosterLoading: true });
  await page.goto('/#/monitoring');
  await page.evaluate(() => window.ProctorHarness.stale(true)); // the server cannot be reached: a real failure, with a reason
  await expect(page.getByRole('alert').filter({ hasText: 'STALE / UNCONFIRMED' })).toContainText('Server unreachable');
  await expect(spinner(page, ROSTER)).toBeVisible();
});

test('a window that is really not connected says so, once the native side has answered', async ({ page }) => {
  await page.goto('/#/monitoring');
  await expect(table(page)).toBeVisible();
  await page.evaluate(() => window.ProctorHarness.disconnected());
  await expect(notConnected(page)).toBeVisible();
  await expect(notConnected(page)).toContainText('Connect from the main window; this window follows it.');
  await expect(table(page)).toHaveCount(0);
  // It follows the main window: connected again, the roster returns and the warning goes.
  await page.evaluate(() => window.ProctorHarness.startConnected());
  await expect(table(page)).toBeVisible();
  await expect(notConnected(page)).toHaveCount(0);
});

test('a connected window with no exam to monitor says so', async ({ page }) => {
  await page.goto('/#/monitoring');
  await expect(table(page)).toBeVisible();
  await page.evaluate(() => window.ProctorHarness.nothingPublished());
  await expect(noExam(page)).toBeVisible();
  await expect(notConnected(page)).toHaveCount(0);
});

test('a window the native side cannot answer says so at once instead of spinning, and recovers', async ({ page }) => {
  await before(page, { snapshotFails: true });
  await page.goto('/#/monitoring');
  await expect(page.getByRole('alert')).toContainText('Native dashboard connection unavailable');
  await expect(spinner(page, DASHBOARD)).toHaveCount(0); // a failure is not a wait
  await expect(notConnected(page)).toHaveCount(0); // not knowing is not the same as being disconnected
  await expect(table(page)).toHaveCount(0);
  await page.evaluate(() => window.ProctorHarness.snapshotFails(false));
  await expect(table(page)).toBeVisible();
  await expect(page.getByRole('alert')).toHaveCount(0);
  const log = await seen(page);
  for (const warning of WARNINGS) expect(log).not.toHaveProperty([warning]);
});

test('the settings row gives the exam duration beside its grace, and extending the grace leaves the duration alone', async ({ page }) => {
  await page.goto('/#/monitoring');
  await expect(table(page)).toBeVisible();
  const chips = page.locator('.settings .chip');
  // Duration first, then what is added to it: the exam's 90 minutes and its 30 seconds of grace.
  await expect(chips).toHaveText(['Duration: 1 h 30 min', 'Grace: 30 s', 'No exam end time', /^Not logged in \d+$/]);
  await expect(chips.first().locator('svg')).toBeVisible();
  await page.getByRole('button', { name: 'Extend grace for everyone…', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Extend grace for everyone', exact: true });
  await dialog.getByLabel('Reason').fill('Class interruption');
  await dialog.getByRole('checkbox').check();
  await dialog.getByRole('button', { name: 'Extend grace by 5 min', exact: true }).click();
  await expect(dialog).toBeHidden();
  await expect(chips).toHaveText(['Duration: 1 h 30 min', 'Grace: 30 s + 5 min added', 'No exam end time', /^Not logged in \d+$/]);
});
