import { test, expect } from '@playwright/test';

// The programmer panel (plan Phases 5, 6 and 6A): bases, word size, the signed/unsigned toggle, flags, the bit
// grid, and one input shared by the Keypad, IEEE 754 and Radix Point tabs — all computed in Go, with the panel's
// settings in the logged details.
async function login(page) {
  await page.getByLabel('I have read these rules').check();
  await page.getByLabel('Account number').fill('00123');
  await page.getByLabel('Exam PIN').fill('synthetic PIN');
  await page.getByRole('button', { name: 'Login / resume' }).click();
  await expect(page.getByText('Fixture exam')).toBeVisible();
}

const bothModes = { calculator: { enabled: true, modes: ['scientific', 'programmer'], angle_mode: 'deg' } };
const onlyProgrammer = { calculator: { enabled: true, modes: ['programmer'], angle_mode: 'user' } };

async function openCalculator(page) {
  await page.getByRole('button', { name: 'Calculator' }).click();
  const dialog = page.getByRole('dialog', { name: 'Calculator' });
  await expect(dialog).toBeVisible();
  return dialog;
}

async function programmerPanel(page, tools = bothModes) {
  await page.goto('/');
  await page.evaluate(value => window.__studentHarness.calculatorTools(value), tools);
  await login(page);
  const dialog = await openCalculator(page);
  if (tools.calculator.modes.length > 1) await dialog.getByRole('tab', { name: 'Programmer' }).click();
  const panel = dialog.locator('.prog');
  await expect(panel).toBeVisible();
  return panel;
}

const wordSize = (panel, size) => panel.getByRole('group', { name: 'Word size' }).getByRole('button', { name: String(size), exact: true });
const inputBase = (panel, name) => panel.getByRole('group', { name: 'Input base' }).getByRole('button', { name, exact: true });
const expression = panel => panel.getByLabel('Programmer expression');

test('the tabs appear when more than one mode is implemented and enabled, and switching is recorded', async ({ page }) => {
  await page.goto('/');
  await page.evaluate(value => window.__studentHarness.calculatorTools(value), bothModes);
  await login(page);
  const dialog = await openCalculator(page);
  await expect(dialog.getByRole('tab', { name: 'Scientific' })).toBeVisible();
  await dialog.getByRole('tab', { name: 'Programmer' }).click();
  await expect.poll(() => page.evaluate(() => window.__studentHarness.nativeCalls)).toContain('CalcModeChanged');
  // The scientific angle indicator is not in the programmer panel.
  await expect(dialog.getByLabel('Angle mode')).toHaveCount(0);
});

test('a single-mode exam shows no mode tabs and opens on its only mode', async ({ page }) => {
  await page.goto('/');
  await page.evaluate(value => window.__studentHarness.calculatorTools(value), onlyProgrammer);
  await login(page);
  const dialog = await openCalculator(page);
  await expect(dialog.getByRole('tablist', { name: 'Calculator mode' })).toHaveCount(0);
  await expect(dialog.locator('.prog')).toBeVisible();
});

test('the flags and readings follow the plan vectors', async ({ page }) => {
  const panel = await programmerPanel(page);
  await expression(panel).fill('0xFF + 1');
  // Set the word size to 8 before evaluating.
  await wordSize(panel, 8).click();
  await panel.getByRole('button', { name: 'Evaluate' }).click();
  await expect(panel).toContainText('DEC signed 0');
  await expect(panel).toContainText('DEC unsigned 0');
  await expect(panel).toContainText('HEX 0x00');
  await expect(panel.locator('.flags')).toContainText('CF 1');
  await expect(panel.locator('.flags')).toContainText('OF 0');
  // 127 + 1: signed -128 with OF=1.
  await expression(panel).fill('127 + 1');
  await panel.getByRole('button', { name: 'Evaluate' }).click();
  await expect(panel).toContainText('DEC signed -128');
  await expect(panel.locator('.flags')).toContainText('OF 1');
  // The settings that reproduce the result go to Go with it.
  const evaluations = await page.evaluate(() => window.__studentHarness.calcEvaluations);
  expect(evaluations.at(-1)).toMatchObject({ input: '127 + 1', wordSize: 8, inputBase: 'dec', signedMode: true });
});

