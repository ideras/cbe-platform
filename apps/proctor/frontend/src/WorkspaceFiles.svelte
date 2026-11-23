<script>
  import { onMount, untrack } from 'svelte';
  import * as api from '$proctor-api';
  import Icon from '../../../../shared/theme/Icon.svelte';
  import { localDateTime } from './dates.js';
  import { treeRows, numberedLines, sizeText } from './workspace.js';

  // The files of a student's workspace, read-only: as they are in the container (a snapshot), or as they were submitted. The files and
  // their names are the student's; they are shown as text, never as markup.
  let { workspace, copy, saving, onDownload } = $props();
  let source = $state(''); // "snapshot:3" or "submission:7"
  let entries = $state([]);
  let expanded = $state(new Set());
  let treeError = $state('');
  let chosen = $state('');
  let file = $state(null);
  let fileError = $state('');
  let treeLoading = $state(false);
  let fileLoading = $state(false);
  let alive = true;
  let epoch = 0, fileEpoch = 0;

  const rows = $derived(treeRows(entries, expanded));
  const options = $derived.by(() => {
    const list = [];
    for (const item of [...workspace.snapshots].reverse()) list.push({ value: `snapshot:${item.id}`, label: `Snapshot · ${localDateTime(item.taken_at)} · ${item.reason.replaceAll('_', ' ')} · ${item.files} files` });
    for (const [i, item] of [...workspace.submissions].reverse().entries()) list.push({ value: `submission:${item.id}`, label: `Submission #${workspace.submissions.length - i}${item.final ? ' · final' : ''} · ${localDateTime(item.submitted_at)} · ${item.files} files` });
    return list;
  });
  // The newest snapshot is shown to begin with.
  $effect(() => {
    if (options.some(option => option.value === source) || (!source && !options.length)) return;
    source = options[0]?.value ?? '';
    untrack(loadTree);
  });

  const lines = $derived(file && !file.binary && !file.too_large ? numberedLines(file.content) : []);
  const highlight = $derived(file && file.size <= 64 * 1024 && /(?:\.(?:c|cpp|cc|cxx|h|hpp|lex|l|mk)|(?:^|\/)Makefile)$/i.test(file.path));
  const keywords = new Set('int char bool void unsigned while true false switch default return static const auto throw enum class struct case if else for break continue include'.split(' '));
  function tokens(text) {
    const pieces = [];
    const pattern = /\/\/[^\n]*|\/\*[\s\S]*?\*\/|^\s*#[^\n]*|"(?:\\.|[^"\\\n])*"|'(?:\\.|[^'\\\n])*'|\b\d+(?:\.\d+)?\b|[A-Za-z_]\w*/gm;
    let last = 0;
    for (const match of text.matchAll(pattern)) {
      const word = match[0];
      const tone = /^\s*(?:#|\/\/|\/\*)/.test(word) ? 'comment' : /^['"]/.test(word) ? 'string' : /^\d/.test(word) ? 'number' : keywords.has(word) ? 'keyword' : '';
      if (!tone) continue;
      if (last < match.index) pieces.push({ text: text.slice(last, match.index) });
      pieces.push({ text: word, tone });
      last = match.index + word.length;
    }
    pieces.push({ text: text.slice(last) });
    return pieces;
  }

  async function loadTree() {
    const mine = ++epoch;
    const previous = chosen;
    ++fileEpoch;
    treeError = ''; file = null; chosen = ''; fileError = ''; fileLoading = false; entries = [];
    treeLoading = !!source;
    if (!source) return;
    const [kind, id] = source.split(':');
    try {
      const list = await api.WorkspaceTree(kind, Number(id));
      if (!alive || mine !== epoch) return;
      entries = list;
      const selected = list.find(row => row.path === previous && row.type === 'file');
      if (selected) open(selected);
    } catch (err) { if (alive && mine === epoch) treeError = `The files could not be read: ${String(err)}`; }
    finally { if (alive && mine === epoch) treeLoading = false; }
  }
  function toggle(row) {
    const next = new Set(expanded);
    if (next.has(row.path)) next.delete(row.path); else next.add(row.path);
    expanded = next;
  }
  async function open(row) {
    const mine = ++fileEpoch;
    chosen = row.path; file = null; fileError = ''; fileLoading = true;
    const [kind, id] = source.split(':');
    try {
      const value = await api.WorkspaceFile(kind, Number(id), row.path);
      if (mine === fileEpoch && alive) file = value;
    } catch (err) { if (mine === fileEpoch && alive) fileError = `The file could not be read: ${String(err)}`; }
    finally { if (mine === fileEpoch && alive) fileLoading = false; }
  }
  onMount(() => () => { alive = false; });
</script>

<div class="review-toolbar">
  {#if options.length}
    <select class="review-select" bind:value={source} onchange={loadTree} aria-label="Show the files of">{#each options as option (option.value)}<option value={option.value}>{option.label}</option>{/each}</select>
    <p class="review-hint"><Icon name="info" size={13} />{source.startsWith('submission:') ? 'Only what the exam submits' : 'The whole workspace, build products too'}</p>
  {:else}<p class="review-hint">No snapshot has been taken yet.</p>{/if}
  <span class="review-spacer"></span>
  <button class="review-button outline" disabled={!workspace.submissions.length || !!saving} onclick={onDownload}><Icon name="download" size={14} />Download submitted files (.zip)…</button>
</div>
{#if treeError}<p role="alert" class="problem">{treeError}</p>{/if}
<div class="files">
  <ul class="tree" role="tree" aria-label="Files of the student's workspace">
    {#each rows as row (row.path)}
      <li role="none" style:padding-left="{(row.level - 1) * 1.1}rem">
        {#if row.type === 'dir'}
          <button type="button" role="treeitem" aria-level={row.level} aria-expanded={row.open} aria-selected="false" onclick={() => toggle(row)}><span class="mark" class:closed={!row.open}><Icon name="chevron" size={12} /></span><span class="folder"><Icon name="folder" size={14} /></span><strong>{row.name}</strong>{#if row.empty}<span class="muted"> (empty)</span>{/if}</button>
        {:else if row.type === 'link'}
          <span class="link" role="treeitem" aria-level={row.level} aria-selected="false"><span class="mark" aria-hidden="true"></span>{row.name} <span class="muted">→ {row.link}</span></span>
        {:else}
          <button type="button" role="treeitem" aria-level={row.level} aria-selected={chosen === row.path} class:chosen={chosen === row.path} onclick={() => open(row)}><span class="mark" aria-hidden="true"></span><span class="filename">{row.name}</span><span class="size muted">{sizeText(row.size)}</span></button>
        {/if}
      </li>
    {:else}<li class="muted empty-tree" role="none">{treeLoading ? 'Reading files…' : 'No files.'}</li>{/each}
  </ul>
  <div class="viewer" aria-live="polite">
    {#if fileError}<p role="alert" class="problem">{fileError}</p>
    {:else if file}
      <div class="vhead"><span class="path mono" title={file.path}>{file.path}</span><span class="muted">{sizeText(file.size)}{file.binary ? ' · binary' : file.too_large ? '' : ` · ${lines.length} lines`}</span><button class="review-copy" aria-label="Copy file" onclick={() => copy(file.binary || file.too_large ? file.path : file.content, file.binary || file.too_large ? 'Path' : 'File')}>Copy</button></div>
      {#if file.binary}<div class="empty-view">This file is not text, so it is not shown.</div>
      {:else if file.too_large}<div class="empty-view">This file is larger than 1 MiB, so it is not shown.</div>
      {:else}
        <!-- svelte-ignore a11y_no_noninteractive_tabindex (Keyboard users need to scroll this region.) -->
        <div class="code" tabindex="0" role="region" aria-label="Contents of {file.path}">
          <div class="code-content">
            <pre class="numbers" aria-hidden="true">{lines.map((_, index) => index + 1).join('\n')}</pre>
            <pre class="text">{#if highlight}{#each tokens(file.content) as piece}{#if piece.tone}<span class={piece.tone}>{piece.text}</span>{:else}{piece.text}{/if}{/each}{:else}{file.content}{/if}</pre>
          </div>
        </div>
      {/if}
    {:else}<div class="empty-view"><Icon name="fileCheck" size={28} />{fileLoading ? 'Reading file…' : 'Pick a file to read it.'}</div>{/if}
  </div>
</div>

<style>
  .problem { color: var(--alert-text); }
  .files { display: grid; grid-template-columns: minmax(12rem, 17rem) minmax(0, 1fr); min-height: 22rem; border: 1px solid var(--border-subtle); border-radius: 10px; overflow: hidden; }
  .tree { margin: 0; padding: .35rem 0; max-height: 32rem; min-width: 0; overflow: auto; list-style: none; background: var(--bg-primary); border-inline-end: 1px solid var(--border-subtle); font: .78rem/1.5 var(--font-mono); }
  .empty-tree { padding: .5rem; }
  .empty-view { display: flex; flex: 1; min-height: 7rem; flex-direction: column; justify-content: center; align-items: center; gap: .5rem; color: var(--text-muted); padding: .6rem; text-align: center; }
  .empty-view :global(svg) { color: var(--muted-dot); }
  .vhead { display: flex; flex-wrap: wrap; align-items: center; gap: .5rem; padding: .4rem .65rem; border-bottom: 1px solid var(--border-subtle); background: var(--bg-primary); }
  .path { flex: 1 1 8rem; min-width: 0; direction: rtl; text-align: left; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  .folder { color: var(--warn-text); display: inline-flex; }
  .filename, .tree strong { min-width: 0; overflow-wrap: anywhere; }
  .size { margin-left: auto; flex: none; }
  .tree li { padding-right: .5rem; }
  .tree button { display: flex; align-items: center; gap: .3rem; width: 100%; min-height: 25px; margin: 0; padding: .15rem .4rem; text-align: left; border: 0; border-radius: 5px; background: var(--transparent); box-shadow: none; color: var(--text-main); font: inherit; }
  .tree button:hover { background: var(--hover-fill); }
  .tree button.chosen { background: var(--selection-fill); font-weight: 700; }
  .tree button:focus-visible { outline: 2px solid var(--action-primary); outline-offset: -2px; }
  .mark { display: inline-flex; width: .9rem; flex: none; color: var(--text-muted); }
  .mark:not(.closed) { transform: rotate(90deg); }
  .link { display: flex; gap: .3rem; padding: .15rem .4rem; }
  .viewer { min-width: 0; display: flex; flex-direction: column; }
  .viewer .problem { padding: .5rem; }
  .code { flex: 1; max-height: 30rem; overflow: auto; background: var(--bg-raised); font-family: var(--font-mono); font-size: var(--text-xs); line-height: 1.55; }
  .code:focus-visible { outline: 2px solid var(--action-primary); }
  /* Keep the row at document height, separate from the capped scrollport. WebKit otherwise clips the gutter's paint. */
  .code-content { display: grid; grid-template-columns: max-content 1fr; min-height: 100%; width: max-content; min-width: 100%; }
  .code pre { margin: 0; padding: .4rem .6rem; font: inherit; border: 0; border-radius: 0; }
  .numbers { position: sticky; left: 0; text-align: right; color: var(--text-muted); background: var(--bg-secondary); user-select: none; }
  .text { white-space: pre; }
  .muted { color: var(--text-muted); font-size: .74rem; }
  .comment { color: var(--text-muted); } .string { color: var(--ok-text); }
  .number { color: var(--warn-text); } .keyword { color: var(--action-hover); font-weight: 700; }
  @media (max-width: 640px) { .files { grid-template-columns: minmax(0, 1fr); } .tree { max-height: 14rem; border-inline-end: 0; border-bottom: 1px solid var(--border-subtle); } }
</style>
