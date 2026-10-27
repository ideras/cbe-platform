<script>
  import Thumbnail from './Thumbnail.svelte';
  import { groups, historyRows, duration } from './history.js';
  import { localTime, localDateTime } from './dates.js';
  // One page of a session's history as a compact table. Answers and event details open in a row below their own; they are
  // student- or client-supplied text and are only ever shown as text.
  let { items, finalVersionId } = $props();
  let shown = $state(Object.fromEntries(groups.map(entry => [entry.id, true])));
  let open = $state({});
  const rows = $derived(historyRows(items, finalVersionId));
  const visible = $derived(rows.filter(row => shown[row.group]));
  const counts = $derived(Object.fromEntries(groups.map(entry => [entry.id, rows.filter(row => row.group === entry.id).length])));
  const MAX_THUMBS = 6;

  const time = value => localTime(value, '');
  const stamp = value => localDateTime(value, '');
  function toggle(id) { open = { ...open, [id]: !open[id] }; }
</script>

<fieldset class="filters">
  <legend>Show</legend>
  {#each groups as entry (entry.id)}
    <label><input type="checkbox" bind:checked={shown[entry.id]} />{entry.label} <span class="muted">({counts[entry.id]})</span></label>
  {/each}
</fieldset>
<div class="table-scroll"><table class="history">
  <thead><tr><th class="time">Time</th><th>Event</th><th>Details</th><th>Screenshots</th><th class="more"><span class="visually-hidden">More</span></th></tr></thead>
  <tbody>
    {#each visible as row (row.id)}
      <tr class="group-{row.group}" class:final={row.final}>
        <td class="time" title={stamp(row.at)}>{time(row.at)}</td>
        <td><strong>{row.title}</strong>{#if row.final} <span class="badge is-final">DESIGNATED FINAL</span>{/if}</td>
        <td>
          {#if row.kind === 'answers'}
            Version #{row.version.version_id} · sequence {row.version.version_seq} · {row.filled} of {row.total} fields answered
          {:else if row.kind === 'away'}
            {#if row.until}{time(row.at)} → {time(row.until)} · <strong>{duration(row.duration)}</strong>{#if row.beyond} <span class="badge beyond">beyond threshold</span>{/if}
            {:else}Not back by the end of this page{/if}
          {:else if row.note}
            {row.note}
          {:else if row.duration != null}
            After {duration(row.duration)}{#if row.beyond} <span class="badge beyond">beyond threshold</span>{/if}
          {/if}
        </td>
        <td><div class="images">
          {#each row.images.slice(0, MAX_THUMBS) as image, index (image.screenshot_id)}<Thumbnail {image} set={row.images} {index} />{/each}
          {#if row.images.length > MAX_THUMBS}<span class="muted">+{row.images.length - MAX_THUMBS} more</span>{/if}
          {#if row.kind === 'away' && row.images.length === 0}<span class="muted" title="Capture may be pending, unsupported, failed, missing or purged — not evidence of misconduct.">none</span>{/if}
        </div></td>
        <td class="more"><button type="button" class="link" aria-expanded={!!open[row.id]} onclick={() => toggle(row.id)}>{open[row.id] ? 'Hide' : row.kind === 'answers' ? 'Answers' : 'Details'}</button></td>
      </tr>
      {#if open[row.id]}
        <tr class="expanded"><td colspan="5">
          {#if row.kind === 'answers'}
            {#if row.total === 0}<p>Empty snapshot.</p>{/if}
            <dl class="answer-list">{#each Object.entries(row.version.answers) as [id, text] (id)}<dt>{id}</dt><dd><pre>{text || '(blank)'}</pre></dd>{/each}</dl>
          {:else}
            <p class="muted">Client fields are untrusted.</p>
            {#each row.details as event (event.event_log_id)}<pre>{event.event_type} #{event.event_log_id}
{JSON.stringify(event.detail, null, 2)}</pre>{/each}
          {/if}
        </td></tr>
      {/if}
    {/each}
    {#if visible.length === 0}<tr><td colspan="5" class="muted">Nothing to show with these filters.</td></tr>{/if}
  </tbody>
</table></div>

<style>
  .filters { display: flex; flex-wrap: wrap; gap: var(--space-1) var(--space-4); margin: 0 0 var(--space-2); padding: 0; border: 0; }
  .filters legend { float: left; margin-right: var(--space-2); font-weight: 600; }
  .filters label { display: inline-flex; align-items: center; gap: .35rem; }
  .table-scroll { overflow-x: auto; border: 1px solid var(--panel-border); border-radius: 10px; }
  table { width: 100%; border-collapse: collapse; min-width: 760px; background: var(--color-surface); font-size: var(--text-sm); }
  th { color: var(--ink-2); text-align: left; letter-spacing: .02em; border-bottom: 1px solid var(--panel-border); white-space: nowrap; }
  th, td { padding: .3rem .6rem; vertical-align: middle; text-align: left; overflow-wrap: anywhere; }
  td { border-bottom: 1px solid var(--line); }
  .time { width: 1%; white-space: nowrap; font-variant-numeric: tabular-nums; }
  .more { width: 1%; white-space: nowrap; text-align: right; }
  .images { display: flex; flex-wrap: wrap; align-items: center; gap: .3rem; }
  tr.group-answers td:first-child { box-shadow: inset 3px 0 0 var(--sea-ink); }
  tr.group-focus td:first-child { box-shadow: inset 3px 0 0 var(--color-focus-border); }
  tr.group-network td:first-child { box-shadow: inset 3px 0 0 var(--color-warning-border); }
  tr.final { background: var(--sea); }
  tr.expanded td { background: var(--hover); }
  .badge { display: inline-block; padding: 0 .3rem; font-size: var(--text-xs, .75rem); font-weight: 700; border: 1px solid; border-radius: var(--radius-sm); }
  .is-final { color: var(--color-success); border-color: var(--color-success); }
  .beyond { color: var(--color-focus); border-color: var(--color-focus-border); background: var(--color-focus-bg); }
  .link { min-height: 0; margin: 0; padding: 0; border: 0; background: none; color: var(--brand-strong); font-weight: 600; text-decoration: underline; }
  .link:hover:not(:disabled) { background: none; color: var(--brand); }
  .answer-list { display: grid; grid-template-columns: max-content minmax(0, 1fr); gap: var(--space-1) var(--space-3); margin: 0; }
  .answer-list dt { font-family: var(--font-mono, monospace); font-weight: 600; }
  .answer-list dd { margin: 0; }
  pre { margin: 0 0 var(--space-2); white-space: pre-wrap; overflow-wrap: anywhere; font-size: var(--text-sm); }
  .visually-hidden { position: absolute; width: 1px; height: 1px; overflow: hidden; clip-path: inset(50%); white-space: nowrap; }
</style>
