import { test, expect } from '@playwright/test';

// The calculator is an in-window overlay of the exam view: the button only exists while the exam itself is
// shown and a mode this build implements is enabled, the modal keeps its keys and paste/drop events to itself,
// and using it calls no native method but the Calc* ones — it must never cause or fake a focus loss.
async function login(page) {
  await page.getByRole('button', { name: 'Test screenshot capture' }).click();
  await expect(page.getByLabel('Screenshot test result')).toContainText('Screenshot capture succeeded');
  await page.getByLabel('I have read these rules').check();
  await page.getByLabel('Account number').fill('00123');
  await page.getByLabel('Exam PIN').fill('synthetic PIN');
  await page.getByRole('button', { name: 'Login / resume' }).click();
  await expect(page.getByText('Fixture exam')).toBeVisible();
}

const calculatorOn = { calculator: { enabled: true, modes: ['scientific'], angle_mode: 'deg' } };

async function openCalculator(page) {
  await page.getByRole('button', { name: 'Calculator' }).click();
  const dialog = page.getByRole('dialog', { name: 'Calculator' });
  await expect(dialog).toBeVisible();
  return dialog;
}

test('an exam without the calculator key has no calculator DOM at all', async ({ page }) => {
  await page.goto('/'); await login(page);
  await expect(page.getByRole('button', { name: 'Calculator' })).toHaveCount(0);
  await expect(page.getByRole('dialog')).toHaveCount(0);
});

test('the exam view offers the calculator, evaluates by keyboard, and closes back to the button', async ({ page }) => {
  await page.goto('/');
  await page.evaluate(value => window.__studentHarness.calculatorTools(value), calculatorOn);
  await login(page);
  await page.evaluate(() => { window.__studentHarness.nativeCallsReset(); window.open = () => { window.__opened = true; return null; }; });
  const dialog = await openCalculator(page);
  await expect(dialog.getByText('Every calculation you make is recorded and is part of your exam.')).toBeVisible();
  const input = dialog.getByLabel('Expression');
  await expect(input).toBeFocused();
  await page.keyboard.type('2+3*4');
  await page.keyboard.press('Enter');
  await expect(dialog.locator('.calc-result')).toContainText('= 14');
  await expect(dialog.getByLabel('Recent calculations')).toContainText('2+3*4 = 14');
  // The keys never left the modal: the exam's editor got nothing typed into it.
  await expect(page.getByRole('textbox', { name: 'Answer for q1' })).toContainText('restored work');
  await page.keyboard.press('Escape');
  await expect(dialog).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Calculator' })).toBeFocused();
  // The only native calls were the calculator's Calc* ones (and the app's own background work: the poll timer
  // may call Snapshot at any time, and the rules effect refreshes on the address it now knows).
  const native = await page.evaluate(() => window.__studentHarness.nativeCalls);
  expect(native.filter(name => !name.startsWith('Calc') && name !== 'Snapshot' && name !== 'Rules')).toEqual([]);
  expect(await page.evaluate(() => window.__opened ?? false)).toBe(false);
  // Nothing about the calculator state is persisted.
  expect(await page.evaluate(() => localStorage.length)).toBe(0);
  expect(await page.evaluate(() => indexedDB.databases().then(list => list.length))).toBe(0);
});

test('function buttons follow the allowed set, and typing a disallowed name answers the standard message', async ({ page }) => {
  await page.goto('/');
  await page.evaluate(value => window.__studentHarness.calculatorTools(value), { calculator: { enabled: true, modes: ['scientific'], angle_mode: 'deg', disabled_functions: ['sinh'] } });
  await login(page);
  const dialog = await openCalculator(page);
  // sinh is hidden, not disabled; sin and the pad stay.
  await expect(dialog.getByRole('button', { name: 'sinh' })).toHaveCount(0);
  await expect(dialog.getByRole('button', { name: 'sin', exact: true })).toBeVisible();
  await expect(dialog.getByRole('button', { name: 'cosh', exact: true })).toBeVisible();
  await expect(dialog.getByRole('button', { name: '⌊x⌋' })).toBeVisible();
  const input = dialog.getByLabel('Expression');
  await input.fill('sinh(1)');
  await page.keyboard.press('Enter');
  await expect(dialog.locator('.calc-result')).toContainText('This function is not available in this exam.');
  // One-argument log explains the three logarithms (D2).
  await input.fill('log(8)');
  await page.keyboard.press('Enter');
  await expect(dialog.locator('.calc-result')).toContainText('This function takes a different number of arguments.');
  await expect(dialog.locator('.calc-result')).toContainText('Use log10(x) for base 10, ln(x) for natural log, or log(x, b) for base b.');
  // An allowed function still evaluates.
  await input.fill('sqrt(4)');
  await page.keyboard.press('Enter');
  await expect(dialog.locator('.calc-result')).toContainText('= 2');
});

