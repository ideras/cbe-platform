import { test, expect } from '@playwright/test';
import { connect, calls, admitting, card, examsPanel, nav, tab } from './helpers.js';

const EXAM = '11111111-1111-4111-8111-111111111111';
const PRACTICE = '33333333-3333-4333-8333-333333333333';

test('the bar names the published exam, its class and how many students are working', async ({ page }) => {
  await connect(page);
  const bar = admitting(page);
  await expect(bar).toContainText('Admitting: Databases midterm — Mathematics 10A · 1 working');
  await expect(bar.getByRole('button', { name: 'Monitor', exact: true })).toBeEnabled();
  await expect(bar.getByRole('button', { name: 'Open class', exact: true })).toBeEnabled();
  // It is the top of the page, above the class list.
  const top = await bar.boundingBox(), list = await page.getByRole('heading', { name: 'Classes', level: 1 }).boundingBox();
  expect(top.y).toBeLessThan(list.y);
});

test('with nothing published the bar says admission is closed and offers nothing to click', async ({ page }) => {
  await connect(page, { prepare: () => window.ProctorHarness.nothingPublished() });
  const bar = admitting(page);
  await expect(bar).toContainText('Admission closed');
  await expect(bar.getByRole('button')).toHaveCount(0);
  await expect(bar).not.toContainText('Admitting:');
});

test('the bar stays on top of every page of the class, and follows what is published', async ({ page }) => {
  await connect(page, { openClass: 'Mathematics 10A' });
  await expect(admitting(page)).toContainText('Databases midterm');
  await tab(page, /^Students/).click();
  await expect(admitting(page)).toContainText('Databases midterm');
  await tab(page, /^Exams/).click();
  await examsPanel(page).getByRole('button', { name: 'Close admission to Databases midterm…' }).click();
  const dialog = page.getByRole('dialog');
  await dialog.getByRole('checkbox').check(); await dialog.getByRole('button', { name: 'Close admission', exact: true }).click();
  await expect(admitting(page)).toContainText('Admission closed');
});

test('Monitor opens the monitoring window of the published exam without publishing anything', async ({ page }) => {
  await connect(page);
  await admitting(page).getByRole('button', { name: 'Monitor', exact: true }).click();
  await expect.poll(() => calls(page).then(c => c.windows)).toEqual([{ name: 'monitoring', url: '/#/monitoring' }]);
  const c = await calls(page);
  expect(c.select).toEqual([]); // the published exam is already the monitored one
  expect(c.publish).toEqual([]);
});

test('Monitor first selects the published exam when another one was being monitored', async ({ page }) => {
  await connect(page, { openClass: 'Mathematics 10A' });
  await examsPanel(page).getByRole('button', { name: 'Monitor Practice exam' }).click();
  await expect.poll(() => calls(page).then(c => c.select)).toEqual([PRACTICE]);
  await admitting(page).getByRole('button', { name: 'Monitor', exact: true }).click();
  await expect.poll(() => calls(page).then(c => c.select)).toEqual([PRACTICE, EXAM]);
  await expect.poll(() => calls(page).then(c => c.windows.length)).toBe(2);
  expect((await calls(page)).publish).toEqual([]);
});

test('Open class goes to the class of the published exam, on its Exams tab', async ({ page }) => {
  await connect(page);
  await admitting(page).getByRole('button', { name: 'Open class', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Mathematics 10A', level: 1 })).toBeVisible();
  await expect(tab(page, /^Exams/)).toHaveAttribute('aria-selected', 'true');
  await expect(examsPanel(page).locator('tbody tr', { hasText: 'Databases midterm' })).toContainText('Published');
});

test('Open class leads back to the exams from anywhere: another class, the students, or the list', async ({ page }) => {
  await connect(page, { openClass: 'History 11B' });
  await admitting(page).getByRole('button', { name: 'Open class', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Mathematics 10A', level: 1 })).toBeVisible();
  await tab(page, /^Students/).click();
  await expect(tab(page, /^Students/)).toHaveAttribute('aria-selected', 'true');
  await admitting(page).getByRole('button', { name: 'Open class', exact: true }).click();
  await expect(tab(page, /^Exams/)).toHaveAttribute('aria-selected', 'true');
  await expect(examsPanel(page)).toBeVisible();
  await nav(page).getByRole('button', { name: 'Classes', exact: true }).click();
  await expect(card(page, 'History 11B')).toBeVisible();
  await admitting(page).getByRole('button', { name: 'Open class', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Mathematics 10A', level: 1 })).toBeVisible();
});

test('the bar is not shown before connecting', async ({ page }) => {
  await page.goto('/');
  await expect(admitting(page)).toHaveCount(0);
});

test('the bar fits a narrow window', async ({ page }) => {
  await page.setViewportSize({ width: 600, height: 700 });
  await connect(page);
  await expect(admitting(page).getByRole('button', { name: 'Open class', exact: true })).toBeInViewport();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
});
