<script>
  import Modal from './Modal.svelte';
  import Icon from '../../../../shared/theme/Icon.svelte';
  import { localTime } from './dates.js';
  let { student = null, sessions = [], exam, busy = false, error = '', onCancel, onConfirm } = $props();
  let minutes = $state(5);
  let reason = $state('');
  let confirmed = $state(false);
  const rows = $derived(student ? [student] : sessions);
  const running = $derived(rows.filter(row => ['active', 'grace'].includes(row.status) && !row.finalization_pending));
  const validMinutes = $derived(Number.isInteger(minutes) && minutes >= 1 && minutes <= 60);
  const amount = $derived(validMinutes ? `${minutes} min` : '— min');
  const ready = $derived(validMinutes && confirmed && reason.trim().length > 0 && (!student || running.length > 0));
  const capped = $derived(validMinutes && exam?.ends_at && running.some(row => new Date(row.grace_until).getTime() + minutes * 60000 > new Date(exam.ends_at).getTime()));
  const newGrace = $derived(student && validMinutes ? new Date(Math.min(new Date(student.grace_until).getTime() + minutes * 60000, exam?.ends_at ? new Date(exam.ends_at).getTime() : Infinity)).toISOString() : null);

  // Changing the amount always requires a fresh confirmation, including typed edits.
  function setMinutes(value) { minutes = Math.max(1, Math.min(60, value)); confirmed = false; }
