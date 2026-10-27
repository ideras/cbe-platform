import { test, expect } from '@playwright/test';

const rows = [
  ['7', '8', '9', '÷', '(', ')', '!'],
  ['4', '5', '6', '×', '^', 'π', 'e'],
  ['1', '2', '3', 'Ans', ',', '[', ']'],
  ['0', '.'],
];

const configurations = [
  { name: 'scientific', modes: ['scientific'] },
  { name: 'scientific with neural', modes: ['scientific', 'neural'] },
  { name: 'factorial disabled', modes: ['scientific', 'neural'], disabled_functions: ['factorial'] },
  { name: 'neural alone', modes: ['neural'] },
];

for (const width of [1280, 360]) {
  for (const config of configurations) {
    test(`${config.name} keeps numbers left and symbols right at ${width}px`, async ({ page }) => {
      await page.setViewportSize({ width, height: 900 });
      await page.goto('/');
      await page.evaluate(config => window.__studentHarness.calculatorTools({ calculator: { enabled: true, angle_mode: 'user', modes: config.modes, disabled_functions: config.disabled_functions ?? [] } }), config);
      await page.getByRole('button', { name: 'Test screenshot capture' }).click();
      await expect(page.getByLabel('Screenshot test result')).toContainText('Screenshot capture succeeded');
      await page.getByLabel('I have read these rules').check();
      await page.getByLabel('Account number').fill('00123');
      await page.getByLabel('Exam PIN').fill('synthetic PIN');
      await page.getByRole('button', { name: 'Login / resume' }).click();
      await page.getByRole('button', { name: 'Calculator', exact: true }).click();
      const calculator = page.getByRole('dialog', { name: 'Calculator' });
      const pad = calculator.locator('.calc-pad');
      await expect(pad).toBeVisible();
      const expected = rows.flatMap((row, y) => row.flatMap((label, x) => {
        if (label === '!' && (!config.modes.includes('scientific') || config.disabled_functions?.includes('factorial'))) return [];
        if (['[', ']'].includes(label) && !config.modes.includes('neural')) return [];
        return [{ label, row: y + 1, column: x + 1 }];
      }));
      await expect(pad.locator('button')).toHaveText(expected.map(key => key.label));
      const cells = await pad.locator('button').evaluateAll(buttons => buttons.map(button => {
        const style = getComputedStyle(button), box = button.getBoundingClientRect();
        return { label: button.textContent, row: Number(style.gridRowStart), column: Number(style.gridColumnStart), x: box.x, y: box.y, width: box.width, right: box.right };
      }));
      for (const [i, cell] of cells.entries()) {
        expect({ label: cell.label, row: cell.row, column: cell.column }).toEqual(expected[i]);
        expect(cell.width).toBeGreaterThan(0);
      }
      // Actual geometry, not just declared grid positions: digit columns never drift
      // when optional symbol cells are absent, and no key spills outside the pad.
      const first = cells.find(key => key.label === '7');
      const second = cells.find(key => key.label === '8');
      const last = cells.find(key => key.label === '0');
      const bounds = await pad.boundingBox();
      for (const cell of cells) {
        expect(Math.abs(cell.x - (first.x + (cell.column - 1) * (second.x - first.x)))).toBeLessThan(1);
        expect(cell.x).toBeGreaterThanOrEqual(bounds.x - 1);
        expect(cell.right).toBeLessThanOrEqual(bounds.x + bounds.width + 1);
      }
      expect(last.y).toBeGreaterThan(cells.find(key => key.label === '1').y);
      expect(bounds.x + bounds.width).toBeLessThanOrEqual(width);
      // Reordering changes neither inserted text nor the focus return to the input.
      for (const label of ['7', '÷', '(', '4', '×', '2', ')']) await pad.getByRole('button', { name: label, exact: true }).click();
      await expect(calculator.getByLabel('Expression')).toHaveValue('7/(4*2)');
      await expect(calculator.getByLabel('Expression')).toBeFocused();
    });
  }
}