test('the input base decides how unprefixed literals are read', async ({ page }) => {
  const panel = await programmerPanel(page);
  await inputBase(panel, 'HEX').click();
  await wordSize(panel, 16).click();
  await expression(panel).fill('ff + 1');
  await panel.getByRole('button', { name: 'Evaluate' }).click();
  await expect(panel).toContainText('DEC unsigned 256');
  await expect(panel).toContainText('HEX 0x0101');
});

test('clicking a bit flips it, writes the literal into the input, and evaluates', async ({ page }) => {
  const panel = await programmerPanel(page);
  await wordSize(panel, 8).click();
  await expression(panel).fill('1');
  await panel.getByRole('button', { name: 'Evaluate' }).click();
  await expect(panel).toContainText('DEC unsigned 1');
  // The grid shows 8 bits; flipping the highest bit writes 0x81 into the input in DEC and evaluates it.
  await panel.getByRole('button', { name: 'Bit 7, set it' }).click();
  await expect(expression(panel)).toHaveValue('129');
  await expect(panel).toContainText('DEC unsigned 129');
  const evaluations = await page.evaluate(() => window.__studentHarness.calcEvaluations);
  expect(evaluations.at(-1)).toMatchObject({ input: '129' });
});

test('the bit view shows 32 bits a row, so 64 bits take two rows, with bit 0 at the right edge', async ({ page }) => {
  const panel = await programmerPanel(page);
  const rows = panel.locator('.bitrow');
  const top = async locator => (await locator.boundingBox()).y;
  await wordSize(panel, 32).click();
  await expression(panel).fill('5');
  await panel.getByRole('button', { name: 'Evaluate' }).click();
  await expect(rows).toHaveCount(1);
  const bit31 = panel.getByRole('button', { name: 'Bit 31, set it' });
  const bit0 = panel.getByRole('button', { name: 'Bit 0, clear it' });
  expect(await top(bit31)).toBe(await top(bit0));
  await expect(rows.first().locator('.pos')).toHaveText(['28', '24', '20', '16', '12', '8', '4', '0']);

  await wordSize(panel, 64).click();
  await expression(panel).fill('5');
  await panel.getByRole('button', { name: 'Evaluate' }).click();
  await expect(rows).toHaveCount(2);
  expect(await top(panel.getByRole('button', { name: 'Bit 63, set it' }))).toBe(await top(panel.getByRole('button', { name: 'Bit 32, set it' })));
  expect(await top(panel.getByRole('button', { name: 'Bit 31, set it' }))).toBeGreaterThan(await top(panel.getByRole('button', { name: 'Bit 32, set it' })));
  const right = async locator => { const box = await locator.boundingBox(); return box.x + box.width; };
  const edge = await right(panel.getByRole('button', { name: 'Bit 0, clear it' }));

  // Every bit of a 64-bit word is still a button: flipping bit 63 writes the literal and evaluates it.
  await panel.getByRole('button', { name: 'Bit 63, set it' }).click();
  await expect(expression(panel)).toHaveValue('9223372036854775813');

  // A shorter word keeps bit 0 in the same column.
  await wordSize(panel, 8).click();
  await expect(rows).toHaveCount(1);
  expect(await right(panel.getByRole('button', { name: 'Bit 0, clear it' }))).toBeCloseTo(edge, 0);
});

test('shrinking the word size re-wraps the last result and warns when bits are lost', async ({ page }) => {
  const panel = await programmerPanel(page);
  await expression(panel).fill('300');
  await panel.getByRole('button', { name: 'Evaluate' }).click();
  await expect(panel).toContainText('DEC unsigned 300');
  // To 8 bits: 300 wraps to 44 and the panel says so.
  await wordSize(panel, 8).click();
  await expect(panel).toContainText('DEC unsigned 44');
  await expect(panel.getByRole('alert')).toContainText('truncated to 8 bits');
});

