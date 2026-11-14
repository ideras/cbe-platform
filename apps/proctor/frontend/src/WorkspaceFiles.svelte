<script>
  import { onMount, untrack } from 'svelte';
  import * as api from '$proctor-api';
  import { localDateTime } from './dates.js';
  import { treeRows, numberedLines, sizeText } from './workspace.js';

  // The files of a student's workspace, read-only: as they are in the container (a snapshot), or as they were submitted. The files and
  // their names are the student's; they are shown as text, never as markup.
  let { workspace } = $props();
  let source = $state(''); // "snapshot:3" or "submission:7"
  let entries = $state([]);
  let expanded = $state(new Set());
  let treeError = $state('');
  let chosen = $state('');
  let file = $state(null);
  let fileError = $state('');
  let alive = true;
  let epoch = 0;

  const rows = $derived(treeRows(entries, expanded));
  const options = $derived.by(() => {
    const list = [];
    for (const item of [...(workspace?.snapshots ?? [])].reverse()) list.push({ value: `snapshot:${item.id}`, label: `Snapshot at ${localDateTime(item.taken_at)} (${item.reason}, ${item.files} files)` });
    for (const item of [...(workspace?.submissions ?? [])].reverse()) list.push({ value: `submission:${item.id}`, label: `${item.final ? 'Final submission' : 'Submission'} at ${localDateTime(item.submitted_at)} (${item.files} files)` });
    return list;
  });
  // The newest snapshot is shown to begin with.
  $effect(() => { if (!source && options.length) { source = options[0].value; untrack(loadTree); } });

  async function loadTree() {
    const mine = ++epoch;
    treeError = ''; file = null; chosen = ''; fileError = '';
    const [kind, id] = source.split(':');
    try {
      const list = await api.WorkspaceTree(kind, Number(id));
      if (!alive || mine !== epoch) return;
      entries = list; expanded = new Set();
    } catch (err) { if (alive && mine === epoch) { entries = []; treeError = `The files could not be read: ${String(err)}`; } }
  }
  function toggle(row) {
    const next = new Set(expanded);
    if (next.has(row.path)) next.delete(row.path); else next.add(row.path);
    expanded = next;
  }
  async function open(row) {
    chosen = row.path; file = null; fileError = '';
    const [kind, id] = source.split(':');
    try {
      const value = await api.WorkspaceFile(kind, Number(id), row.path);
      if (chosen === row.path && alive) file = value;
    } catch (err) { if (chosen === row.path && alive) fileError = `The file could not be read: ${String(err)}`; }
  }
  onMount(() => () => { alive = false; });
</script>

{#if options.length}
  <label class="source">Show the files of <select bind:value={source} onchange={loadTree} aria-label="Show the files of">{#each options as option (option.value)}<option value={option.value}>{option.label}</option>{/each}</select></label>
  <p class="muted">A snapshot is everything in the student's workspace, build products too. A submission is only what the exam submits.</p>
{:else}<p class="muted">No snapshot has been taken yet.</p>{/if}
{#if treeError}<p role="alert" class="problem">{treeError}</p>{/if}
<div class="files">
  <ul class="tree" role="tree" aria-label="Files of the student's workspace">
    {#each rows as row (row.path)}
      <li role="none" style:padding-left="{(row.level - 1) * 1.1}rem">
        {#if row.type === 'dir'}
          <button type="button" role="treeitem" aria-level={row.level} aria-expanded={row.open} aria-selected="false" onclick={() => toggle(row)}><span class="mark" aria-hidden="true">{row.open ? '▾' : '▸'}</span>{row.name}{#if row.empty}<span class="muted"> (empty)</span>{/if}</button>
        {:else if row.type === 'link'}
          <span class="link" role="treeitem" aria-level={row.level} aria-selected="false"><span class="mark" aria-hidden="true"></span>{row.name} <span class="muted">→ {row.link}</span></span>
        {:else}
          <button type="button" role="treeitem" aria-level={row.level} aria-selected={chosen === row.path} class:chosen={chosen === row.path} onclick={() => open(row)}><span class="mark" aria-hidden="true"></span>{row.name}<span class="muted"> {sizeText(row.size)}</span></button>
        {/if}
      </li>
    {:else}<li class="muted" role="none">No files.</li>{/each}
  </ul>
  <div class="viewer" aria-live="polite">
    {#if fileError}<p role="alert" class="problem">{fileError}</p>
    {:else if file}
      <h4><code>{file.path}</code> <span class="muted">{sizeText(file.size)}</span></h4>
      {#if file.binary}<p class="muted">This file is not text, so it is not shown. It is in the zip.</p>
      {:else if file.too_large}<p class="muted">This file is larger than 1 MiB, so it is not shown. It is in the zip.</p>
      {:else}
        <!-- svelte-ignore a11y_no_noninteractive_tabindex (Keyboard users need to scroll this region.) -->
        <div class="code" tabindex="0" role="region" aria-label="Contents of {file.path}">
          <pre class="numbers" aria-hidden="true">{numberedLines(file.content).map((_, index) => index + 1).join('\n')}</pre>
          <pre class="text">{file.content}</pre>
        </div>
      {/if}
    {:else}<p class="muted">Choose a file to read it.</p>{/if}
  </div>
</div>

<style>
  .problem { color: var(--alert-text); }
  h4 { margin: 0 0 var(--space-1); font-size: var(--text-sm); }
  .source { display: flex; flex-wrap: wrap; align-items: center; gap: var(--space-2); margin: 0; font-size: var(--text-sm); }
  .source select { max-width: 100%; }
  .files { display: grid; grid-template-columns: minmax(12rem, 18rem) minmax(0, 1fr); gap: var(--space-3); margin-top: var(--space-2); min-width: 0; }
  .tree { margin: 0; padding: .25rem 0; max-height: 28rem; overflow: auto; list-style: none; background: var(--bg-primary); border: 1px solid var(--border-subtle); border-radius: var(--radius-ctl); font-size: var(--text-sm); }
  .tree li { padding-right: .5rem; }
  .tree button { display: flex; align-items: center; gap: .3rem; width: 100%; margin: 0; padding: .15rem .4rem; text-align: left; border: 0; border-radius: 0; background: var(--transparent); box-shadow: none; color: var(--text-main); font-size: var(--text-sm); }
  .tree button:hover { background: var(--hover-fill); }
  .tree button.chosen { background: var(--selection-fill); font-weight: 700; }
  .tree button:focus-visible { outline: 2px solid var(--action-primary); outline-offset: -2px; }
  .mark { display: inline-block; width: .9rem; flex: none; color: var(--text-muted); }
  .link { display: flex; gap: .3rem; padding: .15rem .4rem; }
  .viewer { min-width: 0; }
  .code { display: flex; max-height: 28rem; overflow: auto; background: var(--bg-primary); border: 1px solid var(--border-subtle); border-radius: var(--radius-ctl); font-family: var(--font-mono); font-size: var(--text-xs); line-height: 1.5; }
  .code:focus-visible { outline: 2px solid var(--action-primary); }
  .code pre { margin: 0; padding: .4rem .6rem; font: inherit; }
  .numbers { position: sticky; left: 0; flex: none; text-align: right; color: var(--text-muted); background: var(--bg-secondary); user-select: none; }
  .text { flex: 1; white-space: pre; }
  .muted { color: var(--text-muted); font-size: var(--text-sm); }
  @media (max-width: 800px) { .files { grid-template-columns: minmax(0, 1fr); } }
</style>
