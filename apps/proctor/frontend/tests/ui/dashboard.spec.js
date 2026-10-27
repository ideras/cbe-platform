import { test, expect } from '@playwright/test';
import { connect as connectTo } from './helpers.js';

const SESSION = '22222222-2222-4222-8222-222222222222';
const sessionRows = page => page.locator('table:has(caption:has-text("All sessions")) tbody tr');
// The live roster is in the monitoring window, which the main window opens once it is connected.
// Another window is another page load, so leave the current page first: changing only the # part would not reload it.
async function connect(page) { await page.goto('about:blank'); await page.goto('/#/monitoring'); await expect(page.getByRole('table', { name: /All sessions/ })).toBeVisible(); }

test('full classroom remains visible with independent warnings and readable fields', async ({ page }) => {
  const external = []; page.on('request', request => { if (!request.url().startsWith('http://127.0.0.1:4179') && !request.url().startsWith('data:')) external.push(request.url()); });
  await connect(page);
  await expect(page.getByRole('heading', { name: 'Live roster: Databases midterm (Mathematics 10A)' })).toBeVisible(); // the exam and the class it belongs to
  await expect(sessionRows(page)).toHaveCount(90);
  await expect(page.getByRole('list', { name: 'Students without a session' }).getByRole('listitem')).toHaveCount(30);
  const row = sessionRows(page).first();
  for (const value of ['Student 000', '0001', 'desk-0', '10.0.0.1', 'Network overdue', 'Sustained focus loss', 'Current focus: unknown', 'Heartbeat age: 35.0s', 'Sound: muted (35%)', 'Remaining: 89s']) await expect(row).toContainText(value);
  // The network warning is a sand pill, never a filled row (color budget); the focus pill stays white.
  expect(await row.evaluate(el => getComputedStyle(el).backgroundColor)).toBe('rgba(0, 0, 0, 0)');
  expect(await row.locator('.network').evaluate(el => getComputedStyle(el).backgroundColor)).toBe('rgb(241, 231, 210)');
  expect(await row.locator('.network').evaluate(el => getComputedStyle(el).backgroundColor)).not.toBe(await row.locator('.focus').evaluate(el => getComputedStyle(el).backgroundColor));
  await expect(page.getByRole('region', { name: 'Live roster' }).getByText('Student 089', { exact: true })).toBeAttached(); await expect(page.getByText('Missing 029', { exact: true })).toBeAttached();
  await page.setViewportSize({ width: 600, height: 700 }); await expect(sessionRows(page)).toHaveCount(90);
  expect(external).toEqual([]);
});

test('a finished session shows its heartbeat counters as of its end, not as live values', async ({ page }) => {
  await connect(page);
  await page.evaluate(() => window.ProctorHarness.finished(1));
  const row = sessionRows(page).nth(1);
  await expect(row).toContainText('Last heartbeat before the end: 1.0s earlier');
  await expect(row).toContainText('Missed intervals before the end: 0');
  await expect(row).not.toContainText('Heartbeat age:');
});

test('the roster shows the sound level each student computer reported', async ({ page }) => {
  await connect(page);
  await expect(sessionRows(page).nth(1)).toContainText('Sound: 70%');
  await expect(sessionRows(page).nth(2)).toContainText('Sound: not reported');
});

test('the dashboard theme applies to the roster, warning fields and confirmation dialog whatever the OS preference', async ({ page }) => {
  await page.emulateMedia({ colorScheme: 'dark' }); await connect(page);
  await expect(page.locator('body')).toHaveCSS('background-color', 'rgb(247, 246, 242)');
  await expect(sessionRows(page).first()).toHaveCSS('color', 'rgb(29, 43, 51)');
  await page.getByRole('button', { name: 'Force-submit 0001…', exact: true }).click();
  await expect(page.getByRole('dialog')).toHaveCSS('background-color', 'rgb(255, 255, 255)');
});

test('stale data remains visibly historical without invented countdown or focus', async ({ page }) => {
  await connect(page); await page.evaluate(() => window.ProctorHarness.stale(true));
  await expect(page.getByRole('alert')).toContainText('STALE / UNCONFIRMED'); await expect(sessionRows(page).first()).toContainText('Current focus: unknown');
  await page.waitForTimeout(1100); await expect(sessionRows(page).first()).toContainText('Remaining: 89s');
  await page.evaluate(() => window.ProctorHarness.stale(false)); await expect(page.getByText('STALE / UNCONFIRMED', { exact: false })).toHaveCount(0);
});