test('a real shows its value, dashes for the integer rows, and no bit view (D20)', async ({ page }) => {
  const panel = await programmerPanel(page);
  await expression(panel).fill('10.625');
  await expression(panel).press('Enter');
  await expect(panel.locator('.readings').first()).toContainText('Value 10.625');
  await expect(panel.locator('.readings').first()).toContainText('DEC unsigned —');
  await expect(panel).toContainText('Whole numbers only. The Radix Point and IEEE 754 tabs show this value.');
  await expect(panel.getByText('Bit view needs a whole number.')).toBeVisible();
  await expect(panel.locator('.bitgrid')).toHaveCount(0);
  // An integer brings the grid back.
  await expression(panel).fill('1');
  await expression(panel).press('Enter');
  await expect(panel.locator('.bitgrid')).toBeVisible();
  await expect(panel.getByText('Bit view needs a whole number.')).toHaveCount(0);
  // An integer operator on a real is refused with its own message.
  await expression(panel).fill('1.5 & 1');
  await expression(panel).press('Enter');
  await expect(panel.locator('.calc-result')).toContainText('Integer operators need whole numbers.');
});

test('an exponent is refused with a hint (D19)', async ({ page }) => {
  const panel = await programmerPanel(page);
  await expression(panel).fill('1e3');
  await expression(panel).press('Enter');
  await expect(panel.locator('.calc-result')).toContainText('This is not a valid expression.');
  await expect(panel.locator('.calc-result')).toContainText('Exponents are not accepted. Write the number in full or use Decode.');
});

test('one input carries across the tabs, each tab reads it, and every tab change is recorded (D21, D22)', async ({ page }) => {
  const panel = await programmerPanel(page);
  await expression(panel).fill('10.625');
  await expect(panel.locator('.hint')).toHaveText('Integer operators need whole numbers.');
  await panel.getByRole('tab', { name: 'Radix Point' }).click();
  // The text stays, and the Radix Point tab reads it on arrival.
  await expect(expression(panel)).toHaveValue('10.625');
  await expect(panel.locator('.hint')).toHaveText('Reads a number. Shows up to 32 fraction digits.');
  const fraction = panel.getByRole('tabpanel');
  await expect(fraction).toContainText('BIN 1010.101');
  await expect(fraction).toContainText('OCT 12.5');
  await expect(fraction).toContainText('HEX A.A');
  await expect(fraction).toContainText('Exact in all three bases.');
  await panel.getByRole('tab', { name: 'IEEE 754' }).click();
  await expect(expression(panel)).toHaveValue('10.625');
  await expect(panel.locator('.hint')).toHaveText('Reads a number and shows its IEEE 754 encoding.');
  await expect(panel.getByRole('tabpanel')).toContainText('Bits 0x412A0000');
  const changes = await page.evaluate(() => window.__studentHarness.calcTabChanges);
  expect(changes).toEqual([{ from: 'keypad', to: 'fraction' }, { from: 'fraction', to: 'ieee754' }]);
  // The Radix Point tab sent the panel settings with the input.
  const evaluations = await page.evaluate(() => window.__studentHarness.calcEvaluations);
  expect(evaluations.find(entry => entry.view === 'fraction')).toMatchObject({ input: '10.625', wordSize: 32, inputBase: 'dec', signedMode: true });
  // The tabs are a tablist: the panel is labeled by its tab.
  await expect(panel.getByRole('tabpanel', { name: 'IEEE 754' })).toBeVisible();
  await expect(panel.getByRole('tab', { name: 'IEEE 754' })).toHaveAttribute('aria-selected', 'true');
});

