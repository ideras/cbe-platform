import { test, expect } from '@playwright/test';
import { connect as connectTo, goTo, nav, calls } from './helpers.js';

test('the Settings page shows the values, the worked-out count and the preview, and saves them', async ({ page }) => {
  await connectTo(page);
  await goTo(page, 'Settings');
  const panel = page.getByRole('region', { name: 'Monitoring rules' });
  await expect(panel).toBeVisible();
  await expect(panel.getByText('Changes apply to exams published from now on;')).toBeVisible();
  // The values the server holds are shown.
  await expect(panel.getByLabel('Heartbeat every (seconds)')).toHaveValue('15');
  await expect(panel.getByLabel('Screenshot every (seconds)')).toHaveValue('5');
  await expect(panel.getByLabel('Lock after away for (seconds)')).toHaveValue('300');
  await expect(panel.getByLabel('Lock after no contact for (seconds)')).toHaveValue('300');
  // With the away lock on, the per-absence count is worked out and the field cannot be edited.
  await expect(panel.getByRole('spinbutton', { name: 'Screenshots per absence', exact: true })).toBeDisabled();
  await expect(panel.getByRole('spinbutton', { name: 'Screenshots per absence', exact: true })).toHaveValue('60');
  await expect(panel.getByRole('spinbutton', { name: 'Screenshots per absence', exact: true })).toHaveAccessibleDescription('Worked out from the away lock: 60 cover the whole absence up to the lock.');
  // The summary says what the students will cost and get.
  const summary = panel.getByRole('region', { name: 'What students get' });
  await expect(summary).toContainText('100 MB per student, worst case');
  await expect(summary).toContainText('Screenshots per absence up to 60');
  await expect(summary).toContainText('Screenshots per student up to 200');
  await expect(summary).toContainText('Exam locks when away 5 min');
  // Nothing is changed yet, so there is nothing to save.
  await expect(panel.getByRole('region', { name: 'Unsaved changes' })).toHaveCount(0);
  // A value outside its range is refused, and the problem is named.
  await panel.getByLabel('Heartbeat every (seconds)').fill('2');
  await expect(panel.getByRole('alert')).toContainText('Heartbeat every must be between 5 and 60 seconds (it is 2).');
  await expect(panel.getByRole('region', { name: 'Unsaved changes' })).toContainText('Fix the problems first, then save.');
  await expect(panel.getByRole('button', { name: 'Save settings' })).toBeDisabled();
  await panel.getByLabel('Heartbeat every (seconds)').fill('15');
  await expect(panel.getByRole('alert')).toHaveCount(0);
  // Back to the server's value, there is nothing left to save.
  await expect(panel.getByRole('region', { name: 'Unsaved changes' })).toHaveCount(0);
  // Saving sends the values with the update time the page loaded.
  await panel.getByLabel('Screenshot every (seconds)').fill('7');
  await panel.getByRole('button', { name: 'Save settings' }).click();
  await expect(page.getByRole('status')).toContainText('Settings saved');
  const sent = await calls(page);
  expect(sent.settings.values.capture_interval_ms).toBe(7000);
  expect(sent.settings.expectedUpdatedAt).toBe('2035-01-01T00:00:00Z');
});

test('an alarm that starts after the away lock says why the settings cannot be saved', async ({ page }) => {
  await connectTo(page);
  await goTo(page, 'Settings');
  const panel = page.getByRole('region', { name: 'Monitoring rules' });
  // The alarm may start later, as long as the away lock comes after it.
  await panel.getByLabel('Alarm repeats every (seconds)').fill('3');
  await panel.getByLabel('Alarm and screenshots start after (seconds)').fill('60');
  await expect(panel.getByRole('alert')).toHaveCount(0);
  await expect(panel.getByRole('button', { name: 'Save settings' })).toBeEnabled();
  // Past the away lock the combination is impossible, and the page says which values collide.
  await panel.getByLabel('Alarm and screenshots start after (seconds)').fill('300');
  await expect(panel.getByRole('alert')).toContainText('Lock after away (300 s) must come after the alarm starts (300 s) plus one screenshot interval (5 s)');
  await expect(panel.getByRole('button', { name: 'Save settings' })).toBeDisabled();
  // The field that was changed is the one marked.
  await expect(panel.getByLabel('Alarm and screenshots start after (seconds)')).toHaveAttribute('aria-invalid', 'true');
  await expect(panel.getByLabel('Lock after away for (seconds)')).toHaveAttribute('aria-invalid', 'true');
  // Raising the lock as well makes the combination valid again.
  await panel.getByLabel('Lock after away for (seconds)').fill('360');
  await expect(panel.getByRole('alert')).toHaveCount(0);
  await expect(panel.getByRole('button', { name: 'Save settings' })).toBeEnabled();
});

