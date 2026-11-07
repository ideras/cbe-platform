import { test, expect } from '@playwright/test';

// The unlock dialog of the live roster. Student 000 (account 0001) is the one that is locked. In the fake server her heartbeats are
// late and her window has no known focus, so she is not ready; a test that needs her ready says so with rowPatch.
const SESSION = '22222222-2222-4222-8222-222222222222';
const sessionRows = page => page.locator('table:has(caption:has-text("All sessions")) tbody tr');
// The monitoring window is another page load, so leave the current page first: changing only the # part would not reload it.
async function connect(page) {
  await page.goto('about:blank'); await page.goto('/#/monitoring');
  await expect(page.getByRole('table', { name: /All sessions/ })).toBeVisible();
}
// Locks her for a reason, waits until the roster says so, and opens the unlock dialog from her row.
async function openUnlock(page, reason = 'network') {
  await page.evaluate(why => window.ProctorHarness.lock(0, why), reason);
  const row = sessionRows(page).first();
  await expect(row).toContainText({ away: 'Locked · away', no_contact: 'Locked · no contact', network: 'Locked · changing network' }[reason]);
  await row.getByRole('button', { name: 'Unlock 0001…' }).click();
  const dialog = page.getByRole('dialog', { name: 'Unlock exam' });
  await expect(dialog).toBeVisible();
  return dialog;
}
// Her lock began at the fake server's time zero. A test that wants time to have passed moves the server's clock, and the dialog's
// too (a fixed Date.now, so that nothing in it depends on how fast the test runs).
const T0 = new Date('2035-01-01T00:00:00Z');
const timed = async (page, { serverTime = '2035-01-01T00:01:00Z', deadline = '2035-01-01T00:20:00Z' } = {}) => {
  await page.clock.setFixedTime(T0);
  await connect(page);
  await page.evaluate(([at, ends]) => window.ProctorHarness.timing(0, { deadline_at: ends }, at), [serverTime, deadline]);
};
const unlockButton = dialog => dialog.getByRole('button', { name: /^(Unlock|Unlocking…)/ });
const reasonField = dialog => dialog.getByLabel('Reason');
const sent = page => page.evaluate(() => window.ProctorHarness.calls.unlock);
const clockOf = (page, iso) => page.evaluate(value => new Date(value).toLocaleTimeString(), iso);

test('the dialog names the exam and class, the student and why the exam is locked', async ({ page }) => {
  await connect(page);
  const dialog = await openUnlock(page, 'network');
  await expect(dialog.getByRole('heading', { name: 'Unlock exam' })).toBeVisible();
  await expect(dialog.locator('header')).toContainText('Databases midterm · Mathematics 10A');
  const student = dialog.locator('.student');
  await expect(student).toContainText('Student 000'); await expect(student).toContainText('0001');
  await expect(student.locator('.avatar')).toHaveText('S0');
  await expect(student.locator('.locked-badge')).toContainText('Locked');
  await expect(student.locator('.why')).toContainText('The student locked it to change network');
  // The reason is the lock's own, so it follows the three kinds.
  for (const [reason, text] of [
    ['away', 'The student was away from the exam window'],
    ['no_contact', 'The app could not reach the server'],
  ]) {
    await dialog.getByRole('button', { name: 'Cancel' }).click();
    await expect(dialog).toBeHidden();
    const again = await openUnlock(page, reason);
    await expect(again.locator('.student .why')).toContainText(text);
  }
});

test('the dialog opens on the reason, and Escape, Cancel and the X send nothing', async ({ page }) => {
  await connect(page);
  const dialog = await openUnlock(page);
  await expect(reasonField(dialog)).toBeFocused();
  await page.keyboard.press('Escape'); await expect(dialog).toBeHidden();
  const second = await openUnlock(page);
  await second.getByRole('button', { name: 'Close', exact: true }).click(); await expect(second).toBeHidden();
  const third = await openUnlock(page);
  await third.getByRole('button', { name: 'Cancel' }).click(); await expect(third).toBeHidden();
  expect(await sent(page)).toBeUndefined();
});

