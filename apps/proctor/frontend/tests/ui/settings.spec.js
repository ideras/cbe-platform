import { test, expect } from '@playwright/test';
import { connect as connectTo, goTo, nav, calls } from './helpers.js';

test('the Settings page shows the values, the worked-out count and the preview, and saves them', async ({ page }) => {
  await connectTo(page);
  await goTo(page, 'Settings');
  const panel = page.getByRole('region', { name: 'Monitoring settings' });
  await expect(panel).toBeVisible();
  await expect(panel.getByText('The published exam keeps the rules it was published with. Changes apply to exams published from now on.')).toBeVisible();
  // The values the server holds are shown.
  await expect(panel.getByLabel('Heartbeat every (seconds)')).toHaveValue('15');
  await expect(panel.getByLabel('Screenshot every (seconds)')).toHaveValue('5');
  await expect(panel.getByLabel('Lock after away for (seconds, 0 = off)')).toHaveValue('300');
  await expect(panel.getByLabel('Lock after no contact for (seconds, 0 = off)')).toHaveValue('300');
  // With the away lock on, the per-absence count is worked out and the field cannot be edited.
  await expect(panel.getByText('the screenshots per absence are worked out')).toBeVisible();
  await expect(panel.getByLabel('Screenshots per absence')).toBeDisabled();
  // The preview says what the students will cost.
  await expect(page.getByRole('region', { name: 'Preview' })).toContainText('Up to 60 screenshots per absence, at most 200 per student');
  // A value outside its range is refused, and the problem is named.
  await panel.getByLabel('Heartbeat every (seconds)').fill('2');
  await expect(panel.getByRole('alert')).toContainText('Heartbeat every must be between 5 and 60 seconds (it is 2).');
  await expect(panel.getByRole('button', { name: 'Save settings' })).toBeDisabled();
  await panel.getByLabel('Heartbeat every (seconds)').fill('15');
  await expect(panel.getByRole('alert')).toHaveCount(0);
  await expect(panel.getByRole('button', { name: 'Save settings' })).toBeEnabled();
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
  const panel = page.getByRole('region', { name: 'Monitoring settings' });
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
  await expect(panel.getByLabel('Lock after away for (seconds, 0 = off)')).toHaveAttribute('aria-invalid', 'true');
  // Raising the lock as well makes the combination valid again.
  await panel.getByLabel('Lock after away for (seconds, 0 = off)').fill('360');
  await expect(panel.getByRole('alert')).toHaveCount(0);
  await expect(panel.getByRole('button', { name: 'Save settings' })).toBeEnabled();
});

const away = panel => panel.getByLabel('Lock after away for (seconds, 0 = off)');
const noContact = panel => panel.getByLabel('Lock after no contact for (seconds, 0 = off)');
const said = (panel, label) => panel.locator('label', { hasText: label }).locator('.said');

test('the locks are set in seconds, and the page says in minutes what a long one is', async ({ page }) => {
  await connectTo(page);
  await goTo(page, 'Settings');
  const panel = page.getByRole('region', { name: 'Monitoring settings' });
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
  const panel = page.getByRole('region', { name: 'Monitoring settings' });
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
  await expect(panel.getByText('2 per absence')).toBeVisible();
  await panel.getByRole('button', { name: 'Save settings' }).click();
  await expect(page.getByRole('status')).toContainText('Settings saved');
  const sent = await calls(page);
  expect(sent.settings.values).toMatchObject({ lock_away_after_ms: 10000, lock_no_contact_after_ms: 31000 });
});

test('a lock is off at zero, a whole number of seconds, and not more than an hour', async ({ page }) => {
  await connectTo(page);
  await goTo(page, 'Settings');
  const panel = page.getByRole('region', { name: 'Monitoring settings' });
  const save = panel.getByRole('button', { name: 'Save settings' });
  // Zero turns the away lock off, which gives the per-absence screenshots back to the proctor.
  await away(panel).fill('0');
  await expect(panel.getByRole('alert')).toHaveCount(0);
  await expect(panel.getByLabel('Screenshots per absence')).toBeEnabled();
  await noContact(panel).fill('0'); await expect(panel.getByRole('alert')).toHaveCount(0);
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
  await panel.getByLabel('Screenshots per student').fill('2000');
  await expect(panel.getByRole('alert')).toHaveCount(0);
  await expect(save).toBeEnabled();
});

test('a lock the server holds in a part of a minute is shown and saved as it is, not rounded to a minute', async ({ page }) => {
  await connectTo(page);
  await page.evaluate(() => window.ProctorHarness.settings({ lock_away_after_ms: 90000, lock_no_contact_after_ms: 45000 }));
  await goTo(page, 'Settings');
  const panel = page.getByRole('region', { name: 'Monitoring settings' });
  await expect(away(panel)).toHaveValue('90'); await expect(noContact(panel)).toHaveValue('45');
  await panel.getByRole('button', { name: 'Save settings' }).click();
  await expect(page.getByRole('status')).toContainText('Settings saved');
  expect((await calls(page)).settings.values).toMatchObject({ lock_away_after_ms: 90000, lock_no_contact_after_ms: 45000 });
});

test('an empty value is refused instead of being read as zero', async ({ page }) => {
  await connectTo(page);
  await goTo(page, 'Settings');
  const panel = page.getByRole('region', { name: 'Monitoring settings' });
  await panel.getByLabel('Alarm repeats every (seconds)').fill('');
  await expect(panel.getByRole('alert')).toContainText('Alarm repeats every is empty');
  await expect(panel.getByRole('button', { name: 'Save settings' })).toBeDisabled();
  await panel.getByLabel('Alarm repeats every (seconds)').fill('2');
  await expect(panel.getByRole('alert')).toHaveCount(0);
});

test('a save that another proctor made first reloads the page and says so', async ({ page }) => {
  await connectTo(page);
  await goTo(page, 'Settings');
  const panel = page.getByRole('region', { name: 'Monitoring settings' });
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
  const panel = page.getByRole('region', { name: 'Monitoring settings' });
  await expect(panel.getByLabel('Screenshot every (seconds)')).toHaveValue('5');
  await goTo(page, 'Classes');
  await page.evaluate(() => window.ProctorHarness.settings({ capture_interval_ms: 10000 }));
  await goTo(page, 'Settings');
  await expect(page.getByRole('region', { name: 'Monitoring settings' }).getByLabel('Screenshot every (seconds)')).toHaveValue('10');
});

test('a failed read of the settings can be tried again', async ({ page }) => {
  await connectTo(page);
  await page.evaluate(() => window.ProctorHarness.settingsFails(true));
  await goTo(page, 'Settings');
  const panel = page.getByRole('region', { name: 'Monitoring settings' });
  await expect(panel.getByRole('alert')).toContainText('authorization was rejected');
  await page.evaluate(() => window.ProctorHarness.settingsFails(false));
  await panel.getByRole('button', { name: 'Try again' }).click();
  await expect(panel.getByLabel('Heartbeat every (seconds)')).toHaveValue('15');
});