const away = panel => panel.getByLabel('Lock after away for (seconds)');
const noContact = panel => panel.getByLabel('Lock after no contact for (seconds)');
const said = (panel, label) => panel.locator('.field', { hasText: label }).locator('.said');

test('the locks are set in seconds, and the page says in minutes what a long one is', async ({ page }) => {
  await connectTo(page);
  await goTo(page, 'Settings');
  const panel = page.getByRole('region', { name: 'Monitoring rules' });
  await expect(said(panel, 'Lock after away for')).toHaveText('5 min');
  await expect(said(panel, 'Lock after no contact for')).toHaveText('5 min');
  await away(panel).fill('90');
  await expect(said(panel, 'Lock after away for')).toHaveText('1 min 30 s');
  // Under a minute the number says it all.
  await away(panel).fill('30');
  await expect(said(panel, 'Lock after away for')).toHaveCount(0);
  await expect(panel.getByRole('alert')).toHaveCount(0);
});

test('a short lock is saved as it was typed, down to what the alarm and the connection allow', async ({ page }) => {
  await connectTo(page);
  await goTo(page, 'Settings');
  const panel = page.getByRole('region', { name: 'Monitoring rules' });
  // The alarm starts at 5 s and a screenshot is taken every 5 s, so the lock cannot be before 10 s.
  await away(panel).fill('9');
  await expect(panel.getByRole('alert')).toContainText('Lock after away (9 s) must come after the alarm starts (5 s) plus one screenshot interval (5 s)');
  await expect(panel.getByRole('button', { name: 'Save settings' })).toBeDisabled();
  await away(panel).fill('10');
  // The connection counts as lost after 30 s, so the no contact lock has to be longer.
  await noContact(panel).fill('30');
  await expect(panel.getByRole('alert')).toContainText('Lock after no contact (30 s) must be longer than the connection lost threshold (30 s)');
  await expect(noContact(panel)).toHaveAttribute('aria-invalid', 'true');
  await noContact(panel).fill('31');
  await expect(panel.getByRole('alert')).toHaveCount(0);
  // The screenshots of an absence still cover it up to the lock: at 5 s and at 10 s.
  await expect(panel.getByRole('region', { name: 'What students get' })).toContainText('Screenshots per absence up to 2');
  await panel.getByRole('button', { name: 'Save settings' }).click();
  await expect(page.getByRole('status')).toContainText('Settings saved');
  const sent = await calls(page);
  expect(sent.settings.values).toMatchObject({ lock_away_after_ms: 10000, lock_no_contact_after_ms: 31000 });
});

const lockSwitch = (panel, name) => panel.getByRole('switch', { name, exact: true });

