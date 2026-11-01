import { test, expect } from '@playwright/test';

async function monitor(page) {
  await page.goto('/#/monitoring');
  await expect(page.getByRole('table', { name: /All sessions/ })).toBeVisible();
}

async function openEveryone(page) {
  await page.getByRole('button', { name: 'Extend grace for everyone…', exact: true }).click();
  return page.getByRole('dialog', { name: 'Extend grace for everyone', exact: true });
}

test('minute stepper, presets and typed edits always require a fresh confirmation', async ({ page }) => {
  await monitor(page);
  const dialog = await openEveryone(page);
  const minutes = dialog.getByRole('spinbutton', { name: 'Minutes', exact: true });
  const confirm = dialog.getByRole('checkbox');
  const submit = dialog.getByRole('button', { name: /^Extend grace by/ });
  await expect(dialog.locator('.stat.change .num')).toHaveText('90');
  await expect(dialog.getByRole('button', { name: '5 minutes', exact: true })).toHaveAttribute('aria-pressed', 'true');
  await confirm.check();
  await expect(submit).toBeDisabled(); // A recorded reason remains mandatory.
  await dialog.getByRole('textbox', { name: /Reason/ }).fill('Interrupted computer');
  await expect(submit).toBeEnabled();
  await dialog.getByRole('button', { name: 'Increase minutes' }).click();
  await expect(minutes).toHaveValue('6');
  await expect(confirm).not.toBeChecked();
  await expect(submit).toHaveAccessibleName('Extend grace by 6 min');
  await dialog.getByRole('button', { name: 'Decrease minutes' }).click();
  await expect(minutes).toHaveValue('5');
  await confirm.check();
  await dialog.getByRole('button', { name: '15 minutes', exact: true }).click();
  await expect(minutes).toHaveValue('15');
  await expect(confirm).not.toBeChecked();
  await expect(dialog.getByRole('button', { name: '15 minutes', exact: true })).toHaveAttribute('aria-pressed', 'true');
  await confirm.check();
  await minutes.fill('12');
  await expect(confirm).not.toBeChecked();
  await expect(submit).toBeDisabled();
  await expect(dialog.getByText('I confirm adding 12 min of grace.', { exact: true })).toBeVisible();
  await dialog.getByRole('button', { name: 'Cancel', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Extend grace for everyone…', exact: true })).toBeFocused();
  await expect(page.getByText('Grace: 30 s', { exact: false })).not.toContainText('added');
});

test('minutes are whole numbers within bounds, with presets available to recover invalid input', async ({ page }) => {
  await monitor(page);
  const dialog = await openEveryone(page);
  const minutes = dialog.getByRole('spinbutton', { name: 'Minutes', exact: true });
  await minutes.fill('1');
  await expect(dialog.getByRole('button', { name: 'Decrease minutes' })).toBeDisabled();
  await minutes.fill('60');
  await expect(dialog.getByRole('button', { name: 'Increase minutes' })).toBeDisabled();
  await dialog.getByRole('textbox', { name: /Reason/ }).fill('Class interruption');
  for (const value of ['0', '61', '1.5', '']) {
    await minutes.fill(value);
    await dialog.getByRole('checkbox').check();
    await expect(minutes).toHaveAttribute('aria-invalid', 'true');
    await expect(dialog.getByText('Enter a whole number from 1 to 60.', { exact: true })).toBeVisible();
    await expect(dialog.getByRole('button', { name: /^Extend grace by/ })).toBeDisabled();
  }
  await dialog.getByRole('button', { name: '10 minutes', exact: true }).click();
  await expect(minutes).toHaveValue('10');
  await expect(minutes).toHaveAttribute('aria-invalid', 'false');
  await expect(dialog.getByRole('checkbox')).not.toBeChecked();
  await dialog.getByRole('checkbox').check();
  await dialog.getByRole('button', { name: 'Extend grace by 10 min', exact: true }).click();
  await expect(page.getByText('Grace: 30 s + 10 min added', { exact: false })).toBeVisible();
});

test('no active sessions warns that only later logins receive the extension', async ({ page }) => {
  await monitor(page);
  await page.evaluate(() => { for (let i = 0; i < 90; i++) window.ProctorHarness.finished(i); });
  await expect(page.getByRole('button', { name: 'Extend grace 0001…', exact: true })).toHaveCount(0);
  const dialog = await openEveryone(page);
  await expect(dialog.locator('.stat.change .num')).toHaveText('0');
  await expect(dialog.locator('.stat.skip .num')).toHaveText('90');
  await expect(dialog).toContainText('No active sessions right now. The extra grace will only apply to students who log in later.');
  await dialog.getByRole('textbox', { name: /Reason/ }).fill('For late arrivals');
  await dialog.getByRole('checkbox').check();
  await dialog.getByRole('button', { name: 'Extend grace by 5 min', exact: true }).click();
  await expect(page.getByText('Grace extended: 0 sessions changed, 90 submitted sessions skipped.', { exact: true })).toBeVisible();
});

for (const width of [1000, 390]) {
  test(`mock-style header, cards and footer fit at ${width}px`, async ({ page }, testInfo) => {
    await page.setViewportSize({ width, height: 800 });
    await monitor(page);
    const dialog = await openEveryone(page);
    if (width === 1000) await expect(dialog).toHaveCSS('width', '500px');
    else expect((await dialog.boundingBox()).width).toBeLessThan(width - 20);
    await expect(dialog.locator('header')).toHaveCSS('color', 'rgb(255, 255, 255)');
    await expect(dialog.locator('header')).toHaveCSS('background-image', /linear-gradient/);
    await expect(dialog.locator('.stat.change')).toHaveCSS('background-color', 'rgb(227, 246, 250)');
    await expect(dialog.locator('.actions')).toHaveCSS('background-color', 'rgb(253, 246, 227)');
    expect(await dialog.evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true);
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    await dialog.screenshot({ path: testInfo.outputPath('grace-dialog.png') });
    await dialog.getByRole('button', { name: 'Cancel', exact: true }).scrollIntoViewIfNeeded();
    await expect(dialog.getByRole('button', { name: 'Cancel', exact: true })).toBeInViewport();
    await page.keyboard.press('Escape');
    await expect(dialog).toHaveCount(0);
    await expect(page.getByRole('button', { name: 'Extend grace for everyone…', exact: true })).toBeFocused();
  });
}
