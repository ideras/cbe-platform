import { test, expect } from '@playwright/test';
import { connect, calls, nav, card, examsPanel, tab } from './helpers.js';

const MATH = '44444444-4444-4444-8444-444444444444';
// The roster is in the Students tab of the class.
async function students(page) {
  await tab(page, /^Students/).click();
  return page.getByRole('region', { name: 'Roster' });
}
const newClass = { file: 'CIQ42026-Students.csv', valid: true, students: 11, warning_count: 1, warnings: [{ row: 5, account_number: '', message: 'empty account_number; skipped' }] };

test('the classes are cards with their size and exams, as in the dashboard design', async ({ page }) => {
  await connect(page);
  await expect(page.getByRole('heading', { name: 'Classes', level: 1 })).toBeVisible();
  const math = card(page, 'Mathematics 10A');
  await expect(math).toContainText('28 students'); await expect(math).toContainText('3 exams · Databases midterm published');
  const history = card(page, 'History 11B');
  await expect(history).toContainText('24 students'); await expect(history).toContainText('No exams yet');
  await expect(page.getByRole('searchbox', { name: 'Search classes' })).toBeVisible();
  await expect(page.getByRole('button', { name: 'Add class' })).toBeVisible();
});

test('a class counts its exams, and names the published one only while it is published', async ({ page }) => {
  await connect(page, { prepare: () => window.ProctorHarness.nothingPublished() });
  const math = card(page, 'Mathematics 10A');
  await expect(math).toContainText('3 exams'); await expect(math).not.toContainText('published');
});

test('the published exam is named on the card of the class it belongs to, whichever class that is', async ({ page }) => {
  await connect(page, { prepare: () => window.ProctorHarness.historyExam() });
  await expect(card(page, 'History 11B')).toContainText('1 exam'); await expect(card(page, 'History 11B')).not.toContainText('published');
  await card(page, 'History 11B').click();
  await examsPanel(page).getByRole('button', { name: 'Publish History final…' }).click();
  const dialog = page.getByRole('dialog', { name: 'Publish exam' });
  await dialog.getByRole('checkbox').check(); await dialog.getByRole('button', { name: 'Publish exam', exact: true }).click();
  await expect(page.getByRole('status')).toContainText('is published to History 11B');
  await page.getByRole('main').getByRole('button', { name: 'Classes', exact: true }).click();
  await expect(card(page, 'History 11B')).toContainText('1 exam · History final published');
  await expect(card(page, 'Mathematics 10A')).toContainText('3 exams'); await expect(card(page, 'Mathematics 10A')).not.toContainText('published');
});

test('searching ignores case and accents and says when nothing matches', async ({ page }) => {
  await connect(page);
  const search = page.getByRole('searchbox', { name: 'Search classes' });
  await search.fill('MATHEMÁTICS');
  await expect(card(page, 'Mathematics 10A')).toBeVisible(); await expect(card(page, 'History 11B')).toHaveCount(0);
  await search.fill('zzz');
  await expect(page.getByText('No class matches “zzz”.')).toBeVisible();
  await search.fill('');
  await expect(card(page, 'History 11B')).toBeVisible();
});

test('with no class the page explains what to do', async ({ page }) => {
  await connect(page, { prepare: () => window.ProctorHarness.noClasses() });
  await expect(page.getByText('No classes yet.')).toBeVisible();
  await expect(page.getByRole('button', { name: 'Add class' })).toBeEnabled();
});

