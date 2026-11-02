import { test, expect } from '@playwright/test';
import { connect, calls, examsPanel, tickPublish as tick } from './helpers.js';

// The publish dialog (mock: publish-exam-dialog.html). Draft quiz is the exam to publish: 2 exercises, 15 minutes, never taken,
// and its login window is open. Practice exam has a closed one. Databases midterm is published, with one student working.
const DRAFT = '77777777-7777-4777-8777-777777777777';
const EXAM = '11111111-1111-4111-8111-111111111111';
// The browser's clock when a spec needs the time fixed: the end times below are measured from it.
const NOW = new Date('2035-01-01T10:00:30');

async function open(page, { title = 'Draft quiz', prepare, now } = {}) {
  if (now) await page.clock.setFixedTime(now);
  await connect(page, { prepare, openClass: 'Mathematics 10A' });
  await examsPanel(page).getByRole('button', { name: `Publish ${title}…` }).click();
  return page.getByRole('dialog', { name: 'Publish exam' });
}
const publishButton = dialog => dialog.getByRole('button', { name: /^(Publish & admit students|Publishing…)$/ });
const timeField = dialog => dialog.getByLabel('Exam ends at (local time)', { exact: true });
const hint = dialog => dialog.locator('.end-hint');
const setTime = dialog => dialog.getByRole('button', { name: 'At a set time' });
const noEnd = dialog => dialog.getByRole('button', { name: 'No end time' });
const pinCode = dialog => dialog.locator('.pin code');
const clipboard = page => page.evaluate(() => navigator.clipboard.readText());

test('the dialog shows the exam, what students need to log in, who can, and when it ends', async ({ page }) => {
  const dialog = await open(page);
  await expect(dialog.getByRole('heading', { name: 'Publish exam' })).toBeVisible();
  await expect(dialog).toContainText('Students can log in as soon as you publish');
  const summary = dialog.locator('.exam');
  await expect(summary).toContainText('Draft quiz'); await expect(summary).toContainText('Mathematics 10A');
  await expect(summary.locator('.fact').nth(0)).toContainText('Exercises'); await expect(summary.locator('.fact').nth(0)).toContainText('2');
  await expect(summary.locator('.fact').nth(1)).toContainText('00:15:00');
  await expect(summary.locator('.fact').nth(2)).toContainText('Login PIN'); await expect(pinCode(dialog)).toHaveText('••••••');
  await expect(dialog.locator('.a.in')).toContainText('28 students of Mathematics 10A');
  await expect(dialog.locator('.a.out')).toContainText('Students of other classes');
  await expect(noEnd(dialog)).toHaveAttribute('aria-pressed', 'true'); await expect(setTime(dialog)).toHaveAttribute('aria-pressed', 'false');
  await expect(timeField(dialog)).toHaveCount(0);
  await expect(hint(dialog)).toHaveText('Each student gets the full 15 min (plus grace) from their first login.');
  await expect(dialog.locator('label.override')).toContainText('Override: publish anyway, although 1 student is still working in “Databases midterm”.');
  await expect(dialog.locator('label.confirm:not(.override)')).toHaveText('I want to publish “Draft quiz”.');
  await expect(dialog.getByRole('button', { name: 'Cancel', exact: true })).toBeEnabled();
  await expect(publishButton(dialog)).toBeDisabled();
});

test('a class of one student says so in the singular', async ({ page }) => {
  const dialog = await open(page, { prepare: () => window.ProctorHarness.working(0) });
  await expect(dialog.locator('label.confirm')).toHaveText('I want to publish “Draft quiz”.');
  await tick(dialog);
  await expect(publishButton(dialog)).toBeEnabled();
});

