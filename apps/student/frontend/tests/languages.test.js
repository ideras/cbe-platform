import { test } from 'node:test';
import assert from 'node:assert/strict';
import { StringStream } from '@codemirror/language';
import { languageName, loadLanguage } from '../src/languages.js';
import { mipsMode } from '../src/mips.js';

test('language aliases are case insensitive and parser promises are shared', async () => {
  for (const name of ['c', 'h', 'C++', 'cpp', 'cc', 'cxx']) assert.equal(languageName(name), 'cpp');
  for (const name of ['python', 'PY']) assert.equal(languageName(name), 'python');
  for (const name of ['mips', 'MIPS32', 'asm', 's']) assert.equal(languageName(name), 'mips');
  assert.deepEqual(await loadLanguage('java'), []);
  assert.equal(loadLanguage('c'), loadLanguage('C++'));
  assert.equal(loadLanguage('mips'), loadLanguage('s'));
});

test('MIPS highlights instructions, labels, registers, directives, numbers and comments, not unknown words', () => {
  const stream = new StringStream('start: addi $t0, $sp, 0x10 # comment', 4, 4);
  const tokens = [];
  while (!stream.eol()) { stream.start = stream.pos; const style = mipsMode.token(stream); if (style) tokens.push([stream.current(), style]); }
  assert.deepEqual(tokens, [['start:', 'labelName'], ['addi', 'keyword'], ['$t0', 'variableName'], ['$sp', 'variableName'], ['0x10', 'number'], ['# comment', 'comment']]);
  for (const [text, expected] of [['.asciiz', 'typeName'], ['"hello"', 'string'], ["'x'", 'string'], ['123', 'number'], ['unknown', null]]) {
    assert.equal(mipsMode.token(new StringStream(text, 4, 4)), expected);
  }
});