test('opening a class shows its exams first, and its read-only roster in the other tab', async ({ page }) => {
  await connect(page);
  await card(page, 'Mathematics 10A').click();
  await expect(page.getByRole('heading', { name: 'Mathematics 10A', level: 1 })).toBeVisible();
  // Two tabs with their counts; the exams are the default.
  await expect(page.getByRole('tablist')).toBeVisible();
  await expect(page.getByRole('tab')).toHaveText(['Exams (3)', 'Students (28)']);
  await expect(tab(page, 'Exams (3)')).toHaveAttribute('aria-selected', 'true');
  await expect(tab(page, 'Students (28)')).toHaveAttribute('aria-selected', 'false');
  await expect(examsPanel(page)).toBeVisible();
  await expect(page.getByRole('region', { name: 'Roster' })).toHaveCount(0);
  expect((await calls(page)).classRoster).toEqual([MATH]);
  await expect(page.getByRole('region', { name: 'Class details' })).toHaveCount(0);

  await tab(page, 'Students (28)').click();
  await expect(tab(page, 'Students (28)')).toHaveAttribute('aria-selected', 'true');
  await expect(examsPanel(page)).toHaveCount(0);
  const roster = page.getByRole('region', { name: 'Roster' });
  await expect(roster).toContainText('Read-only');
  // The count is part of the heading, on the same line: "Roster (28 students)".
  const heading = roster.getByRole('heading', { name: 'Roster (28 students)', level: 2 });
  await expect(heading).toBeVisible();
  expect((await heading.boundingBox()).height).toBeLessThan(32);
  await expect(roster.getByText('28 students', { exact: true })).toHaveCount(0);
  await expect(roster.getByRole('columnheader')).toHaveText(['Account number', 'Full name']);
  const rows = roster.locator('tbody tr');
  await expect(rows).toHaveCount(28);
  await expect(rows.nth(0).getByRole('cell')).toHaveText(['0001', 'Ada Lovelace']);
  await expect(rows.nth(27).getByRole('cell')).toHaveText(['0028', 'Student 028']);
  // The roster was loaded once, when the class was opened, and switching tabs does not load it again.
  await tab(page, 'Exams (3)').click(); await tab(page, 'Students (28)').click();
  expect((await calls(page)).classRoster).toEqual([MATH]);
  // The list cannot be edited student by student: the only ways to change it are to replace the whole file or to reload.
  const buttons = await roster.getByRole('button').allInnerTexts();
  expect(buttons.map(text => text.trim())).toEqual(['Replace roster…', 'Reload']);
  await expect(roster.getByRole('textbox')).toHaveCount(0);
  await page.getByRole('main').getByRole('button', { name: 'Classes', exact: true }).click(); // the back button
  await expect(card(page, 'History 11B')).toBeVisible();
  await card(page, 'Mathematics 10A').click();
  await expect(tab(page, 'Exams (3)')).toHaveAttribute('aria-selected', 'true'); // a class opens on its exams again
  await nav(page).getByRole('button', { name: 'Classes', exact: true }).click(); // the navigation entry leads back to the list too
  await expect(card(page, 'History 11B')).toBeVisible();
});

test('the tabs follow the keyboard: arrows, Home and End, with only the chosen tab in the tab order', async ({ page }) => {
  await connect(page, { openClass: 'Mathematics 10A' });
  const exams = tab(page, 'Exams (3)'), roster = tab(page, 'Students (28)');
  await expect(exams).toHaveAttribute('tabindex', '0'); await expect(roster).toHaveAttribute('tabindex', '-1');
  await exams.focus();
  await page.keyboard.press('ArrowRight');
  await expect(roster).toBeFocused(); await expect(roster).toHaveAttribute('aria-selected', 'true');
  await expect(page.getByRole('region', { name: 'Roster' })).toBeVisible();
  await page.keyboard.press('ArrowRight'); // wraps around
  await expect(exams).toBeFocused(); await expect(exams).toHaveAttribute('aria-selected', 'true');
  await page.keyboard.press('End'); await expect(roster).toBeFocused();
  await page.keyboard.press('Home'); await expect(exams).toBeFocused();
  await page.keyboard.press('ArrowLeft'); await expect(roster).toBeFocused();
  // Each tab labels its own panel.
  await expect(page.getByRole('tabpanel', { name: 'Students (28)' })).toBeVisible();
  await expect(page.getByRole('tabpanel', { name: 'Exams (3)' })).toBeHidden();
});

test('the roster loading state, a load failure and a retry', async ({ page }) => {
  await connect(page, { prepare: () => { window.ProctorHarness.holdRoster(); window.ProctorHarness.rosterFails('Server unreachable or request timed out.'); } });
  await card(page, 'History 11B').click();
  const roster = await students(page);
  await expect(roster).toContainText('Loading roster…');
  await expect(roster.getByRole('button', { name: 'Reload' })).toBeDisabled();
  await page.evaluate(() => window.ProctorHarness.releaseRoster());
  await expect(roster.getByRole('alert')).toContainText('The roster could not be loaded. Server unreachable or request timed out.');
  await page.evaluate(() => window.ProctorHarness.rosterFails(''));
  await roster.getByRole('button', { name: 'Reload' }).click();
  await expect(roster.locator('tbody tr')).toHaveCount(24);
  await expect(roster.getByRole('alert')).toHaveCount(0);
});