test('the angle mode is a fixed label when configured and a toggle when the student chooses', async ({ page }) => {
  await page.goto('/');
  await page.evaluate(value => window.__studentHarness.calculatorTools(value), { calculator: { enabled: true, modes: ['scientific'], angle_mode: 'rad' } });
  await login(page);
  const dialog = await openCalculator(page);
  await expect(dialog.locator('.calc-angle')).toContainText('RAD');
  await expect(dialog.getByRole('button', { name: /Switch the angle mode/ })).toHaveCount(0);
});

test('a user-selectable angle mode shows the toggle and the choice reaches the evaluation', async ({ page }) => {
  await page.goto('/');
  await page.evaluate(value => window.__studentHarness.calculatorTools(value), { calculator: { enabled: true, modes: ['scientific'], angle_mode: 'user' } });
  await login(page);
  const dialog = await openCalculator(page);
  await expect(dialog.locator('.calc-angle')).toContainText('DEG');
  await dialog.getByRole('button', { name: 'Switch the angle mode to radians' }).click();
  await expect(dialog.locator('.calc-angle')).toContainText('RAD');
  // The chosen angle reaches the Go evaluation.
  await dialog.getByLabel('Expression').fill('2+2');
  await page.keyboard.press('Enter');
  const evaluations = await page.evaluate(() => window.__studentHarness.calcEvaluations);
  expect(evaluations.at(-1)).toMatchObject({ angleMode: 'rad' });
});

test('pasting and dropping into the calculator do nothing', async ({ page }) => {
  await page.goto('/');
  await page.evaluate(value => window.__studentHarness.calculatorTools(value), calculatorOn);
  await login(page);
  const dialog = await openCalculator(page);
  const input = dialog.getByLabel('Expression');
  await page.evaluate(() => {
    const target = document.querySelector('.calc-input');
    const event = new ClipboardEvent('paste', { bubbles: true, cancelable: true, clipboardData: new DataTransfer() });
    event.clipboardData.setData('text/plain', '999');
    target.dispatchEvent(event);
    const drop = new DragEvent('drop', { bubbles: true, cancelable: true, dataTransfer: new DataTransfer() });
    drop.dataTransfer.setData('text/plain', '999');
    target.dispatchEvent(drop);
  });
  await expect(input).toHaveValue('');
});

test('locking the exam closes the calculator and keeps the history; the unlock shows the button again', async ({ page }) => {
  await page.goto('/');
  await page.evaluate(value => window.__studentHarness.calculatorTools(value), calculatorOn);
  await login(page);
  const dialog = await openCalculator(page);
  await page.keyboard.type('2+3*4');
  await page.keyboard.press('Enter');
  await expect(dialog.locator('.calc-result')).toContainText('= 14');
  await page.evaluate(() => window.__studentHarness.lock('away'));
  await expect(page.getByRole('dialog', { name: 'Calculator' })).toHaveCount(0);
  await page.evaluate(() => window.__studentHarness.unlock());
  await expect(page.getByRole('button', { name: 'Calculator' })).toBeVisible();
  const reopened = await openCalculator(page);
  await expect(reopened.getByLabel('Recent calculations')).toContainText('2+3*4 = 14');
  // The button is gone from the lock screen itself.
  await page.keyboard.press('Escape');
  await page.evaluate(() => window.__studentHarness.lock('away'));
  await expect(page.getByRole('button', { name: 'Calculator' })).toHaveCount(0);
});

