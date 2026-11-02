<script>
  import { onMount, untrack } from 'svelte';
  import Modal from './Modal.svelte';
  import EndTimeField from './EndTimeField.svelte';
  import Icon from '../../../../shared/theme/Icon.svelte';
  import { parseTime, localDateTime } from './dates.js';
  import { suggestedEndTime, endTimeLabel, endTimeETA } from './end-time.js';
  let { exam, serverTime, clear = false, busy = false, error = '', onCancel, onConfirm } = $props();
  let value = $state(untrack(() => suggestedEndTime(exam.ends_at)));
  let mode = $state(untrack(() => clear ? 'none' : 'set'));
  let confirmed = $state(false);
  let preview = $state(null);
  let problem = $state('');
  let now = $state(Date.now());
  const current = $derived(parseTime(exam.ends_at));
  const proposed = $derived(mode === 'set' ? parseTime(preview?.ends_at) : null);
  const newLabel = $derived(mode === 'none' ? 'No end time' : endTimeLabel(preview?.ends_at, 'Choose a time'));
  const unchanged = $derived(mode === 'none' ? !current : !!current && !!proposed && current.getTime() === proposed.getTime());
  const tooSoon = $derived(!!proposed && proposed.getTime() - now < 60000);
  const soon = $derived(!!proposed && !tooSoon && proposed.getTime() - now < 10 * 60000);
  const ready = $derived(confirmed && !unchanged && (mode === 'none' || !!value && !!proposed && !problem && !tooSoon));
  const action = $derived(unchanged ? 'No change' : mode === 'none' ? 'Remove end time' : proposed ? `Set end to ${newLabel}` : 'Set end time');
  const eta = $derived(endTimeETA(proposed, now));

  function changeMode(next) { mode = next; confirmed = false; }
  onMount(() => { const timer = setInterval(() => now = Date.now(), 30000); return () => clearInterval(timer); });
