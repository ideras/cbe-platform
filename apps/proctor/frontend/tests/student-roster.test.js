import assert from 'node:assert/strict';
import { test } from 'node:test';
import { foldStudentText, studentHighlights, visibleStudents } from '../src/student-roster.js';

const students = [
  { account_number: '20210033', student_name: 'Luis Fernando Hernández Rivera' },
  { account_number: '20190001', student_name: 'Mariana Isabel López Castro' },
  { account_number: '20200027', student_name: 'Ñandú Ortiz' },
  { account_number: '20220052', student_name: '' },
];
const accounts = list => list.map(student => student.account_number);

test('text is folded without case or accents, so a search finds a name as it is spoken', () => {
  assert.equal(foldStudentText('José NÚÑEZ'), 'jose nunez');
  assert.equal(foldStudentText('José'), 'jose'); // a decomposed accent folds the same way
  assert.equal(foldStudentText(null), '');
  assert.equal(foldStudentText(undefined), '');
});

test('students are listed by account number or by name, in either direction', () => {
  assert.deepEqual(accounts(visibleStudents(students, '')), ['20190001', '20200027', '20210033', '20220052']);
  assert.deepEqual(accounts(visibleStudents(students, '', 'account_number', false)), ['20220052', '20210033', '20200027', '20190001']);
  // The empty name sorts first; ties fall back to the account number.
  assert.deepEqual(accounts(visibleStudents(students, '', 'student_name')), ['20220052', '20210033', '20190001', '20200027']);
  assert.deepEqual(accounts(visibleStudents(students, '', 'student_name', false)), ['20200027', '20190001', '20210033', '20220052']);
  assert.deepEqual(accounts(visibleStudents(students, '', 'anything else')), ['20190001', '20200027', '20210033', '20220052']);
});

test('a search matches the name or the account without regard to case or accents, and never reorders the roster it was given', () => {
  const before = structuredClone(students);
  assert.deepEqual(accounts(visibleStudents(students, 'hernandez')), ['20210033']);
  assert.deepEqual(accounts(visibleStudents(students, '  LOPEZ ')), ['20190001']);
  assert.deepEqual(accounts(visibleStudents(students, 'nandu')), ['20200027']);
  assert.deepEqual(accounts(visibleStudents(students, '2021')), ['20210033']);
  assert.deepEqual(accounts(visibleStudents(students, 'zzz')), []);
  assert.deepEqual(students, before);
});

test('a student without a name is still listed, searched and sorted', () => {
  assert.deepEqual(accounts(visibleStudents(students, '20220052')), ['20220052']);
  assert.deepEqual(accounts(visibleStudents([{ account_number: '1' }], '')), ['1']);
});

test('highlights are text spans cut from the original text, whatever the accents or case', () => {
  assert.deepEqual(studentHighlights('Hernández Rivera', 'hernandez'), [{ text: '', match: false }, { text: 'Hernández', match: true }, { text: ' Rivera', match: false }]);
  assert.deepEqual(studentHighlights('Luis López', 'LOPEZ'), [{ text: 'Luis ', match: false }, { text: 'López', match: true }, { text: '', match: false }]);
  assert.deepEqual(studentHighlights('20190001', '0001'), [{ text: '2019', match: false }, { text: '0001', match: true }, { text: '', match: false }]);
});

test('without a match, or a search, the text stays whole', () => {
  assert.deepEqual(studentHighlights('Ada Lovelace', ''), [{ text: 'Ada Lovelace', match: false }]);
  assert.deepEqual(studentHighlights('Ada Lovelace', '   '), [{ text: 'Ada Lovelace', match: false }]);
  assert.deepEqual(studentHighlights('Ada Lovelace', 'turing'), [{ text: 'Ada Lovelace', match: false }]);
  assert.deepEqual(studentHighlights(null, 'a'), [{ text: '', match: false }]);
});

test('a highlight keeps decomposed accents and emoji together and never splits a character', () => {
  // "José" written with a combining accent: the match covers the whole letter with its mark.
  const decomposed = 'José Pérez';
  const found = studentHighlights(decomposed, 'jose');
  assert.deepEqual(found.map(part => part.text), ['', 'José', ' Pérez']);
  assert.equal(found.map(part => part.text).join(''), decomposed);
  // Text after an emoji (two UTF-16 units) is cut at the right offset.
  const emoji = 'Ana 😀 María';
  const span = studentHighlights(emoji, 'maria');
  assert.deepEqual(span.map(part => part.text), ['Ana 😀 ', 'María', '']);
  assert.deepEqual(studentHighlights('Ana 😀 María', '😀').map(part => part.text), ['Ana ', '😀', ' María']);
  for (const query of ['a', 'maria', 'ana 😀', 'é']) {
    const text = 'Ana 😀 Maríá é';
    assert.equal(studentHighlights(text, query).map(part => part.text).join(''), text, `rejoins for "${query}"`);
  }
});
