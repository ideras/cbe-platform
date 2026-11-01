// What a class name has to be, as the server judges it (the dashboard service checks the same before it asks): plain text of at
// most 120 characters that no other class uses. The server compares names ignoring case, for ASCII letters only (SQLite's
// NOCASE), and lets a class change the case of its own name.
export const CLASS_NAME_MAX = 120;

// The name as it would be saved: no space around it, and every run of white space (a pasted tab, say) as one space.
export const cleanName = text => text.trim().replace(/\s+/g, ' ');

// Characters, not UTF-16 units, as the server counts them.
export const nameLength = name => [...name].length;

const fold = text => text.replace(/[A-Z]/g, letter => letter.toLowerCase());

// What the rename dialog says about the text in its field. `current` is the class's name now and `others` the names of the other
// classes, not its own. `error` is empty when the name could be saved; `ready` also needs it to be a change.
export function checkClassName(raw, current, others = []) {
  const name = cleanName(raw);
  const length = nameLength(name);
  const changed = name !== current;
  let error = '';
  if (!name) error = 'The class needs a name.';
  else if (length > CLASS_NAME_MAX) error = `Too long; use at most ${CLASS_NAME_MAX} characters.`;
  else if (/\p{Cc}/u.test(name)) error = 'The name cannot contain control characters.';
  else if (others.some(other => fold(other) === fold(name))) error = 'Another class already uses this name.';
  return { name, length, changed, error, message: error || (changed ? 'Looks good.' : 'Same as the current name.'), ready: !error && changed };
}