test('Monitor opens the monitoring window, and Review opens a window per student', async ({ page }) => {
  await connectTo(page, { openClass: 'Mathematics 10A' });
  await expect(page.getByRole('region', { name: 'Live roster' })).toHaveCount(0);
  await page.getByRole('button', { name: 'Monitor Databases midterm' }).click();
  await expect.poll(() => page.evaluate(() => window.ProctorHarness.calls.windows)).toEqual([{ name: 'monitoring', url: '/#/monitoring' }]);
  await expect(page.getByRole('button', { name: 'Monitor Draft quiz' })).toBeDisabled();
  await expect(page.getByRole('button', { name: 'Monitor Draft quiz' })).toHaveAttribute('title', 'Nobody has taken this exam yet; publish it first');
  await connect(page);
  await page.getByRole('button', { name: 'Review 0001', exact: true }).click();
  await expect.poll(() => page.evaluate(() => window.ProctorHarness.calls.windows)).toEqual([{ name: `review-${SESSION}`, url: `/#/review/${SESSION}` }]);
  await expect(page.getByRole('region', { name: 'Session review' })).toHaveCount(0);
});

test('the review window shows the history as a table, merges a focus loss with its return, and keeps student text inert', async ({ page }) => {
  await page.goto(`/#/review/${SESSION}`);
  const review = page.getByRole('region', { name: 'Session review' });
  await expect(review).toContainText('Student 000 (0001)');
  await expect(review).toContainText('Version #12 · sequence 3');
  const table = review.getByRole('table');
  const away = table.getByRole('row').filter({ hasText: 'Left the exam window' });
  await expect(away).toContainText('2 min 30 s'); await expect(away).toContainText('beyond threshold');
  await expect(away.getByRole('button', { name: /Open screenshot/ })).toHaveCount(3);
  await expect(table.getByRole('row').filter({ hasText: 'Back in the exam window' })).toHaveCount(0);
  await expect(table.getByRole('row').filter({ hasText: 'Network gap' })).toHaveCount(1);
  const answers = table.getByRole('row').filter({ hasText: 'Answers saved' });
  await expect(answers).toContainText('1 of 1 fields answered');
  await answers.getByRole('button', { name: 'Answers' }).click();
  await expect(review.getByText('<img src=x onerror="window.injected=true">student text', { exact: true })).toBeVisible();
  expect(await page.evaluate(() => window.injected)).toBeUndefined();
  await away.getByRole('button', { name: 'Details' }).click();
  await expect(review.getByText('<script>untrusted</script>', { exact: false })).toBeVisible();
  await review.getByRole('checkbox', { name: /Focus/ }).uncheck();
  await expect(table.getByRole('row').filter({ hasText: 'Left the exam window' })).toHaveCount(0);
  await review.getByRole('checkbox', { name: /Focus/ }).check();
  await away.getByRole('button', { name: 'Open screenshot #8' }).click();
  await expect.poll(() => page.evaluate(() => window.ProctorHarness.calls.windows)).toEqual([{ name: 'image-7', url: '/#/image?set=7%2C8%2C9&at=1' }]);
  await review.getByRole('button', { name: 'Next page' }).click();
  await expect(table).toContainText('DESIGNATED FINAL'); await expect(review).toContainText('end of history');
  await review.getByRole('button', { name: 'Previous page' }).click(); await expect(table).toContainText('Left the exam window');
});

test('a missing thumbnail is reported in the review window', async ({ page }) => {
  await page.addInitScript(() => { window.__fakeImagesMissing = true; });
  await page.goto(`/#/review/${SESSION}`);
  await expect(page.getByRole('alert').first()).toContainText('Image unavailable');
});

