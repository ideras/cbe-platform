// Paths of a workspace, written the way the agent has them: from /work down, with / between names, "" being /work itself
// (docs/workspace/agent-protocol.md). The agent and the server check every path again; these only keep the student from sending
// one that is bound to be refused, and say why in words.

export const MAX_NAME_BYTES = 255;
const encoder = new TextEncoder();

export const join = (folder, name) => (folder ? `${folder}/${name}` : name);
export const dirname = path => { const i = path.lastIndexOf('/'); return i < 0 ? '' : path.slice(0, i); };
export const basename = path => path.slice(path.lastIndexOf('/') + 1);
export const isInside = (path, folder) => folder === '' || path === folder || path.startsWith(`${folder}/`);

// Replaces the folder a path was under: used when a folder is renamed and the open files under it move with it.
export function moved(path, from, to) {
  return path === from ? to : path.startsWith(`${from}/`) ? to + path.slice(from.length) : path;
}

// Says what is wrong with a name for a file or folder, or returns ''.
export function nameProblem(name) {
  if (name === '') return 'Type a name.';
  if (name === '.' || name === '..') return 'That name cannot be used.';
  if (name.includes('/')) return 'A name cannot contain "/". Create folders one at a time.';
  if (name.includes('\\')) return 'A name cannot contain "\\".';
  // eslint-disable-next-line no-control-regex
  if (/[\u0000-\u001f\u007f]/.test(name)) return 'A name cannot contain control characters.';
  if (name.startsWith('.cbe-')) return 'Names that start with ".cbe-" are kept for the system.';
  if (encoder.encode(name).length > MAX_NAME_BYTES) return `A name can be at most ${MAX_NAME_BYTES} bytes.`;
  return '';
}

// The language label the editor's highlighter knows (languages.js), from a file name; '' is plain text.
export function languageOf(path) {
  const name = basename(path).toLowerCase();
  if (name === 'makefile' || name === 'gnumakefile') return '';
  const dot = name.lastIndexOf('.');
  const extension = dot < 0 ? '' : name.slice(dot + 1);
  if (['c', 'h', 'cc', 'cpp', 'cxx', 'hh', 'hpp', 'hxx'].includes(extension)) return 'cpp';
  if (['py', 'pyw'].includes(extension)) return 'python';
  if (['s', 'asm'].includes(extension)) return 'mips';
  return '';
}

// Folders first, then names in a stable order a person expects (case-insensitive, numbers by value).
const collator = new Intl.Collator(undefined, { numeric: true, sensitivity: 'base' });
export function sortEntries(entries) {
  return [...entries].sort((a, b) => (a.kind === 'dir') === (b.kind === 'dir') ? collator.compare(a.name, b.name) || (a.name < b.name ? -1 : a.name > b.name ? 1 : 0) : a.kind === 'dir' ? -1 : 1);
}