test('switching the tabs and the direction calls nothing native but the Calc* methods', async ({ page }) => {
  const panel = await programmerPanel(page);
  await page.evaluate(() => { window.__opened = 0; window.open = () => { window.__opened += 1; return null; }; });
  await page.evaluate(() => window.__studentHarness.nativeCallsReset());
  await expression(panel).fill('1.0');
  await panel.getByRole('tab', { name: 'IEEE 754' }).click();
  await panel.getByRole('group', { name: 'Direction' }).getByRole('button', { name: 'Decode' }).click();
  await panel.getByRole('group', { name: 'Direction' }).getByRole('button', { name: 'Encode' }).click();
  await panel.getByRole('tab', { name: 'Radix Point' }).click();
  await panel.getByRole('tab', { name: 'Keypad' }).click();
  const calls = await page.evaluate(() => window.__studentHarness.nativeCalls);
  // The app's own timers may call Snapshot and Rules at any time, as in the scientific test.
  expect(calls.filter(name => !name.startsWith('Calc') && name !== 'Snapshot' && name !== 'Rules')).toEqual([]);
  expect(calls).toContain('CalcTabChanged');
  expect(await page.evaluate(() => window.__opened)).toBe(0);
});

test('the arrow keys move between the tabs and Enter selects one', async ({ page }) => {
  const panel = await programmerPanel(page);
  await panel.getByRole('tab', { name: 'Keypad' }).focus();
  await page.keyboard.press('ArrowRight');
  await expect(panel.getByRole('tab', { name: 'IEEE 754' })).toBeFocused();
  await page.keyboard.press('ArrowRight');
  await expect(panel.getByRole('tab', { name: 'Radix Point' })).toBeFocused();
  await page.keyboard.press('ArrowRight'); // wraps around
  await expect(panel.getByRole('tab', { name: 'Keypad' })).toBeFocused();
  await page.keyboard.press('ArrowLeft');
  await page.keyboard.press('Enter');
  await expect(panel.getByRole('tab', { name: 'Radix Point' })).toHaveAttribute('aria-selected', 'true');
  await expect(panel.getByRole('tabpanel', { name: 'Radix Point' })).toBeVisible();
  // Typing while a tab has the focus goes to the shared input.
  await page.keyboard.type('7');
  await expect(expression(panel)).toHaveValue('7');
});

test('the keypad types into the shared input, and digits the base cannot read are inactive', async ({ page }) => {
  const panel = await programmerPanel(page);
  const keys = panel.getByRole('group', { name: 'Programmer keys' });
  await expect(keys.getByRole('button', { name: 'A', exact: true })).toHaveAttribute('aria-disabled', 'true');
  await keys.getByRole('button', { name: 'A', exact: true }).dispatchEvent('click'); // inactive: Playwright will not click it
  await expect(expression(panel)).toHaveValue('');
  await keys.getByRole('button', { name: '1', exact: true }).click();
  await keys.getByRole('button', { name: 'Decimal point' }).click();
  await keys.getByRole('button', { name: '5', exact: true }).click();
  await expect(expression(panel)).toHaveValue('1.5');
  await keys.getByRole('button', { name: 'Clear the input' }).click();
  await expect(expression(panel)).toHaveValue('');
  // In HEX, A–F are active; in BIN, only 0 and 1.
  await inputBase(panel, 'HEX').click();
  await expect(keys.getByRole('button', { name: 'A', exact: true })).not.toHaveAttribute('aria-disabled', 'true');
  await inputBase(panel, 'BIN').click();
  await expect(keys.getByRole('button', { name: '2', exact: true })).toHaveAttribute('aria-disabled', 'true');
  await expect(keys.getByRole('button', { name: '1', exact: true })).not.toHaveAttribute('aria-disabled', 'true');
  // The programmer functions show when allowed; there is no >>> key (removed in 1.3).
  await expect(keys.getByRole('button', { name: 'nand' })).toBeVisible();
  await expect(keys.getByRole('button', { name: '>>>' })).toHaveCount(0);
  await expect(keys.getByRole('button', { name: 'Ans' })).toBeVisible();
});

