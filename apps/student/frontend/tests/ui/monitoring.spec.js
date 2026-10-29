import { test, expect } from '@playwright/test';

test('disclosure gates participation, bundled audio deduplicates, and monitoring failures do not lock editor', async ({ page }) => {
  await page.addInitScript(() => {
    window.__plays = 0;
    HTMLMediaElement.prototype.play = function () { window.__plays++; return Promise.resolve(); };
    HTMLMediaElement.prototype.pause = function () {};
  });
  await page.goto('/');
  const login = page.getByRole('button', { name: 'Login / resume' });
  await expect(login).toBeDisabled();
  await expect(page.getByRole('region', { name: 'Participation rules' })).toContainText('Stay in this window until you submit');
  await expect(page.getByRole('region', { name: 'Participation rules' })).toContainText('when you leave this window');
  await expect(page.getByRole('region', { name: 'Participation rules' })).toContainText('turns it up during the exam and restores it afterwards');
  await page.getByLabel('I have read these rules').check();
  await page.getByLabel('Account number').fill('00123'); await page.getByLabel('Exam PIN').fill('synthetic PIN'); await login.click();
  await expect(page.getByText('Fixture exam')).toBeVisible();
  await page.evaluate(() => { window.__studentHarness.alert(1); window.__studentHarness.alert(1); window.__studentHarness.alert(2); });
  expect(await page.evaluate(() => window.__plays)).toBe(3);
  await page.evaluate(() => window.__studentHarness.monitoring({ active: true, policy: { heartbeat_interval_ms: 15000, focus_threshold_ms: 20000 }, policy_source: 'server', local_focus: 'unknown', heartbeat_error: 'Heartbeat reporting unavailable', event_error: 'Event delivery failed', dropped_events: 2 }));
  for (const problem of ['Heartbeat reporting unavailable', 'Event delivery failed', 'History may be incomplete']) await expect(page.getByRole('region', { name: 'Notifications' }).getByRole('status').filter({ hasText: problem })).toBeVisible();
  await page.getByRole('textbox', { name: 'Answer for q1' }).fill('saved despite monitoring failure');
  await expect(page.getByLabel('Save status')).toContainText('Saved');
});
