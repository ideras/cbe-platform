import { test, expect } from '@playwright/test';

const capture = {
  enabled: true,
  permission: { supported: true, granted: false, test_required: true, message: 'Wayland portal capture requires verification.' },
  state: 'idle', pending: 0, local_incident: 0, event_log_id: 0, screenshot_id: 0, failures: 0, error: '',
};

async function start(page) {
  await page.goto('/');
  await page.evaluate(value => window.__studentHarness.capture(value), capture);
  const access = page.getByRole('region', { name: 'Screenshot monitoring', exact: true });
  return { access, result: access.getByLabel('Screenshot test result'), button: access.getByRole('button', { name: 'Test screenshot capture' }) };
}

test('real Wayland test success arms capture but still requires separate disclosure acceptance', async ({ page }) => {
  const { access, result, button } = await start(page);
  expect(await page.evaluate(() => window.__studentHarness.captureCalls || 0)).toBe(0);
  await expect(result).toContainText('Required before you can log in');
  await button.click();
  await expect(result).toContainText('Screenshot capture succeeded (1920 × 1080)');
  await expect(result).toContainText('Test image discarded; nothing uploaded');
  await expect(result).toHaveCSS('color', 'rgb(30, 94, 88)');
  await expect(access.getByRole('alert')).toHaveCount(0);
  await expect(access.getByRole('note')).toHaveCount(0);
  await expect(page.getByRole('checkbox')).not.toBeChecked();
  await expect(page.getByRole('button', { name: 'Login / resume' })).toBeDisabled();
  await page.waitForTimeout(1100);
  expect(await page.evaluate(() => window.__studentHarness.captureCalls)).toBe(1);
  expect(await page.evaluate(() => window.__studentHarness.permissionChecks)).toBe(0);
  await expect(result).toHaveCSS('color', 'rgb(30, 94, 88)');
});

test('a failed or malformed retest replaces previous green success and permits retry', async ({ page }) => {
  const { result, button } = await start(page);
  await button.click(); await expect(result).toContainText('Screenshot capture succeeded');
  for (const behavior of ['failure', 'invalid']) {
    await page.evaluate(value => window.__studentHarness.captureBehavior(value), behavior);
    await button.click();
    await expect(result).toContainText('Screenshot capture failed');
    await expect(result).toHaveAttribute('role', 'alert');
    await expect(result).toHaveCSS('color', 'rgb(176, 69, 44)');
    await expect(button).toBeEnabled();
  }
  await page.evaluate(() => window.__studentHarness.captureBehavior('success'));
  await button.click(); await expect(result).toContainText('Screenshot capture succeeded');
  await expect(result).toHaveAttribute('role', 'status');
});

test('pending screenshot test disables repeat capture and admission, supports cancellation and retry', async ({ page }) => {
  const { access, result, button } = await start(page);
  await page.getByRole('checkbox').check();
  await page.evaluate(() => window.__studentHarness.captureBehavior('hold'));
  await button.click();
  await expect(result).toContainText('Capturing…');
  await expect(access.getByRole('button', { name: 'Testing screenshot capture…' })).toBeDisabled();
  await expect(page.getByRole('button', { name: 'Login / resume' })).toBeDisabled();
  await expect(page.getByLabel('Account number')).toBeDisabled();
  await access.getByRole('button', { name: 'Cancel capture' }).click();
  await expect(result).toContainText('Screenshot test cancelled. No image saved');
  await expect(button).toBeEnabled();
  await expect(page.getByRole('button', { name: 'Login / resume' })).toBeDisabled();
  expect(await page.evaluate(() => window.__studentHarness.captureCalls)).toBe(1);
  await page.evaluate(() => window.__studentHarness.captureBehavior('success'));
  await button.click(); await expect(result).toContainText('Screenshot capture succeeded');
});

test('capture failure locks current work, preserves autosave and timer, and requires an acknowledged retest', async ({ page }) => {
  const { button } = await start(page);
  await button.click();
  await page.getByRole('checkbox').check();
  await page.getByLabel('Account number').fill('00123'); await page.getByLabel('Exam PIN').fill('synthetic PIN');
  await page.getByRole('button', { name: 'Login / resume' }).click();
  const editor = page.getByRole('textbox', { name: 'Answer for q1' });
  await editor.fill('preserved through capture failure');
  await page.evaluate(value => window.__studentHarness.capture(value), { state: 'failed', failures: 1, ready: false, blocked: true, pending_reports: 1 });
  await expect(editor).not.toHaveAttribute('contenteditable', 'true');
  await expect(editor).toContainText('preserved through capture failure');
  await expect(page.getByLabel('Save status')).toContainText('Saved');
  await expect(page.getByRole('button', { name: 'Test screenshot capture' })).toBeDisabled();
  await expect(page.getByRole('region', { name: 'Server timing' })).toContainText('60 seconds');
  await page.evaluate(value => window.__studentHarness.capture(value), { pending_reports: 0 });
  await page.getByRole('button', { name: 'Test screenshot capture' }).click();
  await expect(editor).toHaveAttribute('contenteditable', 'true');
  await expect(editor).toContainText('preserved through capture failure');
  await editor.fill('work continues after retest');
  await expect(page.getByLabel('Save status')).toContainText('Saved');
});
