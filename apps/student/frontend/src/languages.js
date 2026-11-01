// Cache promises, including concurrent editors: a parser is loaded at most once.
// Unknown language labels deliberately stay plain text.
const loaded = new Map();
export function languageName(name) {
  const key = String(name ?? '').toLowerCase();
  if (['c', 'h', 'c++', 'cpp', 'cc', 'cxx'].includes(key)) return 'cpp';
  if (['python', 'py'].includes(key)) return 'python';
  if (['mips', 'mips32', 'asm', 's'].includes(key)) return 'mips';
  return '';
}
export function loadLanguage(name) {
  const key = languageName(name);
  if (!key) return Promise.resolve([]);
  if (!loaded.has(key)) {
    const language = key === 'cpp' ? import('@codemirror/lang-cpp').then(module => module.cpp())
      : key === 'python' ? import('@codemirror/lang-python').then(module => module.python())
      : import('./mips.js').then(module => module.mips());
    loaded.set(key, language);
  }
  return loaded.get(key);
}
