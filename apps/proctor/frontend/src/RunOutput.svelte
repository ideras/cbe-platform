<script>
  import Modal from './Modal.svelte';
  import { sizeText } from './workspace.js';

  let { run, copy, onClose } = $props();
  let stream = $state('out');
  let view = $state('table');
  let feedback = $state('');
  const streams = $derived([
    { id: 'out', label: 'Standard output', text: run.stdout ?? '', truncated: run.stdout_truncated },
    { id: 'err', label: 'Standard error', text: run.stderr ?? '', truncated: run.stderr_truncated },
  ]);
  const sizes = $derived(streams.map(item => sizeText(new TextEncoder().encode(item.text).length)));
  const current = $derived(streams.find(item => item.id === stream));
  const tableAvailable = $derived(run.state === 'done' && !run.stdout_truncated && !!run.results?.length);
  function jsonTokens(text) {
    const tokens = [];
    let last = 0;
    for (const match of text.matchAll(/"(?:\\.|[^"\\])*"(?:\s*:)?|\b(?:true|false|null)\b|-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?/g)) {
      const value = match[0];
      if (match.index > last) tokens.push({ text: text.slice(last, match.index) });
      tokens.push({ text: value, tone: value.endsWith(':') ? 'key' : value[0] === '"' ? 'string' : 'value' });
      last = match.index + value.length;
    }
    if (last < text.length) tokens.push({ text: text.slice(last) });
    return tokens;
  }
  async function copyOutput() { feedback = await copy(current.text, 'Output') ? 'Output copied' : 'Could not copy output'; }
</script>

<Modal title="What run #{run.id} printed" titleId="review-output-title" subtitle="Grade program · submission {run.submission}{run.duration_ms ? ` · ${Number((run.duration_ms / 1000).toFixed(2))} s` : ''}" icon="terminal" tone="deep" width="56rem" lightDismiss {onClose}>
  <div class="review-modal output-body">
    <div class="review-toolbar">
      <div class="segmented" role="group" aria-label="Output stream">
        {#each streams as item, i (item.id)}<button type="button" aria-pressed={stream === item.id} onclick={() => { stream = item.id; feedback = ''; }}>{item.label} <span class="mono">{sizes[i]}</span></button>{/each}
      </div>
      <span class="review-spacer"></span>
      {#if stream === 'out' && tableAvailable}
        <div class="segmented" role="group" aria-label="Output view">
          <button type="button" aria-pressed={view === 'table'} onclick={() => { view = 'table'; }}>Table</button>
          <button type="button" aria-pressed={view === 'raw'} onclick={() => { view = 'raw'; }}>Raw JSON</button>
        </div>
      {/if}
    </div>
    {#if current.truncated}<p class="review-hint">This stream was cut at the output limit.</p>{/if}
    {#if stream === 'out' && tableAvailable && view === 'table'}
      <div class="output-table">
        <table aria-label="Standard output as a table">
          <thead><tr><th scope="col">Key</th><th scope="col">Passed</th><th scope="col">Points</th><th scope="col">Log</th></tr></thead>
          <tbody>{#each run.results as result (result.todo_id)}<tr><th scope="row" class="mono">{result.todo_id}</th><td><span class="review-pill {result.passed ? 'ok' : 'alert'}">{String(result.passed)}</span></td><td class="mono">{result.points}</td><td>{result.log || '—'}</td></tr>{/each}</tbody>
        </table>
      </div>
    {:else}
      <!-- svelte-ignore a11y_no_noninteractive_tabindex (Keyboard users can scroll long output.) -->
      <pre class="raw" role="region" tabindex="0" aria-label="{current.label} of the run">{#if stream === 'out' && tableAvailable && current.text && current.text.length <= 65536}{#each jsonTokens(current.text) as token}{#if token.tone}<span class={token.tone}>{token.text}</span>{:else}{token.text}{/if}{/each}{:else}{current.text || `(${current.label.toLowerCase()} was empty)`}{/if}</pre>
    {/if}
  </div>
  {#snippet footer()}
    <div class="review-modal review-toolbar">
      <button class="review-button" onclick={copyOutput}>Copy output</button>
      <span class="review-hint" role="status">{feedback}</span>
      <span class="review-spacer"></span>
      <!-- svelte-ignore a11y_autofocus (The dialog opens from a deliberate user action.) -->
      <button class="review-button primary" autofocus onclick={onClose}>Close</button>
    </div>
  {/snippet}
</Modal>

<style>
  .output-body { display: grid; gap: .7rem; padding: 1rem; }
  .segmented { display: flex; flex-wrap: wrap; padding: 3px; gap: 3px; border: 1px solid var(--border-subtle); border-radius: 8px; background: var(--bg-secondary); }
  .segmented button { min-height: 27px; margin: 0; padding: .15rem .35rem; border: 0; border-radius: 5px; font-size: .75rem; box-shadow: none; color: var(--text-muted); background: none; }
  .segmented button[aria-pressed='true'] { background: var(--bg-raised); color: var(--action-hover); box-shadow: var(--shadow-card); }
  .segmented .mono { font-size: .68rem; margin-inline-start: .15rem; }
  .output-table { overflow: auto; border: 1px solid var(--border-subtle); border-radius: 8px; }
  table { width: 100%; border-collapse: collapse; font-size: .8rem; }
  th, td { padding: .4rem .55rem; text-align: left; vertical-align: top; border-bottom: 1px solid var(--border-subtle); overflow-wrap: anywhere; }
  thead th { background: var(--bg-secondary); color: var(--text-muted); text-transform: uppercase; font-size: .7rem; }
  tbody tr:last-child > * { border-bottom: 0; }
  td:last-child { white-space: pre-wrap; }
  .raw { min-height: 9rem; max-height: 55vh; overflow: auto; margin: 0; background: var(--term-ground); color: var(--term-text); border: 0; border-radius: 8px; white-space: pre-wrap; overflow-wrap: anywhere; }
  .key { color: var(--term-cyan); } .string { color: var(--term-green); } .value { color: var(--term-yellow); }
  @media (max-width: 480px) { .output-body { padding: .65rem; } th, td { padding: .3rem; } }
</style>