test('the Radix Point tab converts exactly, with the digit cap and its dots', async ({ page }) => {
  const panel = await programmerPanel(page);
  await panel.getByRole('tab', { name: 'Radix Point' }).click();
  await expression(panel).fill('0.1');
  await expression(panel).press('Enter');
  await expect(panel.getByRole('tabpanel')).toContainText('0.00011001100110011001100110011001...'); // 32 digits, then the dots (D6)
  await expect(panel.getByRole('tabpanel')).toContainText('it stops after 32 digits');
});

test('the IEEE 754 tab encodes and decodes, and its fields carry text labels', async ({ page }) => {
  const panel = await programmerPanel(page, onlyProgrammer);
  await panel.getByRole('tab', { name: 'IEEE 754' }).click();
  const inspector = panel.getByRole('tabpanel');
  await expression(panel).fill('1.0');
  await inspector.getByRole('button', { name: 'binary16', exact: true }).click(); // a new format reads the input
  await expect(inspector).toContainText('Bits 0x3C00');
  await expect(inspector).toContainText('Sign 0');
  await expect(inspector).toContainText('Exponent field 15');
  await expect(inspector).toContainText('Unbiased exponent 0');
  await expect(inspector).toContainText('Mantissa field 0');
  await expect(inspector).toContainText('Class normal');
  await expect(inspector).toContainText('Value 1');
  await expect(inspector).toContainText('Exponent, 5 bits');
  await expect(inspector).toContainText('Mantissa, 10 bits');
  // 0x1e3 is a number to Encode (D21).
  await expression(panel).fill('0x1e3');
  await inspector.getByRole('button', { name: 'Inspect' }).click();
  await expect(inspector).toContainText('Bits 0x5F8C');
  // Decode reads an integer expression as the bits; the control re-reads the input.
  await expression(panel).fill('0x3C00 | 0x8000');
  await inspector.getByRole('group', { name: 'Direction' }).getByRole('button', { name: 'Decode' }).click();
  await expect(panel.locator('.hint')).toHaveText('Reads a bit pattern and shows the number it encodes.');
  await expect(inspector).toContainText('Value -1');
  // A pattern wider than the format is refused.
  await expression(panel).fill('0x412A0000');
  await expression(panel).press('Enter');
  await expect(panel.locator('.calc-result')).toContainText('This bit pattern is wider than the format.');
  const inspections = await page.evaluate(() => window.__studentHarness.calcInspections);
  expect(inspections.at(-1)).toMatchObject({ input: '0x412A0000', format: 'binary16', direction: 'decode', wordSize: 32, inputBase: 'dec', signedMode: true });
});

test('with the inspector disabled the IEEE 754 tab does not exist', async ({ page }) => {
  const panel = await programmerPanel(page, { calculator: { enabled: true, modes: ['programmer'], angle_mode: 'user', disabled_functions: ['ieee754'] } });
  await expect(panel.getByRole('tab', { name: 'IEEE 754' })).toHaveCount(0);
  // The rest of the programmer panel stays, and the exponent hint does not offer Decode.
  await expect(panel.getByRole('tab', { name: 'Radix Point' })).toBeVisible();
  await expression(panel).fill('1e3');
  await expression(panel).press('Enter');
  await expect(panel.locator('.calc-result')).toContainText('Exponents are not accepted. Write the number in full.');
  await expect(panel.locator('.calc-result')).not.toContainText('Decode');
});


test('programmer input accepts paste and each displayed base has its own copy control', async ({ page, context }) => {
  await context.grantPermissions(['clipboard-read', 'clipboard-write']);
  const panel = await programmerPanel(page, onlyProgrammer);
  await page.evaluate(() => navigator.clipboard.writeText('127'));
  await expression(panel).press('Control+V');
  await expect(expression(panel)).toHaveValue('127');
  await expression(panel).press('Enter');
  await panel.getByRole('button', { name: 'Copy signed decimal', exact: true }).click();
  expect(await page.evaluate(() => navigator.clipboard.readText())).toBe('127');
  await panel.getByRole('button', { name: 'Copy hexadecimal', exact: true }).click();
  expect(await page.evaluate(() => navigator.clipboard.readText())).toMatch(/^0x0*7F$/);
});
