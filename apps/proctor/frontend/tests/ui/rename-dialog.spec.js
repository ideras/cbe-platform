import { test, expect } from '@playwright/test';
import { connect, calls } from './helpers.js';

const MATH = '44444444-4444-4444-8444-444444444444';
const opener = page => page.getByRole('button', { name: 'Rename…' });
const dialogOf = page => page.getByRole('dialog', { name: 'Rename class' });

// Opens a class on its page and its Rename dialog; returns the dialog and the pieces of it the tests use.
async function open(page, { prepare, openClass = 'Mathematics 10A' } = {}) {
  await connect(page, { prepare, openClass });
  await opener(page).click();
  const dialog = dialogOf(page);
  await expect(dialog).toBeVisible();
  return { dialog, field: dialog.getByLabel('Class name'), submit: dialog.getByRole('button', { name: /^Rename/ }), counter: dialog.getByText(/^\d+ \/ 120$/), note: dialog.locator('#rename-class-msg') };
}

test('the dialog opens on the class name, selected, and says what stays as it is', async ({ page }) => {
  const { dialog, field, submit, counter, note } = await open(page);
  // Named after the class it changes, with the current name under the title.
  await expect(dialog.locator('header').getByText('Mathematics 10A', { exact: true })).toBeVisible();
  // Ready to type: the field has the focus and its whole text is selected, so typing replaces the name.
  await expect(field).toBeFocused();
  await expect(field).toHaveValue('Mathematics 10A');
  expect(await field.evaluate(input => [input.selectionStart, input.selectionEnd])).toEqual([0, 15]);
  await expect(counter).toHaveText('15 / 120');
  await expect(note).toHaveText('Same as the current name.');
  await expect(submit).toBeDisabled(); // nothing to change yet
  // The class keeps its students and exams, and the dialog counts them as the page does.
  const keeps = dialog.getByRole('list', { name: 'Stays exactly as it is' });
  await expect(keeps.getByRole('listitem')).toHaveText(['28 students', '3 exams']);
  await expect(dialog.getByText('Enter', { exact: true })).toBeVisible(); await expect(dialog.getByText('Esc', { exact: true })).toBeVisible();
});

test('one exam is not plural, and the count is the page’s own', async ({ page }) => {
  const { dialog } = await open(page, { prepare: () => window.ProctorHarness.historyExam(), openClass: 'History 11B' });
  await expect(dialog.getByRole('list', { name: 'Stays exactly as it is' }).getByRole('listitem')).toHaveText(['24 students', '1 exam']);
});

test('what is typed is judged as the server will judge it, before anything is sent', async ({ page }) => {
  const { dialog, field, submit, counter, note } = await open(page);
  const judge = async (text, message, { ready = false, invalid = false } = {}) => {
    await field.fill(text);
    await expect(note).toHaveText(message);
    if (ready) await expect(submit).toBeEnabled(); else await expect(submit).toBeDisabled();
    await expect(field).toHaveAttribute('aria-invalid', String(invalid));
  };
  await judge('', 'The class needs a name.', { invalid: true });
  await expect(counter).toHaveText('0 / 120');
  await expect(dialog.getByRole('button', { name: 'Clear' })).toHaveCount(0); // nothing to clear
  await judge('   ', 'The class needs a name.', { invalid: true });
  await judge('  Mathematics   10A ', 'Same as the current name.'); // space around and inside does not make a different name
  await expect(counter).toHaveText('15 / 120');
  await judge('Mathematics  11A', 'Looks good.', { ready: true });
  await expect(counter).toHaveText('15 / 120'); // counted as it will be saved
  await judge('MATHEMATICS 10A', 'Looks good.', { ready: true }); // a class may change the case of its own name
  // Another class's name, whatever its case or spacing.
  await judge('history 11b', 'Another class already uses this name.', { invalid: true });
  await judge(' History   11B ', 'Another class already uses this name.', { invalid: true });
  // The server's limit is 120 characters.
  await judge('a'.repeat(120), 'Looks good.', { ready: true });
  await expect(counter).toHaveText('120 / 120'); await expect(counter).toHaveClass(/near/);
  await judge('a'.repeat(121), 'Too long; use at most 120 characters.', { invalid: true });
  await expect(counter).toHaveText('121 / 120'); await expect(counter).toHaveClass(/over/);
  await judge('a'.repeat(100), 'Looks good.', { ready: true });
  await expect(counter).not.toHaveClass(/near|over/);
  expect((await calls(page)).renameClass).toEqual([]);
});

test('the clear button empties the field and keeps the cursor in it', async ({ page }) => {
  const { dialog, field, submit } = await open(page);
  await dialog.getByRole('button', { name: 'Clear' }).click();
  await expect(field).toHaveValue(''); await expect(field).toBeFocused();
  await expect(dialog.getByRole('button', { name: 'Clear' })).toHaveCount(0);
  await expect(submit).toBeDisabled();
  await field.fill('Mathematics 11A');
  await expect(dialog.getByRole('button', { name: 'Clear' })).toBeVisible();
});

test('Escape cancels and gives the focus back to the button that opened the dialog', async ({ page }) => {
  const { dialog, field } = await open(page);
  await field.fill('Mathematics 11A');
  await page.keyboard.press('Escape');
  await expect(dialog).toBeHidden();
  await expect(opener(page)).toBeFocused();
  await expect(page.getByRole('heading', { name: 'Mathematics 10A', level: 1 })).toBeVisible();
  expect((await calls(page)).renameClass).toEqual([]);
  // The next time it opens on the real name, not on what was abandoned.
  await opener(page).click();
  await expect(dialogOf(page).getByLabel('Class name')).toHaveValue('Mathematics 10A');
});

