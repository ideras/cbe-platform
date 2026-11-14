import { test } from 'node:test';
import assert from 'node:assert/strict';
import { WorkspaceModel, AUTOSAVE_MS, REFRESH_MS, MAX_TERMINALS } from '../src/workspace/model.js';
import { FakeWorkspaceServer, versionOf } from './fake-workspace-server.js';

// A clock the test moves, so a second of idle is no real second.
function clock() {
  let now = 0; let next = 1; const timers = new Map();
  return {
    set(fn, ms) { const id = next++; timers.set(id, { at: now + ms, fn }); return id; },
    clear(id) { timers.delete(id); },
    async advance(ms) {
      await settle(); // what was just done may have scheduled a timer
      const until = now + ms;
      for (;;) {
        const due = [...timers].filter(([, t]) => t.at <= until).sort((a, b) => a[1].at - b[1].at)[0];
        if (!due) break;
        timers.delete(due[0]); now = due[1].at; due[1].fn();
        await settle();
      }
      now = until;
      await settle();
    },
    now: () => 1000 + now,
  };
}
const settle = async () => { for (let i = 0; i < 5; i += 1) { await new Promise(resolve => setImmediate(resolve)); await new Promise(resolve => setTimeout(resolve, 1)); } };

function setup(options = {}) {
  const server = new FakeWorkspaceServer(options.server ?? { files: { 'Makefile': 'all:\n', 'src/list.cpp': 'int a;\n', 'src/main.cpp': 'int main(){}\n', 'notes.txt': 'hi\n' } });
  const time = clock();
  const notes = [];
  const model = new WorkspaceModel({
    transport: { request: (type, fields) => server.request(type, fields), input: async (term, data) => server.input(term, data), reconnect: async () => {} },
    notify: note => notes.push(note), scheduler: time, now: time.now,
  });
  server.onMessage = message => model.handleMessage(message);
  server.onOutput = (term, data) => model.output(term, data);
  const view = { connection: 'connected', generation: 1, terminal: options.terminal ?? true, can_run: true, can_check: true };
  return { server, model, time, notes, view,
    async connect() { model.setView(this.view); await model.syncing; await settle(); },
    async drop() { this.view = { ...this.view, connection: 'reconnecting' }; server.online = false; model.setView(this.view); },
    async back() { server.online = true; this.view = { ...this.view, connection: 'connected', generation: this.view.generation + 1 }; model.setView(this.view); await model.syncing; await settle(); } };
}
// An editor for a file: the text is the editor's, the model is told when it changes.
function editor(model, path, text) {
  const e = { value: text, text() { return this.value; }, replace(next) { this.value = next; }, type(next) { this.value = next; model.edited(path); } };
  model.registerBuffer(path, e);
  return e;
}
async function open(h, path) {
  await h.model.openFile(path);
  return editor(h.model, path, h.model.tab(path).savedText);
}

test('connecting lists the root and opens nothing', async () => {
  const h = setup();
  await h.connect();
  assert.deepEqual(h.model.folders.get('').entries.map(e => `${e.kind}:${e.name}`), ['dir:src', 'file:Makefile', 'file:notes.txt']);
  assert.deepEqual(h.model.tabs, []);
  assert.equal(h.model.readOnly, false);
});

test('opening a file reads it, and opening it again only selects it', async () => {
  const h = setup();
  await h.connect();
  await h.model.openFile('notes.txt');
  assert.equal(h.model.tab('notes.txt').state, 'ready');
  assert.equal(h.model.tab('notes.txt').savedText, 'hi\n');
  await h.model.openFile('Makefile');
  await h.model.openFile('notes.txt');
  assert.equal(h.model.tabs.length, 2);
  assert.equal(h.model.activePath, 'notes.txt');
  assert.equal(h.server.requests.filter(r => r.type === 'fs.read' && r.path === 'notes.txt').length, 1);
});

