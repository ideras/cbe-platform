<script>
  import { onMount } from 'svelte';
  import * as api from '$proctor-api';
  import Panel from './Panel.svelte';
  import TranscriptPlayer from './TranscriptPlayer.svelte';
  import { localDateTime } from './dates.js';
  import { treeRows, numberedLines, kindLabel, endText, sizeText, todoNote } from './workspace.js';

  // A student's workspace, read-only: the files as they are in the container (the latest snapshot), or as they were submitted, the
  // transcripts of their terminals, and the downloads. The files, the transcripts and the TODO ticks are the student's; they are shown
  // as text, never as markup, and the ticks are what the student says they did.
  let { sessionId } = $props();
  let workspace = $state(null);
  let problem = $state('');
  let source = $state(''); // "snapshot:3" or "submission:7"
  let entries = $state([]);
  let expanded = $state(new Set());
  let treeError = $state('');
  let chosen = $state('');
  let file = $state(null);
  let fileError = $state('');
  let replay = $state(null); // { id, title, text }
  let saving = $state('');
  let saved = $state('');
  let alive = true;
  let epoch = 0;

  const rows = $derived(treeRows(entries, expanded));
  const ticked = $derived(Object.entries(workspace?.done ?? {}).filter(([, done]) => done).map(([id]) => id).sort());
  const latestSubmission = $derived(workspace?.submissions.at(-1) ?? null);
  const options = $derived.by(() => {
    const list = [];
    for (const item of [...(workspace?.snapshots ?? [])].reverse()) list.push({ value: `snapshot:${item.id}`, label: `Snapshot at ${localDateTime(item.taken_at)} (${item.reason}, ${item.files} files)` });
    for (const item of [...(workspace?.submissions ?? [])].reverse()) list.push({ value: `submission:${item.id}`, label: `${item.final ? 'Final submission' : 'Submission'} at ${localDateTime(item.submitted_at)} (${item.files} files)` });
    return list;
  });

  async function loadSession() {
    try {
      const value = await api.WorkspaceSession(sessionId);
      if (!alive) return;
      workspace = value; problem = '';
      if (!source && options.length) { source = options[0].value; await loadTree(); }
    } catch (err) {
      // A session that is not a workspace's has no workspace to show, which is not a problem to report.
      if (alive && !workspace) workspace = null;
      if (alive && workspace) problem = `The workspace could not be read: ${String(err)}`;
    }
  }
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
  async function play(terminal) {
    replay = null;
    try { replay = { id: terminal.id, title: `${kindLabel(terminal.kind)} at ${localDateTime(terminal.started_at)}`, text: await api.WorkspaceTranscript(terminal.id) }; }
    catch (err) { problem = `The transcript could not be read: ${String(err)}`; }
  }
  async function save(what) {
    saving = what; saved = ''; problem = '';
    try {
      const path = what === 'zip' ? await api.SaveWorkspaceZip(sessionId, latestSubmission.id) : await api.SaveWorkspaceReport(sessionId);
      if (path) saved = `Saved to ${path}`;
    } catch (err) { problem = String(err); }
    finally { saving = ''; }
  }
  onMount(() => {
    loadSession();
    const timer = setInterval(loadSession, 5000);
    return () => { alive = false; clearInterval(timer); };
  });
</script>

{#if workspace}
  <Panel label="Workspace" title="Workspace">
    <dl class="summary">
      <dt>Container</dt><dd><strong>{workspace.state}</strong>{#if workspace.agent_restarts} · restarted {workspace.agent_restarts} time{workspace.agent_restarts === 1 ? '' : 's'}{/if}</dd>
      <dt>TODO ({todoNote})</dt><dd>{#if ticked.length}{ticked.join(', ')} ticked by the student{:else}nothing ticked{/if}. Nothing checked these ticks.</dd>
      <dt>Submissions</dt><dd>{#if latestSubmission}{workspace.submissions.length} · latest {latestSubmission.final ? 'is the final one' : 'was made by the student pressing Submit'}, {localDateTime(latestSubmission.submitted_at)}{:else}none yet{/if}</dd>
    </dl>
    <div class="actions">
      <button type="button" disabled={!latestSubmission || !!saving} onclick={() => save('zip')}>Download submitted files (zip)…</button>
      <button type="button" disabled={!latestSubmission || !!saving} onclick={() => save('report')}>Download report (Markdown)…</button>
      {#if saved}<span class="saved" role="status">{saved}</span>{/if}
    </div>
    {#if problem}<p role="alert" class="problem">{problem}</p>{/if}

    <h3>Files</h3>
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

    <h3>Terminals</h3>
    {#if workspace.terminals.length}
      <table class="terminals">
        <caption class="visually-hidden">Transcripts of the student's terminals</caption>
        <thead><tr><th scope="col">Terminal</th><th scope="col">Started</th><th scope="col">How it ended</th><th scope="col">Output</th><th scope="col"><span class="visually-hidden">Replay</span></th></tr></thead>
        <tbody>{#each workspace.terminals as terminal (terminal.id)}
          <tr><td>{kindLabel(terminal.kind)}</td><td>{localDateTime(terminal.started_at)}</td><td>{endText(terminal)}</td><td>{sizeText(terminal.bytes)}{terminal.truncated ? ' (cut at the limit)' : ''}</td>
            <td><button type="button" onclick={() => play(terminal)} aria-label="Replay {kindLabel(terminal.kind)} of {localDateTime(terminal.started_at)}">Replay</button></td></tr>
        {/each}</tbody>
      </table>
    {:else}<p class="muted">The student has not opened a terminal, or run anything.</p>{/if}
    {#if replay}{#key replay.id}<TranscriptPlayer title={replay.title} text={replay.text} onClose={() => replay = null} />{/key}{/if}
  </Panel>
{/if}

<style>
  .summary { display: grid; grid-template-columns: max-content minmax(0, 1fr); gap: .2rem var(--space-3); margin: 0 0 var(--space-2); }
  .summary dt { color: var(--text-muted); font-size: var(--text-sm); }
  .summary dd { margin: 0; }
  .actions { display: flex; flex-wrap: wrap; align-items: center; gap: var(--space-2); margin: var(--space-2) 0; }
  .actions button { margin: 0; }
  .saved { color: var(--ok-text); font-size: var(--text-sm); overflow-wrap: anywhere; }
  .problem { color: var(--alert-text); }
  h3 { margin: var(--space-3) 0 var(--space-1); font-size: var(--text-base); }
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
  .terminals { width: 100%; border-collapse: collapse; font-size: var(--text-sm); }
  .terminals th, .terminals td { padding: .3rem .5rem; text-align: left; border-bottom: 1px solid var(--border-subtle); }
  .terminals th { color: var(--action-hover); font-size: var(--text-xs); letter-spacing: .06em; text-transform: uppercase; }
  .terminals button { margin: 0; }
  .muted { color: var(--text-muted); font-size: var(--text-sm); }
  .visually-hidden { position: absolute; width: 1px; height: 1px; margin: -1px; overflow: hidden; clip-path: inset(50%); white-space: nowrap; }
  @media (max-width: 800px) { .files { grid-template-columns: minmax(0, 1fr); } }
</style>
