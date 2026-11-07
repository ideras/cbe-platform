// A span of time in the words the student's screens use: "30 seconds", "5 minutes", "1 minute 30 seconds". To the nearest second, and
// never rounded to a whole minute: the locks can be set to the second. A span that is not there, or not positive, is no words.
export function spokenSpan(millis) {
  if (!Number.isFinite(millis) || millis <= 0) return '';
  const total = Math.round(millis / 1000);
  const unit = (count, word) => `${count} ${word}${count === 1 ? '' : 's'}`;
  if (total < 60) return unit(total, 'second');
  const minutes = Math.floor(total / 60), seconds = total % 60;
  return seconds ? `${unit(minutes, 'minute')} ${unit(seconds, 'second')}` : unit(minutes, 'minute');
}

// How much time the instructor gave back, for the note that the exam was unlocked: "7 minutes were added", "1 minute was added".
export function addedText(seconds) {
  const text = spokenSpan(seconds * 1000);
  return `${text} ${text === '1 second' || text === '1 minute' ? 'was' : 'were'} added`;
}