test('files that are not text or too big are said so, not shown', async () => {
  const h = setup({ server: { files: { 'a.bin': 'x\0y', 'big.txt': 'x'.repeat((1 << 20) + 1) } } });
  await h.connect();
  await h.model.openFile('a.bin');
  await h.model.openFile('big.txt');
  assert.equal(h.model.tab('a.bin').state, 'binary');
  assert.equal(h.model.tab('big.txt').state, 'toolarge');
  assert.match(h.model.tab('big.txt').message, /1 MiB/);
});

test('an edit is saved after one second of idle, and typing keeps postponing it', async () => {
  const h = setup();
  await h.connect();
  const e = await open(h, 'notes.txt');
  e.type('hi there\n');
  assert.equal(h.model.tab('notes.txt').dirty, true);
  await h.time.advance(AUTOSAVE_MS - 100);
  e.type('hi there!\n');
  await h.time.advance(AUTOSAVE_MS - 100);
  assert.equal(h.server.files.get('notes.txt'), 'hi\n', 'still waiting: the student is typing');
  await h.time.advance(200);
  assert.equal(h.server.files.get('notes.txt'), 'hi there!\n');
  assert.equal(h.model.tab('notes.txt').dirty, false);
  assert.ok(h.model.lastSave);
});

test('switching tabs saves the file left behind at once', async () => {
  const h = setup();
  await h.connect();
  const e = await open(h, 'notes.txt');
  await h.model.openFile('Makefile');
  h.model.activate('notes.txt');
  e.type('changed\n');
  h.model.activate('Makefile');
  await settle();
  assert.equal(h.server.files.get('notes.txt'), 'changed\n');
});

test('typing while a write is on its way is not lost, and is written next', async () => {
  const h = setup();
  await h.connect();
  const e = await open(h, 'notes.txt');
  h.server.delay = 5;
  e.type('one\n');
  const first = h.model.save('notes.txt');
  e.type('one two\n');
  await first;
  h.server.delay = 0;
  await h.time.advance(AUTOSAVE_MS + 50);
  assert.equal(h.server.files.get('notes.txt'), 'one two\n');
  assert.equal(h.model.tab('notes.txt').dirty, false);
});

test('a file changed in the terminal while it is open and untouched is reloaded in place', async () => {
  const h = setup();
  await h.connect();
  const e = await open(h, 'notes.txt');
  h.server.externalWrite('notes.txt', 'from the terminal\n');
  await h.time.advance(REFRESH_MS + 10);
  assert.equal(e.value, 'from the terminal\n');
  assert.equal(h.model.tab('notes.txt').dirty, false);
  assert.equal(h.model.tab('notes.txt').conflict, null);
});

test('a file changed in the terminal while it has unsaved edits asks, and Reload takes theirs', async () => {
  const h = setup();
  await h.connect();
  const e = await open(h, 'notes.txt');
  e.type('mine\n');
  h.server.externalWrite('notes.txt', 'theirs\n');
  await h.time.advance(REFRESH_MS + 10);
  const tab = h.model.tab('notes.txt');
  assert.deepEqual(tab.conflict, { version: versionOf('theirs\n'), content: 'theirs\n' });
  assert.equal(e.value, 'mine\n', 'the edits are not touched while the question is open');
  await h.time.advance(AUTOSAVE_MS * 3);
  assert.equal(h.server.files.get('notes.txt'), 'theirs\n', 'nothing is written over the file while it waits');
  await h.model.resolveConflict('notes.txt', 'reload');
  assert.equal(e.value, 'theirs\n');
  assert.equal(tab.dirty, false);
  assert.equal(tab.conflict, null);
});

test('Keep yours writes the student\'s text over the file', async () => {
  const h = setup();
  await h.connect();
  const e = await open(h, 'notes.txt');
  e.type('mine\n');
  h.server.externalWrite('notes.txt', 'theirs\n');
  await h.time.advance(REFRESH_MS + 10);
  await h.model.resolveConflict('notes.txt', 'keep');
  assert.equal(h.server.files.get('notes.txt'), 'mine\n');
  assert.equal(h.model.tab('notes.txt').dirty, false);
  assert.equal(h.model.tab('notes.txt').conflict, null);
});

