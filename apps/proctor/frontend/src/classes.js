// What a class has to do with exams: how many it holds, and the one being given now when it is one of them.
export function examLine(entry) {
  if (entry.exam_count === 0) return 'No exams yet';
  const count = entry.exam_count === 1 ? '1 exam' : `${entry.exam_count} exams`;
  return entry.published_exam ? `${count} · ${entry.published_exam.title} published` : count;
}
