<script>
  import Icon from '../../../../shared/theme/Icon.svelte';
  import Thumbnail from './Thumbnail.svelte';
  import { groups, historyRows, duration } from './history.js';
  import { localTime, localDateTime } from './dates.js';
  // One page of a session's history as a compact table. Answers and event details open in a row below their own; they are
  // student- or client-supplied text and are only ever shown as text.
  let { items, finalVersionId, showWorkspace, actions, status } = $props();
  let shown = $state(Object.fromEntries(groups.map(entry => [entry.id, true])));
  let open = $state(null);
  const rows = $derived(historyRows(items, finalVersionId));
  const visible = $derived(rows.filter(row => shown[row.group]));
  const counts = $derived(Object.fromEntries(groups.map(entry => [entry.id, rows.filter(row => row.group === entry.id).length])));
  const MAX_THUMBS = 6;

  const time = value => localTime(value, '');
  const stamp = value => localDateTime(value, '');
  function toggle(id) { open = open === id ? null : id; }
</script>

<div class="review-toolbar">
  <div class="filters" role="group" aria-label="Show events">
    {#each groups.filter(entry => entry.id !== 'workspace' || showWorkspace || counts.workspace > 0) as entry (entry.id)}
      <button class="chip group-{entry.id}" class:zero={!counts[entry.id]} aria-pressed={shown[entry.id]} title={counts[entry.id] ? '' : 'No events of this kind on this page'} onclick={() => { shown[entry.id] = !shown[entry.id]; }}><span class="dot"></span>{entry.label} <span class="count">{counts[entry.id]}</span></button>
    {/each}
  </div>
  <span class="review-spacer"></span>
  {@render actions()}
</div>
{@render status()}
<div class="table-scroll"><table class="history">
  <thead><tr><th class="time">Time</th><th class="event">Event</th><th>Details</th><th class="more"><span class="visually-hidden">More</span></th></tr></thead>
  <tbody>
    {#each visible as row (row.id)}
      <!-- The details button provides the equivalent keyboard action for the whole row. -->
      <!-- svelte-ignore a11y_click_events_have_key_events -->
      <tr class="event-row group-{row.group}" class:final={row.final} class:open={open === row.id}
        onclick={event => { if (!event.target.closest('button, a') && !window.getSelection()?.toString()) toggle(row.id); }}>
        <td class="time" title={stamp(row.at)}>{time(row.at)}</td>
        <td><strong><span class="dot"></span>{row.title}</strong>{#if row.final} <span class="badge is-final">DESIGNATED FINAL</span>{/if}</td>
        <td>
          {#if row.kind === 'answers'}
            Version #{row.version.version_id} · sequence {row.version.version_seq} · {row.filled} of {row.total} fields answered
          {:else if row.kind === 'away'}
            {#if row.until}{time(row.at)} → {time(row.until)} · <strong>{duration(row.duration)}</strong>{#if row.beyond} <span class="badge beyond">beyond threshold</span>{/if}
            {:else}Not back by the end of this page{/if}
            {#if row.note}<br /><span class="muted">{row.note}</span>{/if}
          {:else if row.note}
            {row.note}
          {:else if row.duration != null}
            After {duration(row.duration)}{#if row.beyond} <span class="badge beyond">beyond threshold</span>{/if}
          {/if}
        <div class="images">
          {#each row.images.slice(0, MAX_THUMBS) as image, index (image.screenshot_id)}<Thumbnail {image} set={row.images} {index} />{/each}
          {#if row.images.length > MAX_THUMBS}<span class="muted">+{row.images.length - MAX_THUMBS} more</span>{/if}
          {#if (row.kind === 'away' || row.expectsImages) && row.images.length === 0}<span class="muted" title="Capture may be pending, unsupported, failed, missing or purged — not evidence of misconduct.">none</span>{/if}
        </div></td>
        <td class="more"><button type="button" class="caret-button" aria-label={row.kind === 'answers' ? 'Answers' : 'Details'} aria-expanded={open === row.id} onclick={() => toggle(row.id)}><span class="review-caret"><Icon name="chevron" size={14} /></span></button></td>
      </tr>
      {#if open === row.id}
        <tr class="expanded"><td colspan="4">
          {#if row.kind === 'answers'}
            {#if row.total === 0}<p>Empty snapshot.</p>{/if}
            <dl class="answer-list">{#each Object.entries(row.version.answers) as [id, text] (id)}<dt>{id}</dt><dd><pre>{text || '(blank)'}</pre></dd>{/each}</dl>
          {:else if row.group === 'workspace'}
            {#each row.details as event (event.event_log_id)}
              {#if event.detail && Object.keys(event.detail).length}
                <dl class="event-facts">
                  {#each Object.entries(event.detail) as [key, value] (key)}
                    <div><dt>{key.replaceAll('_', ' ')}</dt><dd>{typeof value === 'string' ? value : JSON.stringify(value)}</dd></div>
                  {/each}
                </dl>
              {:else}<p class="muted">No additional details were recorded.</p>{/if}
            {/each}
          {:else}
            <p class="muted">Client fields are untrusted.</p>
            {#each row.details as event (event.event_log_id)}<pre>{event.event_type} #{event.event_log_id}
{JSON.stringify(event.detail, null, 2)}</pre>{/each}
          {/if}
        </td></tr>
      {/if}
    {/each}
    {#if visible.length === 0}<tr><td colspan="4" class="empty">No events match the filters.</td></tr>{/if}
  </tbody>
</table></div>

<style>
  .filters { display: flex; flex-wrap: wrap; gap: .3rem; }
  .chip { display: inline-flex; align-items: center; gap: .3rem; min-height: 26px; padding: .1rem .5rem; margin: 0; border-radius: 999px; font-size: .73rem; box-shadow: none; color: var(--text-muted); }
  .chip[aria-pressed='true'] { background: var(--hover-fill); border-color: var(--action-border); color: var(--action-hover); }
  .chip.zero { color: var(--text-muted); background: var(--bg-primary); }
  .chip[aria-pressed='false'] .dot { background: var(--muted-dot); }
  .count { font-family: var(--font-mono); }
  .dot { display: inline-block; width: 7px; height: 7px; margin-inline-end: .25em; border-radius: 50%; background: var(--event-color, var(--muted-dot)); }
  .group-answers { --event-color: var(--action-primary); } .group-focus { --event-color: var(--alert); }
  .group-network { --event-color: var(--warn); } .group-calculator { --event-color: var(--ok); }
  .group-workspace { --event-color: var(--accent-cyan); }
  .table-scroll { border: 1px solid var(--border-subtle); border-radius: 10px; overflow: hidden; }
  table { width: 100%; table-layout: fixed; border-collapse: collapse; background: var(--bg-raised); font-size: inherit; }
  th { background: var(--bg-secondary); color: var(--text-muted); font-size: .72rem; font-weight: 700; letter-spacing: .05em; text-transform: uppercase; }
  th, td { padding: .4rem .65rem; vertical-align: middle; text-align: left; overflow-wrap: anywhere; }
  tbody tr { border-top: 1px solid var(--border-subtle); }
  .event-row { cursor: pointer; }
  .event-row:hover, .event-row.open { background: var(--hover-fill); }
  .time { width: 102px; color: var(--text-muted); font-family: var(--font-mono); font-size: .75rem; }
  .event { width: 190px; }
  .more { width: 40px; padding-inline: .25rem; }
  .images { display: flex; flex-wrap: wrap; align-items: center; gap: .3rem; }
  .images:not(:empty) { margin-top: .25rem; }
  tr.final { background: var(--ok-fill); }
  tr.expanded td { background: var(--bg-primary); }
  .empty { text-align: center; padding: 1rem; color: var(--text-muted); }
  @media (max-width: 550px) { th, td { padding-inline: .35rem; } .time { width: 68px; font-size: .68rem; } .event { width: 28%; } .more { width: 32px; } }
  .badge { display: inline-block; padding: 0 .45rem; font-size: var(--text-xs, .75rem); font-weight: 700; border: 1px solid; border-radius: 999px; }
  .is-final { background: var(--ok-fill); color: var(--ok-text); border-color: var(--ok-border); font-family: var(--font-mono); letter-spacing: .04em; }
  .beyond { color: var(--alert-text); border-color: var(--alert-border); background: var(--alert-fill); }
  .caret-button { display: grid; place-items: center; min-height: 27px; width: 27px; margin: 0; padding: 0; border: 0; background: none; color: var(--text-muted); box-shadow: none; }
  .answer-list { display: grid; grid-template-columns: minmax(0, max-content) minmax(0, 1fr); gap: var(--space-1) var(--space-3); margin: 0; }
  .answer-list dt { font-family: var(--font-mono, monospace); font-weight: 600; }
  .answer-list dd { margin: 0; }
  .event-facts { display: flex; flex-wrap: wrap; gap: .4rem 1.3rem; margin: .2rem 0; }
  .event-facts div { display: flex; align-items: baseline; flex-wrap: wrap; gap: .4rem; max-width: 100%; }
  .event-facts dt { text-transform: capitalize; color: var(--text-muted); }
  .event-facts dd { margin: 0; min-width: 0; font-family: var(--font-mono); overflow-wrap: anywhere; }
  pre { margin: 0 0 var(--space-2); white-space: pre-wrap; overflow-wrap: anywhere; font-size: var(--text-sm); }
  .visually-hidden { position: absolute; width: 1px; height: 1px; overflow: hidden; clip-path: inset(50%); white-space: nowrap; }
</style>