test('student names are shown as text, an empty name is marked and a long roster scrolls inside its frame', async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  await connect(page, { prepare: () => window.ProctorHarness.classRoster('44444444-4444-4444-8444-444444444444', [['0001', '<img src=x onerror=alert(1)> María Ñandú'], ['0002', ''], ...Array.from({ length: 120 }, (_, i) => [String(i + 3).padStart(4, '0'), `Student ${i + 3}`])]) });
  await card(page, 'Mathematics 10A').click();
  const roster = await students(page);
  await expect(roster.locator('tbody tr').nth(0).getByRole('cell').nth(1)).toHaveText('<img src=x onerror=alert(1)> María Ñandú');
  await expect(roster.locator('tbody img')).toHaveCount(0);
  await expect(roster.locator('tbody tr').nth(1).getByRole('cell').nth(1)).toHaveText('—');
  const frame = roster.locator('.table-scroll');
  const metrics = await frame.evaluate(element => ({ client: element.clientHeight, scroll: element.scrollHeight }));
  expect(metrics.scroll).toBeGreaterThan(metrics.client); expect(metrics.client).toBeLessThanOrEqual(500);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
});

test('a roster cut short by the server says so', async ({ page }) => {
  await connect(page, { prepare: () => window.ProctorHarness.classRoster('44444444-4444-4444-8444-444444444444', [['0001', 'Ada'], ['0002', 'Bob']], 6000) });
  await card(page, 'Mathematics 10A').click();
  await tab(page, /^Students/).click();
  await expect(page.getByRole('heading', { name: 'Roster (6000 students)' })).toBeVisible();
  await expect(page.getByRole('region', { name: 'Roster' })).toContainText('Showing the first 2 of 6000.');
});

test('adding a class checks the roster file first and stores it only on Add class', async ({ page }) => {
  await connect(page, { prepare: () => window.ProctorHarness.classFile({ file: 'CIQ42026-Students.csv', valid: true, students: 11, warning_count: 1, warnings: [{ row: 5, account_number: '', message: 'empty account_number; skipped' }] }) });
  await page.getByRole('button', { name: 'Add class' }).click();
  const dialog = page.getByRole('dialog', { name: 'Add class' });
  await expect(dialog.getByRole('button', { name: 'Add class' })).toBeDisabled();
  await dialog.getByRole('button', { name: 'Browse…' }).click();
  await expect(dialog).toContainText('CIQ42026-Students.csv'); await expect(dialog).toContainText('11 students found in this file.');
  await dialog.getByText('1 warning').click(); await expect(dialog).toContainText('Row 5: empty account_number; skipped');
  const name = dialog.getByLabel('Class name');
  await expect(name).toHaveValue('CIQ42026-Students');
  await name.fill('CIQ4 2026');
  expect((await calls(page)).createClass).toEqual([]);
  await dialog.getByRole('button', { name: 'Add class' }).click();
  await expect(dialog).toBeHidden();
  await expect(page.getByRole('status')).toContainText('Class “CIQ4 2026” added with 11 students.');
  await expect(card(page, 'CIQ4 2026')).toContainText('11 students');
  expect((await calls(page)).createClass).toEqual(['CIQ4 2026']);
});

test('cancelling forgets the file, and an unusable file lists its problems and cannot be added', async ({ page }) => {
  await connect(page, { prepare: () => window.ProctorHarness.classFile({ file: 'bad.csv', valid: false, students: 0, problems: ['roster CSV missing required column "email"'] }) });
  await page.getByRole('button', { name: 'Add class' }).click();
  const dialog = page.getByRole('dialog', { name: 'Add class' });
  await dialog.getByRole('button', { name: 'Browse…' }).click();
  await expect(dialog.getByRole('alert')).toContainText('This file cannot be used');
  await expect(dialog).toContainText('missing required column "email"');
  await dialog.getByLabel('Class name').fill('Anything');
  await expect(dialog.getByRole('button', { name: 'Add class' })).toBeDisabled();
  await dialog.getByRole('button', { name: 'Cancel', exact: true }).click();
  await expect(dialog).toBeHidden();
  expect(await calls(page)).toMatchObject({ discardClassFile: 1, createClass: [] });
});