test('a lock is switched off and on, is off at zero, a whole number of seconds, and not more than an hour', async ({ page }) => {
  await connectTo(page);
  await goTo(page, 'Settings');
  const panel = page.getByRole('region', { name: 'Monitoring rules' });
  const save = panel.getByRole('button', { name: 'Save settings' });
  const summary = panel.getByRole('region', { name: 'What students get' });
  // Off, the away lock gives the per-absence screenshots back to the proctor, and the field says off.
  await lockSwitch(panel, 'Lock after away').click();
  await expect(lockSwitch(panel, 'Lock after away')).not.toBeChecked();
  await expect(away(panel)).toBeDisabled();
  await expect(away(panel)).toHaveAttribute('placeholder', 'off');
  await expect(away(panel)).toHaveAccessibleDescription('Off: the alarm keeps going, and the exam never locks for being away.');
  await expect(panel.getByRole('spinbutton', { name: 'Screenshots per absence', exact: true })).toBeEnabled();
  await expect(summary).toContainText('Exam locks when away never');
  await expect(panel.getByRole('img', { name: /alarm and the screenshots start/ })).toHaveAccessibleName(/and the exam never locks for it\.$/);
  await expect(panel.getByRole('alert')).toHaveCount(0);
  // On again, it comes back with the value it had.
  await lockSwitch(panel, 'Lock after away').click();
  await expect(away(panel)).toHaveValue('300');
  // Typing zero turns a lock off too.
  await noContact(panel).fill('0');
  await expect(lockSwitch(panel, 'Lock after no contact')).not.toBeChecked();
  await expect(panel.getByRole('alert')).toHaveCount(0);
  await lockSwitch(panel, 'Lock after no contact').click();
  await expect(noContact(panel)).toHaveValue('300');
  await away(panel).fill('0.5');
  await expect(panel.getByRole('alert')).toContainText('Lock after away must be a whole number of seconds (it is 0.5).');
  await expect(save).toBeDisabled();
  await away(panel).fill('-1');
  await expect(panel.getByRole('alert')).toContainText('Lock after away must be off or between 1 second and 60 minutes (it is -1 s).');
  await away(panel).fill('3601');
  await expect(panel.getByRole('alert').first()).toContainText('Lock after away must be off or between 1 second and 60 minutes (it is 3601 s).');
  await noContact(panel).fill('3601');
  await expect(panel.getByRole('alert')).toContainText('Lock after no contact must be off or between 1 second and 60 minutes (it is 3601 s).');
  // An hour is allowed, with room for the screenshots that cover it.
  await away(panel).fill('3600'); await noContact(panel).fill('3600');
  await panel.getByRole('spinbutton', { name: 'Screenshots per student', exact: true }).fill('2000');
  await expect(panel.getByRole('alert')).toHaveCount(0);
  await expect(save).toBeEnabled();
});

test('a lock the server holds in a part of a minute is shown and saved as it is, not rounded to a minute', async ({ page }) => {
  await connectTo(page);
  await page.evaluate(() => window.ProctorHarness.settings({ lock_away_after_ms: 90000, lock_no_contact_after_ms: 45000 }));
  await goTo(page, 'Settings');
  const panel = page.getByRole('region', { name: 'Monitoring rules' });
  await expect(away(panel)).toHaveValue('90'); await expect(noContact(panel)).toHaveValue('45');
  // Saving another value sends the locks as they were.
  await panel.getByLabel('Default grace period (seconds)').fill('40');
  await panel.getByRole('button', { name: 'Save settings' }).click();
  await expect(page.getByRole('status')).toContainText('Settings saved');
  expect((await calls(page)).settings.values).toMatchObject({ lock_away_after_ms: 90000, lock_no_contact_after_ms: 45000, default_grace_ms: 40000 });
});

test('an empty value is refused instead of being read as zero', async ({ page }) => {
  await connectTo(page);
  await goTo(page, 'Settings');
  const panel = page.getByRole('region', { name: 'Monitoring rules' });
  await panel.getByLabel('Alarm repeats every (seconds)').fill('');
  await expect(panel.getByRole('alert')).toContainText('Alarm repeats every is empty');
  await expect(panel.getByRole('button', { name: 'Save settings' })).toBeDisabled();
  await panel.getByLabel('Alarm repeats every (seconds)').fill('2');
  await expect(panel.getByRole('alert')).toHaveCount(0);
});

test('a save that another proctor made first reloads the page and says so', async ({ page }) => {
  await connectTo(page);
  await goTo(page, 'Settings');
  const panel = page.getByRole('region', { name: 'Monitoring rules' });
  await expect(panel.getByLabel('Heartbeat every (seconds)')).toHaveValue('15');
  // Another proctor saves a different heartbeat in the meantime.
  await page.evaluate(() => { window.ProctorHarness.settings({ heartbeat_interval_ms: 20000, overdue_threshold_ms: 40000 }); window.ProctorHarness.settingsStale(); });
  await panel.getByLabel('Alarm repeats every (seconds)').fill('3');
  await panel.getByRole('button', { name: 'Save settings' }).click();
  await expect(page.getByRole('alert').filter({ hasText: 'Another proctor saved the settings first' }).first()).toBeVisible();
  // The page shows what the server holds now, not what this proctor typed.
  await expect(panel.getByLabel('Heartbeat every (seconds)')).toHaveValue('20');
  await expect(panel.getByLabel('Alarm repeats every (seconds)')).toHaveValue('2');
});