test('the image window zooms, steps through the focus loss and saves the original', async ({ page }) => {
  await page.goto('/#/image?set=7%2C8%2C9&at=1');
  await expect(page.getByText('Screenshot #8', { exact: true })).toBeVisible();
  await expect(page.getByText('2 of 3')).toBeVisible();
  await expect(page.getByRole('img', { name: 'Screenshot #8' })).toBeVisible();
  await page.getByRole('button', { name: 'Zoom in' }).click(); await expect(page.locator('.zoom')).not.toHaveText('Fit');
  await page.getByRole('button', { name: '100%' }).click(); await expect(page.locator('.zoom')).toHaveText('100%');
  await page.getByRole('button', { name: 'Zoom in' }).click(); await expect(page.locator('.zoom')).toHaveText('150%');
  await page.keyboard.press('0'); await expect(page.locator('.zoom')).toHaveText('Fit');
  await page.getByRole('button', { name: 'Next screenshot' }).click(); await expect(page.getByText('Screenshot #9', { exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Next screenshot' })).toBeDisabled();
  await page.keyboard.press('ArrowLeft'); await expect(page.getByText('Screenshot #8', { exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Save as…' }).click();
  await expect(page.getByRole('status')).toContainText('Saved to /home/proctor/screenshot-8.png');
  await expect(page.getByRole('status')).toContainText('not removed by purging');
  await page.evaluate(() => window.ProctorHarness.saveCancelled(true));
  await page.getByRole('button', { name: 'Save as…' }).click();
  await expect(page.getByRole('status')).toHaveCount(0);
  expect(await page.evaluate(() => window.ProctorHarness.calls.saved)).toEqual([8, 8]);
  // The full-size images are the ones the proctor steps through; the strip loads thumbnails of the whole series.
  const images = await page.evaluate(() => window.ProctorHarness.calls.image);
  expect(images.filter(call => !call.thumbnail)).toEqual([{ id: 8, thumbnail: false }, { id: 9, thumbnail: false }, { id: 8, thumbnail: false }]);
  expect(images.filter(call => call.thumbnail).map(call => call.id)).toEqual([7, 8, 9]);
});

test('force-submit cancellation sends nothing and duplicate clicks preserve a concurrent student final', async ({ page }) => {
  await connect(page); await page.getByRole('button', { name: 'Force-submit 0001…', exact: true }).click();
  await expect(page.getByRole('dialog')).toContainText('Student 000 (0001)'); await expect(page.getByRole('dialog')).toContainText('Irreversible');
  await page.getByRole('button', { name: 'Cancel', exact: true }).click(); expect(await page.evaluate(() => window.ProctorHarness.calls.force)).toEqual([]);
  await page.evaluate(() => { window.ProctorHarness.holdForce(); window.ProctorHarness.originalSubmit(); });
  await page.getByRole('button', { name: 'Force-submit 0001…', exact: true }).click(); await page.getByLabel('Operator reason').fill('Manual synthetic intervention');
  await page.getByRole('checkbox').check(); await page.getByRole('button', { name: 'Confirm irreversible action' }).click();
  await expect(page.getByRole('button', { name: 'Waiting for server…' })).toBeDisabled(); expect((await page.evaluate(() => window.ProctorHarness.calls.force)).length).toBe(1);
  await page.evaluate(() => window.ProctorHarness.releaseForce()); await expect(page.getByRole('status')).toContainText('Server confirmed submitted');
  await expect(page.getByRole('status')).not.toContainText('force_submitted');
});

test('authorization failure is visible and nothing is kept in browser storage', async ({ page }) => {
  await connect(page);
  await page.evaluate(() => window.ProctorHarness.failForce(true)); await page.getByRole('button', { name: 'Force-submit 0001…', exact: true }).click(); await page.getByLabel('Operator reason').fill('reason'); await page.getByRole('checkbox').check(); await page.getByRole('button', { name: 'Confirm irreversible action' }).click(); await expect(page.getByRole('dialog').getByRole('alert')).toContainText('authorization was rejected');
  expect(await page.evaluate(() => localStorage.length + sessionStorage.length)).toBe(0);
});


test('a locked student comes first, is highlighted and can be unlocked by the proctor', async ({ page }) => {
  await connect(page);
  await page.evaluate(() => window.ProctorHarness.lock(0, 'away'));
  const rows = sessionRows(page);
  // The locked student is the first row and says why.
  await expect(rows.first()).toContainText('Locked · away 5 min');
  await expect(rows.first()).toContainText('Student 000');
  // The window's title says how many students are locked.
  await expect.poll(() => page.title()).toContain('(1 locked) Monitoring');
  // The unlock dialog asks for a reason and does not give the time back on its own.
  await rows.first().getByRole('button', { name: 'Unlock 0001…' }).click();
  const dialog = page.getByRole('dialog', { name: 'Unlock exam' });
  await expect(dialog).toBeVisible();
  await expect(dialog.getByText('Locked because the student was away from the exam window,')).toBeVisible();
  await expect(dialog.getByRole('checkbox', { name: /Give back the/ })).not.toBeChecked();
  await expect(dialog.getByRole('button', { name: 'Unlock', exact: true })).toBeDisabled();
  await dialog.getByLabel('Reason').fill('the student came back');
  await expect(dialog.getByRole('button', { name: 'Unlock', exact: true })).toBeEnabled();
  await dialog.getByRole('button', { name: 'Unlock', exact: true }).click();
  await expect(dialog).toBeHidden();
  await expect(page.getByRole('status')).toContainText('Unlocked 0001');
  const sent = await page.evaluate(() => window.ProctorHarness.calls.unlock);
  expect(sent).toMatchObject({ reason: 'the student came back', giveBackTime: false, confirmed: true });
});

test('the admitting bar says how many students are locked', async ({ page }) => {
  await page.goto('/');
  await page.evaluate(() => window.ProctorHarness.startConnected());
  await page.evaluate(() => window.ProctorHarness.lock(0, 'no_contact'));
  await expect(page.getByRole('region', { name: 'Now admitting' })).toContainText('1 locked');
});

test('the header shows the rules the exam was published with', async ({ page }) => {
  await connect(page);
  await expect(page.getByText('Exam rules: alarm after 5 s · screenshot every 5 s · locks after 5 min away or 5 min without contact.')).toBeVisible();
});

test('the lock alert plays once for each new lock', async ({ page }) => {
  await page.addInitScript(() => {
    window.__plays = 0;
    HTMLMediaElement.prototype.play = function () { window.__plays++; return Promise.resolve(); };
  });
  await connect(page);
  expect(await page.evaluate(() => window.__plays)).toBe(0);
  await page.evaluate(() => window.ProctorHarness.lock(0, 'away'));
  await expect.poll(() => page.evaluate(() => window.__plays)).toBe(1);
  // The roster keeps refreshing; the same lock does not play again.
  await page.waitForTimeout(600);
  expect(await page.evaluate(() => window.__plays)).toBe(1);
  await page.evaluate(() => window.ProctorHarness.lock(1, 'no_contact'));
  await expect.poll(() => page.evaluate(() => window.__plays)).toBe(2);
});

test('the image strip shows the whole series, marks the current one and says when each was taken', async ({ page }) => {
  const times = ['2035-01-01T10:42:05Z', '2035-01-01T10:42:10Z', '2035-01-01T10:42:15Z'];
  await page.goto('/#/image?set=7%2C8%2C9&at=0&times=' + encodeURIComponent(times.join(',')));
  const strip = page.getByRole('group', { name: 'Screenshots of this focus loss' });
  await expect(strip.getByRole('button')).toHaveCount(3);
  await expect(strip.getByRole('button', { name: 'Screenshot #7' })).toHaveAttribute('aria-current', 'true');
  const taken = await page.evaluate(at => new Date(at).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' }), times[2]);
  await strip.getByRole('button', { name: 'Screenshot #9' }).click();
  await expect(page.getByText('Screenshot #9', { exact: true })).toBeVisible();
  await expect(page.getByText(`3 of 3 · ${taken}`)).toBeVisible();
  await expect(strip.getByRole('button', { name: 'Screenshot #9' })).toHaveAttribute('aria-current', 'true');
  await page.keyboard.press('ArrowLeft');
  await expect(page.getByText('Screenshot #8', { exact: true })).toBeVisible();
  await expect(strip.getByRole('button', { name: 'Screenshot #8' })).toHaveAttribute('aria-current', 'true');
});

test('the review window lists the calculator rows and filters them', async ({ page }) => {
  await page.goto(`/#/review/${SESSION}`);
  const review = page.getByRole('region', { name: 'Session review' });
  const table = review.getByRole('table');
  // A calc_evaluated row reads the expression with its result and angle mode; a failure reads its code.
  await expect(table.getByRole('row').filter({ hasText: 'sin(30) = 0.5 (deg)' })).toContainText('Calculator evaluated');
  await expect(table.getByRole('row').filter({ hasText: 'log(8) → ARITY' })).toBeVisible();
  await expect(table.getByRole('row').filter({ hasText: 'open for 30 s · scientific' })).toBeVisible();
  // Phase 6A: a tab change, an inspection with its direction, and the programmer settings with the signedness.
  await expect(table.getByRole('row').filter({ hasText: 'Keypad → IEEE 754' })).toContainText('Calculator tab changed');
  // The tab ids are named as the student sees the tabs: the logged id "fraction" is the Radix Point tab.
  await expect(table.getByRole('row').filter({ hasText: 'IEEE 754 → Radix Point' })).toContainText('Calculator tab changed');
  await expect(table.getByRole('row').filter({ hasText: 'radix point: 10.625 = 1010.101 (32 bits, dec, signed)' })).toBeVisible();
  await expect(table.getByRole('row').filter({ hasText: 'decode 0x3C00 → 0x3C00 (binary16; 32 bits, dec, signed)' })).toBeVisible();
  await expect(table.getByRole('row').filter({ hasText: '0xFF / 2 = 127 (8 bits, hex, unsigned)' })).toBeVisible();
  // The filter group counts them and hides them.
  const calculator = review.getByRole('checkbox', { name: /Calculator/ });
  await expect(calculator).toBeChecked();
  await calculator.uncheck();
  await expect(table.getByRole('row').filter({ hasText: 'sin(30)' })).toHaveCount(0);
  await expect(table.getByRole('row').filter({ hasText: 'Network gap' })).toBeVisible();
  await calculator.check();
  await expect(table.getByRole('row').filter({ hasText: 'sin(30)' })).toBeVisible();
});