test('how long it has been locked, and the times, follow the server clock and keep counting', async ({ page }) => {
  await timed(page);
  const dialog = await openUnlock(page);
  await expect(dialog.locator('.ago')).toHaveText('1:00 ago');
  const keep = dialog.locator('.opt').nth(0), give = dialog.locator('.opt').nth(1);
  await expect(keep).toContainText('Keep the deadline');
  await expect(keep.locator('.dl b')).toHaveText(await clockOf(page, '2035-01-01T00:20:00Z'));
  await expect(keep.locator('.left b')).toHaveText('19:00');
  // Giving it back adds what the lock has lasted to the deadline.
  await expect(give.locator('.plus')).toHaveText('+1:00');
  await expect(give.locator('.dl b')).toHaveText(await clockOf(page, '2035-01-01T00:21:00Z'));
  await expect(give.locator('.left b')).toHaveText('20:00');
  // Half a minute later by this computer's clock: it is still locked, and the numbers moved with it.
  await page.clock.setFixedTime(new Date('2035-01-01T00:00:30Z'));
  await expect(dialog.locator('.ago')).toHaveText('1:30 ago');
  await expect(give.locator('.plus')).toHaveText('+1:30');
  await expect(keep.locator('.left b')).toHaveText('18:30');
  await expect(give.locator('.left b')).toHaveText('20:00');
});

test('the deadline is kept unless the proctor gives the time back, and the button says how much', async ({ page }) => {
  await timed(page);
  const dialog = await openUnlock(page);
  const keep = dialog.getByRole('radio', { name: 'Keep the deadline' }), give = dialog.getByRole('radio', { name: /^Give back/ });
  await expect(keep).toBeChecked(); await expect(give).not.toBeChecked();
  await expect(dialog).toContainText('The deadline doesn’t move while the exam is locked; the clock kept running.');
  await reasonField(dialog).fill('back online');
  await expect(unlockButton(dialog)).toHaveText('Unlock');
  await dialog.locator('.opt').nth(1).click();
  await expect(give).toBeChecked(); await expect(keep).not.toBeChecked();
  await expect(unlockButton(dialog)).toHaveText('Unlock · +1:00');
  await unlockButton(dialog).click();
  await expect(dialog).toBeHidden();
  expect(await sent(page)).toMatchObject({ sessionID: SESSION, reason: 'back online', giveBackTime: true, confirmed: true });
  await expect(page.getByRole('status')).toContainText('Unlocked 0001');
});

test('the keep option sends no time back', async ({ page }) => {
  await connect(page);
  const dialog = await openUnlock(page);
  await reasonField(dialog).fill('came back');
  await unlockButton(dialog).click();
  await expect(dialog).toBeHidden();
  expect(await sent(page)).toMatchObject({ reason: 'came back', giveBackTime: false, confirmed: true });
});

test('the reason is required, can be picked from the usual ones, and goes with the unlock', async ({ page }) => {
  await connect(page);
  const dialog = await openUnlock(page);
  await expect(unlockButton(dialog)).toBeDisabled();
  await expect(dialog.getByText('saved in the audit log')).toBeVisible();
  // Spaces are not a reason.
  await reasonField(dialog).fill('   '); await expect(unlockButton(dialog)).toBeDisabled();
  const chips = dialog.locator('.chips').getByRole('button');
  await expect(chips).toHaveText(['Network change', 'Computer restarted', 'App crashed', 'Left by mistake']);
  await chips.nth(1).click();
  await expect(reasonField(dialog)).toHaveValue('Computer restarted');
  await expect(chips.nth(1)).toHaveAttribute('aria-pressed', 'true'); await expect(chips.nth(0)).toHaveAttribute('aria-pressed', 'false');
  await expect(unlockButton(dialog)).toBeEnabled();
  // Changing the text is no longer that choice; another choice replaces it.
  await reasonField(dialog).fill('Computer restarted twice');
  await expect(chips.nth(1)).toHaveAttribute('aria-pressed', 'false');
  await chips.nth(0).click(); await expect(reasonField(dialog)).toHaveValue('Network change');
  // Enter in the field unlocks, as the button does.
  await reasonField(dialog).press('Enter');
  await expect(dialog).toBeHidden();
  expect(await sent(page)).toMatchObject({ reason: 'Network change', giveBackTime: false });
});

test('the reason is limited to what the server keeps', async ({ page }) => {
  await connect(page);
  const dialog = await openUnlock(page);
  await expect(reasonField(dialog)).toHaveAttribute('maxlength', '1024');
});

