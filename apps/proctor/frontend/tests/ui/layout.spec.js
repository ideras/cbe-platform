import { test, expect } from '@playwright/test';
import { connect, card, admitting, tab } from './helpers.js';

for (const viewport of [{ width: 1280, height: 800 }, { width: 600, height: 700 }]) {
  test(`compact controls, roster and dialogs remain readable at ${viewport.width}px`, async ({ page }, info) => {
    await page.setViewportSize(viewport);
    await page.goto('/#/monitoring');
    const rows = page.locator('table:has(caption:has-text("All sessions")) tbody tr');
    await expect(rows).toHaveCount(90);
    const row = rows.first();
    await expect(row).toContainText('Network overdue'); await expect(row).toContainText('Sustained focus loss');
    expect(await row.locator('td').first().evaluate(cell => parseFloat(getComputedStyle(cell).paddingTop))).toBeLessThanOrEqual(10);
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    await page.screenshot({ path: info.outputPath('roster.png') });
    await page.getByRole('button', { name: 'Force-submit 0001…', exact: true }).click();
    const dialog = page.getByRole('dialog');
    await expect(dialog).toBeVisible();
    const bounds = await dialog.boundingBox(); expect(bounds.x).toBeGreaterThanOrEqual(0); expect(bounds.x + bounds.width).toBeLessThanOrEqual(viewport.width);
    await expect(dialog.getByRole('button', { name: 'Cancel', exact: true })).toBeInViewport();
    expect((await dialog.getByRole('button', { name: 'Cancel', exact: true }).boundingBox()).height).toBeLessThanOrEqual(40);
    await page.screenshot({ path: info.outputPath('confirmation.png') });
    await dialog.getByRole('button', { name: 'Cancel', exact: true }).click();
    expect(await page.evaluate(() => window.ProctorHarness.calls.force)).toEqual([]);
    await page.goto('about:blank');
    await connect(page);
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    await page.screenshot({ path: info.outputPath('classes.png') });
    // A class: the bar above it, the tabs and the exams table all stay inside the window.
    await expect(admitting(page)).toBeInViewport();
    await card(page, 'Mathematics 10A').click();
    for (const name of ['Exams (3)', 'Students (28)']) await expect(tab(page, name)).toBeInViewport();
    await expect(page.getByRole('region', { name: 'Exams', exact: true }).locator('tbody tr')).toHaveCount(3);
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    await page.screenshot({ path: info.outputPath('class-exams.png') });
  });
}