test('a name the server refuses stays in the dialog so it can be changed', async ({ page }) => {
  await connect(page, { prepare: () => { window.ProctorHarness.classFile({ file: 'class.csv', valid: true, students: 3 }); window.ProctorHarness.createFails('The server refused the change: a class with this name already exists.'); } });
  await page.getByRole('button', { name: 'Add class' }).click();
  const dialog = page.getByRole('dialog', { name: 'Add class' });
  await dialog.getByRole('button', { name: 'Browse…' }).click();
  await dialog.getByLabel('Class name').fill('Mathematics 10A');
  await dialog.getByRole('button', { name: 'Add class' }).click();
  await expect(dialog.getByRole('alert')).toContainText('a class with this name already exists');
  await expect(dialog).toBeVisible();
  await page.evaluate(() => window.ProctorHarness.createFails(''));
  await dialog.getByLabel('Class name').fill('Mathematics 10B');
  await dialog.getByRole('button', { name: 'Add class' }).click();
  await expect(dialog).toBeHidden();
  await expect(card(page, 'Mathematics 10B')).toBeVisible();
});

test('closing the file dialog without a choice changes nothing', async ({ page }) => {
  await connect(page);
  await page.getByRole('button', { name: 'Add class' }).click();
  const dialog = page.getByRole('dialog', { name: 'Add class' });
  await dialog.getByRole('button', { name: 'Browse…' }).click();
  await expect(dialog).toContainText('No file chosen');
  await expect(dialog.getByRole('button', { name: 'Add class' })).toBeDisabled();
});

test('the classes page stays inside a narrow window', async ({ page }) => {
  await page.setViewportSize({ width: 600, height: 700 });
  await connect(page);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await card(page, 'Mathematics 10A').click();
  await expect(examsPanel(page)).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await tab(page, /^Students/).click();
  await expect(page.getByRole('region', { name: 'Roster' })).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
});

const open = async (page, name = 'Mathematics 10A') => { await card(page, name).click(); await expect(page.getByRole('heading', { name, level: 1 })).toBeVisible(); await expect(tab(page, /^Exams/)).toBeVisible(); };

test('a class is renamed from its page and keeps everything else', async ({ page }) => {
  await connect(page);
  await open(page, 'History 11B');
  await page.getByRole('button', { name: 'Rename…' }).click();
  const dialog = page.getByRole('dialog', { name: 'Rename class' });
  const name = dialog.getByLabel('Class name');
  await expect(name).toHaveValue('History 11B');
  await expect(dialog.getByRole('button', { name: 'Rename', exact: true })).toBeDisabled(); // unchanged
  await name.fill('   '); await expect(dialog.getByRole('button', { name: 'Rename', exact: true })).toBeDisabled();
  await name.fill('History 11C');
  await dialog.getByRole('button', { name: 'Rename', exact: true }).click();
  await expect(dialog).toBeHidden();
  await expect(page.getByRole('status')).toContainText('The class is now called “History 11C”.');
  await expect(page.getByRole('heading', { name: 'History 11C', level: 1 })).toBeVisible();
  await expect(tab(page, 'Students (24)')).toBeVisible();
  await expect((await students(page)).locator('tbody tr')).toHaveCount(24);
  expect((await calls(page)).renameClass).toEqual([{ id: '55555555-5555-4555-8555-555555555555', name: 'History 11C' }]);
});

test('a name the server refuses stays in the rename dialog, and cancelling changes nothing', async ({ page }) => {
  await connect(page, { prepare: () => window.ProctorHarness.renameFails('The server refused the change: a class with this name already exists.') });
  await open(page);
  await page.getByRole('button', { name: 'Rename…' }).click();
  const dialog = page.getByRole('dialog', { name: 'Rename class' });
  await dialog.getByLabel('Class name').fill('History 11B');
  await dialog.getByRole('button', { name: 'Rename', exact: true }).click();
  await expect(dialog.getByRole('alert')).toContainText('already exists');
  await expect(dialog).toBeVisible();
  await dialog.getByRole('button', { name: 'Cancel', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'Mathematics 10A', level: 1 })).toBeVisible();
});

