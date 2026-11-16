import { join } from './paths.js';

// The rows the file tree shows: the folders that are open, with what they hold, in order. A folder that is open and not read yet shows
// "Loading…", one that could not be read shows why, one with nothing in it says so.
export function treeRows(folders, expanded) {
  const rows = [];
  const visit = (path, level) => {
    const folder = folders.get(path);
    if (!folder) return;
    for (const entry of folder.entries) {
      const child = join(path, entry.name);
      const isDir = entry.kind === 'dir';
      rows.push({ path: child, name: entry.name, kind: entry.kind, level, isDir, open: isDir && expanded.has(child) });
      if (isDir && expanded.has(child)) {
        const inner = folders.get(child);
        if (!inner || (!inner.loaded && !inner.error)) rows.push({ path: `${child}/\u0000note`, name: 'Loading…', kind: 'note', level: level + 1 });
        else if (inner.error && !inner.loaded) rows.push({ path: `${child}/\u0000note`, name: inner.error, kind: 'note', level: level + 1 });
        else if (inner.entries.length === 0) rows.push({ path: `${child}/\u0000note`, name: 'Empty folder', kind: 'note', level: level + 1 });
        else visit(child, level + 1);
      }
    }
  };
  visit('', 1);
  return rows;
}

// The folder a "new file" goes into: the selected folder, or the folder of the selected file, or the top.
export function folderFor(selected, rows) {
  if (selected === null || selected === undefined) return '';
  const row = rows.find(item => item.path === selected);
  if (!row) return '';
  if (row.isDir) return row.path;
  const i = row.path.lastIndexOf('/');
  return i < 0 ? '' : row.path.slice(0, i);
}
