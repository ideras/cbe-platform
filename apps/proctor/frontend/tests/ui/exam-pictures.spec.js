import { test, expect } from '@playwright/test';
import { connect } from './helpers.js';

const png = 'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==';
const svg = Buffer.from('<svg xmlns="http://www.w3.org/2000/svg" width="40" height="20"><rect width="40" height="20" fill="#000"/></svg>').toString('base64');

// The preview shows the compiled exam as students will see it, so the same pictures appear, and nothing else gets in as an <img>.
test('the exam preview shows the embedded pictures and drops any other <img>', async ({ page }) => {
  const external = [];
  await page.route('**/*', route => { if (!/^(http:\/\/127\.0\.0\.1:4179\/|data:)/.test(route.request().url())) { external.push(route.request().url()); return route.abort(); } return route.continue(); });
  await connect(page, { openClass: 'Mathematics 10A' });
  await page.evaluate(({ png, svg }) => window.ProctorHarness.examFile({
    file: 'figuras.md', valid: true, title: 'Prueba con figuras', exercise_count: 1, slot_count: 2,
    navigation: 'free', duration_seconds: 1800, grace_seconds: 30, min_volume_percent: 80, calculator: null, opens_at: null, closes_at: null,
    content: {
      instructions: `<p>Vea la figura.</p><img src="data:image/svg+xml;base64,${svg}" alt="Autómata de ejemplo"><img src="https://example.invalid/a.png" alt="remote"><img src="x" onerror="window.__unsafe=true">`,
      exercises: [{
        id: '1', title: 'Con figura', points: 5,
        body: `<img src="data:image/png;base64,${png}" alt="Un punto"><table><tbody><tr><td><img src="data:image/png;base64,${png}" alt="Otro punto"></td><td><span class="slot" data-slot="1.cell"></span></td></tr></tbody></table><div class="slot" data-slot="1.answer"></div>`,
        slots: [{ id: '1.cell', kind: 'blank' }, { id: '1.answer', kind: 'text', lines: 4 }],
      }],
    },
  }), { png, svg });
  await page.getByRole('button', { name: 'Import exam…' }).click();
  await page.locator('.import-stack').getByRole('button', { name: 'Preview the exam', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'Preview — Prueba con figuras', exact: true });
  const preview = dialog.locator('.exam-preview');
  await expect(preview).toBeVisible();
  for (const name of ['Autómata de ejemplo', 'Un punto', 'Otro punto']) {
    const figure = preview.getByRole('img', { name });
    await expect(figure).toBeVisible();
    expect(await figure.evaluate(image => image.complete && image.naturalWidth)).toBeGreaterThan(0);
    await expect(figure).toHaveCSS('background-color', 'rgb(255, 255, 255)');
  }
  await expect(preview.locator('img')).toHaveCount(3);
  // The fields beside the pictures are still marked with their IDs.
  await expect(preview.getByText('1.cell', { exact: true })).toBeVisible();
  await expect(preview.getByText('1.answer · text, 4 lines')).toBeVisible();
  expect(await page.evaluate(() => window.__unsafe)).toBeUndefined();
  expect(external).toEqual([]);
});