test('deleting a class asks first, names what goes and what stays, and returns to the list', async ({ page }) => {
  await connect(page);
  await open(page, 'History 11B');
  await page.getByRole('button', { name: 'Delete class…' }).click();
  const dialog = page.getByRole('dialog', { name: 'Delete class' });
  await expect(dialog).toContainText('Delete History 11B (24 students)?');
  await expect(dialog).toContainText('It has no exams');
  await expect(dialog).toContainText('never logged in are removed from the server');
  const confirm = dialog.getByRole('button', { name: 'Delete class', exact: true });
  await expect(confirm).toBeDisabled();
  await dialog.getByRole('button', { name: 'Cancel', exact: true }).click();
  expect((await calls(page)).deleteClass).toEqual([]);
  await page.getByRole('button', { name: 'Delete class…' }).click();
  await dialog.getByRole('checkbox').check();
  await confirm.click();
  await expect(page.getByRole('heading', { name: 'Classes', level: 1 })).toBeVisible();
  await expect(page.getByRole('status')).toContainText('Class “History 11B” deleted; 2 students who never logged in left the server too.');
  await expect(card(page, 'History 11B')).toHaveCount(0); await expect(card(page, 'Mathematics 10A')).toBeVisible();
  expect((await calls(page)).deleteClass).toEqual([{ id: '55555555-5555-4555-8555-555555555555', confirmed: true }]);
});

test('a class with exams cannot be deleted, and the page says why', async ({ page }) => {
  await connect(page);
  await open(page); // Mathematics 10A has three exams, one of them published
  await expect(page.getByRole('button', { name: 'Delete class…' })).toBeDisabled();
  await expect(page.getByText('Delete its exams first. Exams students have taken cannot be deleted.')).toBeVisible();
  await expect(page.getByRole('button', { name: 'Delete class…' })).toHaveAccessibleDescription('Delete its exams first. Exams students have taken cannot be deleted.');
  await expect(page.getByRole('button', { name: 'Rename…' })).toBeEnabled();
  expect((await calls(page)).deleteClass).toEqual([]);
});

test('a class can be deleted again once its exams are deleted', async ({ page }) => {
  await connect(page, { prepare: () => window.ProctorHarness.historyExam() });
  await open(page, 'History 11B');
  const remove = page.getByRole('button', { name: 'Delete class…' });
  await expect(remove).toBeDisabled();
  await examsPanel(page).getByRole('button', { name: 'Delete exam History final…' }).click();
  const dialog = page.getByRole('dialog', { name: 'Delete exam' });
  await dialog.getByRole('checkbox').check(); await dialog.getByRole('button', { name: 'Delete exam', exact: true }).click();
  await expect(page.getByRole('status')).toContainText('Exam “History final” deleted.');
  await expect(remove).toBeEnabled();
  await expect(page.getByText('Delete its exams first.')).toHaveCount(0);
  await expect(tab(page, 'Exams (0)')).toBeVisible();
});

test('a deletion the server refuses is explained and nothing disappears', async ({ page }) => {
  await connect(page, { prepare: () => window.ProctorHarness.deleteFails('The server refused the change: the situation changed. Refresh and try again.') });
  await open(page, 'History 11B');
  await page.getByRole('button', { name: 'Delete class…' }).click();
  const dialog = page.getByRole('dialog', { name: 'Delete class' });
  await dialog.getByRole('checkbox').check(); await dialog.getByRole('button', { name: 'Delete class', exact: true }).click();
  await expect(dialog.getByRole('alert')).toContainText('the situation changed');
  await dialog.getByRole('button', { name: 'Cancel', exact: true }).click();
  await expect(page.getByRole('heading', { name: 'History 11B', level: 1 })).toBeVisible();
});

