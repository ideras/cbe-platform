import { test, expect } from '@playwright/test';

// Neural mode (plan Phase 7) shares the scientific panel (owner, 2026-10-25): no Neural tab, bracket keys and a
// row of neural functions when the exam enables neural mode, a matrix shown one row per line with its columns
// aligned, and the hints that explain bce and the elementwise *. Everything is computed in Go.
async function login(page) {
  await page.getByRole('button', { name: 'Test screenshot capture' }).click();
  await expect(page.getByLabel('Screenshot test result')).toContainText('Screenshot capture succeeded');
  await page.getByLabel('I have read these rules').check();
  await page.getByLabel('Account number').fill('00123');
  await page.getByLabel('Exam PIN').fill('synthetic PIN');
  await page.getByRole('button', { name: 'Login / resume' }).click();
  await expect(page.getByText('Fixture exam')).toBeVisible();
}

async function calculator(page, calculatorConfig) {
  await page.goto('/');
  await page.evaluate(value => window.__studentHarness.calculatorTools(value), { calculator: { enabled: true, angle_mode: 'deg', ...calculatorConfig } });
  await login(page);
  await page.getByRole('button', { name: 'Calculator' }).click();
  const dialog = page.getByRole('dialog', { name: 'Calculator' });
  await expect(dialog).toBeVisible();
  return dialog;
}

async function evaluate(dialog, page, text) {
  await dialog.getByLabel('Expression').fill(text);
  await page.keyboard.press('Enter');
}

test('neural mode shares the scientific panel: no Neural tab, and its keys appear there', async ({ page }) => {
  const dialog = await calculator(page, { modes: ['scientific', 'neural', 'programmer'] });
  const tabs = dialog.getByRole('tablist', { name: 'Calculator mode' }).getByRole('tab');
  await expect(tabs).toHaveText(['Scientific', 'Programmer']);
  const neural = dialog.getByRole('group', { name: 'Neural functions' });
  await expect(neural.getByRole('button')).toHaveCount(20); // every neural name but tanh, whose key is in the scientific row
  await expect(neural.getByRole('button', { name: 'bce', exact: true })).toHaveAttribute('title', /natural log/);
  // The brackets type into the expression like any key.
  await dialog.getByRole('button', { name: 'softmax', exact: true }).click();
  await dialog.getByRole('button', { name: 'Open bracket' }).click();
  await page.keyboard.type('1000, 1000');
  await dialog.getByRole('button', { name: 'Close bracket' }).click();
  await page.keyboard.type(')');
  await expect(dialog.getByLabel('Expression')).toHaveValue('softmax([1000, 1000])');
  await page.keyboard.press('Enter');
  await expect(dialog.locator('.calc-result')).toHaveText('= [0.5, 0.5]');
  // The scientific functions are still there, with the angle mode.
  await expect(dialog.getByRole('button', { name: 'sin', exact: true })).toBeVisible();
  await expect(dialog.getByLabel('Angle mode')).toContainText('DEG');
});

test('a matrix shows one row per line with aligned columns, and its one-line form for screen readers and the history', async ({ page }) => {
  const dialog = await calculator(page, { modes: ['scientific', 'neural'] });
  await evaluate(dialog, page, 'matmul([[1,2],[3,4]], [[5,6],[7,8]])');
  const matrix = dialog.getByRole('img', { name: '= [[19, 22], [43, 50]]' });
  await expect(matrix).toBeVisible();
  const cells = matrix.locator('.matrix span');
  await expect(cells).toHaveText(['19', '22', '43', '50']);
  const box = async index => cells.nth(index).boundingBox();
  const [a, b, c, d] = [await box(0), await box(1), await box(2), await box(3)];
  expect(a.y).toBe(b.y); // one row per line
  expect(c.y).toBeGreaterThan(a.y);
  expect(Math.abs((a.x + a.width) - (c.x + c.width))).toBeLessThan(1); // columns aligned on the right
  expect(Math.abs((b.x + b.width) - (d.x + d.width))).toBeLessThan(1);
  await expect(dialog.getByLabel('Recent calculations')).toContainText('matmul([[1,2],[3,4]], [[5,6],[7,8]]) = [[19, 22], [43, 50]]');
  // A one-column matrix is a column.
  await evaluate(dialog, page, 'transpose([[1, 2.5, -3]])');
  await expect(dialog.getByRole('img', { name: '= [[1], [2.5], [-3]]' }).locator('.matrix span')).toHaveText(['1', '2.5', '-3']);
});

