<script>
  import * as api from '$proctor-api';
  import { addEndMinutes } from './end-time.js';
  let { value = $bindable(''), exam = null, serverTime = null, appearance = 'plain', busy = false,
    preview = $bindable(null), problem = $bindable(''), onChange = () => {} } = $props();
  const zone = Intl.DateTimeFormat().resolvedOptions().timeZone;
  $effect(() => {
    const time = value;
    let live = true;
    preview = null; problem = '';
    if (time) api.PreviewEndTime(time).then(result => { if (live) preview = result; }).catch(err => { if (live) problem = String(err); });
    return () => { live = false; };
  });
  const allotted = $derived(preview && exam ? Math.max(0, Math.floor((new Date(preview.ends_at) - new Date(serverTime || Date.now())) / 60000)) : null);
  // HH:MM cannot represent an end more than 24 hours away. Do not let a
  // midnight-crossing preset wrap back to an earlier occurrence instead.
  function canAdd(minutes) { return !preview?.ends_at || Date.parse(preview.ends_at) + minutes * 60000 <= Date.now() + 24 * 60 * 60000; }
  function add(minutes) { if (canAdd(minutes)) { value = addEndMinutes(value, minutes); onChange(); } }
</script>
<div class:styled={appearance === 'dialog'} class="end-time-field">
  {#if appearance === 'dialog'}
    <div class="time-row">
      <input class="time-input" type="time" bind:value required disabled={busy} aria-label="Exam ends at (local time)" aria-describedby="end-time-zone" oninput={() => onChange()} />
      <span class="zone" id="end-time-zone">{zone}</span>
      <div class="presets" role="group" aria-label="Adjust end time">
        {#each [{ minutes: 15, text: '+15m' }, { minutes: 30, text: '+30m' }, { minutes: 60, text: '+1h' }] as preset}
          <button type="button" aria-label="Add {preset.minutes} minutes" class="preset" disabled={busy || !canAdd(preset.minutes)} title={canAdd(preset.minutes) ? 'Add to the selected end time' : 'Would exceed 24 hours from now'} onclick={() => add(preset.minutes)}>{preset.text}</button>
        {/each}
      </div>
    </div>
  {:else}
    <label>Exam ends at (optional, local time) <input type="time" bind:value disabled={busy} /></label>
  {/if}
  {#if preview}
    <p class="preview">{preview.label}</p>
    {#if preview.clock_warning}<p class="callout warning">The server and this computer's clocks differ by more than one minute. Check the end time carefully.</p>{/if}
    {#if allotted < exam?.duration_seconds / 60}<p class="callout warning">A student who logs in now gets {allotted} of {exam.duration_seconds / 60} minutes.</p>{/if}
    {#if exam?.opens_at && new Date(preview.ends_at) <= new Date(exam.opens_at)}<p class="callout warning">This is before admission opens: nobody can take the exam.</p>{/if}
    {#if exam?.closes_at && new Date(preview.ends_at) > new Date(exam.closes_at)}<p class="muted">This is after admission closes. Running sessions can continue until the end time.</p>{/if}
  {/if}
  {#if problem}<p role="alert">{problem}</p>{/if}
</div>
<style>
  label { display: block; margin-block: var(--space-3); }
  label input { margin-inline-start: var(--space-2); }
  .time-row { display: flex; align-items: center; flex-wrap: wrap; gap: .6rem; margin-top: .7rem; }
  .time-input { height: 42px; min-width: 0; max-width: 100%; padding: 0 .75rem; font-family: var(--font-mono); font-size: 1.05rem; font-weight: 700; border-radius: 9px; }
  .time-input::-webkit-calendar-picker-indicator { cursor: pointer; opacity: .6; }
  .zone { color: var(--text-muted); font-size: .82rem; overflow-wrap: anywhere; }
  .presets { display: flex; flex-wrap: wrap; gap: .35rem; margin-left: auto; }
  .preset { min-height: 0; padding: .25rem .6rem; border-radius: 999px; background: var(--bg-primary); color: var(--text-muted); font-size: .8rem; font-family: var(--font-mono); font-weight: 400; }
  .styled .preview { margin: .5rem 0 0; font-size: .82rem; color: var(--text-muted); }
  .styled .callout, .styled [role="alert"] { margin: .7rem 0 0; font-size: .86rem; padding: .55rem .7rem; border-radius: 9px; }
  @media (max-width: 480px) { .presets { margin-left: 0; width: 100%; } }
</style>