test('a write refused as a conflict is read back, and the same text is no conflict', async () => {
  const h = setup();
  await h.connect();
  const e = await open(h, 'notes.txt');
  e.type('same\n');
  h.server.files.set('notes.txt', 'same\n'); // the terminal wrote the very same text, without an event the model has seen
  await h.model.save('notes.txt');
  assert.equal(h.model.tab('notes.txt').dirty, false);
  assert.equal(h.model.tab('notes.txt').conflict, null);
  assert.equal(h.model.tab('notes.txt').version, versionOf('same\n'));
});

test('an answer lost on the way is not a conflict when the write was made', async () => {
  const h = setup();
  await h.connect();
  const e = await open(h, 'notes.txt');
  e.type('lost reply\n');
  h.server.loseNextReply = true;
  assert.equal(await h.model.save('notes.txt'), false);
  assert.equal(h.server.files.get('notes.txt'), 'lost reply\n');
  assert.equal(h.model.tab('notes.txt').dirty, true, 'the model cannot know, so it keeps the edit');
  assert.equal(await h.model.save('notes.txt'), true);
  assert.equal(h.model.tab('notes.txt').dirty, false);
  assert.equal(h.model.tab('notes.txt').conflict, null);
});

test('edits made while the connection is down are written after it comes back, through the conflict check', async () => {
  const h = setup();
  await h.connect();
  const e = await open(h, 'notes.txt');
  e.type('before the drop\n');
  await h.drop();
  assert.equal(h.model.readOnly, true);
  await h.time.advance(AUTOSAVE_MS * 5);
  assert.equal(h.server.files.get('notes.txt'), 'hi\n');
  assert.equal(h.model.tab('notes.txt').dirty, true, 'the edit waits in memory');
  await h.back();
  assert.equal(h.server.files.get('notes.txt'), 'before the drop\n');
  assert.equal(h.model.tab('notes.txt').dirty, false);
});

test('edits made before a drop that changed in the terminal meanwhile are not written over it', async () => {
  const h = setup();
  await h.connect();
  const e = await open(h, 'notes.txt');
  e.type('mine\n');
  await h.drop();
  h.server.files.set('notes.txt', 'theirs\n');
  await h.back();
  assert.equal(h.server.files.get('notes.txt'), 'theirs\n');
  assert.equal(h.model.tab('notes.txt').conflict?.content, 'theirs\n');
  assert.equal(e.value, 'mine\n');
});

test('a disk that is full keeps the edit and says so once', async () => {
  const h = setup();
  await h.connect();
  const e = await open(h, 'notes.txt');
  h.server.diskFull = true;
  e.type('too much\n');
  await h.time.advance(AUTOSAVE_MS + 10);
  assert.equal(h.model.tab('notes.txt').dirty, true);
  assert.equal(e.value, 'too much\n');
  assert.ok(h.notes.some(n => n.key === 'disk-full' && n.kind === 'error'));
  h.server.diskFull = false;
  assert.equal(await h.model.save('notes.txt'), true);
});

test('a file deleted in the terminal keeps its edits and can be put back by saving', async () => {
  const h = setup();
  await h.connect();
  const e = await open(h, 'notes.txt');
  e.type('precious\n');
  h.server.externalDelete('notes.txt');
  await h.time.advance(REFRESH_MS + 10);
  assert.equal(h.model.tab('notes.txt').missing, true);
  assert.equal(await h.model.save('notes.txt'), true);
  assert.equal(h.server.files.get('notes.txt'), 'precious\n');
  assert.equal(h.model.tab('notes.txt').missing, false);
});