test('every scientific function has a button and is reachable by keyboard', async ({ page }, info) => {
  await page.goto('/');
  await page.evaluate(value => window.__studentHarness.calculatorTools(value), calculatorOn);
  await login(page);
  const dialog = await openCalculator(page);
  // The pad holds the digits, the separators and the postfix !; the function rows hold every registry name of
  // the scientific mode except factorial, which the ! is.
  const names = ['log10', 'ln', 'log', 'sqrt', 'cbrt', 'exp', 'abs', 'floor', 'ceil', 'round', 'mod', 'sin', 'cos', 'tan', 'asin', 'acos', 'atan', 'atan2', 'sinh', 'cosh', 'tanh'];
  const labels = { log10: 'log₁₀', sqrt: '√', cbrt: '∛', exp: 'eˣ', abs: '|x|', floor: '⌊x⌋', ceil: '⌈x⌉', asin: 'sin⁻¹', acos: 'cos⁻¹', atan: 'tan⁻¹', atan2: 'atan₂' };
  for (const name of names) await expect(dialog.getByRole('button', { name: labels[name] ?? name, exact: true })).toBeVisible();
  // Reachable by keyboard: type a function call and evaluate it.
  const input = dialog.getByLabel('Expression');
  await page.keyboard.type('log10(1000)');
  await page.keyboard.press('Enter');
  await expect(dialog.locator('.calc-result')).toContainText('= 3');
  await input.fill('');
  await page.keyboard.type('asin(0.5)');
  await page.keyboard.press('Enter');
  await expect(dialog.locator('.calc-result')).toContainText('= 30');
});

test('Tab cycles inside the modal and screen-reader roles are in place', async ({ page }) => {
  await page.goto('/');
  await page.evaluate(value => window.__studentHarness.calculatorTools(value), calculatorOn);
  await login(page);
  const dialog = await openCalculator(page);
  await expect(dialog).toHaveAttribute('aria-modal', 'true');
  await expect(dialog.getByLabel('Recent calculations')).toHaveCount(0); // no history yet
  // Tab walks the modal's own buttons and comes back around; it never reaches the exam page.
  const inside = async () => await page.evaluate(() => Boolean(document.activeElement?.closest('[role="dialog"]')));
  for (let i = 0; i < 30; i++) {
    await page.keyboard.press('Tab');
    if (!(await inside())) break;
  }
  expect(await inside()).toBe(true);
  // The result line announces changes politely.
  await expect(dialog.locator('.calc-result')).toHaveAttribute('aria-live', 'polite');
});

test('every evaluation is queued with the expression as typed, and a full log queue pauses the calculator', async ({ page }) => {
  await page.goto('/');
  await page.evaluate(value => window.__studentHarness.calculatorTools(value), calculatorOn);
  await login(page);
  const dialog = await openCalculator(page);
  // Opening is recorded.
  await expect.poll(() => page.evaluate(() => window.__studentHarness.nativeCalls)).toContain('CalcOpened');
  const input = dialog.getByLabel('Expression');
  await input.fill('1/0');
  await page.keyboard.press('Enter');
  await expect(dialog.locator('.calc-result')).toContainText('You cannot divide by zero.');
  // The failed evaluation is queued exactly as typed too (D4).
  const evaluations = await page.evaluate(() => window.__studentHarness.calcEvaluations);
  expect(evaluations.at(-1)).toMatchObject({ input: '1/0' });
  // A full log queue pauses evaluations with the visible warning until it drains.
  await page.evaluate(() => window.__studentHarness.calcQueueFull(true));
  await input.fill('2+3*4');
  await page.keyboard.press('Enter');
  await expect(dialog.locator('.calc-result')).toContainText('The calculator is paused until your calculations reach the server.');
  await page.evaluate(() => window.__studentHarness.calcQueueFull(false));
  await page.keyboard.press('Enter');
  await expect(dialog.locator('.calc-result')).toContainText('= 14');
  // Closing is recorded.
  await page.keyboard.press('Escape');
  await expect.poll(() => page.evaluate(() => window.__studentHarness.nativeCalls)).toContain('CalcClosed');
  // The exam's end clears the calculator: no button, no dialog, nothing persisted.
  await page.evaluate(() => window.__studentHarness.status({ status: 'force_submitted', seconds_remaining: 0 }));
  await expect(page.getByRole('button', { name: 'Calculator' })).toHaveCount(0);
  await expect(page.getByRole('dialog')).toHaveCount(0);
  expect(await page.evaluate(() => localStorage.length)).toBe(0);
});