test('the Settings page reads the server again each time it opens', async ({ page }) => {
  await connectTo(page);
  await goTo(page, 'Settings');
  const panel = page.getByRole('region', { name: 'Monitoring rules' });
  await expect(panel.getByLabel('Screenshot every (seconds)')).toHaveValue('5');
  await goTo(page, 'Classes');
  await page.evaluate(() => window.ProctorHarness.settings({ capture_interval_ms: 10000 }));
  await goTo(page, 'Settings');
  await expect(page.getByRole('region', { name: 'Monitoring rules' }).getByLabel('Screenshot every (seconds)')).toHaveValue('10');
});

test('a failed read of the settings can be tried again', async ({ page }) => {
  await connectTo(page);
  await page.evaluate(() => window.ProctorHarness.settingsFails(true));
  await goTo(page, 'Settings');
  const panel = page.getByRole('region', { name: 'Monitoring rules' });
  await expect(panel.getByRole('alert')).toContainText('authorization was rejected');
  await page.evaluate(() => window.ProctorHarness.settingsFails(false));
  await panel.getByRole('button', { name: 'Try again' }).click();
  await expect(panel.getByLabel('Heartbeat every (seconds)')).toHaveValue('15');
});

// The fields by their accessible names, exactly: a hint beside a field is its description and must not be part of its name.
const numberField = (panel, name) => panel.getByRole('spinbutton', { name, exact: true });
const alarm = panel => numberField(panel, 'Alarm and screenshots start after (seconds)');
const lost = panel => numberField(panel, 'Connection lost after (seconds)');
const beat = panel => numberField(panel, 'Heartbeat every (seconds)');
const interval = panel => numberField(panel, 'Screenshot every (seconds)');
const students = panel => numberField(panel, 'Screenshots per student');
const grace = panel => numberField(panel, 'Default grace period (seconds)');
const saveOf = panel => panel.getByRole('button', { name: 'Save settings' });
const fitOf = panel => panel.getByRole('button', { name: /^Fit the other values to/ });
const noteOf = panel => panel.getByRole('status');
const open = async page => { await connectTo(page); await goTo(page, 'Settings'); return page.getByRole('region', { name: 'Monitoring rules' }); };

test('each limit the other values put on a field is said beside it, and follows them', async ({ page }) => {
  const panel = await open(page);
  // Each field keeps its name, so the hint is only its description.
  for (const name of ['Lock after away for (seconds)', 'Lock after no contact for (seconds)']) await expect(numberField(panel, name)).toHaveCount(1);
  // The field keeps its name: the hint is its description, not part of the label.
  await expect(away(panel)).toHaveAccessibleDescription('Off, or at least 10 s: the alarm starts at 5 s, plus a screenshot every 5 s.');
  await expect(noContact(panel)).toHaveAccessibleDescription('Off, or at least 31 s: longer than the 30 s after which the connection counts as lost.');
  await expect(alarm(panel)).toHaveAccessibleDescription('At most 4 min 55 s: the lock after away (5 min) has to come one screenshot (5 s) after it.');
  await expect(lost(panel)).toHaveAccessibleDescription('From 30 s (two heartbeats) to 4 min 59 s (just under the lock after no contact, 5 min).');
  // A longer screenshot interval or a later alarm start moves the least the away lock may be.
  await interval(panel).fill('7');
  await expect(away(panel)).toHaveAccessibleDescription('Off, or at least 12 s: the alarm starts at 5 s, plus a screenshot every 7 s.');
  await alarm(panel).fill('20');
  await expect(away(panel)).toHaveAccessibleDescription('Off, or at least 27 s: the alarm starts at 20 s, plus a screenshot every 7 s.');
  // A slower heartbeat moves the least the connection may take to count as lost, and with it the least of the no contact lock.
  await beat(panel).fill('40');
  await expect(lost(panel)).toHaveAccessibleDescription(/^From 1 min 20 s \(two heartbeats\)/);
  await lost(panel).fill('90');
  await expect(noContact(panel)).toHaveAccessibleDescription('Off, or at least 1 min 31 s: longer than the 1 min 30 s after which the connection counts as lost.');
  // With a lock off there is nothing to stay under, so the field says what it is instead.
  await away(panel).fill('0');
  await expect(alarm(panel)).toHaveAccessibleDescription('How long a student may be away before the alarm sounds and the screenshots start.');
  await noContact(panel).fill('0');
  await expect(lost(panel)).toHaveAccessibleDescription('At least 1 min 20 s: two heartbeats.');
  // A field with no limit from the other values says what it is.
  await expect(beat(panel)).toHaveAccessibleDescription('How often each student’s computer checks in with the server.');
  await expect(grace(panel)).toHaveAccessibleDescription('Extra time after the exam ends. Each exam can change it.');
});

