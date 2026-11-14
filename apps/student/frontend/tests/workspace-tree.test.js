import { test } from 'node:test';
import assert from 'node:assert/strict';
import { treeRows, folderFor } from '../src/workspace/tree.js';

const folder = (...entries) => ({ entries: entries.map(([name, kind]) => ({ name, kind })), loaded: true, error: '' });

test('only the open folders show what they hold', () => {
  const folders = new Map([['', folder(['src', 'dir'], ['lib', 'dir'], ['Makefile', 'file'])], ['src', folder(['a.c', 'file'], ['deep', 'dir'])], ['src/deep', folder(['x.c', 'file'])]]);
  const closed = treeRows(folders, new Set(['']));
  assert.deepEqual(closed.map(r => r.name), ['src', 'lib', 'Makefile']);
  const open = treeRows(folders, new Set(['', 'src', 'src/deep']));
  assert.deepEqual(open.map(r => `${r.level}:${r.path}`), ['1:src', '2:src/a.c', '2:src/deep', '3:src/deep/x.c', '1:lib', '1:Makefile']);
  assert.equal(open[0].open, true);
  assert.equal(open[2].open, true);
  assert.equal(open[4].open, false);
});

test('a folder that is open says when it is loading, empty or unreadable', () => {
  const folders = new Map([['', folder(['a', 'dir'], ['b', 'dir'], ['c', 'dir'])], ['b', folder()], ['c', { entries: [], loaded: false, error: 'cannot be read' }]]);
  const rows = treeRows(folders, new Set(['', 'a', 'b', 'c']));
  assert.deepEqual(rows.map(r => r.name), ['a', 'Loading…', 'b', 'Empty folder', 'c', 'cannot be read']);
  assert.deepEqual(rows.filter(r => r.kind === 'note').map(r => r.level), [2, 2, 2]);
});

test('a new file goes into the selected folder, or beside the selected file', () => {
  const rows = [{ path: 'src', isDir: true }, { path: 'src/a.c', isDir: false }, { path: 'Makefile', isDir: false }];
  assert.equal(folderFor('src', rows), 'src');
  assert.equal(folderFor('src/a.c', rows), 'src');
  assert.equal(folderFor('Makefile', rows), '');
  assert.equal(folderFor(null, rows), '');
  assert.equal(folderFor('gone', rows), '');
});
