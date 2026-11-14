<script>
  import Icon from '../../../../../shared/theme/Icon.svelte';
  import { treeRows, folderFor } from './tree.js';
  import { dirname } from './paths.js';
  // The file tree: a WAI-ARIA tree with one tab stop (arrow keys move inside it), and the actions on the selected item as buttons
  // above it and as keys (F2 rename, Delete delete). New files and folders go into the selected folder.
  let { folders, expanded, activePath = null, readOnly = false, onToggle, onOpen, onNew, onRename, onDelete } = $props();
  const rows = $derived(treeRows(folders, expanded));
  let selected = $state(null);
  let focused = $state(null);
  let list;
  const real = $derived(rows.filter(row => row.kind !== 'note'));
  const current = $derived(real.find(row => row.path === focused) ?? real.find(row => row.path === selected) ?? real[0] ?? null);
  // The item the buttons act on: the one selected, or, when that has gone (renamed, deleted), the file in front.
  const chosen = $derived(real.find(row => row.path === selected) ?? real.find(row => row.path === activePath) ?? null);

  function focusRow(path) {
    focused = path;
    queueMicrotask(() => list?.querySelector(`[data-path="${CSS.escape(path)}"]`)?.focus());
  }
  function choose(row) {
    selected = row.path;
    focused = row.path;
    if (row.isDir) onToggle(row.path); else onOpen(row.path);
  }
  function keydown(event) {
    if (!current || event.target.closest('button') && event.target.closest('button').classList.contains('act')) return;
    const index = real.findIndex(row => row.path === current.path);
    const go = row => { if (row) { event.preventDefault(); selected = row.path; focusRow(row.path); } };
    switch (event.key) {
      case 'ArrowDown': go(real[Math.min(real.length - 1, index + 1)]); break;
      case 'ArrowUp': go(real[Math.max(0, index - 1)]); break;
      case 'Home': go(real[0]); break;
      case 'End': go(real.at(-1)); break;
      case 'ArrowRight':
        if (current.isDir && !current.open) { event.preventDefault(); onToggle(current.path); }
        else if (current.isDir) go(real[index + 1]?.level > current.level ? real[index + 1] : null);
        break;
      case 'ArrowLeft':
        if (current.isDir && current.open) { event.preventDefault(); onToggle(current.path); }
        else go([...real.slice(0, index)].reverse().find(row => row.level < current.level));
        break;
      case 'Enter': case ' ': event.preventDefault(); choose(current); break;
      case 'F2': if (!readOnly) { event.preventDefault(); onRename(current.path); } break;
      case 'Delete': if (!readOnly) { event.preventDefault(); onDelete(current.path); } break;
      default:
    }
  }
  const folder = () => folderFor(chosen?.path ?? null, rows);
</script>
<section class="files" aria-label="Files">
  <div class="bar" role="toolbar" aria-label="File actions">
    <button type="button" disabled={readOnly} onclick={() => onNew('file', folder())} title="New file"><Icon name="plus" size={16} /><span>File</span></button>
    <button type="button" disabled={readOnly} onclick={() => onNew('folder', folder())} title="New folder"><Icon name="folder" size={16} /><span>Folder</span></button>
    <button type="button" disabled={readOnly || !chosen} onclick={() => onRename(chosen.path)} aria-label="Rename" title="Rename (F2)"><Icon name="pencil" size={16} /></button>
    <button type="button" disabled={readOnly || !chosen} onclick={() => onDelete(chosen.path)} aria-label="Delete" title="Delete (Delete key)"><Icon name="deleteExam" size={16} /></button>
  </div>
  <ul role="tree" aria-label="Project files" bind:this={list} onkeydown={keydown}>
    {#each rows as row, position (row.path)}
      {#if row.kind === 'note'}
        <li role="none" class="note" style:padding-left="{row.level * 1.1 + .5}rem">{row.name}</li>
      {:else}
        <li role="treeitem" data-path={row.path} aria-level={row.level} aria-selected={selected === row.path || (selected === null && activePath === row.path)}
          aria-expanded={row.isDir ? row.open : undefined} tabindex={current?.path === row.path ? 0 : -1}
          class:active={activePath === row.path} class:chosen={selected === row.path}
          style:padding-left="{(row.level - 1) * 1.1 + .4}rem" onclick={() => choose(row)} onfocus={() => { focused = row.path; }}>
          {#if row.isDir}<span class="chev" class:open={row.open}><Icon name="chevron" size={14} /></span><Icon name="folder" size={16} />{:else}<span class="chev"></span><span class="file-mark" aria-hidden="true"></span>{/if}
          <span class="name">{row.name}{#if row.kind === 'link'}<span class="visually-hidden"> (link)</span>{/if}</span>
        </li>
      {/if}
    {/each}
  </ul>
  {#if rows.length === 0}<p class="empty">The project has no files yet.</p>{/if}
</section>
<style>
  .files { display: grid; grid-template-rows: auto minmax(0, 1fr); min-height: 0; background: var(--bg-raised); border: 1px solid var(--border-subtle); border-radius: var(--radius-ctl); overflow: hidden; }
  .bar { display: flex; flex-wrap: wrap; gap: .2rem; padding: .3rem; border-bottom: 1px solid var(--border-subtle); }
  .bar button { display: inline-flex; align-items: center; gap: .25rem; margin: 0; padding: .2rem .45rem; font-size: var(--text-xs); }
  ul { margin: 0; padding: .25rem 0; list-style: none; overflow: auto; font-size: var(--text-sm); }
  li[role='treeitem'] { display: flex; align-items: center; gap: .3rem; padding-block: .15rem; padding-right: .5rem; cursor: pointer; white-space: nowrap; }
  li[role='treeitem']:hover { background: var(--hover-fill); }
  li[role='treeitem']:focus-visible { outline: 2px solid var(--action-primary); outline-offset: -2px; }
  li.active .name { color: var(--action-hover); font-weight: 700; }
  li.chosen { background: var(--selection-fill); }
  .name { overflow: hidden; text-overflow: ellipsis; }
  .chev { display: inline-flex; width: 14px; flex: none; transition: transform .1s; }
  .chev.open { transform: rotate(90deg); }
  .file-mark { width: 16px; flex: none; }
  .note { padding-block: .15rem; color: var(--text-muted); font-size: var(--text-xs); font-style: italic; }
  .empty { margin: .5rem; color: var(--text-muted); font-size: var(--text-sm); }
  .visually-hidden { position: absolute; width: 1px; height: 1px; margin: -1px; overflow: hidden; clip-path: inset(50%); white-space: nowrap; }
</style>