test('files created and deleted in the terminal appear in the tree within the refresh delay', async () => {
  const h = setup();
  await h.connect();
  await h.model.toggleFolder('src');
  h.server.externalWrite('src/new.cpp', 'x');
  h.server.externalWrite('fresh.txt', 'x');
  await h.time.advance(REFRESH_MS + 10);
  assert.ok(h.model.folders.get('').entries.some(e => e.name === 'fresh.txt'));
  assert.ok(h.model.folders.get('src').entries.some(e => e.name === 'new.cpp'));
  h.server.externalDelete('src/new.cpp');
  await h.time.advance(REFRESH_MS + 10);
  assert.ok(!h.model.folders.get('src').entries.some(e => e.name === 'new.cpp'));
});

test('a build that writes many files is one listing', async () => {
  const h = setup();
  await h.connect();
  const before = h.server.requests.filter(r => r.type === 'fs.list').length;
  for (let i = 0; i < 40; i += 1) h.server.externalWrite(`o${i}.o`, 'x');
  await h.time.advance(REFRESH_MS + 10);
  assert.equal(h.server.requests.filter(r => r.type === 'fs.list').length - before, 1);
});

test('creating a file or a folder opens it or shows it, and a name that is taken is refused in words', async () => {
  const h = setup();
  await h.connect();
  assert.equal(await h.model.createFile('src', 'util.cpp'), '');
  assert.equal(h.model.activePath, 'src/util.cpp');
  assert.ok(h.model.folders.get('src').entries.some(e => e.name === 'util.cpp'));
  assert.equal(await h.model.createFolder('', 'tests'), '');
  assert.ok(h.model.folders.get('').entries.some(e => e.name === 'tests' && e.kind === 'dir'));
  assert.match(await h.model.createFile('src', 'util.cpp'), /already exists/);
  assert.match(await h.model.createFile('nowhere', 'a.c'), /not there/);
});

test('renaming a file or a folder moves the tabs under it, and their unsaved text goes with them', async () => {
  const h = setup();
  await h.connect();
  await h.model.toggleFolder('src');
  const e = await open(h, 'src/list.cpp');
  e.type('edited\n');
  assert.equal(await h.model.rename('src', 'source'), '');
  assert.equal(h.model.tab('source/list.cpp')?.state, 'ready');
  assert.equal(h.model.activePath, 'source/list.cpp');
  assert.equal(h.server.files.get('source/list.cpp'), 'edited\n', 'the edit was saved before the move');
  assert.ok(h.model.folders.get('').entries.some(x => x.name === 'source'));
  assert.ok(!h.model.folders.get('').entries.some(x => x.name === 'src'));
  e.value = 'edited again\n'; h.model.edited('source/list.cpp');
  await h.model.save('source/list.cpp');
  assert.equal(h.server.files.get('source/list.cpp'), 'edited again\n');
  assert.match(await h.model.rename('source/list.cpp', 'main.cpp'), /already exists/);
});

test('deleting closes the tabs under it and leaves the rest', async () => {
  const h = setup();
  await h.connect();
  await h.model.toggleFolder('src');
  await open(h, 'src/list.cpp');
  await open(h, 'notes.txt');
  assert.equal(await h.model.remove('src'), '');
  assert.deepEqual(h.model.tabs.map(t => t.path), ['notes.txt']);
  assert.equal(h.model.activePath, 'notes.txt');
  assert.ok(!h.model.folders.get('').entries.some(e => e.name === 'src'));
  assert.ok(!h.server.files.has('src/list.cpp'));
});

test('closing a tab saves it first, and a tab that cannot be saved stays', async () => {
  const h = setup();
  await h.connect();
  const e = await open(h, 'notes.txt');
  e.type('kept\n');
  assert.equal(await h.model.closeTab('notes.txt'), true);
  assert.equal(h.server.files.get('notes.txt'), 'kept\n');
  const f = await open(h, 'Makefile');
  f.type('all:\n\techo\n');
  h.server.diskFull = true;
  assert.equal(await h.model.closeTab('Makefile'), false);
  assert.ok(h.model.tab('Makefile'));
});

