import { test } from 'node:test';
import assert from 'node:assert/strict';
import { join, dirname, basename, isInside, moved, nameProblem, languageOf, sortEntries } from '../src/workspace/paths.js';

test('paths are written from /work down', () => {
  assert.equal(join('', 'a.c'), 'a.c');
  assert.equal(join('src', 'a.c'), 'src/a.c');
  assert.equal(dirname('src/a/b.c'), 'src/a');
  assert.equal(dirname('a.c'), '');
  assert.equal(basename('src/a/b.c'), 'b.c');
  assert.equal(basename('b.c'), 'b.c');
  assert.ok(isInside('src/a.c', 'src'));
  assert.ok(isInside('src', 'src'));
  assert.ok(!isInside('srcs/a.c', 'src'));
  assert.ok(isInside('anything', ''));
  assert.equal(moved('src/a.c', 'src', 'lib'), 'lib/a.c');
  assert.equal(moved('src', 'src', 'lib'), 'lib');
  assert.equal(moved('srcs/a.c', 'src', 'lib'), 'srcs/a.c');
});

test('a name the agent would refuse is refused here, in words', () => {
  for (const good of ['a.c', 'Makefile', 'my file.txt', '.gitignore', 'ñ世界.cpp', 'x'.repeat(255)]) assert.equal(nameProblem(good), '', good);
  for (const bad of ['', '.', '..', 'a/b', 'a\\b', 'a\u0000b', 'a\nb', 'a\u007fb', '.cbe-tmp', 'x'.repeat(256), 'é'.repeat(128)]) assert.notEqual(nameProblem(bad), '', JSON.stringify(bad));
  assert.match(nameProblem('a/b'), /one at a time/);
});

test('the editor language comes from the extension, and Makefile is plain', () => {
  assert.equal(languageOf('src/main.cpp'), 'cpp');
  assert.equal(languageOf('a.H'), 'cpp');
  assert.equal(languageOf('x.py'), 'python');
  assert.equal(languageOf('boot.S'), 'mips');
  assert.equal(languageOf('Makefile'), '');
  assert.equal(languageOf('notes.txt'), '');
  assert.equal(languageOf('noextension'), '');
});

test('folders come first and names sort the way a person expects', () => {
  const sorted = sortEntries([{ name: 'b.c', kind: 'file' }, { name: 'src', kind: 'dir' }, { name: 'A.c', kind: 'file' }, { name: 'file10', kind: 'file' }, { name: 'file2', kind: 'file' }, { name: 'Zed', kind: 'dir' }]);
  assert.deepEqual(sorted.map(e => e.name), ['src', 'Zed', 'A.c', 'b.c', 'file2', 'file10']);
});
