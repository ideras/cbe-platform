<script>
  // One timeline of the Settings page (settings-timeline.js works it out). It is a picture of the values above the fields: screen
  // readers get its one-sentence summary, and the values themselves are in the fields.
  import Icon from '../../../../shared/theme/Icon.svelte';
  let { model, empty = 'The timeline appears once these values fit together.' } = $props();
</script>

{#if model}
  <figure class="timeline" role="img" aria-label={model.summary}>
    <figcaption class="tl-title" aria-hidden="true"><span>{model.title}</span><span class="tl-end">{model.end}</span></figcaption>
    <div class="tl-line" aria-hidden="true">
      {#each model.segments as segment, i (i)}
        <span class="seg-bar {segment.kind}" class:dense={segment.dense} style:left="{segment.from}%" style:width="{Math.max(0, segment.to - segment.from)}%"></span>
      {/each}
      {#each model.marks as mark, i (i)}
        <span class="mark {mark.kind}-dot" style:left="{mark.at}%" title={mark.title}>{#if mark.kind === 'lock'}<Icon name="lock" size={11} />{/if}</span>
      {/each}
      {#each model.labels as entry, i (i)}
        <span class="tl-label {entry.place} {entry.tone} {entry.align}" style:left="{entry.at}%"><b>{entry.value}</b>{entry.text}</span>
      {/each}
    </div>
    <p class="legend" aria-hidden="true">
      {#each model.legend as entry, i (i)}<span><i class="key {entry.kind}-dot"></i>{entry.text}</span>{/each}
    </p>
  </figure>
{:else}
  <p class="timeline tl-empty">{empty}</p>
{/if}

<style>
  .timeline { margin: 0; padding: .75rem .85rem .6rem; background: var(--bg-primary); border: 1px solid var(--border-subtle); border-radius: var(--radius-md); }
  .tl-title { display: flex; gap: var(--space-2); margin-bottom: 2.5rem; font-size: .7rem; font-weight: 700; letter-spacing: .08em; text-transform: uppercase; color: var(--text-muted); }
  .tl-end { margin-left: auto; text-transform: none; letter-spacing: 0; font-weight: 500; }
  .tl-line { position: relative; height: 10px; margin: 0 .5rem 2.6rem; border-radius: 999px; background: var(--bg-secondary); }
  .seg-bar { position: absolute; top: 0; bottom: 0; border-radius: 999px; }
  .seg-bar.calm { background: var(--selection-fill); }
  .seg-bar.alarm { background: var(--warn); }
  /* More screenshots than marks can show: the stretch is striped, one stripe a few screenshots wide. */
  .seg-bar.alarm.dense { background: repeating-linear-gradient(90deg, var(--warn) 0 3px, var(--warn-fill) 3px 6px); }
  .seg-bar.lost { background: var(--bar-alarm); }
  .mark { position: absolute; top: 50%; transform: translate(-50%, -50%); }
  .shot-dot { width: 11px; height: 11px; border-radius: 50%; background: var(--bg-raised); border: 2px solid var(--warn); }
  .beat-dot { width: 2px; height: 16px; border-radius: 2px; background: var(--ok); }
  .lost-dot { width: 12px; height: 12px; border-radius: 50%; background: var(--alert); box-shadow: 0 0 0 3px var(--bg-primary); }
  .lock-dot { display: grid; place-items: center; width: 20px; height: 20px; border-radius: 50%; background: var(--alert); color: var(--on-dark); box-shadow: 0 0 0 3px var(--bg-primary); }
  .tl-label { position: absolute; transform: translateX(-50%); font-size: .7rem; line-height: 1.15; white-space: nowrap; text-align: center; color: var(--text-muted); }
  .tl-label b { display: block; font-family: var(--font-mono); color: var(--text-main); }
  .tl-label.up { bottom: 18px; }
  .tl-label.down { top: 18px; }
  .tl-label.alert b { color: var(--alert-text); }
  .tl-label.warn b { color: var(--warn-text); }
  .tl-label.start { transform: none; text-align: left; }
  .tl-label.end { transform: translateX(-100%); text-align: right; }
  .legend { display: flex; flex-wrap: wrap; gap: .4rem .9rem; margin: 0; font-size: .72rem; color: var(--text-muted); }
  .legend span { display: inline-flex; align-items: center; gap: .35rem; }
  .key { display: inline-block; }
  .key.shot-dot { width: 9px; height: 9px; }
  .key.alarm-dot { width: 16px; height: 6px; border-radius: 3px; background: var(--warn); }
  .key.beat-dot { height: 12px; }
  .tl-empty { color: var(--text-muted); font-size: var(--text-sm); }
</style>