</script>
<Modal title={student ? 'Extend grace' : 'Extend grace for everyone'} titleId="extend-grace-title" tone="deep" icon="clock" width="500px"
  subtitle={student ? 'Add extra time to this student’s session' : 'Adds extra time to every active session'} {busy} onClose={onCancel}>
  <form onsubmit={event => { event.preventDefault(); if (ready && !busy) onConfirm({ minutes, reason: reason.trim() }); }}>
    <div class="content">
      {#if student}<p class="student"><strong>{student.student_name}</strong><span>{student.account_number}</span></p>{/if}
      <div class="impact" aria-label="Extension impact">
        <div class="stat change"><span class="num">{running.length}</span><span class="lbl">sessions<br />will change</span></div>
        <div class="stat skip"><span class="num">{rows.length - running.length}</span><span class="lbl">already submitted<br />(not reopened)</span></div>
      </div>
      {#if running.length === 0}
        <div class="notice warning"><Icon name="warning" size={16} /><span>{student ? 'This session has ended and cannot be reopened.' : 'No active sessions right now. The extra grace will only apply to students who log in later.'}</span></div>
      {/if}
      {#if !student}<div class="notice note"><Icon name="info" size={16} /><span>Students who log in later also receive this extra grace.</span></div>{/if}

      <div class="field">
        <label for="grace-minutes">Minutes</label>
        <div class="minutes-row">
          <div class="stepper">
            <button type="button" aria-label="Decrease minutes" disabled={busy || !validMinutes || minutes <= 1} onclick={() => setMinutes(minutes - 1)}>−</button>
            <input id="grace-minutes" type="number" min="1" max="60" step="1" inputmode="numeric" required bind:value={minutes} disabled={busy}
              aria-describedby="grace-minutes-hint" aria-invalid={!validMinutes} oninput={() => confirmed = false} />
            <button type="button" aria-label="Increase minutes" disabled={busy || !validMinutes || minutes >= 60} onclick={() => setMinutes(minutes + 1)}>+</button>
          </div>
          <span class="unit">min</span>
          <div class="presets" role="group" aria-label="Preset minutes">
            {#each [5, 10, 15, 30] as preset}<button type="button" class="preset" class:active={minutes === preset} aria-label="{preset} minutes" aria-pressed={minutes === preset} disabled={busy} onclick={() => setMinutes(preset)}>{preset}</button>{/each}
          </div>
        </div>
        <p id="grace-minutes-hint" class="field-hint" class:invalid={!validMinutes}>{validMinutes ? 'Between 1 and 60 minutes.' : 'Enter a whole number from 1 to 60.'}</p>
      </div>
      <div class="field">
        <label for="grace-reason">Reason <span class="optional">· saved in the audit log</span></label>
        <textarea id="grace-reason" rows="2" required maxlength="1024" bind:value={reason} disabled={busy} placeholder="e.g. Server restart at 10:15 interrupted the lab"></textarea>
      </div>
      {#if newGrace}<p class="field-hint grace-end">New grace end: <strong>{localTime(newGrace)}</strong></p>{/if}
      {#if capped}<div class="notice warning"><Icon name="warning" size={16} /><span>The exam end time caps this extension. Move the end time to make all the added time available.</span></div>{/if}
      <label class="confirm" class:checked={confirmed}>
        <span class="checkmark"><input type="checkbox" bind:checked={confirmed} disabled={busy} /><Icon name="check" size={14} /></span>
        <span>I confirm adding <strong>{amount}</strong> of grace.</span>
      </label>
      {#if error}<p role="alert">{error}</p>{/if}
    </div>
    <div class="actions"><button type="button" class="cancel" onclick={onCancel} disabled={busy}>Cancel</button><button type="submit" class="primary" disabled={busy || !ready}><Icon name="plus" size={16} />{busy ? 'Extending…' : `Extend grace by ${amount}`}</button></div>
  </form>
</Modal>

<style>
  .content { display: flex; flex-direction: column; gap: 1rem; padding: 1.1rem; }
  p { margin: 0; }
  .student { display: flex; flex-wrap: wrap; align-items: baseline; gap: .5rem; overflow-wrap: anywhere; }
  .student span { color: var(--text-muted); font-family: var(--font-mono); font-size: var(--text-sm); }
  .impact { display: grid; grid-template-columns: minmax(0, 1fr) minmax(0, 1fr); gap: .6rem; }
  .stat { display: flex; align-items: center; gap: .65rem; padding: .6rem .75rem; border-radius: 10px; border: 1px solid var(--border-subtle); background: var(--bg-primary); }
  .num { font-family: var(--font-mono); font-size: 1.4rem; font-weight: 700; line-height: 1; }
  .lbl { color: var(--text-muted); font-size: .82rem; line-height: 1.25; }
  .change { background: var(--hover-fill); border-color: var(--action-border); }
  .change .num { color: var(--action-hover); }
  .skip .num { color: var(--text-muted); }
  .notice { display: flex; align-items: flex-start; gap: .55rem; padding: .55rem .7rem; border-radius: 9px; font-size: .88rem; overflow-wrap: anywhere; }
  .notice :global(svg) { margin-top: .1rem; }
  .note { color: var(--text-muted); background: var(--bg-primary); border: 1px dashed var(--border-subtle); }
  .note :global(svg) { color: var(--action-primary); }
  .warning { color: var(--warn-text); background: var(--warn-fill); border: 1px solid var(--warn-border); border-left: 4px solid var(--warn); }
  .field > label { display: block; font-size: .88rem; font-weight: 600; margin-bottom: .35rem; }
  .optional { color: var(--text-muted); font-weight: 400; }
  .minutes-row { display: flex; align-items: center; flex-wrap: wrap; gap: .6rem; }
  .stepper { display: inline-flex; align-items: stretch; height: 40px; border: 1px solid var(--control-border); border-radius: 9px; background: var(--bg-primary); }
  .stepper:focus-within { border-color: var(--action-primary); box-shadow: 0 0 0 3px var(--focus-halo); }
  .stepper button { width: 38px; min-height: 0; padding: 0; border: 0; border-radius: 0; font-size: 1.15rem; font-weight: 700; }
  .stepper button:first-child { border-radius: 8px 0 0 8px; }
  .stepper button:last-child { border-radius: 0 8px 8px 0; }
  .stepper input { width: 56px; min-height: 0; padding: 0; border: 0; border-radius: 0; text-align: center; font-family: var(--font-mono); font-size: 1.05rem; font-weight: 700; appearance: textfield; -moz-appearance: textfield; }
  .stepper input:focus-visible { box-shadow: none; }
  .stepper input::-webkit-inner-spin-button, .stepper input::-webkit-outer-spin-button { -webkit-appearance: none; margin: 0; }
  .unit { color: var(--text-muted); font-size: .88rem; }
  .presets { display: flex; gap: .35rem; margin-left: auto; }
  .preset { min-height: 0; padding: .25rem .6rem; border-radius: 999px; background: var(--bg-primary); color: var(--text-muted); font-family: var(--font-mono); font-size: .82rem; font-weight: 400; }
  .preset.active { background: var(--selection-fill); border-color: var(--action-primary); color: var(--action-hover); font-weight: 700; }
  .field-hint { color: var(--text-muted); font-size: .8rem; margin-top: .3rem; }
  .field-hint.invalid { color: var(--alert-text); }
  .grace-end { margin: 0; }
  textarea { display: block; width: 100%; min-height: 64px; padding: .55rem .7rem; border-radius: 9px; font-size: .92rem; }
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
    .impact { grid-template-columns: minmax(0, 1fr); }
    .presets { margin-left: 0; width: 100%; }
    .actions { flex-direction: column-reverse; }
    .actions button { width: 100%; }
  }
</style>