test('Enter renames with the name as it will be saved; with nothing to change it does nothing', async ({ page }) => {
  const { dialog, field } = await open(page);
  await page.keyboard.press('Enter');
  await expect(dialog).toBeVisible(); // the name has not changed
  expect((await calls(page)).renameClass).toEqual([]);
  await field.fill('  Mathematics    11A ');
  await field.press('Enter');
  await expect(dialog).toBeHidden();
  expect((await calls(page)).renameClass).toEqual([{ id: MATH, name: 'Mathematics 11A' }]);
  await expect(page.getByRole('heading', { name: 'Mathematics 11A', level: 1 })).toBeVisible();
  await expect(page.getByRole('status')).toContainText('The class is now called “Mathematics 11A”.');
  await expect(opener(page)).toBeFocused();
});

test('another class’s name cannot be sent, and neither can a name the field refuses', async ({ page }) => {
  const { dialog, field, submit } = await open(page);
  await field.fill('History 11B');
  await expect(submit).toBeDisabled();
  await field.press('Enter');
  await expect(dialog).toBeVisible();
  await submit.click({ force: true }).catch(() => {}); // a disabled button sends nothing
  expect((await calls(page)).renameClass).toEqual([]);
});

test('a refusal the server still gives stays under the field until the name is edited', async ({ page }) => {
  const { dialog, field, submit, note } = await open(page, { prepare: () => window.ProctorHarness.renameFails('The server refused the change: a class with this name already exists.') });
  await field.fill('Mathematics 11A');
  await submit.click();
  const refusal = dialog.getByRole('alert');
  await expect(refusal).toContainText('a class with this name already exists');
  await expect(field).toHaveAttribute('aria-invalid', 'true');
  await expect(dialog).toBeVisible(); await expect(submit).toBeEnabled(); // it can be tried again
  expect((await calls(page)).renameClass).toEqual([{ id: MATH, name: 'Mathematics 11A' }]);
  await field.pressSequentially('B');
  await expect(refusal).toHaveCount(0);
  await expect(note).toHaveText('Looks good.');
  await expect(page.getByRole('heading', { name: 'Mathematics 10A', level: 1 })).toBeVisible();
});

test('while the server works the dialog cannot be edited or dismissed', async ({ page }) => {
  const { dialog, field, submit } = await open(page, { prepare: () => window.ProctorHarness.holdRename() });
  await field.fill('Mathematics 11A');
  await submit.click();
  await expect(dialog.getByRole('button', { name: 'Renaming…' })).toBeDisabled();
  for (const control of [field, dialog.getByRole('button', { name: 'Cancel', exact: true }), dialog.getByRole('button', { name: 'Close', exact: true }), dialog.getByRole('button', { name: 'Clear' })]) await expect(control).toBeDisabled();
  await page.keyboard.press('Escape');
  await expect(dialog).toBeVisible();
  await page.evaluate(() => window.ProctorHarness.releaseRename());
  await expect(dialog).toBeHidden();
  await expect(page.getByRole('heading', { name: 'Mathematics 11A', level: 1 })).toBeVisible();
  expect((await calls(page)).renameClass).toEqual([{ id: MATH, name: 'Mathematics 11A' }]);
});

test('a very long name wraps under the title instead of widening the dialog', async ({ page }) => {
  const long = 'M'.repeat(120); // no space to break at
  const { field, submit } = await open(page);
  await field.fill(long);
  await submit.click();
  await expect(page.getByRole('heading', { name: long, level: 1 })).toBeVisible();
  await opener(page).click();
  const dialog = dialogOf(page);
  await expect(dialog.locator('header').getByText(long, { exact: true })).toBeVisible();
  expect(await dialog.evaluate(element => element.scrollWidth <= element.clientWidth)).toBe(true);
  const box = await dialog.boundingBox();
  expect(box.x).toBeGreaterThanOrEqual(0); expect(box.x + box.width).toBeLessThanOrEqual(page.viewportSize().width);
});

test('in a narrow window the dialog fits, drops the key hint and stacks full-width buttons with the main one first', async ({ page }) => {
  await page.setViewportSize({ width: 400, height: 700 });
  const { dialog, field, submit } = await open(page);
  const box = await dialog.boundingBox();
  expect(box.x).toBeGreaterThanOrEqual(0); expect(box.x + box.width).toBeLessThanOrEqual(400);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await expect(dialog.getByText('Enter', { exact: true })).toBeHidden();
  await field.fill('Mathematics 11A');
  const cancel = dialog.getByRole('button', { name: 'Cancel', exact: true });
  const [main, other] = [await submit.boundingBox(), await cancel.boundingBox()];
  expect(main.y).toBeLessThan(other.y);
  expect(Math.abs(main.width - other.width)).toBeLessThan(1); expect(main.width).toBeGreaterThan(300);
});

test('the dialog is a dark-headed card with a pencil, as in the mockup', async ({ page }) => {
  const { dialog, submit, field } = await open(page);
  const token = name => page.evaluate(name => { const probe = document.createElement('span'); probe.style.color = `var(--${name})`; document.body.append(probe); const color = getComputedStyle(probe).color; probe.remove(); return color; }, name);
  await expect(dialog.locator('header')).toHaveCSS('color', await token('paper'));
  expect(await dialog.locator('header').evaluate(header => getComputedStyle(header).backgroundImage)).toContain('linear-gradient');
  await expect(dialog.locator('header svg').first()).toBeVisible(); // the pencil
  await field.fill('Mathematics 11A');
  await expect(submit).toHaveCSS('background-color', await token('action-primary'));
  await expect(dialog.getByRole('button', { name: 'Cancel', exact: true })).toHaveCSS('color', await token('text-muted'));
});