test('a problem has buttons that solve it, each saying what it sets', async ({ page }) => {
  const panel = await open(page);
  await away(panel).fill('9');
  await panel.getByRole('button', { name: 'Raise the lock to 10 s' }).click();
  await expect(away(panel)).toHaveValue('10');
  await expect(panel.getByRole('alert')).toHaveCount(0);
  await expect(saveOf(panel)).toBeEnabled();
  // The other way is the alarm start, which comes down and leaves the lock where it was.
  await away(panel).fill('8');
  await panel.getByRole('button', { name: 'Lower the alarm start to 3 s' }).click();
  await expect(alarm(panel)).toHaveValue('3');
  await expect(away(panel)).toHaveValue('8');
  await expect(panel.getByRole('alert')).toHaveCount(0);
  // A value out of its range goes to its nearest limit; an empty one to the default.
  await beat(panel).fill('2');
  await panel.getByRole('button', { name: 'Use 5 s' }).click();
  await expect(beat(panel)).toHaveValue('5');
  await grace(panel).fill('');
  await panel.getByRole('button', { name: 'Use 30 s' }).click();
  await expect(grace(panel)).toHaveValue('30');
  await away(panel).fill('-4');
  await panel.getByRole('button', { name: 'Turn the lock off' }).click();
  await expect(lockSwitch(panel, 'Lock after away')).not.toBeChecked();
  await expect(away(panel)).toHaveAttribute('placeholder', 'off');
  // A lock that needs more screenshots than a student may have: raise the limit to what the lock needs.
  await lockSwitch(panel, 'Lock after away').click();
  await away(panel).fill('3600');
  await expect(panel.getByRole('alert')).toContainText('Screenshots per student (200) must be at least the per-absence count (720)');
  await panel.getByRole('button', { name: 'Raise to 720' }).click();
  await expect(students(panel)).toHaveValue('720');
  await expect(panel.getByRole('alert')).toHaveCount(0);
  await expect(saveOf(panel)).toBeEnabled();
});

