import { test, expect } from '@playwright/test';

async function monitor(page) {
  await page.goto('/#/monitoring');
  await expect(page.getByRole('table', { name: /All sessions/ })).toBeVisible();
}

test('grace controls confirm one student or everyone and report skipped submissions', async ({ page }) => {
  await monitor(page);
  await page.getByRole('button', { name: 'Extend grace 0001…', exact: true }).click();
  let dialog = page.getByRole('dialog');
  await expect(dialog).toContainText('New grace end:');
  await dialog.getByLabel('Reason').fill('Interrupted computer');
  await expect(dialog.getByRole('button', { name: 'Extend grace', exact: true })).toBeDisabled();
  await dialog.getByRole('checkbox').check();
  await dialog.getByRole('button', { name: 'Extend grace', exact: true }).click();
  await expect(page.getByText('Grace extended: 1 sessions changed', { exact: false })).toBeVisible();
  await page.evaluate(() => window.ProctorHarness.finished(1));
  await page.getByRole('button', { name: 'Extend grace for everyone…', exact: true }).click();
  dialog = page.getByRole('dialog');
  await expect(dialog).toContainText('skips 1 already submitted');
  await expect(dialog).toContainText('Students who log in later');
  await dialog.getByLabel('Reason').fill('Class interruption');
  await dialog.getByRole('checkbox').check();
  await dialog.getByRole('button', { name: 'Extend grace', exact: true }).click();
  await expect(page.getByText('Grace extended: 89 sessions changed, 1 submitted sessions skipped.', { exact: true })).toBeVisible();
  await expect(page.getByText('Grace: 30 s + 5 min added', { exact: false })).toBeVisible();
});

test('end time preview names today or tomorrow; change and clear require confirmation', async ({ page }) => {
  await monitor(page);
  await page.getByRole('button', { name: 'Change end time…' }).click();
  let dialog = page.getByRole('dialog');
  const hhmm = await page.evaluate(() => { const end = new Date(Date.now() + 3600000); return `${String(end.getHours()).padStart(2, '0')}:${String(end.getMinutes()).padStart(2, '0')}`; });
  await dialog.getByLabel('Exam ends at', { exact: false }).fill(hhmm);
  await expect(dialog.getByText(new RegExp(`Ends (today|tomorrow) at ${hhmm}`))).toBeVisible();
  await expect(dialog.getByRole('button', { name: 'Change end time', exact: true })).toBeDisabled();
  await dialog.getByRole('checkbox').check();
  await dialog.getByRole('button', { name: 'Change end time', exact: true }).click();
  await expect(page.getByRole('button', { name: 'Clear end time', exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Clear end time', exact: true }).click();
  dialog = page.getByRole('dialog');
  await expect(dialog).toContainText("Restore each student's own deadline and grace end.");
  await dialog.getByRole('checkbox').check();
  await dialog.getByRole('button', { name: 'Clear end time', exact: true }).click();
  await expect(page.getByText('No exam end time', { exact: false })).toBeVisible();
});
