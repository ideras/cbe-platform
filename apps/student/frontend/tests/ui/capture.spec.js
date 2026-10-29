import { test, expect } from '@playwright/test';

test('capture failure locks editing for retest without losing answer durability', async ({ page }) => {
  await page.goto('/');
  const capture = { enabled: true, permission: { supported: true, granted: false, message: 'Grant Screen Recording permission, then fully quit/relaunch and notify the instructor.' }, state: 'idle', pending: 0, local_incident: 0, event_log_id: 0, screenshot_id: 0, failures: 0, error: '' };
  await page.evaluate(value => window.__studentHarness.capture(value), capture);
  await expect(page.getByRole('region', { name: 'Participation rules' })).toContainText('deleted after grading');
  await expect(page.getByRole('region', { name: 'Participation rules' })).toContainText('including anything else open on them');
  await expect(page.getByRole('region', { name: 'Screenshot monitoring' })).toContainText('Grant Screen Recording permission');
  await expect(page.getByRole('region', { name: 'Screenshot monitoring' }).getByRole('alert')).toHaveCount(0);
  await page.getByLabel('I have read these rules').check();
  await page.getByLabel('Account number').fill('00123'); await page.getByLabel('Exam PIN').fill('synthetic PIN');
  await page.getByRole('button', { name: 'Login / resume' }).click(); await expect(page.getByText('Fixture exam')).toBeVisible();
  await page.evaluate(value => window.__studentHarness.capture(value), { ...capture, state: 'failed', failures: 1, error: 'Screenshot upload was not confirmed. Current answers are preserved; editing requires a retest.' });
  await expect(page.getByRole('region', { name: 'Screenshot monitoring' })).toContainText('Unconfirmed/failed screenshot incidents: 1');
  await expect(page.getByRole('textbox', { name: 'Answer for q1' })).not.toHaveAttribute('contenteditable', 'true');
  await page.getByRole('button', { name: 'Test screenshot capture' }).click();
  await expect(page.getByRole('textbox', { name: 'Answer for q1' })).toHaveAttribute('contenteditable', 'true');
  await page.getByRole('textbox', { name: 'Answer for q1' }).fill('saved after capture retest');
  await expect(page.getByLabel('Save status')).toContainText('Saved');
  await page.evaluate(value => { window.__studentHarness.capture(value); window.__studentHarness.status({ status: 'force_submitted', seconds_remaining: 0 }); }, { ...capture, state: 'uploading', pending: 1, local_incident: 1, event_log_id: 7 });
  await expect(page.getByText('Submission closed')).toBeVisible();
  await expect(page.getByRole('status').filter({ hasText: 'A screenshot is still being uploaded (1 pending)' })).toBeVisible();
  await expect(page.getByRole('textbox', { name: 'Answer for q1' })).toHaveCount(0);
});
