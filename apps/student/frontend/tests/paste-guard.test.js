import { test } from 'node:test';
import assert from 'node:assert/strict';
import { PasteGuard, guardWindow } from '../src/workspace/paste-guard.js';

test('only the last text copied inside the application may be pasted', () => {
  const guard = new PasteGuard();
  assert.equal(guard.allows('anything'), false, 'nothing was copied yet');
  guard.noteCopy('int x = 1;');
  assert.equal(guard.allows('int x = 1;'), true);
  assert.equal(guard.allows('int x = 2;'), false);
  assert.equal(guard.allows(''), false);
  assert.equal(guard.allows(undefined), false);
  guard.noteCopy('second');
  assert.equal(guard.allows('int x = 1;'), false, 'only the last copy counts');
  assert.equal(guard.allows('second'), true);
  guard.noteCopy('');
  assert.equal(guard.allows('second'), true, 'an empty copy changes nothing');
});

test('line breaks are compared as one kind', () => {
  const guard = new PasteGuard();
  guard.noteCopy('a\r\nb\rc\n');
  assert.equal(guard.allows('a\nb\nc\n'), true);
  assert.equal(guard.allows('a\r\nb\r\nc\r\n'), true);
});

// A page with just enough of a window: listeners by event name and phase, and a selection.
function fakeWindow(selection = '') {
  const listeners = [];
  const target = () => ({
    addEventListener: (type, fn, capture = false) => listeners.push({ type, fn, capture: !!capture, owner: this }),
    removeEventListener: (type, fn) => { const i = listeners.findIndex(l => l.type === type && l.fn === fn); if (i >= 0) listeners.splice(i, 1); },
  });
  const win = { ...target(), document: target(), getSelection: () => ({ toString: () => selection }), listeners };
  // Fire an event: capture listeners first, as a browser does, stopping at stopImmediatePropagation.
  win.fire = (type, init = {}) => {
    const event = { type, ...init, defaultPrevented: false, stopped: false,
      preventDefault() { this.defaultPrevented = true; }, stopPropagation() {}, stopImmediatePropagation() { this.stopped = true; } };
    for (const l of [...listeners.filter(x => x.type === type && x.capture), ...listeners.filter(x => x.type === type && !x.capture)]) { if (event.stopped) break; l.fn(event); }
    return event;
  };
  return win;
}
const clip = text => ({ getData: kind => (kind === 'text/plain' ? text : '') });

test('a paste from outside is stopped before anything sees it, and one from inside goes through', () => {
  const win = fakeWindow();
  const guard = new PasteGuard();
  const blocked = [];
  guardWindow(win, guard, kind => blocked.push(kind));
  const seen = [];
  win.addEventListener('paste', () => seen.push('editor'), true); // an editor listening later in the same phase
  win.fire('copy', { clipboardData: clip('copied inside') });
  const inside = win.fire('paste', { clipboardData: clip('copied inside') });
  assert.equal(inside.defaultPrevented, false);
  assert.deepEqual(blocked, []);
  seen.length = 0;
  const outside = win.fire('paste', { clipboardData: clip('from a web page') });
  assert.equal(outside.defaultPrevented, true);
  assert.deepEqual(blocked, ['paste']);
  assert.deepEqual(seen, [], 'the editor never saw the paste');
});

test('a copy with no clipboard data falls back to the selection, and a cut counts too', () => {
  const win = fakeWindow('selected text');
  const guard = new PasteGuard();
  guardWindow(win, guard, () => {});
  win.fire('copy', { clipboardData: clip('') });
  assert.equal(guard.allows('selected text'), true);
  win.fire('cut', { clipboardData: clip('cut text') });
  assert.equal(guard.allows('cut text'), true);
});

test('a drag inside the application is a copy; a drop of files or of other text is stopped', () => {
  const win = fakeWindow('dragged selection');
  const guard = new PasteGuard();
  const blocked = [];
  guardWindow(win, guard, kind => blocked.push(kind));
  win.fire('dragstart');
  const text = text => ({ files: [], items: [], getData: () => text });
  assert.equal(win.fire('drop', { dataTransfer: text('dragged selection') }).defaultPrevented, false);
  assert.equal(win.fire('drop', { dataTransfer: text('something else') }).defaultPrevented, true);
  assert.equal(win.fire('drop', { dataTransfer: { files: [{}], items: [], getData: () => 'dragged selection' } }).defaultPrevented, true);
  assert.equal(win.fire('drop', { dataTransfer: { files: [], items: [{ kind: 'file' }], getData: () => '' } }).defaultPrevented, true);
  assert.deepEqual(blocked, ['drop', 'drop', 'drop']);
  assert.equal(win.fire('dragover').defaultPrevented, true, 'the page takes the drop, so the webview does not open the file');
});

test('the listeners come off again', () => {
  const win = fakeWindow();
  const off = guardWindow(win, new PasteGuard(), () => {});
  assert.ok(win.listeners.length > 0);
  off();
  assert.equal(win.listeners.length, 0);
});
