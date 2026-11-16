import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync, readdirSync } from 'node:fs';

// Nothing of a workspace is written to the student's computer (plan, Phase 4: "no localStorage, IndexedDB or file writes from workspace
// code"). The UI test watches the page while it is used; this reads the code, so a later change cannot add a way without a test noticing.
const forbiddenInPage = /\b(localStorage|sessionStorage|indexedDB|caches|showSaveFilePicker|showOpenFilePicker|showDirectoryPicker|FileSystemFileHandle|FileSystemDirectoryHandle|document\.cookie|navigator\.storage|BroadcastChannel|Cache\b|StorageManager|openDatabase|createWritable)\b/;
const forbiddenInGo = /\bos\.(Create|CreateTemp|MkdirTemp|MkdirAll|Mkdir|WriteFile|OpenFile|Rename|Remove|RemoveAll)\b|ioutil\.|os\.File\b|\bos\.Truncate\b/;
const read = path => readFileSync(new URL(path, import.meta.url), 'utf8');
const stripComments = text => text.replace(/\/\*[\s\S]*?\*\/|\/\/.*$/gm, '').replace(/<!--[\s\S]*?-->/g, '');

test('the workspace page never touches the browser\'s storage or the file system', () => {
  const dir = new URL('../src/workspace/', import.meta.url);
  const names = readdirSync(dir).filter(name => /\.(js|svelte)$/.test(name));
  assert.ok(names.length >= 8, 'the workspace files are where this test looks');
  for (const name of names) assert.doesNotMatch(stripComments(read(`../src/workspace/${name}`)), forbiddenInPage, name);
});

test('the client\'s workspace code never writes a file', () => {
  assert.doesNotMatch(stripComments(read('../../internal/client/workspace.go')), forbiddenInGo);
});

test('the guard finds what it is looking for', () => {
  for (const bad of ['localStorage.setItem(a, b)', 'window.indexedDB.open("x")', 'await showSaveFilePicker()', 'document.cookie = "a=b"']) assert.match(bad, forbiddenInPage);
  for (const bad of ['os.WriteFile(path, data, 0o600)', 'os.Create(name)']) assert.match(bad, forbiddenInGo);
});