test('Run saves every file first, so the program is the one on the screen', async () => {
  const h = setup();
  await h.connect();
  const e = await open(h, 'src/list.cpp');
  e.type('int a; // DONE\n');
  await h.model.run('check');
  assert.equal(h.server.files.get('src/list.cpp'), 'int a; // DONE\n');
  const check = h.model.terminals.at(-1);
  assert.equal(check.kind, 'check');
  assert.equal(h.model.activeTerm, check.term);
  const order = h.server.requests.map(r => r.type);
  assert.ok(order.indexOf('fs.write') < order.indexOf('run.start'));
  await settle();
  assert.equal(h.model.terminal(check.term).exitCode, 0);
});

test('Run does not start while a file has an unresolved conflict', async () => {
  const h = setup();
  await h.connect();
  const e = await open(h, 'notes.txt');
  e.type('mine\n');
  h.server.externalWrite('notes.txt', 'theirs\n');
  await h.time.advance(REFRESH_MS + 10);
  await h.model.run('run');
  assert.equal(h.server.requests.filter(r => r.type === 'run.start').length, 0);
  assert.ok(h.notes.some(n => n.key === 'run-unsaved'));
});

test('a new Run takes the place of the last one, so the tabs do not pile up', async () => {
  const h = setup();
  await h.connect();
  await h.model.run('run'); await settle();
  await h.model.run('run'); await settle();
  await h.model.run('run'); await settle();
  assert.equal(h.model.terminals.filter(t => t.kind === 'run').length, 1);
});

test('terminal output that comes before its view is kept for it, in order', async () => {
  const h = setup();
  await h.connect();
  await h.model.run('run');
  await settle();
  const term = h.model.activeTerm;
  const seen = [];
  h.model.registerSink(term, { write: bytes => seen.push(new TextDecoder().decode(bytes)), reset() {} });
  assert.match(seen.join(''), /list: 3 2 1/);
  h.model.output(term, new TextEncoder().encode('more'));
  assert.equal(seen.at(-1), 'more');
});

test('a shell takes keystrokes and shows what the terminal changed in the tree', async () => {
  const h = setup();
  await h.connect();
  await h.model.openTerminal();
  const term = h.model.activeTerm;
  const seen = [];
  h.model.registerSink(term, { write: bytes => seen.push(new TextDecoder().decode(bytes)), reset() {} });
  await settle();
  h.model.type(term, 'touch made.txt\r');
  await h.time.advance(REFRESH_MS + 10);
  assert.ok(h.model.folders.get('').entries.some(e => e.name === 'made.txt'));
  assert.match(seen.join(''), /touch made\.txt/);
});

test('at most four terminals, and a terminal that is not allowed is not opened', async () => {
  const h = setup();
  await h.connect();
  for (let i = 0; i < MAX_TERMINALS + 2; i += 1) await h.model.openTerminal();
  assert.equal(h.model.terminals.length, MAX_TERMINALS);
  assert.deepEqual(h.model.terminals.map(t => t.title), ['Terminal 1', 'Terminal 2', 'Terminal 3', 'Terminal 4']);
  assert.ok(h.notes.some(n => n.key === 'terminal-limit'));
  await h.model.closeTerminal(h.model.terminals[1].term);
  await h.model.openTerminal();
  assert.deepEqual(h.model.terminals.map(t => t.title).sort(), ['Terminal 1', 'Terminal 2', 'Terminal 3', 'Terminal 4']);
  const g = setup({ terminal: false, server: { files: {}, terminal: false } });
  await g.connect();
  await g.model.openTerminal();
  assert.equal(g.model.terminals.length, 0);
  g.model.terminals.push({ term: 9, exited: false });
  g.model.type(9, 'ls\r');
  assert.equal(g.server.requests.filter(r => r.type === 'pty.open').length, 0);
});

