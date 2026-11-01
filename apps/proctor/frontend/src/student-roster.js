export const foldStudentText = value => String(value ?? '').normalize('NFD').replace(/\p{M}/gu, '').toLowerCase();
const collator = new Intl.Collator('es', { sensitivity: 'base' });

export function visibleStudents(students, query, key = 'account_number', ascending = true) {
  const needle = foldStudentText(query.trim());
  const field = key === 'student_name' ? key : 'account_number';
  return students.filter(student => !needle || foldStudentText(student.student_name).includes(needle) || foldStudentText(student.account_number).includes(needle))
    .sort((a, b) => (ascending ? 1 : -1) * (collator.compare(a[field] ?? '', b[field] ?? '') || collator.compare(a.account_number, b.account_number)));
}

// Return text spans, never HTML. Map folded matches back to original UTF-16
// offsets so accents, decomposed combining marks and emoji remain intact.
export function studentHighlights(text, query) {
  text = String(text ?? '');
  const needle = foldStudentText(query.trim());
  if (!needle) return [{ text, match: false }];
  let normalized = '', offset = 0;
  const positions = [];
  for (const char of text) {
    const folded = foldStudentText(char);
    if (!folded && positions.length) {
      const lastStart = positions.at(-1).start;
      for (let i = positions.length - 1; i >= 0 && positions[i].start === lastStart; i--) positions[i].end = offset + char.length;
    }
    for (let i = 0; i < folded.length; i++) positions.push({ start: offset, end: offset + char.length });
    normalized += folded;
    offset += char.length;
  }
  const index = normalized.indexOf(needle);
  if (index < 0) return [{ text, match: false }];
  const start = positions[index].start, end = positions[index + needle.length - 1].end;
  return [{ text: text.slice(0, start), match: false }, { text: text.slice(start, end), match: true }, { text: text.slice(end), match: false }];
}
