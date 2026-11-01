import assert from 'node:assert/strict';
import { test } from 'node:test';
import { CLASS_NAME_MAX, checkClassName, cleanName, nameLength } from '../src/class-name.js';

const others = ['Organización de Computadoras Q42026', 'Test Class'];

test('a name is saved without the space around it, with runs of white space as one space', () => {
  assert.equal(cleanName('  Compiladores   I\tQ42026 '), 'Compiladores I Q42026');
  assert.equal(checkClassName('  Compiladores   II ', 'Compiladores I').name, 'Compiladores II');
});

test('a changed, free name is ready; the same name is not a change', () => {
  assert.deepEqual(checkClassName('Compiladores I Q12027', 'Compiladores I Q42026', others),
    { name: 'Compiladores I Q12027', length: 21, changed: true, error: '', message: 'Looks good.', ready: true });
  const same = checkClassName(' Compiladores I Q42026 ', 'Compiladores I Q42026', others);
  assert.equal(same.ready, false); assert.equal(same.error, ''); assert.equal(same.message, 'Same as the current name.');
});

test('an empty name, or one of nothing but white space, is refused', () => {
  for (const text of ['', '   ', '\t ']) {
    const result = checkClassName(text, 'History 11B');
    assert.equal(result.ready, false); assert.equal(result.error, 'The class needs a name.'); assert.equal(result.message, result.error);
  }
});

test('the limit is the server’s 120 characters, counted after cleaning and by character', () => {
  assert.equal(CLASS_NAME_MAX, 120);
  assert.equal(checkClassName('a'.repeat(120), 'x').ready, true);
  const long = checkClassName('a'.repeat(121), 'x');
  assert.equal(long.ready, false); assert.equal(long.error, 'Too long; use at most 120 characters.');
  assert.equal(checkClassName(`${'a'.repeat(120)}   `, 'x').ready, true); // trailing space is not saved
  assert.equal(nameLength('📚'.repeat(120)), 120); // 240 UTF-16 units
  assert.equal(checkClassName('📚'.repeat(120), 'x').ready, true);
  assert.equal(checkClassName('📚'.repeat(121), 'x').ready, false);
});

test('control characters are refused, as the server refuses them', () => {
  assert.equal(checkClassName('Math\u0007 10A', 'History').error, 'The name cannot contain control characters.');
  assert.equal(checkClassName('Math\u0085 10A', 'History').ready, false);
  assert.equal(checkClassName('Math\t10A', 'History').name, 'Math 10A'); // a tab is white space, not an error
});

test('another class’s name is refused whatever its ASCII case, as the server compares names', () => {
  for (const text of ['test class', 'TEST CLASS', ' Test   Class ']) {
    const result = checkClassName(text, 'History 11B', others);
    assert.equal(result.ready, false); assert.equal(result.error, 'Another class already uses this name.');
  }
  // SQLite's NOCASE folds ASCII only, so a name that differs from another's only in a non-ASCII letter's case is free.
  assert.equal(checkClassName('ORGANIZACIÓN DE COMPUTADORAS Q42026', 'History', others).error, '');
  assert.equal(checkClassName('organización de computadoras q42026', 'History', others).error, 'Another class already uses this name.');
  assert.equal(checkClassName('ORGANIZACIÓN DE COMPUTADORAS Q42026', 'History', ['organización de computadoras q42026']).error, '');
});

test('a class may change the case of its own name, which is not among the others', () => {
  const result = checkClassName('test class', 'Test Class', ['Other class']);
  assert.equal(result.ready, true); assert.equal(result.message, 'Looks good.');
});