</script>
<Modal title={clear ? 'Clear end time' : 'Change end time'} titleId="end-time-title" subtitle={exam.title} tone="deep" icon="clock" width="520px" {busy} onClose={onCancel}>
  <form onsubmit={event => { event.preventDefault(); if (ready && !busy) onConfirm(mode === 'none' ? '' : value); }}>
    <div class="content">
      <div class="scope">
        <div class="yes"><Icon name="check" size={15} /><span>Applies to running sessions and later logins</span></div>
        <div class="no"><Icon name="close" size={15} /><span>Finished submissions are never reopened</span></div>
      </div>
      <div class="compare" aria-label="End time comparison">
        <div class="side"><span class="label">Current</span><span class="value" class:none={!current} title={current ? localDateTime(exam.ends_at) : undefined}>{endTimeLabel(exam.ends_at)}</span></div>
        <span class="arrow" aria-hidden="true">→</span>
        <div class="side new"><span class="label">New</span><span class="value" class:none={mode === 'none'} title={proposed ? localDateTime(preview.ends_at) : undefined}>{newLabel}</span></div>
        {#if mode === 'set' && eta}<span class="eta">{eta}</span>{/if}
      </div>
      <div class="setting">
        <span class="field-label">Exam ends <span class="optional">· optional, local time</span></span>
        <div class="segmented" role="group" aria-label="End time mode">
          <button type="button" class:on={mode === 'set'} aria-pressed={mode === 'set'} disabled={busy} onclick={() => changeMode('set')}>At a set time</button>
          <button type="button" class:on={mode === 'none'} aria-pressed={mode === 'none'} disabled={busy} onclick={() => changeMode('none')}>No end time</button>
        </div>
        {#if mode === 'set'}
          <EndTimeField bind:value bind:preview bind:problem {exam} {serverTime} {busy} onChange={() => confirmed = false} />
          <p class="hint">Times mean their next occurrence (today or tomorrow). Choose at least one minute from now, within 24 hours.</p>
        {/if}
      </div>
      {#if mode === 'none'}
        <div class="notice info"><Icon name="info" size={16} /><span>Sessions end only by their own exam time plus grace. Restore each student's own deadline and grace end.</span></div>
      {:else}
        {#if tooSoon}<div class="notice warning"><Icon name="warning" size={16} /><span>Choose an end time at least one minute from now.</span></div>
        {:else if soon}<div class="notice warning"><Icon name="clock" size={16} /><span>Less than 10 minutes from now. Students will get very little warning.</span></div>{/if}
        <p class="hint">Unfinished sessions are submitted automatically at this time, with no grace after the exam end.</p>
      {/if}
      <label class="confirm" class:checked={confirmed}>
        <span class="checkmark"><input type="checkbox" bind:checked={confirmed} disabled={busy} /><Icon name="check" size={14} /></span>
        <span>{#if mode === 'none'}I confirm the exam has <strong>no end time</strong>.{:else}I confirm the exam ends at <strong>{newLabel}</strong>.{/if}</span>
      </label>
      {#if error}<p role="alert">{error}</p>{/if}
    </div>
    <div class="actions"><button type="button" class="cancel" onclick={onCancel} disabled={busy}>Cancel</button><button type="submit" class="primary" disabled={busy || !ready}><Icon name="check" size={16} />{busy ? 'Saving…' : action}</button></div>
  </form>
</Modal>

<style>
  .content { display: flex; flex-direction: column; gap: 1rem; padding: 1.1rem; }
  p { margin: 0; }
  .scope { display: grid; grid-template-columns: minmax(0, 1fr) minmax(0, 1fr); gap: .6rem; }
  .scope > div { display: flex; align-items: flex-start; gap: .5rem; padding: .55rem .7rem; border: 1px solid var(--border-subtle); border-radius: 10px; background: var(--bg-primary); color: var(--text-muted); font-size: .84rem; }
  .scope .yes { background: var(--hover-fill); border-color: var(--action-border); color: var(--action-hover); }
  .scope :global(svg) { margin-top: .15rem; }
  .scope .yes :global(svg) { color: var(--action-hover); }
  .compare { display: flex; align-items: center; flex-wrap: wrap; gap: .75rem; padding: .7rem .85rem; background: var(--bg-primary); border: 1px solid var(--border-subtle); border-radius: 10px; }
  .side { display: flex; flex-direction: column; min-width: 0; }
  .label { color: var(--text-muted); font-size: .72rem; letter-spacing: .08em; text-transform: uppercase; font-weight: 700; }
  .value { font-family: var(--font-mono); font-size: 1.05rem; font-weight: 700; overflow-wrap: anywhere; }
  .value.none { color: var(--text-muted); font-weight: 600; }
  .new .value { color: var(--action-hover); }
  .arrow { color: var(--action-hover); font-size: 1.4rem; }
  .eta { margin-left: auto; padding: .15rem .55rem; border-radius: 999px; background: var(--warn-fill); color: var(--warn-text); font-family: var(--font-mono); font-size: .8rem; white-space: nowrap; }
  .field-label { display: block; margin-bottom: .4rem; font-size: .88rem; font-weight: 600; }
  .optional { color: var(--text-muted); font-weight: 400; }
  .segmented { display: inline-flex; flex-wrap: wrap; padding: 3px; gap: 3px; border-radius: 10px; background: var(--bg-secondary); }
  .segmented button { padding: .4rem .9rem; border: 0; border-radius: 8px; background: var(--transparent); color: var(--text-muted); font-size: .88rem; font-weight: 600; }
  .segmented button:hover:not(:disabled) { color: var(--action-hover); }
  .segmented button.on { background: var(--bg-raised); color: var(--action-hover); box-shadow: var(--shadow-card); }
  .hint { color: var(--text-muted); font-size: .8rem; }
  .setting > .hint { margin-top: .5rem; }
  .notice { display: flex; align-items: flex-start; gap: .55rem; padding: .55rem .7rem; border-radius: 9px; font-size: .86rem; overflow-wrap: anywhere; }
  .notice :global(svg) { margin-top: .15rem; }
  .info { background: var(--hover-fill); color: var(--action-hover); border: 1px solid var(--action-border); border-left: 4px solid var(--action-primary); }
  .warning { background: var(--warn-fill); color: var(--warn-text); border: 1px solid var(--warn-border); border-left: 4px solid var(--warn); }
  .confirm { display: flex; align-items: center; gap: .65rem; padding: .65rem .8rem; border: 1px solid var(--border-subtle); border-left: 4px solid var(--warn); border-radius: 9px; background: var(--bg-primary); font-size: .92rem; font-weight: 600; cursor: pointer; user-select: none; transition: background-color .2s ease, border-color .2s ease; }
  .confirm:hover { background: var(--hover-fill); }
  .confirm.checked { border-left-color: var(--ok); background: var(--ok-fill); color: var(--ok-text); }
  .confirm strong { font-family: var(--font-mono); color: inherit; }
  .checkmark { display: inline-flex; position: relative; flex: none; }
  .checkmark input { appearance: none; width: 20px; height: 20px; margin: 0; padding: 0; border: 2px solid var(--text-muted); border-radius: 5px; }
  .checkmark input:checked { background: var(--ok-text); border-color: var(--ok-text); }
  .checkmark :global(svg) { position: absolute; top: 3px; left: 3px; color: var(--bg-primary); opacity: 0; pointer-events: none; }
  .checkmark input:checked + :global(svg) { opacity: 1; }
  @media (forced-colors: active) { .checkmark input { appearance: auto; } .checkmark :global(svg) { display: none; } }
  .actions { display: flex; justify-content: flex-end; flex-wrap: wrap; gap: .6rem; padding: .85rem 1.1rem; background: var(--bg-primary); border-top: 1px solid var(--border-subtle); }
  .actions button { display: inline-flex; align-items: center; justify-content: center; gap: .45rem; min-height: 40px; margin: 0; padding: 0 1.1rem; border-radius: 9px; font-weight: 700; }
  .cancel { background: var(--transparent); color: var(--text-muted); }
  .cancel:hover:not(:disabled) { background: var(--bg-secondary); color: var(--text-main); }
  @media (max-width: 480px) {
    .scope { grid-template-columns: minmax(0, 1fr); }
    .eta { margin-left: 0; }
    .actions { flex-direction: column-reverse; }
    .actions button { width: 100%; }
  }
</style>
