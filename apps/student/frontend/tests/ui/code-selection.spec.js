import { test, expect } from '@playwright/test';

// What a student sees, not what the page says. The selection is drawn in a layer behind the text, so the DOM reports a
// selection even when something painted on the line hides it: only the pixels of a real screenshot tell.
const STARTER = '// a comment line to select\nint second = 22222222; // trailing\nconst char *s = "text";';

async function open(page) {
  await page.setViewportSize({ width: 1000, height: 700 });
  await page.goto('/');
  await page.evaluate(starter => window.__studentHarness.exam({ navigation: 'free', content: { exercises: [{ id: '1', title: 'Select', body: '<div class="slot" data-slot="c"></div>',
    slots: [{ id: 'c', kind: 'code', language: 'c', starter }] }] } }), STARTER);
  await page.getByLabel('I have read these rules').check();
  await page.getByLabel('Account number').fill('00123');
  await page.getByLabel('Exam PIN').fill('PIN');
  await page.getByRole('button', { name: 'Login / resume' }).click();
  const editor = page.getByRole('textbox', { name: 'c code answer for c', exact: true });
  await expect(editor).toBeVisible();
  await editor.click();
  return editor;
}
const token = (page, name) => page.evaluate(name => { const probe = document.createElement('span'); probe.style.color = `var(--${name})`; document.body.append(probe); const color = getComputedStyle(probe).color; probe.remove(); return color; }, name);
// The colour most of a row of pixels has, across `x` to `x + width` at height `y` of the page, taken from a screenshot. Letters cover
// a minority of the row, so this is the ground behind them.
async function groundAt(page, x, y, width) {
  const png = (await page.screenshot({ clip: { x, y, width, height: 1 } })).toString('base64');
  return page.evaluate(async data => {
    const bitmap = await createImageBitmap(new Blob([Uint8Array.from(atob(data), c => c.charCodeAt(0))], { type: 'image/png' }));
    const canvas = document.createElement('canvas'); canvas.width = bitmap.width; canvas.height = 1;
    const context = canvas.getContext('2d'); context.drawImage(bitmap, 0, 0);
    const pixels = context.getImageData(0, 0, bitmap.width, 1).data, counts = new Map();
    for (let i = 0; i < pixels.length; i += 4) { const key = `rgb(${pixels[i]}, ${pixels[i + 1]}, ${pixels[i + 2]})`; counts.set(key, (counts.get(key) ?? 0) + 1); }
    return [...counts.entries()].sort((a, b) => b[1] - a[1])[0][0];
  }, png);
}
// The boxes the selection is drawn in, one per line it touches, in page coordinates. The layer is drawn a moment after the key
// press, so this waits for the `count` boxes the selection should have, each with some width.
async function selectionBoxes(page, count = 1) {
  const read = () => page.evaluate(() => [...document.querySelectorAll('.cm-selectionBackground')].map(element => element.getBoundingClientRect().toJSON()));
  await expect.poll(async () => { const boxes = await read(); return boxes.length === count && boxes.every(box => box.width > 4) }).toBe(true);
  return read();
}
// Looks above the letters' middle, where only capitals, ascenders and slashes reach.
const upperRow = box => box.y + box.height * 0.25;
async function expectShown(page, box, ground) {
  await expect.poll(() => groundAt(page, box.x + 1, upperRow(box), box.width - 2)).toBe(await token(page, 'selection-fill'));
  // The rest of the same line, past its last letter, is the ground of the editor (a selection that runs on to the next line fills
  // the line to its edge, and then there is no rest to look at).
  const line = await page.locator('.cm-content').boundingBox();
  const rest = line.x + line.width - box.right - 80;
  if (rest > 20) expect(await groundAt(page, box.right + 40, upperRow(box), rest)).toBe(ground);
}

test('a selection made on the cursor’s own line shows, where the cursor is', async ({ page }) => {
  const editor = await open(page);
  const paper = await token(page, 'paper');
  await editor.press('Control+Home'); await editor.press('Shift+End'); // the whole first line, which is the line the cursor is on
  const [box] = await selectionBoxes(page, 1);
  await expectShown(page, box, paper);
});

test('a selection inside a line of code shows too', async ({ page }) => {
  const editor = await open(page);
  await editor.press('Control+Home'); await editor.press('ArrowDown'); await editor.press('Home');
  for (let i = 0; i < 4; i++) await editor.press('ArrowRight');
  for (let i = 0; i < 12; i++) await editor.press('Shift+ArrowRight');
  const [box] = await selectionBoxes(page, 1);
  await expectShown(page, box, await token(page, 'paper'));
});

test('a selection across lines shows on every line it touches', async ({ page }) => {
  const editor = await open(page);
  await editor.press('Control+Home'); await editor.press('Shift+ArrowDown'); await editor.press('Shift+End');
  const boxes = await selectionBoxes(page, 2);
  for (const box of boxes) await expectShown(page, box, await token(page, 'paper'));
});

test('the selection keeps the theme’s colour when the editor loses the focus', async ({ page }) => {
  const editor = await open(page);
  await editor.press('Control+Home'); await editor.press('Shift+End');
  await page.evaluate(() => document.activeElement.blur()); // the editor is no longer focused
  await expect(editor).not.toBeFocused();
  const [box] = await selectionBoxes(page, 1); // the selection stays
  await expect.poll(() => groundAt(page, box.x + 1, upperRow(box), box.width - 2)).toBe(await token(page, 'selection-fill'));
});

test('the cursor’s line is told by its number, and its text has no fill of its own', async ({ page }) => {
  const editor = await open(page);
  // The first gutter element is a hidden spacer that sizes the column; the real numbers follow it.
  const number = n => page.locator('.cm-lineNumbers .cm-gutterElement', { hasText: new RegExp(`^${n}$`) }).last();
  await editor.press('Control+Home'); await editor.press('ArrowDown');
  await expect(number(2)).toHaveCSS('font-weight', '700');
  await expect(number(2)).toHaveCSS('color', await token(page, 'action-hover'));
  await expect(number(1)).not.toHaveCSS('font-weight', '700');
  await expect(page.locator('.cm-activeLine')).toHaveCount(0);
  await editor.press('ArrowUp');
  await expect(number(1)).toHaveCSS('font-weight', '700');
  await expect(number(2)).not.toHaveCSS('font-weight', '700');
});
