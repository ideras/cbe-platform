// Helpers over the exam content the server sends: exercises, their items and answer slots.

/** The answer slots of an exercise, its own and its items', in reading order. */
export function exerciseSlots(exercise) {
  return [...(exercise.slots ?? []), ...(exercise.items ?? []).flatMap(item => item.slots ?? [])];
}

/** How many slots of an exercise hold an answer: something other than blank space or the untouched starter text. */
export function answeredCount(exercise, answers) {
  return exerciseSlots(exercise).filter(slot => {
    const text = answers[slot.id] ?? '';
    return text.trim() !== '' && text !== (slot.starter ?? '');
  }).length;
}

/** "1 pt", "2.5 pts". */
export function points(value) {
  return `${value} ${value === 1 ? 'pt' : 'pts'}`;
}