test('the working count follows the server while the dialog is open, and the override is asked for again', async ({ page }) => {
  const dialog = await open(page, { prepare: () => window.ProctorHarness.working(0) });
  const override = dialog.locator('label.override input');
  await expect(dialog.locator('label.override')).toHaveCount(0); await expect(dialog).toContainText('nobody is working in it');
  await dialog.locator('label.confirm').click(); await expect(publishButton(dialog)).toBeEnabled();
  // A student logs in to the published exam after the dialog opened: the warning and the override appear, and the button waits.
  await page.evaluate(() => window.ProctorHarness.working(2));
  await expect(dialog.locator('label.override')).toContainText('although 2 students are still working in “Databases midterm”');
  await expect(dialog).toContainText('2 students still working in “Databases midterm”'); await expect(dialog).not.toContainText('nobody is working in it');
  await expect(publishButton(dialog)).toBeDisabled();
  await dialog.locator('label.override').click(); await expect(override).toBeChecked(); await expect(publishButton(dialog)).toBeEnabled();
  // The number changes after it was ticked: it is a different decision, so the override is asked for again. The other tick stays.
  await page.evaluate(() => window.ProctorHarness.working(3));
  await expect(dialog.locator('label.override')).toContainText('although 3 students are still working');
  await expect(override).not.toBeChecked(); await expect(dialog.locator('label.confirm:not(.override) input')).toBeChecked();
  await expect(publishButton(dialog)).toBeDisabled();
  // They all finish: nothing is left to override.
  await page.evaluate(() => window.ProctorHarness.working(0));
  await expect(dialog.locator('label.override')).toHaveCount(0); await expect(dialog).toContainText('nobody is working in it');
  await expect(publishButton(dialog)).toBeEnabled();
  await publishButton(dialog).click();
  await expect(dialog.getByRole('status')).toContainText('Now admitting');
  expect((await calls(page)).publish).toEqual([{ examId: DRAFT, expectedExam: EXAM, allowActive: false, confirmed: true }]);
});

test('a student who logs in just before publishing is refused, then the override is offered', async ({ page }) => {
  const dialog = await open(page, { prepare: () => window.ProctorHarness.working(0) });
  await dialog.locator('label.confirm').click();
  await page.evaluate(() => window.ProctorHarness.studentsLogInLate(2));
  await publishButton(dialog).click();
  await expect(dialog.getByRole('alert')).toContainText('students are still working');
  await expect(dialog.getByRole('status')).toHaveCount(0);
  await expect(dialog.locator('label.override')).toContainText('although 2 students are still working in “Databases midterm”');
  await expect(publishButton(dialog)).toBeDisabled();
  await tick(dialog); await publishButton(dialog).click();
  await expect(dialog.getByRole('status')).toContainText('Now admitting');
  expect((await calls(page)).publish).toEqual([
    { examId: DRAFT, expectedExam: EXAM, allowActive: false, confirmed: true },
    { examId: DRAFT, expectedExam: EXAM, allowActive: true, confirmed: true },
  ]);
});

test('the PIN stays hidden until it is revealed, can be copied from the form, and is read once', async ({ page }) => {
  await page.context().grantPermissions(['clipboard-read', 'clipboard-write']);
  const dialog = await open(page);
  await expect(pinCode(dialog)).toHaveText('••••••');
  await dialog.getByRole('button', { name: 'Show PIN' }).click();
  await expect(pinCode(dialog)).toHaveText('482719');
  await expect(dialog.getByRole('button', { name: 'Hide PIN' })).toHaveAttribute('aria-pressed', 'true');
  await dialog.getByRole('button', { name: 'Hide PIN' }).click();
  await expect(pinCode(dialog)).toHaveText('••••••');
  // Copying does not reveal it.
  await dialog.getByRole('button', { name: 'Copy PIN' }).click();
  await expect(dialog.locator('.toast')).toHaveText('PIN copied');
  expect(await clipboard(page)).toBe('482719');
  await expect(pinCode(dialog)).toHaveText('••••••');
  expect((await calls(page)).pin).toEqual([DRAFT]);
});

test('a PIN the server will not give is reported only when asked for, and publishing still works', async ({ page }) => {
  const dialog = await open(page, { prepare: () => window.ProctorHarness.pinFails('The server refused the request.') });
  await expect(dialog.getByRole('alert')).toHaveCount(0);
  await dialog.getByRole('button', { name: 'Show PIN' }).click();
  await expect(dialog.getByRole('alert')).toContainText('The PIN could not be read: Error: The server refused the request.');
  await expect(pinCode(dialog)).toHaveText('••••••');
  await expect(dialog.getByRole('button', { name: 'Show PIN' })).toHaveAttribute('aria-pressed', 'false');
  await tick(dialog); await publishButton(dialog).click();
  await expect(dialog.getByRole('status')).toContainText('Now admitting');
  await expect(dialog.locator('.big-pin')).toContainText('Not shown: the PIN could not be read.');
  await expect(dialog.getByRole('button', { name: 'Copy PIN' })).toHaveCount(0);
  await page.evaluate(() => window.ProctorHarness.pinFails(''));
  await dialog.getByRole('button', { name: 'Try again' }).click();
  await expect(dialog.locator('.big-pin code')).toHaveText('482719');
  await expect(dialog.getByRole('alert')).toHaveCount(0);
});