test('after a reconnect the terminals are found again and attached, and the ones that are gone are dropped', async () => {
  const h = setup();
  await h.connect();
  await h.model.openTerminal();
  await h.model.openTerminal();
  const [a, b] = h.model.terminals.map(t => t.term);
  await h.drop();
  h.server.terminals.delete(b); // closed while we were away
  const resets = [];
  h.model.registerSink(a, { write() {}, reset: () => resets.push(a) });
  await h.back();
  assert.deepEqual(h.model.terminals.map(t => t.term), [a]);
  assert.deepEqual(resets, [a]);
  assert.ok(h.server.requests.some(r => r.type === 'pty.attach' && r.term === a));
});

test('a restarted workspace closes the terminals and says the files are safe', async () => {
  const h = setup();
  await h.connect();
  await h.model.openTerminal();
  h.model.handleMessage({ type: 'ws.agent_restarted' });
  assert.deepEqual(h.model.terminals, []);
  assert.equal(h.model.activeTerm, null);
  assert.ok(h.notes.some(n => n.key === 'agent-restarted' && /files are safe/.test(n.text)));
});

test('a TODO tick is shown at once and put back if the server does not take it', async () => {
  const h = setup();
  await h.connect();
  h.model.setTodoState({ t1: false });
  await h.model.setTodo('t1', true);
  assert.equal(h.model.todo.t1, true);
  assert.equal(h.server.todo.t1, true);
  h.server.failNext = { type: 'todo.set', code: 'INTERNAL' };
  await h.model.setTodo('t2', true);
  assert.equal(h.model.todo.t2, false);
  assert.ok(h.notes.some(n => n.key === 'todo'));
});

test('Submit saves first and records the time and the snapshot', async () => {
  const h = setup();
  await h.connect();
  const e = await open(h, 'notes.txt');
  e.type('final\n');
  assert.equal(await h.model.submit(), true);
  assert.equal(h.server.submits.at(-1).get('notes.txt'), 'final\n');
  assert.equal(h.model.lastSubmit.snapshot, 1);
  const g = setup();
  await g.connect();
  await g.drop();
  assert.equal(await g.model.submit(), false);
});

test('a student can keep working through a drop that lasts: edits stay, and nothing is lost over several reconnects', async () => {
  const h = setup();
  await h.connect();
  const e = await open(h, 'notes.txt');
  e.type('1\n');
  await h.drop(); await h.back();
  e.type('2\n');
  await h.drop(); await h.drop();
  assert.equal(h.model.tab('notes.txt').dirty, true);
  await h.back();
  assert.equal(h.server.files.get('notes.txt'), '2\n');
});

test('a workspace that is still starting is asked again, and the tree fills in when it is ready', async () => {
  const h = setup();
  let starting = 2;
  const request = h.server.request.bind(h.server);
  h.server.request = async (type, fields) => (starting > 0 && type === 'fs.list' && starting-- ? { type, ok: false, code: 'UNAVAILABLE', message: 'the workspace is not ready' } : request(type, fields));
  await h.connect();
  assert.equal(h.model.folders.get('').loaded, false);
  assert.match(h.model.folders.get('').error, /starting/);
  await h.time.advance(1000);
  assert.equal(h.model.folders.get('').loaded, false, 'still starting once more');
  await h.time.advance(1000);
  assert.equal(h.model.folders.get('').loaded, true);
  assert.equal(h.model.folders.get('').error, '');
  assert.ok(h.model.folders.get('').entries.length > 0);
});

test('a file that cannot be opened yet is opened when the workspace is ready', async () => {
  const h = setup();
  await h.connect();
  let starting = 1;
  const request = h.server.request.bind(h.server);
  h.server.request = async (type, fields) => (starting > 0 && type === 'fs.read' && starting-- ? { type, ok: false, code: 'UNAVAILABLE', message: 'x' } : request(type, fields));
  await h.model.openFile('notes.txt');
  assert.equal(h.model.tab('notes.txt').state, 'loading');
  await h.time.advance(1000);
  assert.equal(h.model.tab('notes.txt').state, 'ready');
  assert.equal(h.model.tab('notes.txt').savedText, 'hi\n');
});