test('the ready checks say whether the student is back, from the roster as it is now, and never stop the unlock', async ({ page }) => {
  await connect(page);
  const dialog = await openUnlock(page);
  const network = dialog.locator('.chk').nth(0), window_ = dialog.locator('.chk').nth(1);
  await expect(dialog.getByText('Ready to unlock?')).toBeVisible();
  // In the fake server her heartbeats are late and her window has no known focus.
  await expect(network).toContainText('Exam network'); await expect(network).toContainText('Not reachable yet');
  await expect(window_).toContainText('Exam window'); await expect(window_).toContainText('Waiting for the student');
  await expect(network).toHaveClass(/no/); await expect(window_).toHaveClass(/no/);
  await expect(dialog.locator('.foot-note')).toHaveText('Student isn’t ready yet');
  // Not ready is something to know, not a rule: with a reason the proctor can unlock.
  await reasonField(dialog).fill('the student is on her way');
  await expect(unlockButton(dialog)).toBeEnabled();
  // The heartbeats come back, then the student comes back to the window: each check follows the roster while the dialog is open.
  await page.evaluate(() => window.ProctorHarness.rowPatch(0, { network_warning: false, presence: { connectivity: 'healthy' } }));
  await expect(network).toContainText('Connected · heartbeat ok'); await expect(network).toHaveClass(/yes/);
  await expect(window_).toHaveClass(/no/); await expect(dialog.locator('.foot-note')).toHaveText('Student isn’t ready yet');
  await page.evaluate(() => window.ProctorHarness.rowPatch(0, { presence: { focus: { state: 'focused' } } }));
  await expect(window_).toContainText('Back in the exam window'); await expect(window_).toHaveClass(/yes/);
  await expect(dialog.locator('.foot-note')).toHaveCount(0);
  // And she can leave again.
  await page.evaluate(() => window.ProctorHarness.rowPatch(0, { presence: { focus: { state: 'unfocused' } } }));
  await expect(window_).toHaveClass(/no/); await expect(dialog.locator('.foot-note')).toHaveText('Student isn’t ready yet');
});

test('a lock for the network or lack of contact says the alarm was quiet and offers the screenshots', async ({ page }) => {
  await connect(page);
  for (const reason of ['network', 'no_contact']) {
    const dialog = await openUnlock(page, reason);
    const note = dialog.locator('.while');
    await expect(note).toContainText('the alarm stayed quiet'); await expect(note).toContainText('the student could use the computer to reconnect');
    await expect(note).toContainText('Their screens were captured');
    await dialog.getByRole('button', { name: 'Cancel' }).click();
  }
  // The lock the rules make has no such promise.
  const away = await openUnlock(page, 'away');
  await expect(away.locator('.while')).toHaveCount(0);
});

test('the screenshots open from the dialog, which stays open', async ({ page }) => {
  await connect(page);
  const dialog = await openUnlock(page, 'network');
  await dialog.getByRole('button', { name: 'Review screenshots' }).click();
  await expect.poll(() => page.evaluate(() => window.ProctorHarness.calls.windows)).toEqual([{ name: `review-${SESSION}`, url: `/#/review/${SESSION}` }]);
  await expect(dialog).toBeVisible();
  expect(await sent(page)).toBeUndefined();
});

test('an unlock the server refuses is said in the dialog, which stays open for another try', async ({ page }) => {
  await connect(page);
  const dialog = await openUnlock(page);
  await page.evaluate(() => window.ProctorHarness.unlockFails('The server refused the unlock.'));
  await reasonField(dialog).fill('came back');
  await unlockButton(dialog).click();
  await expect(dialog.getByRole('alert')).toContainText('The server refused the unlock.');
  await expect(dialog).toBeVisible(); await expect(unlockButton(dialog)).toBeEnabled();
  await page.evaluate(() => window.ProctorHarness.unlockFails(''));
  await unlockButton(dialog).click();
  await expect(dialog).toBeHidden();
  await expect(page.getByRole('status')).toContainText('Unlocked 0001');
});

test('an exam that someone else unlocked meanwhile is said so, and there is nothing left to unlock', async ({ page }) => {
  await connect(page);
  const dialog = await openUnlock(page);
  await reasonField(dialog).fill('came back');
  await expect(unlockButton(dialog)).toBeEnabled();
  await page.evaluate(() => window.ProctorHarness.unlock(0));
  await expect(dialog.getByRole('status')).toContainText('This exam is no longer locked.');
  await expect(unlockButton(dialog)).toBeDisabled();
  expect(await sent(page)).toBeUndefined();
});

test('the dialog fits a narrow window, with its footer buttons stacked', async ({ page }) => {
  await page.setViewportSize({ width: 400, height: 760 });
  await connect(page);
  const dialog = await openUnlock(page);
  await expect(dialog.locator('.chk')).toHaveCount(2);
  expect(await dialog.evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true);
  const boxes = await Promise.all([dialog.getByRole('button', { name: 'Cancel' }), unlockButton(dialog)].map(button => button.boundingBox()));
  expect(boxes[1].y).toBeLessThan(boxes[0].y);
  expect(Math.abs(boxes[0].width - boxes[1].width)).toBeLessThan(2);
});