test('a set time shows how far away it is and what it means for students who log in', async ({ page }) => {
  const dialog = await open(page, { now: NOW });
  await setTime(dialog).click();
  await expect(setTime(dialog)).toHaveAttribute('aria-pressed', 'true');
  // Roughly two hours ahead, to the nearest five minutes.
  await expect(timeField(dialog)).toHaveValue('12:05');
  await expect(dialog.locator('.eta')).toHaveText('in 2 h 05 min');
  await expect(hint(dialog)).toHaveText(/^Latest login that still gets the full 15 min: 11:50\sAM\.$/);
  await expect(hint(dialog)).not.toHaveClass(/warn/);
  // Less time than the exam lasts: students who start now would be cut short.
  await timeField(dialog).fill('10:10');
  await expect(dialog.locator('.eta')).toHaveText('in 10 min');
  await expect(hint(dialog)).toHaveText('Students who start now get only 10 of their 15 min.');
  await expect(hint(dialog)).toHaveClass(/warn/);
  // The server wants at least a minute.
  await timeField(dialog).fill('10:01');
  await expect(hint(dialog)).toHaveText('Choose an end time at least one minute from now.');
  await tick(dialog);
  await expect(publishButton(dialog)).toBeDisabled();
  // A time that has passed today means that time tomorrow.
  await timeField(dialog).fill('09:00');
  await expect(dialog.locator('.eta')).toHaveText('tomorrow · in 23 h 00 min');
  await expect(hint(dialog)).not.toHaveClass(/warn/);
  await tick(dialog);
  await expect(publishButton(dialog)).toBeEnabled();
  // Going back to no end time hides the field, the pill and the warning.
  await noEnd(dialog).click();
  await expect(timeField(dialog)).toHaveCount(0); await expect(dialog.locator('.eta')).toHaveCount(0);
  await expect(hint(dialog)).toHaveText('Each student gets the full 15 min (plus grace) from their first login.');
});

test('any change to when the exam ends asks for the tick again', async ({ page }) => {
  const dialog = await open(page, { now: NOW });
  const box = dialog.locator('label.confirm:not(.override) input'), override = dialog.locator('label.override input');
  await tick(dialog); await expect(box).toBeChecked(); await expect(publishButton(dialog)).toBeEnabled();
  await setTime(dialog).click();
  // Only the wish to publish is asked again: the override is about the exam being replaced, which the end time does not change.
  await expect(box).not.toBeChecked(); await expect(override).toBeChecked(); await expect(publishButton(dialog)).toBeDisabled();
  await tick(dialog); await expect(box).toBeChecked();
  await timeField(dialog).fill('12:30');
  await expect(box).not.toBeChecked(); await expect(publishButton(dialog)).toBeDisabled();
  await tick(dialog); await noEnd(dialog).click();
  await expect(box).not.toBeChecked();
});

test('publishing with a set time sends it, and the result says when the exam ends', async ({ page }) => {
  const dialog = await open(page, { now: NOW });
  await setTime(dialog).click(); await timeField(dialog).fill('12:00');
  await tick(dialog); await publishButton(dialog).click();
  await expect(dialog.getByRole('status')).toContainText('Now admitting');
  const c = await calls(page);
  expect(c.publishTimed).toEqual([{ examId: DRAFT, hhmm: '12:00' }]);
  expect(c.publish).toHaveLength(1);
  await expect(dialog.locator('.done-facts')).toHaveText(/Students\s*28 of Mathematics 10A\s*Ends\s*12:00\sPM/);
});

test('no end time publishes without one; an end time the exam kept is cleared first, so the choice is true', async ({ page }) => {
  let dialog = await open(page, { now: NOW });
  await tick(dialog); await publishButton(dialog).click();
  await expect(dialog.getByRole('status')).toContainText('Now admitting');
  expect(await calls(page)).toMatchObject({ publishTimed: [], endTimes: [] });
  await expect(dialog.locator('.done-facts')).toHaveText(/Ends\s*No end time/);

  // An exam published before keeps its earlier end time: the dialog offers it again, and "No end time" removes it.
  dialog = await open(page, { now: NOW, prepare: () => window.ProctorHarness.examEndsAt('77777777-7777-4777-8777-777777777777', '2035-01-02T23:30:00Z') });
  await expect(setTime(dialog)).toHaveAttribute('aria-pressed', 'true');
  await expect(timeField(dialog)).not.toHaveValue('');
  await noEnd(dialog).click(); await tick(dialog); await publishButton(dialog).click();
  await expect(dialog.getByRole('status')).toContainText('Now admitting');
  expect(await calls(page)).toMatchObject({ publishTimed: [], endTimes: [{ examId: DRAFT, hhmm: '' }] });
  await expect(dialog.locator('.done-facts')).toHaveText(/Ends\s*No end time/);
});