test('fitting the other values to the one changed last shows what it will change, does it, and can be undone', async ({ page }) => {
  const panel = await open(page);
  // Nothing conflicts, so there is nothing to fit.
  await expect(fitOf(panel)).toHaveCount(0);
  await away(panel).fill('8');
  await expect(fitOf(panel)).toHaveText('Fit the other values to “Lock after away for”');
  await expect(panel.locator('.fit')).toContainText('changes only what conflicts with it');
  await expect(panel.locator('.fit')).toContainText('Alarm and screenshots start after 5 s → 3 s.');
  // It has not changed anything yet.
  await expect(alarm(panel)).toHaveValue('5');
  await fitOf(panel).click();
  await expect(alarm(panel)).toHaveValue('3');
  await expect(interval(panel)).toHaveValue('5');
  await expect(away(panel)).toHaveValue('8');
  await expect(panel.getByRole('alert')).toHaveCount(0);
  await expect(saveOf(panel)).toBeEnabled();
  for (const field of [alarm(panel), away(panel), interval(panel)]) await expect(field).toHaveAttribute('aria-invalid', 'false');
  await expect(noteOf(panel)).toContainText('Fitted to “Lock after away for”: Alarm and screenshots start after 5 s → 3 s.');
  await expect(fitOf(panel)).toHaveCount(0);
  // Undo brings back what it changed, the conflict with it.
  await noteOf(panel).getByRole('button', { name: 'Undo' }).click();
  await expect(alarm(panel)).toHaveValue('5');
  await expect(away(panel)).toHaveValue('8');
  await expect(noteOf(panel)).toHaveCount(0);
  await expect(panel.getByRole('alert')).toContainText('Lock after away (8 s) must come after the alarm starts');
  await expect(fitOf(panel)).toHaveText('Fit the other values to “Lock after away for”');
  // Another edit ends the note, since Undo would take that edit back too.
  await fitOf(panel).click();
  await expect(noteOf(panel)).toBeVisible();
  await grace(panel).fill('45');
  await expect(noteOf(panel)).toHaveCount(0);
  // A save after a fit sends what the fit made and leaves only the saved note.
  await away(panel).fill('7');
  await fitOf(panel).click();
  await saveOf(panel).click();
  await expect(page.getByRole('status')).toContainText('Settings saved');
  expect((await calls(page)).settings.values).toMatchObject({ lock_away_after_ms: 7000, focus_threshold_ms: 2000, capture_interval_ms: 5000, default_grace_ms: 45000 });
});

test('fitting holds the field that was changed last, and takes the conflicts one after the other', async ({ page }) => {
  const panel = await open(page);
  await away(panel).fill('8');
  await noContact(panel).fill('20');
  // Two conflicts, in two groups of values: the one changed last is held first.
  await expect(fitOf(panel)).toHaveText('Fit the other values to “Lock after no contact for”');
  await fitOf(panel).click();
  await expect(beat(panel)).toHaveValue('9');
  await expect(lost(panel)).toHaveValue('19');
  await expect(noContact(panel)).toHaveValue('20');
  await expect(alarm(panel)).toHaveValue('5');
  await expect(fitOf(panel)).toHaveText('Fit the other values to “Lock after away for”');
  await fitOf(panel).click();
  await expect(alarm(panel)).toHaveValue('3');
  await expect(panel.getByRole('alert')).toHaveCount(0);
  await expect(saveOf(panel)).toBeEnabled();
});

test('a button on a problem makes the field it set the one held, so a fit can follow it', async ({ page }) => {
  const panel = await open(page);
  await noContact(panel).fill('100');
  // A heartbeat of a minute needs two minutes before the connection counts as lost...
  await beat(panel).fill('60');
  await panel.getByRole('button', { name: 'Raise connection lost to 2 min' }).click();
  await expect(lost(panel)).toHaveValue('120');
  // ... which is longer than the lock after no contact, and the field the button set is the one the rest is fitted to.
  await expect(panel.getByRole('alert')).toContainText('Lock after no contact (100 s) must be longer than the connection lost threshold (120 s).');
  await expect(fitOf(panel)).toHaveText('Fit the other values to “Connection lost after”');
  await fitOf(panel).click();
  await expect(noContact(panel)).toHaveValue('121');
  await expect(lost(panel)).toHaveValue('120');
  await expect(beat(panel)).toHaveValue('60');
  await expect(panel.getByRole('alert')).toHaveCount(0);
});

test('fitting is not offered when it cannot be done, and the buttons of the problems still are', async ({ page }) => {
  const panel = await open(page);
  // The screenshot interval is wrong on its own: it has to be put right first.
  await away(panel).fill('8');
  await interval(panel).fill('1');
  await expect(fitOf(panel)).toHaveCount(0);
  await panel.getByRole('button', { name: 'Use 3 s' }).first().click();
  await expect(interval(panel)).toHaveValue('3');
  // A lock of 3 seconds has no room for the alarm and a screenshot.
  await away(panel).fill('3');
  await expect(fitOf(panel)).toHaveCount(0);
  await expect(panel.getByRole('button', { name: 'Raise the lock to 8 s' })).toBeVisible();
  // A value out of range that has nothing to do with a conflict is not a conflict.
  await away(panel).fill('300'); await interval(panel).fill('5');
  await beat(panel).fill('2');
  await expect(fitOf(panel)).toHaveCount(0);
});

