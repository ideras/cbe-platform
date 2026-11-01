import { test, expect } from '@playwright/test';

test.use({ timezoneId: 'UTC' });

async function monitor(page, time = '2035-01-01T00:00:00Z') {
  await page.clock.install({ time: new Date(time) });
  await page.goto('/#/monitoring');
  await expect(page.getByRole('table', { name: /All sessions/ })).toBeVisible();
}
async function open(page) {
  await page.getByRole('button', { name: 'Change end time…', exact: true }).click();
  return page.getByRole('dialog');
}
const timeInput = dialog => dialog.getByLabel('Exam ends at (local time)', { exact: true });
const submit = dialog => dialog.locator('button[type="submit"]');

test('comparison, presets and mode changes require explicit fresh confirmation', async ({ page }) => {
  await monitor(page);
  const dialog = await open(page);
  await expect(dialog.locator('.compare .side').first()).toContainText('No end time');
  await expect(timeInput(dialog)).toHaveValue('02:05');
  await expect(dialog.locator('.eta')).toContainText('in 2 h 05 min');
  await expect(dialog).toContainText('Ends today at 02:05');
  await expect(submit(dialog)).toBeDisabled();
  await dialog.getByRole('checkbox').check();
  await expect(submit(dialog)).toBeEnabled();
  await dialog.getByRole('button', { name: 'Add 15 minutes', exact: true }).click();
  await expect(timeInput(dialog)).toHaveValue('02:20');
  await expect(dialog.getByRole('checkbox')).not.toBeChecked();
  await dialog.getByRole('checkbox').check();
  await dialog.getByRole('button', { name: 'Add 30 minutes', exact: true }).click();
  await expect(timeInput(dialog)).toHaveValue('02:50');
  await expect(dialog.getByRole('checkbox')).not.toBeChecked();
  await dialog.getByRole('button', { name: 'Add 60 minutes', exact: true }).click();
  await expect(timeInput(dialog)).toHaveValue('03:50');
  await dialog.getByRole('checkbox').check();
  await timeInput(dialog).fill('04:00');
  await expect(dialog.getByRole('checkbox')).not.toBeChecked();
  await dialog.getByRole('button', { name: 'No end time', exact: true }).click();
  await expect(timeInput(dialog)).toHaveCount(0);
  await expect(dialog).toContainText('Sessions end only by their own exam time plus grace.');
  await dialog.getByRole('checkbox').check();
  await expect(submit(dialog)).toHaveText('No change');
  await expect(submit(dialog)).toBeDisabled();
  await dialog.getByRole('button', { name: 'At a set time', exact: true }).click();
  await expect(timeInput(dialog)).toHaveValue('04:00');
  await expect(dialog.getByRole('checkbox')).not.toBeChecked();
  await dialog.getByRole('button', { name: 'Cancel', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Change end time…', exact: true })).toBeFocused();
  await expect(page.getByText('No exam end time', { exact: false })).toBeVisible();
});

test('an existing end is shown; unchanged settings are disabled and no-end mode removes it', async ({ page }) => {
  await monitor(page);
  let dialog = await open(page);
  await dialog.getByRole('checkbox').check();
  await submit(dialog).click();
  await expect(dialog).toHaveCount(0);
  dialog = await open(page);
  await expect(timeInput(dialog)).toHaveValue('02:05');
  await expect(dialog.locator('.compare .side').first()).not.toContainText('No end time');
  await expect(submit(dialog)).toHaveText('No change');
  await dialog.getByRole('checkbox').check();
  await expect(submit(dialog)).toBeDisabled();
  await dialog.getByRole('button', { name: 'No end time', exact: true }).click();
  await expect(dialog.getByRole('checkbox')).not.toBeChecked();
  await expect(submit(dialog)).toHaveText('Remove end time');
  await expect(submit(dialog)).toBeDisabled();
  await dialog.getByRole('checkbox').check();
  await submit(dialog).click();
  await expect(page.getByText('No exam end time', { exact: false })).toBeVisible();
});

test('midnight follows the next occurrence rule; near ends warn and sub-minute ends cannot be submitted', async ({ page }) => {
  await monitor(page, '2035-01-01T23:55:30Z');
  const dialog = await open(page);
  await timeInput(dialog).fill('18:00');
  await expect(dialog).toContainText('Ends tomorrow at 18:00');
  await dialog.getByRole('button', { name: 'Add 60 minutes', exact: true }).click();
  await expect(timeInput(dialog)).toHaveValue('19:00');
  await expect(dialog).toContainText('Ends tomorrow at 19:00');
  await timeInput(dialog).fill('23:58');
  await expect(dialog).toContainText('Less than 10 minutes from now.');
  await timeInput(dialog).fill('23:56');
  await expect(dialog).toContainText('Choose an end time at least one minute from now.');
  await dialog.getByRole('checkbox').check();
  await expect(submit(dialog)).toBeDisabled();
  await timeInput(dialog).fill('23:55');
  await expect(dialog).toContainText('Ends tomorrow at 23:55');
  // Adding past the 24-hour bound must not silently wrap to an earlier end.
  await expect(dialog.getByRole('button', { name: 'Add 15 minutes', exact: true })).toBeDisabled();
  await expect(dialog).toContainText('no grace after the exam end');
  await expect(dialog).not.toContainText('grace still applies');
});

test('failed preview cannot be confirmed and clock warnings survive the new layout', async ({ page }) => {
  await monitor(page);
  await page.evaluate(() => window.ProctorHarness.endTimePreviewFailure('Choose an end time at least one minute away.'));
  const dialog = await open(page);
  await expect(dialog.getByRole('alert')).toContainText('at least one minute away');
  await dialog.getByRole('checkbox').check();
  await expect(submit(dialog)).toBeDisabled();
  await page.evaluate(() => { window.ProctorHarness.endTimePreviewFailure(''); window.ProctorHarness.endTimeClockWarning(true); });
  await timeInput(dialog).fill('03:00');
  await expect(dialog).toContainText("The server and this computer's clocks differ by more than one minute.");
  await expect(dialog.getByRole('checkbox')).not.toBeChecked();
  await dialog.getByRole('checkbox').check();
  await expect(submit(dialog)).toBeEnabled();
  await timeInput(dialog).fill('');
  await expect(submit(dialog)).toBeDisabled();
  await dialog.getByRole('button', { name: 'Add 15 minutes', exact: true }).click();
  await expect(timeInput(dialog)).toHaveValue('00:15');
});

for (const width of [1000, 390]) {
  test(`end-time mock fits at ${width}px with a framed header and footer`, async ({ page }, testInfo) => {
    await page.setViewportSize({ width, height: 850 });
    await monitor(page);
    const dialog = await open(page);
    await expect(dialog).toContainText('Ends today at 02:05');
    if (width === 1000) await expect(dialog).toHaveCSS('width', '520px');
    await expect(dialog.locator('header')).toHaveCSS('background-image', /linear-gradient/);
    await expect(dialog.locator('.scope .yes')).toHaveCSS('background-color', 'rgb(227, 246, 250)');
    await expect(dialog.locator('.actions')).toHaveCSS('background-color', 'rgb(253, 246, 227)');
    expect(await dialog.evaluate(el => el.scrollWidth <= el.clientWidth)).toBe(true);
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    await dialog.screenshot({ path: testInfo.outputPath('end-time-dialog.png') });
    await dialog.getByRole('button', { name: 'Cancel', exact: true }).scrollIntoViewIfNeeded();
    await expect(dialog.getByRole('button', { name: 'Cancel', exact: true })).toBeInViewport();
    await page.keyboard.press('Escape');
    await expect(dialog).toHaveCount(0);
    await expect(page.getByRole('button', { name: 'Change end time…', exact: true })).toBeFocused();
  });
}