test('clocks that disagree and a time the dashboard cannot read are said before publishing', async ({ page }) => {
  let dialog = await open(page, { now: NOW, prepare: () => window.ProctorHarness.endTimeClockWarning(true) });
  await setTime(dialog).click();
  await expect(dialog).toContainText("The server and this computer's clocks differ by more than one minute.");

  dialog = await open(page, { now: NOW, prepare: () => window.ProctorHarness.endTimePreviewFailure('That time is not valid.') });
  await setTime(dialog).click();
  await expect(dialog.getByRole('alert')).toContainText('That time is not valid.');
  await tick(dialog);
  await expect(publishButton(dialog)).toBeDisabled();
});

test('after publishing, the dialog shows who is admitted, the PIN to read out and the way to the live roster', async ({ page }) => {
  await page.context().grantPermissions(['clipboard-read', 'clipboard-write']);
  const dialog = await open(page);
  await tick(dialog); await publishButton(dialog).click();
  const hero = dialog.getByRole('status');
  await expect(hero).toContainText('Now admitting'); await expect(hero).toContainText('Draft quiz');
  await expect(dialog.locator('.big-pin code')).toHaveText('482719');
  await expect(dialog.locator('.big-pin')).toContainText('Login PIN for students');
  await expect(dialog.locator('.big-pin')).toContainText('Share it with Mathematics 10A only');
  await expect(dialog.locator('.done-facts')).toHaveText(/Students\s*28 of Mathematics 10A\s*Ends\s*No end time/);
  // The form is gone, and Cancel with it; the result offers Close and the roster, and the roster has the focus.
  await expect(dialog.getByRole('checkbox')).toHaveCount(0);
  await expect(dialog.getByRole('button', { name: 'Cancel', exact: true })).toHaveCount(0);
  await expect(dialog.getByRole('button', { name: 'Open live roster' })).toBeFocused();

  await dialog.getByRole('button', { name: 'Copy PIN' }).click();
  await expect(dialog.locator('.toast')).toHaveText('PIN copied');
  expect(await clipboard(page)).toBe('482719');

  await dialog.getByRole('button', { name: 'Open live roster' }).click();
  await expect.poll(async () => (await calls(page)).windows).toEqual([{ name: 'monitoring', url: '/#/monitoring' }]);
  await expect(dialog).toBeVisible(); // the PIN is still there to read out
  await dialog.locator('.foot').getByRole('button', { name: 'Close', exact: true }).click();
  await expect(dialog).toHaveCount(0);
});

test('Escape and Cancel close the dialog without publishing', async ({ page }) => {
  let dialog = await open(page);
  await tick(dialog);
  await page.keyboard.press('Escape');
  await expect(dialog).toHaveCount(0);
  await examsPanel(page).getByRole('button', { name: 'Publish Draft quiz…' }).click();
  dialog = page.getByRole('dialog', { name: 'Publish exam' });
  await expect(dialog.getByRole('checkbox')).toHaveCount(2); await expect(dialog.getByRole('checkbox').and(page.locator(':checked'))).toHaveCount(0); // a new dialog starts unticked
  await dialog.getByRole('button', { name: 'Cancel', exact: true }).click();
  await expect(dialog).toHaveCount(0);
  const c = await calls(page);
  expect(c.publish).toEqual([]); expect(c.publishTimed).toEqual([]);
});

test('a long PIN and a narrow window fit the dialog', async ({ page }) => {
  await page.setViewportSize({ width: 400, height: 800 });
  const dialog = await open(page, { prepare: () => window.ProctorHarness.examPin('correct horse battery staple 2035 ¡señal!') });
  const fits = () => dialog.evaluate(element => element.scrollWidth <= element.clientWidth);
  await dialog.getByRole('button', { name: 'Show PIN' }).click();
  await expect(pinCode(dialog)).toHaveText('correct horse battery staple 2035 ¡señal!');
  expect(await fits()).toBe(true);
  // On a narrow window the primary action comes first and fills the width.
  const publish = await publishButton(dialog).boundingBox(), cancel = await dialog.getByRole('button', { name: 'Cancel', exact: true }).boundingBox();
  expect(publish.y).toBeLessThan(cancel.y);
  expect(Math.abs(publish.width - cancel.width)).toBeLessThan(2);
  await tick(dialog); await publishButton(dialog).click();
  await expect(dialog.locator('.big-pin code')).toHaveText('correct horse battery staple 2035 ¡señal!');
  expect(await fits()).toBe(true);
});