test('the defaults can be put back, with a note that says so and an Undo, and are saved only when saved', async ({ page }) => {
  const panel = await open(page);
  const restore = panel.getByRole('button', { name: 'Restore the defaults' });
  // The server's own values are the defaults: the per-absence count it works out does not count.
  await expect(restore).toBeDisabled();
  await beat(panel).fill('20'); await away(panel).fill('45'); await students(panel).fill('99'); await grace(panel).fill('90');
  await expect(restore).toBeEnabled();
  await restore.click();
  await expect(beat(panel)).toHaveValue('15');
  await expect(away(panel)).toHaveValue('300');
  await expect(students(panel)).toHaveValue('200');
  await expect(grace(panel)).toHaveValue('30');
  await expect(restore).toBeDisabled();
  await expect(noteOf(panel)).toContainText('The defaults are back. They are saved when you save the settings.');
  expect((await calls(page)).settings).toBeUndefined();
  await noteOf(panel).getByRole('button', { name: 'Undo' }).click();
  await expect(beat(panel)).toHaveValue('20');
  await expect(away(panel)).toHaveValue('45');
  await expect(grace(panel)).toHaveValue('90');
  await expect(noteOf(panel)).toHaveCount(0);
  // Restored again, the values are the server's own, so there is nothing to save.
  await restore.click();
  await expect(panel.getByRole('region', { name: 'Unsaved changes' })).toHaveCount(0);
});

test('a server that holds other values can be given the defaults back', async ({ page }) => {
  await connectTo(page);
  await page.evaluate(() => window.ProctorHarness.settings({ heartbeat_interval_ms: 20000, overdue_threshold_ms: 45000, default_grace_ms: 90000 }));
  await goTo(page, 'Settings');
  const panel = page.getByRole('region', { name: 'Monitoring rules' });
  await expect(beat(panel)).toHaveValue('20');
  await panel.getByRole('button', { name: 'Restore the defaults' }).click();
  await expect(panel.getByRole('region', { name: 'Unsaved changes' })).toContainText('3 unsaved changes');
  await saveOf(panel).click();
  await expect(page.getByRole('status')).toContainText('Settings saved');
  expect((await calls(page)).settings.values).toMatchObject({ heartbeat_interval_ms: 15000, overdue_threshold_ms: 30000, focus_threshold_ms: 5000,
    alert_interval_ms: 2000, capture_interval_ms: 5000, lock_away_after_ms: 300000, lock_no_contact_after_ms: 300000, session_captures: 200, default_grace_ms: 30000 });
});

test('a save that fails leaves the values and the note with its Undo', async ({ page }) => {
  const panel = await open(page);
  await away(panel).fill('8');
  await fitOf(panel).click();
  await expect(noteOf(panel)).toContainText('Fitted to “Lock after away for”');
  await page.evaluate(() => window.ProctorHarness.settingsFails(true));
  await saveOf(panel).click();
  await expect(panel.getByRole('alert')).toContainText('Proctor authorization was rejected.');
  await expect(noteOf(panel)).toContainText('Fitted to “Lock after away for”');
  await expect(alarm(panel)).toHaveValue('3');
  await noteOf(panel).getByRole('button', { name: 'Undo' }).click();
  await expect(alarm(panel)).toHaveValue('5');
  await expect(noteOf(panel)).toHaveCount(0);
});

