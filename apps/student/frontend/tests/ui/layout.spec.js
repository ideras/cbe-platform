import { test, expect } from '@playwright/test';

const permission = {
  enabled: true,
  permission: { supported: true, granted: false, message: 'Screen Recording permission is required. Check access before participating.' },
  state: 'idle', pending: 0, local_incident: 0, event_log_id: 0, screenshot_id: 0, failures: 0, error: '',
};

for (const viewport of [{ width: 1280, height: 800 }, { width: 1000, height: 700 }, { width: 390, height: 844 }]) {
  test(`compact branded login keeps disclosures and permissions together at ${viewport.width}px`, async ({ page }, info) => {
    await page.setViewportSize(viewport);
    await page.emulateMedia({ colorScheme: 'dark', reducedMotion: 'reduce' });
    const errors = []; const external = [];
    page.on('pageerror', error => errors.push(error.message));
    page.on('request', request => { if (!request.url().startsWith('http://127.0.0.1:4178/')) external.push(request.url()); });
    await page.goto('/');
    await page.evaluate(value => window.__studentHarness.capture(value), permission);
    const rules = page.getByRole('region', { name: 'Participation rules', exact: true });
    const login = page.getByRole('region', { name: 'Login', exact: true });
    const access = rules.getByRole('region', { name: 'Screenshot monitoring', exact: true });
    await expect(access).toContainText('Screen Recording permission is required');
    await expect(page.getByRole('region', { name: 'Screenshot monitoring', exact: true })).toHaveCount(1);
    await expect(page.getByRole('img', { name: 'Computer Based Evaluation desktop logo' })).toBeVisible();
    await expect(page.locator('.brand').getByText('CBE Portal', { exact: true })).toBeVisible();
    await expect(page).toHaveTitle('CBE Portal');
    await expect(page.getByRole('heading', { level: 1, name: 'CBE Portal' })).toHaveCount(1); // present for screen readers, not drawn
    await expect(page.locator('header:not(.card-head)')).toHaveCount(0); // no page-level title bar
    expect(await page.getByRole('img').evaluate(image => image.complete && image.naturalWidth > 0)).toBe(true);
    const left = await rules.boundingBox(); const right = await login.boundingBox();
    if (viewport.width > 720) {
      expect(left.x + left.width).toBeLessThan(right.x);
      await expect(page.getByRole('button', { name: 'Login / resume' })).toBeInViewport();
    } else {
      expect(left.y + left.height).toBeLessThan(right.y);
    }
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    for (const label of ['Server address', 'Account number', 'Exam PIN']) {
      const height = (await page.getByLabel(label).boundingBox()).height;
      expect(height).toBeGreaterThanOrEqual(28); expect(height).toBeLessThanOrEqual(34);
    }
    const submit = page.getByRole('button', { name: 'Login / resume' });
    await expect(submit).toBeDisabled();
    await access.getByRole('button', { name: 'Test screenshot capture' }).click();
    await expect(access.getByLabel('Screenshot test result')).toContainText('Screenshot capture succeeded');
    expect(await page.evaluate(() => window.__studentHarness.captureCalls)).toBe(1);
    await page.screenshot({ path: info.outputPath('login.png'), fullPage: true });
    await rules.getByRole('checkbox').check();
    await expect(submit).toBeEnabled();
    await page.getByLabel('Account number').fill('00123');
    await page.getByLabel('Exam PIN').fill('synthetic PIN');
    await submit.click();
    await expect(page.getByText('Fixture exam')).toBeVisible();
    await page.getByRole('textbox', { name: 'Answer for q1' }).fill('Work remains editable with compact controls.');
    await expect(page.getByLabel('Save status')).toContainText('Saved');
    expect(external).toEqual([]); expect(errors).toEqual([]);
  });
}
