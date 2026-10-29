import { test, expect } from '@playwright/test';

const capture = {
  enabled: true,
  permission: { supported: true, granted: false, test_required: true, message: 'Wayland portal capture requires verification.' },
  state: 'idle', pending: 0, local_incident: 0, event_log_id: 0, screenshot_id: 0, failures: 0, error: '',
};

async function start(page) {
  await page.goto('/');
  await page.evaluate(value => window.__studentHarness.capture(value), capture);
  await page.getByLabel('I have read these rules').check();
  await page.getByLabel('Account number').fill('00123'); await page.getByLabel('Exam PIN').fill('synthetic PIN');
  return { access: page.getByRole('region', { name: 'Screenshot monitoring', exact: true }), login: page.getByRole('button', { name: 'Login / resume' }), toasts: page.getByRole('region', { name: 'Notifications' }) };
}
const checks = page => page.evaluate(() => window.__studentHarness.captureCalls || 0);
const serverLogins = page => page.evaluate(() => window.__studentHarness.loginRequests);

test('the login explains the check, starts none on its own, and the click verifies before the exam opens', async ({ page }) => {
  const { access, login } = await start(page);
  await expect(access).toContainText('Wayland portal capture requires verification');
  await expect(access).toContainText('takes one test screenshot');
  await expect(access.getByRole('button')).toHaveCount(0);
  await page.waitForTimeout(600);
  expect(await checks(page)).toBe(0);
  await login.click();
  await expect(page.getByText('Fixture exam')).toBeVisible();
  expect(await checks(page)).toBe(1);
  expect(await serverLogins(page)).toBe(1);
});

test('while the capture dialog is open the login says so, cannot be repeated, and can be cancelled', async ({ page }) => {
  let { login, toasts } = await start(page);
  await page.evaluate(() => window.__studentHarness.captureBehavior('hold'));
  await login.click();
  login = page.locator('#student-login button.primary'); // the same button, renamed while it checks
  await expect(login).toHaveText('Checking screen capture…');
  await expect(login).toBeDisabled();
  await expect(page.getByRole('status').filter({ hasText: 'allow it' })).toBeVisible();
  await expect(page.getByLabel('Account number')).toBeDisabled();
  await login.click({ force: true });
  expect(await checks(page)).toBe(1); // a second click started neither another capture nor another login
  await page.getByRole('button', { name: 'Cancel', exact: true }).click();
  await expect(toasts).toContainText('capture check was cancelled');
  await expect(login).toBeEnabled();
  expect(await serverLogins(page)).toBe(0);
  await expect(page.getByLabel('Exam PIN')).toHaveValue('synthetic PIN'); // the server never saw it, so it need not be typed again
  await page.evaluate(() => window.__studentHarness.captureBehavior('success'));
  await login.click();
  await expect(page.getByText('Fixture exam')).toBeVisible();
  expect(await checks(page)).toBe(2);
});

test('a failed or unusable capture keeps the exam closed with a toast, and the same button tries again', async ({ page }) => {
  const { login, toasts } = await start(page);
  for (const behavior of ['failure', 'invalid']) {
    await page.evaluate(value => window.__studentHarness.captureBehavior(value), behavior);
    await login.click();
    await expect(page.getByRole('alert')).toContainText('exam did not start');
    await expect(login).toBeEnabled();
    await expect(page.getByLabel('Account number')).toBeEnabled();
    await expect(page.getByRole('textbox', { name: 'Answer for q1' })).toHaveCount(0);
    expect(await serverLogins(page)).toBe(0);
    await toasts.getByRole('button', { name: 'Dismiss error' }).click();
  }
  await page.evaluate(() => window.__studentHarness.captureBehavior('success'));
  await login.click();
  await expect(page.getByText('Fixture exam')).toBeVisible();
  expect(await serverLogins(page)).toBe(1);
});

test('capture failure locks current work, preserves autosave and timer, and requires an acknowledged retest', async ({ page }) => {
  const { login } = await start(page);
  await login.click();
  const editor = page.getByRole('textbox', { name: 'Answer for q1' });
  await editor.fill('preserved through capture failure');
  await page.evaluate(value => window.__studentHarness.capture(value), { state: 'failed', failures: 1, ready: false, blocked: true, pending_reports: 1 });
  await expect(editor).not.toHaveAttribute('contenteditable', 'true');
  await expect(editor).toContainText('preserved through capture failure');
  await expect(page.getByLabel('Save status')).toContainText('Saved');
  await expect(page.getByRole('button', { name: 'Test screenshot capture' })).toBeDisabled();
  await expect(page.getByRole('region', { name: 'Server timing' })).toContainText('00:01:00');
  await page.evaluate(value => window.__studentHarness.capture(value), { pending_reports: 0 });
  await page.getByRole('button', { name: 'Test screenshot capture' }).click();
  await expect(editor).toHaveAttribute('contenteditable', 'true');
  await expect(editor).toContainText('preserved through capture failure');
  await editor.fill('work continues after retest');
  await expect(page.getByLabel('Save status')).toContainText('Saved');
});

test('a failed retest during the exam replaces the previous success and permits retry', async ({ page }) => {
  const { login } = await start(page);
  await login.click();
  await page.evaluate(value => window.__studentHarness.capture(value), { state: 'failed', failures: 1, ready: false, blocked: true });
  const result = page.getByLabel('Screenshot test result');
  const button = page.getByRole('button', { name: 'Test screenshot capture' });
  for (const behavior of ['failure', 'invalid']) {
    await page.evaluate(value => window.__studentHarness.captureBehavior(value), behavior);
    await button.click();
    await expect(result).toContainText('Screenshot capture failed');
    await expect(result).toHaveAttribute('role', 'alert');
    await expect(button).toBeEnabled();
  }
  await page.evaluate(() => window.__studentHarness.captureBehavior('success'));
  await button.click();
  await expect(button).toHaveCount(0); // the retest is no longer needed: editing resumes
  await expect(page.getByRole('textbox', { name: 'Answer for q1' })).toHaveAttribute('contenteditable', 'true');
});