test('the − and + buttons step a value within its own range, and the bar of unsaved changes saves or discards them', async ({ page }) => {
  const panel = await open(page);
  const bar = panel.getByRole('region', { name: 'Unsaved changes' });
  await expect(bar).toHaveCount(0);
  await panel.getByRole('button', { name: 'Increase Alarm repeats every' }).click();
  await expect(numberField(panel, 'Alarm repeats every (seconds)')).toHaveValue('3');
  // The changed field is marked, and the bar counts the changes.
  await expect(panel.locator('.field').filter({ has: page.getByRole('spinbutton', { name: 'Alarm repeats every (seconds)', exact: true }) }).locator('.changed'))
    .toHaveAttribute('title', 'Changed, not saved yet');
  await expect(bar).toContainText('1 unsaved change');
  await expect(bar).toContainText('Applies to exams published from now on.');
  // The screenshots per student step by ten, the grace period by five seconds.
  await panel.getByRole('button', { name: 'Increase Screenshots per student' }).click();
  await expect(students(panel)).toHaveValue('210');
  await panel.getByRole('button', { name: 'Decrease Default grace period' }).click();
  await expect(grace(panel)).toHaveValue('25');
  await expect(bar).toContainText('3 unsaved changes');
  // A value at the end of its own range cannot be stepped past it, and a value it makes impossible says so in the bar.
  await beat(panel).fill('60');
  await expect(bar).toContainText('Fix the problems first, then save.');
  await expect(panel.getByRole('button', { name: 'Increase Heartbeat every' })).toBeDisabled();
  await expect(panel.getByRole('button', { name: 'Decrease Heartbeat every' })).toBeEnabled();
  // The worked-out count cannot be stepped at all.
  await expect(panel.getByRole('button', { name: 'Increase Screenshots per absence' })).toBeDisabled();
  // Discard puts the server's values back, and the bar goes.
  await bar.getByRole('button', { name: 'Discard' }).click();
  await expect(beat(panel)).toHaveValue('15');
  await expect(numberField(panel, 'Alarm repeats every (seconds)')).toHaveValue('2');
  await expect(students(panel)).toHaveValue('200');
  await expect(grace(panel)).toHaveValue('30');
  await expect(bar).toHaveCount(0);
  expect((await calls(page)).settings).toBeUndefined();
  // Stepped and saved, the value reaches the server.
  await panel.getByRole('button', { name: 'Increase Alarm repeats every' }).click();
  await bar.getByRole('button', { name: 'Save settings' }).click();
  await expect(page.getByRole('status')).toContainText('Settings saved');
  await expect(bar).toHaveCount(0);
  expect((await calls(page)).settings.values).toMatchObject({ alert_interval_ms: 3000 });
});

test('the list of sections goes to a section and marks the ones with a value to fix', async ({ page }) => {
  const panel = await open(page);
  const list = panel.getByRole('navigation', { name: 'Settings sections' });
  await expect(list.getByRole('button')).toHaveText(['Leaving the window', 'Connection', 'Screenshot limits', 'Exam time']);
  await list.getByRole('button', { name: 'Exam time' }).click();
  await expect(panel.getByRole('heading', { name: 'Exam time' })).toBeFocused();
  await expect(panel.getByRole('heading', { name: 'Exam time' })).toBeInViewport();
  // A value to fix marks its section, for screen readers too.
  await beat(panel).fill('2');
  await expect(list.getByRole('button', { name: 'Connection (a value needs fixing)' })).toBeVisible();
  await expect(list.getByRole('button', { name: 'Leaving the window', exact: true })).toBeVisible();
  await beat(panel).fill('15');
  await expect(list.getByRole('button', { name: 'Connection', exact: true })).toBeVisible();
});

test('the timelines draw what the values do, and wait while they do not fit together', async ({ page }) => {
  const panel = await open(page);
  const absence = panel.getByRole('img', { name: /^After a student leaves the exam window/ });
  const silence = panel.getByRole('img', { name: /^After a computer was last heard from/ });
  await expect(absence).toHaveAccessibleName('After a student leaves the exam window, the alarm and the screenshots start at 5 s, a screenshot every 5 s, and the exam locks at 5 min.');
  await expect(silence).toHaveAccessibleName('After a computer was last heard from, its connection counts as lost at 30 s, and the exam locks at 5 min.');
  await expect(absence).toContainText('locks at 5 min');
  // They follow the values.
  await away(panel).fill('30');
  await expect(absence).toHaveAccessibleName(/and the exam locks at 30 s\.$/);
  // A conflict leaves nothing true to draw.
  await away(panel).fill('8');
  await expect(panel.getByText('The timeline appears once these values fit together.')).toBeVisible();
  await expect(absence).toHaveCount(0);
  await expect(silence).toBeVisible();
});
