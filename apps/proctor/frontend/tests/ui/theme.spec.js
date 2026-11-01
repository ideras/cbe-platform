import { test, expect } from '@playwright/test';
import { connect } from './helpers.js';

async function token(page, name) {
  return page.evaluate(name => {
    const probe = document.createElement('span'); probe.style.color = `var(--${name})`;
    document.body.append(probe); const color = getComputedStyle(probe).color; probe.remove(); return color;
  }, name);
}

test('theme roles apply to the actual class controls, inline icons, focus and calculator preview', async ({ page }) => {
  const external = [];
  page.on('request', request => { if (!request.url().startsWith('http://127.0.0.1:4179') && !request.url().startsWith('data:')) external.push(request.url()); });
  await connect(page, { openClass: 'Mathematics 10A' });
  const ink = await token(page, 'text-main'), secondary = await token(page, 'text-muted'), ocean = await token(page, 'action-primary');
  // The chosen tab is raised and blue with a gradient underline; the other keeps the ink (muted text is under 4.5:1 on its ground).
  await page.mouse.move(0, 0); // the pointer is where the class card was clicked, over a tab; these are the resting colors
  const exams = page.getByRole('tab', { name: 'Exams (3)' }), students = page.getByRole('tab', { name: 'Students (28)' });
  await expect(exams).toHaveCSS('color', await token(page, 'action-hover'));
  await expect(students).toHaveCSS('color', ink);
  const underline = await exams.evaluate(element => { const after = getComputedStyle(element, '::after'); return { height: after.height, image: after.backgroundImage }; });
  expect(underline.height).toBe('3px');
  expect(underline.image).toContain(ocean); expect(underline.image).toContain(await token(page, 'accent-cyan'));
  expect(await students.evaluate(element => getComputedStyle(element, '::after').content)).toBe('none');
  await expect(page.getByRole('button', { name: 'Import exam…' })).toHaveCSS('background-color', ocean);
  // The exam being monitored has the one filled action; the others are quiet bordered squares that fill on hover.
  const monitored = page.getByRole('button', { name: 'Monitor Databases midterm', exact: true });
  await expect(monitored).toHaveCSS('background-color', ocean);
  await expect(monitored.locator('svg')).toHaveCSS('color', await token(page, 'bg-primary'));
  const monitor = page.getByRole('button', { name: 'Monitor Practice exam', exact: true });
  await expect(monitor).toHaveCSS('border-top-color', await token(page, 'border-subtle'));
  await expect(monitor.locator('svg')).toHaveCSS('color', secondary);
  await monitor.hover();
  await expect(monitor).toHaveCSS('background-color', await token(page, 'selection-fill'));
  const door = page.getByRole('button', { name: 'Close admission to Databases midterm…' }).locator('svg');
  await expect(door.locator('rect[x="14"][y="16"]')).toHaveCount(1);
  const sidebar = page.getByRole('navigation', { name: 'Main navigation' }).getByRole('button', { name: 'Classes', exact: true });
  // Reached by keyboard, so :focus-visible applies (a programmatic focus after the hover above would not show it).
  await page.getByRole('navigation', { name: 'Main navigation' }).getByRole('button', { name: 'Server connection' }).focus();
  await page.keyboard.press('Tab');
  await expect(sidebar).toBeFocused();
  await expect(sidebar).toHaveCSS('outline-style', 'solid');
  await expect(sidebar).toHaveCSS('outline-color', await token(page, 'accent-cyan-soft'));
  await page.getByRole('button', { name: 'Preview Draft quiz' }).click();
  await page.getByRole('button', { name: 'Open the calculator' }).click();
  const calculator = page.getByRole('dialog', { name: 'Calculator', exact: true });
  await expect(calculator.getByRole('tab', { name: 'Scientific' })).toHaveCSS('background-color', ocean);
  await expect(calculator.getByRole('button', { name: 'Evaluate' })).toHaveCSS('background-color', ocean);
  const pad = calculator.locator('.calc-pad');
  const seven = await pad.getByRole('button', { name: '7', exact: true }).boundingBox();
  const four = await pad.getByRole('button', { name: '4', exact: true }).boundingBox();
  const nine = await pad.getByRole('button', { name: '9', exact: true }).boundingBox();
  const divide = await pad.getByRole('button', { name: '÷', exact: true }).boundingBox();
  expect(Math.abs(seven.x - four.x)).toBeLessThan(1);
  expect(four.y).toBeGreaterThan(seven.y);
  expect(divide.x).toBeGreaterThan(nine.x);
  await calculator.getByRole('tab', { name: 'Programmer' }).click();
  await expect(calculator.locator('.seg button[aria-pressed="true"]').first()).toHaveCSS('background-color', await token(page, 'action-hover'));
  expect(external).toEqual([]);
});

test('multiple locked students do not create competing primary row actions', async ({ page }) => {
  await page.goto('/#/monitoring');
  await page.getByRole('table', { name: /All sessions/ }).waitFor();
  await page.evaluate(() => { window.ProctorHarness.lock(0, 'away'); window.ProctorHarness.lock(1, 'no_contact'); });
  for (const account of ['0001', '0002']) {
    const unlock = page.getByRole('button', { name: `Unlock ${account}…`, exact: true });
    await expect(unlock).toBeVisible();
    await expect(unlock).not.toHaveClass(/primary/);
  }
  await expect(page.locator('table button.primary')).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Extend grace for everyone…', exact: true })).toHaveClass(/primary/);
  await page.getByRole('button', { name: 'Unlock 0001…', exact: true }).click();
  await expect(page.getByRole('dialog').getByRole('button', { name: 'Unlock', exact: true })).toHaveClass(/primary/);
});

test('the import confirmation becomes the one primary button while a validated preview is open', async ({ page }) => {
  await connect(page, { openClass: 'Mathematics 10A', prepare: () => window.ProctorHarness.examFile({ file: 'theme.md', valid: true, title: 'Theme fixture', exercise_count: 1, slot_count: 1, navigation: 'free', duration_seconds: 600 }) });
  const choose = page.getByRole('button', { name: 'Import exam…' });
  await choose.click();
  await expect(choose).not.toHaveClass(/primary/);
  await expect(page.getByRole('button', { name: 'Import exam', exact: true })).toHaveClass(/primary/);
  await expect(page.locator('button.primary')).toHaveCount(1);
});
