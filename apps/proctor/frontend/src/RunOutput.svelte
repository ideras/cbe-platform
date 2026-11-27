<script>
  import Modal from './Modal.svelte';
  import RunOutputResults from './RunOutputResults.svelte';
  import RunOutputStream from './RunOutputStream.svelte';
  import Icon from '../../../../shared/theme/Icon.svelte';
  import { sizeText } from './workspace.js';

  let { run, todos, copy, onClose } = $props();
  let stream = $state('out');
  let view = $state('results');
  let query = $state('');
  let feedback = $state('');
  const streams = $derived([
    { id: 'out', label: 'Standard output', text: run.stdout ?? '', truncated: run.stdout_truncated },
    { id: 'err', label: 'Standard error', text: run.stderr ?? '', truncated: run.stderr_truncated },
  ]);
  const sizes = $derived(streams.map(item => sizeText(new TextEncoder().encode(item.text).length)));
  const current = $derived(streams.find(item => item.id === stream));
  const hasResults = $derived(run.state === 'done' && !run.stdout_truncated && !!run.results?.length);
  const showResults = $derived(hasResults && stream === 'out' && view === 'results');
  const note = $derived(showResults ? 'Parsed from the JSON the grade program printed' :
    stream === 'err' ? 'Progress and errors the grade program wrote' : 'Standard output from the grade program');

  async function copyOutput() {
    const { id, label, text } = current;
    const copied = await copy(text, label);
    if (id === stream) feedback = copied ? `${label} copied` : `Could not copy ${label.toLowerCase()}`;
  }
</script>

<Modal title="What run #{run.id} printed" titleId="review-output-title" eyebrow="Grade program output" icon="terminal" tone="deep run-output" width="880px" lightDismiss {onClose}>
  {#snippet headerDetails()}
    <p class="metadata">
      <span>Submission <b>{run.submission}</b></span>
      {#if run.duration_ms != null} · <span>took <b>{Number((run.duration_ms / 1000).toFixed(2))} s</b></span>{/if}
      {#if run.exit_code != null} · <span>exit code <b>{run.exit_code}</b></span>{/if}
    </p>
  {/snippet}
  <div class="shell">
    <div class="toolbar">
      <div class="segmented" role="group" aria-label="Output stream">
        {#each streams as item, i (item.id)}
          <button aria-pressed={stream === item.id} onclick={() => { stream = item.id; feedback = ''; }}>
            {#if item.id === 'err' && item.text}<i class="dot"></i>{/if}{item.label} <small>{sizes[i]}</small>
          </button>
        {/each}
      </div>
      {#if hasResults && stream === 'out'}
        {#if showResults}
          <label class="search"><Icon name="search" size={15}/><input type="search" aria-label="Find a check" placeholder="Find a check…" autocomplete="off" spellcheck="false" bind:value={query}/></label>
        {/if}
        <div class="segmented views" role="group" aria-label="Output view">
          <button aria-pressed={view === 'results'} onclick={() => view = 'results'}>Results</button>
          <button aria-pressed={view === 'raw'} onclick={() => view = 'raw'}>Raw JSON</button>
        </div>
      {/if}
    </div>
    {#if hasResults}
      <div class="content" hidden={!showResults}>
        <RunOutputResults {run} {todos} {query}/>
      </div>
    {/if}
    {#if !showResults}
      {#key stream}
        <div class="content"><RunOutputStream text={current.text} label={current.label} truncated={current.truncated} json={hasResults && stream === 'out'} stderr={stream === 'err'}/></div>
      {/key}
    {/if}
  </div>
  {#snippet footer()}
    <div class="output-footer">
      <span class="note">{note}</span>
      {#if feedback}<span role="status" class="feedback">{feedback}</span>{/if}
      <button class="copy" onclick={copyOutput}><Icon name="copy" size={15}/> Copy {current.label.toLowerCase()}</button>
      <!-- svelte-ignore a11y_autofocus (The dialog opens from a deliberate user action.) -->
      <button class="primary" autofocus onclick={onClose}>Close</button>
    </div>
  {/snippet}
</Modal>

<style>
  :global(dialog.run-output[open]) { height: min(820px, calc(100% - 2rem)); }
  .metadata { margin: .15rem 0 0; color: var(--side-text); font-size: .82rem; }
  .metadata span { white-space: nowrap; }
  .metadata b { color: var(--on-dark); font-family: var(--mono); }
  .shell { height: 100%; display: grid; grid-template-rows: auto minmax(0, 1fr); }
  .toolbar { display: flex; align-items: center; flex-wrap: wrap; gap: .5rem; padding: .6rem 1.1rem; background: var(--bg-primary); border-bottom: 1px solid var(--border-subtle); }
  .segmented { display: flex; padding: 3px; gap: 3px; border-radius: 10px; background: var(--bg-secondary); }
  .segmented button { display: inline-flex; align-items: center; gap: .35rem; min-height: 28px; padding: .3rem .65rem; border: 0; border-radius: 8px; font-size: .82rem; font-weight: 650; white-space: nowrap; color: var(--text-muted); background: none; box-shadow: none; }
  .segmented button[aria-pressed='true'] { background: var(--bg-raised); color: var(--action-hover); box-shadow: var(--shadow-card); }
  .segmented small { color: var(--text-muted); font: .7rem var(--mono); }
  .dot { width: 7px; height: 7px; border-radius: 50%; background: var(--warn); }
  .views { margin-left: auto; }
  .search { position: relative; display: flex; align-items: center; min-width: 0; flex: 0 1 200px; margin-left: auto; color: var(--text-muted); }
  .search :global(svg) { position: absolute; left: .55rem; pointer-events: none; }
  .search input { width: 100%; min-width: 0; padding-left: 1.85rem; min-height: 32px; border-color: var(--border-subtle); background: var(--bg-raised); font-size: .84rem; }
  .search + .views { margin-left: 0; }
  .content { min-height: 0; overflow: auto; padding: .9rem 1.1rem 1.1rem; }
  .content[hidden] { display: none; }
  .output-footer { position: relative; display: flex; align-items: center; gap: .6rem; }
  .note { margin-right: auto; color: var(--text-muted); font-size: .78rem; }
  .output-footer button { display: inline-flex; justify-content: center; align-items: center; gap: .4rem; min-height: 38px; padding: .35rem 1rem; font-size: .86rem; }
  .copy { color: var(--text-muted); background: var(--transparent); }
  .feedback { position: absolute; right: 0; bottom: calc(100% + .5rem); margin: 0; font-size: .82rem; box-shadow: var(--shadow-raised); }
  @media (max-width: 720px) {
    .segmented small, .note { display: none; }
    .toolbar, .content { padding-inline: .75rem; }
    .search { order: 1; flex-basis: 100%; }
    .output-footer button { flex: 1 0 auto; }
  }
</style>