test('a new roster is compared first and replaces the old one only on confirmation', async ({ page }) => {
  await connect(page, { prepare: () => window.ProctorHarness.rosterChange({ file: 'class-a-new.csv', valid: true, students: 30, added: 4, removed: 2, warning_count: 1, warnings: [{ row: 7, account_number: '', message: 'empty account_number; skipped' }], pruned: 1 }) });
  await open(page);
  await tab(page, /^Students/).click();
  await page.getByRole('button', { name: 'Replace roster…' }).click();
  const dialog = page.getByRole('dialog', { name: 'Replace roster' });
  await expect(dialog.getByRole('button', { name: 'Replace roster', exact: true })).toBeDisabled();
  await dialog.getByRole('button', { name: 'Browse…' }).click();
  await expect(dialog).toContainText('class-a-new.csv');
  await expect(dialog).toContainText('30 students in the file: 4 new to this class, 2 in the class now but not in the file.');
  await expect(dialog).toContainText('2 students will no longer be able to log in through this class.');
  await expect(dialog).toContainText('This class is the one admitted to the published exam right now');
  await dialog.getByText('1 warning').click(); await expect(dialog).toContainText('Row 7: empty account_number; skipped');
  const replace = dialog.getByRole('button', { name: 'Replace roster', exact: true });
  await expect(replace).toBeDisabled(); // the removed students have to be acknowledged
  expect((await calls(page)).replaceRoster).toEqual([]);
  await dialog.getByRole('checkbox').check();
  await replace.click();
  await expect(dialog).toBeHidden();
  await expect(page.getByRole('status')).toContainText('Roster replaced: 30 students (4 new, 2 removed; 1 student no longer on the server).');
  await expect(page.getByRole('heading', { name: 'Roster (30 students)' })).toBeVisible();
  await expect(tab(page, 'Students (30)')).toBeVisible();
  expect((await calls(page)).replaceRoster).toEqual([{ id: '44444444-4444-4444-8444-444444444444', confirmed: true }]);
});

test('a roster that only adds students needs no acknowledgement', async ({ page }) => {
  await connect(page, { prepare: () => window.ProctorHarness.rosterChange({ file: 'more.csv', valid: true, students: 30, added: 2, removed: 0 }) });
  await open(page, 'History 11B');
  await tab(page, /^Students/).click();
  await page.getByRole('button', { name: 'Replace roster…' }).click();
  const dialog = page.getByRole('dialog', { name: 'Replace roster' });
  await dialog.getByRole('button', { name: 'Browse…' }).click();
  await expect(dialog).not.toContainText('will no longer be able to log in');
  await expect(dialog.getByRole('checkbox')).toHaveCount(0);
  await dialog.getByRole('button', { name: 'Replace roster', exact: true }).click();
  await expect(page.getByRole('status')).toContainText('Roster replaced: 30 students (2 new, 0 removed).');
});

test('cancelling forgets the new roster, and an unusable file lists its problems', async ({ page }) => {
  await connect(page, { prepare: () => window.ProctorHarness.rosterChange({ file: 'bad.csv', valid: false, problems: ['roster CSV missing required column "email"'], students: 0 }) });
  await open(page);
  await tab(page, /^Students/).click();
  await page.getByRole('button', { name: 'Replace roster…' }).click();
  const dialog = page.getByRole('dialog', { name: 'Replace roster' });
  await dialog.getByRole('button', { name: 'Browse…' }).click();
  await expect(dialog.getByRole('alert')).toContainText('This file cannot be used');
  await expect(dialog).toContainText('missing required column "email"');
  await expect(dialog.getByRole('button', { name: 'Replace roster', exact: true })).toBeDisabled();
  await dialog.getByRole('button', { name: 'Cancel', exact: true }).click();
  expect(await calls(page)).toMatchObject({ discardRosterFile: 1, replaceRoster: [] });
});

test('a refused replacement is explained in the dialog', async ({ page }) => {
  await connect(page, { prepare: () => { window.ProctorHarness.rosterChange({ file: 'more.csv', valid: true, students: 30, added: 2, removed: 0 }); window.ProctorHarness.replaceFails('The server refused the change: the situation changed.'); } });
  await open(page);
  await tab(page, /^Students/).click();
  await page.getByRole('button', { name: 'Replace roster…' }).click();
  const dialog = page.getByRole('dialog', { name: 'Replace roster' });
  await dialog.getByRole('button', { name: 'Browse…' }).click();
  await dialog.getByRole('button', { name: 'Replace roster', exact: true }).click();
  await expect(dialog.getByRole('alert')).toContainText('the situation changed');
  await expect(page.getByRole('heading', { name: 'Roster (28 students)' })).toBeVisible();
});