test('the neural hints explain bce and the elementwise *, and the size limit is stated', async ({ page }) => {
  const dialog = await calculator(page, { modes: ['scientific', 'neural'] });
  await evaluate(dialog, page, 'bce(1, 1)');
  await expect(dialog.locator('.calc-result')).toContainText('outside the function’s domain');
  await expect(dialog.locator('.calc-result')).toContainText('It uses the natural log (ln).');
  await evaluate(dialog, page, '[1,2] * [1,2,3]');
  await expect(dialog.locator('.calc-result')).toContainText('These values have incompatible shapes.');
  await expect(dialog.locator('.calc-result')).toContainText('Use matmul(A, B) for the matrix product.');
  await evaluate(dialog, page, 'log([1,2], 2)');
  await expect(dialog.locator('.calc-result')).toContainText('This operation got a value of the wrong kind.');
});

test('neural mode alone is one panel named Neural, without the angle mode or the scientific keys', async ({ page }) => {
  const dialog = await calculator(page, { modes: ['neural'] });
  await expect(dialog.getByRole('tablist')).toHaveCount(0);
  await expect(dialog.getByLabel('Angle mode')).toHaveCount(0);
  await expect(dialog.getByRole('button', { name: 'sin', exact: true })).toHaveCount(0);
  await expect(dialog.getByRole('button', { name: 'tanh', exact: true })).toBeVisible(); // tanh is shared
  await evaluate(dialog, page, 'sigmoid(0)');
  await expect(dialog.locator('.calc-result')).toHaveText('= 0.5');
  expect(await page.evaluate(() => window.__studentHarness.calcOpenings)).toEqual(['neural']);
  await evaluate(dialog, page, 'sin(30)');
  await expect(dialog.locator('.calc-result')).toContainText('This function is not available in this exam.');
});

test('without neural mode there are no bracket or neural keys, and a disabled neural function has no key', async ({ page }) => {
  let dialog = await calculator(page, { modes: ['scientific'] });
  await expect(dialog.getByRole('button', { name: 'Open bracket' })).toHaveCount(0);
  await expect(dialog.getByRole('group', { name: 'Neural functions' })).toHaveCount(0);
  await evaluate(dialog, page, '[1,2]^2');
  await expect(dialog.locator('.calc-result')).toContainText('This is not a valid expression.');

  dialog = await calculator(page, { modes: ['scientific', 'neural'], disabled_functions: ['softmax'] });
  await expect(dialog.getByRole('group', { name: 'Neural functions' }).getByRole('button', { name: 'softmax', exact: true })).toHaveCount(0);
  await expect(dialog.getByRole('group', { name: 'Neural functions' }).getByRole('button', { name: 'sigmoid', exact: true })).toBeVisible();
  await evaluate(dialog, page, 'softmax([1000, 1000])');
  await expect(dialog.locator('.calc-result')).toContainText('This function is not available in this exam.');
});

test('using neural mode calls nothing native but the Calc* methods and opens no window', async ({ page }) => {
  const dialog = await calculator(page, { modes: ['scientific', 'neural'] });
  await page.evaluate(() => { window.__studentHarness.nativeCallsReset(); window.open = () => { window.__opened = true; return null; }; });
  await evaluate(dialog, page, '[1,2]^2');
  await expect(dialog.locator('.calc-result')).toHaveText('= [1, 4]');
  await evaluate(dialog, page, 'matmul([[1,2],[3,4]], [[5,6],[7,8]])');
  await page.keyboard.press('Escape');
  const native = await page.evaluate(() => window.__studentHarness.nativeCalls);
  expect(native.filter(name => !name.startsWith('Calc') && name !== 'Snapshot' && name !== 'Rules')).toEqual([]);
  expect(await page.evaluate(() => window.__opened ?? false)).toBe(false);
});
