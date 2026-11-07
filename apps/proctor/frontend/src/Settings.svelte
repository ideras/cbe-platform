<script>
  import { onMount } from 'svelte';
  import * as api from '$proctor-api';
  import Panel from './Panel.svelte';
  import { message } from './errors.js';
  import { spoken } from './roster.js';
  import { DEFAULTS, LABELS, effectiveCaptures, findProblems, hints, fit, heldField, changeText } from './settings-rules.js';
  // The proctor's Settings: the monitoring values every student of the next published exam follows. They are stored on the
  // server, so every proctor computer sees the same ones. A published exam keeps the rules it was published with.
  let { disabled = false } = $props();
  let values = $state(null);
  let busy = $state(false);
  let problem = $state('');
  let saved = $state('');
  let reload = $state(false);

  // The form works in seconds for every time but the screenshots' counts. The locks run from one second to an hour, so seconds say
  // them all; what a long one is in minutes is said beside the field.
  let form = $state({ ...DEFAULTS });
  // The fields the proctor changed, the last one first: the one to hold when the others are fitted to it. And what the last fit, or
  // the restoring of the defaults, changed, with the values from before it so that it can be undone.
  let recent = $state([]);
  let note = $state(null);

  function load(into) {
    form = {
      heartbeatSeconds: Math.round(into.heartbeat_interval_ms / 1000),
      overdueSeconds: Math.round(into.overdue_threshold_ms / 1000),
      focusSeconds: Math.round(into.focus_threshold_ms / 1000),
      alertSeconds: Math.round(into.alert_interval_ms / 1000),
      captureSeconds: Math.round(into.capture_interval_ms / 1000),
      lockAwaySeconds: into.lock_away_after_ms / 1000,
      lockNoContactSeconds: into.lock_no_contact_after_ms / 1000,
      incidentCaptures: into.incident_captures,
      sessionCaptures: into.session_captures,
      graceSeconds: Math.round(into.default_grace_ms / 1000),
    };
    recent = []; note = null;
  }
  async function read() {
    busy = true; problem = '';
    try { const value = await api.Settings(); values = value; load(value); }
    catch (err) { problem = message(err); }
    finally { busy = false; }
  }
  onMount(read);

  // A lock of a minute or more in the words of the roster ("5 min", "1 min 30 s"); under a minute the number says it all.
  const said = seconds => Number.isInteger(seconds) && seconds >= 60 ? spoken(seconds * 1000) : '';

  // With the away lock on, the number of screenshots per absence is worked out: the screenshots cover the whole absence
  // up to the lock.
  let effective = $derived(effectiveCaptures(form));
  // How much room the students' screenshots take: the server's average size, or half a megabyte when it has none yet.
  let averageBytes = $derived(values?.average_screenshot_bytes ?? 0);
  let worstCase = $derived.by(() => {
    const each = averageBytes > 0 ? averageBytes : 0.5 * 1024 * 1024;
    return form.sessionCaptures * each;
  });
  let megabytes = $derived.by(() => Math.round(worstCase / (1024 * 1024)));
  let sizeNote = $derived(averageBytes > 0 ? '' : ' (estimate)');

  // The problems of the current values, each naming the fields it is about, so the proctor knows what to change, and the buttons that
  // would fix it. The rules are in settings-rules.js.
  let problems = $derived(findProblems(form));
  // The limits the other values put on a field right now, said beside it.
  let limit = $derived(hints(form));
  // What fitting the others to the field changed last, among those that decide a conflict, would do; nothing when it cannot.
  let held = $derived(heldField(problems, recent));
  let plan = $derived(held ? fit(form, held) : null);
  // Whether the form already holds the defaults. The screenshots per absence do not count while the away lock is on: the server works
  // them out from the lock then, and the field cannot be edited.
  let isDefault = $derived(Object.keys(DEFAULTS).every(field => (field === 'incidentCaptures' && form.lockAwaySeconds > 0) || form[field] === DEFAULTS[field]));
  // The problems that are about one field, so the field can say so itself.
  function problemsOf(field) { return problems.filter(entry => entry.fields.includes(field)).map(entry => entry.message); }

  const remember = field => { recent = [field, ...recent.filter(other => other !== field)]; };
  // Every edit of a field, typed or stepped, makes it the one to hold, and ends the note about what the last fit changed.
  function edited(event) {
    const field = event.target?.name;
    if (field in DEFAULTS) { remember(field); note = null; }
  }
  function apply(fix) {
    for (const [field, value] of Object.entries(fix.set)) form[field] = value;
    remember(Object.keys(fix.set)[0]); note = null;
  }
  function fitTheOthers() {
    if (!plan) return;
    note = { text: `Fitted to “${LABELS[held]}”`, changes: plan.changes.map(changeText), before: $state.snapshot(form), recent: [...recent] };
    form = plan.form;
  }
  function restoreDefaults() {
    note = { text: 'The defaults are back. They are saved when you save the settings.', changes: [], before: $state.snapshot(form), recent: [...recent] };
    form = { ...DEFAULTS }; recent = [];
  }
  function undo() {
    form = note.before; recent = note.recent; note = null;
  }

  async function save() {
    if (busy || problems.length) return;
    busy = true; problem = ''; saved = ''; reload = false;
    const sent = {
      heartbeat_interval_ms: form.heartbeatSeconds * 1000,
      overdue_threshold_ms: form.overdueSeconds * 1000,
      focus_threshold_ms: form.focusSeconds * 1000,
      alert_interval_ms: form.alertSeconds * 1000,
      capture_interval_ms: form.captureSeconds * 1000,
      lock_away_after_ms: form.lockAwaySeconds * 1000,
      lock_no_contact_after_ms: form.lockNoContactSeconds * 1000,
      incident_captures: form.incidentCaptures,
      session_captures: form.sessionCaptures,
      default_grace_ms: form.graceSeconds * 1000,
    };
    try {
      const value = await api.SaveSettings(sent, values?.updated_at ?? '');
      values = value; load(value);
      saved = 'Settings saved. Exams published from now on follow them.';
    } catch (err) {
      const text = message(err);
      problem = text;
      // Another proctor saved in between: the page is reloaded with what the server holds.
      if (text.includes('Another proctor')) { reload = true; await read(); }
    } finally { busy = false; }
  }
</script>

<div class="settings">
  <Panel label="Monitoring settings" title="Monitoring settings" tone="info">
    <p class="muted">These values are stored on the server, so every proctor computer sees the same ones. <strong>The published exam keeps the rules it was published with. Changes apply to exams published from now on.</strong></p>
    {#if problem}<p role="alert">{problem}</p>{/if}
    {#if saved}<p role="status">{saved}</p>{/if}
    {#if reload}<p role="alert">Another proctor saved the settings first. The values above were reloaded; check them and save again.</p>{/if}
    {#if values}
      <form onsubmit={event => { event.preventDefault(); save(); }}>
        {#if problems.length}
          <ul class="problems" role="alert" aria-label="What the settings cannot be saved as">
            {#each problems as entry}
              <li>{entry.message}{#each entry.fixes as fix}<button type="button" class="fix" onclick={() => apply(fix)} disabled={busy}>{fix.label}</button>{/each}</li>
            {/each}
          </ul>
          {#if plan}
            <div class="fit">
              <button type="button" onclick={fitTheOthers} disabled={busy}>Fit the other values to “{LABELS[held]}”</button>
              <small class="muted">It keeps that value and changes only what conflicts with it, as little as it takes: {plan.changes.map(changeText).join('; ')}.</small>
            </div>
          {/if}
        {/if}
        {#if note}
          <div class="note" role="status">
            <p>{note.text}{#if note.changes.length}: {note.changes.join('; ')}.{/if}</p>
            <button type="button" onclick={undo} disabled={busy}>Undo</button>
          </div>
        {/if}
        {#snippet hint(field)}{#if limit[field]}<small class="limit" id={`limit-${field}`} aria-hidden="true">{limit[field]}</small>{/if}{/snippet}
        <div class="grid" oninput={edited}>
          <label><span>Heartbeat every (seconds)</span><input name="heartbeatSeconds" type="number" min="5" max="60" step="1" bind:value={form.heartbeatSeconds} disabled={busy} aria-invalid={problemsOf('heartbeatSeconds').length > 0} title={problemsOf('heartbeatSeconds').join(' ')} /></label>
          <label><span>Connection lost after (seconds)</span>{@render hint('overdueSeconds')}<input name="overdueSeconds" type="number" min="1" max="600" step="1" bind:value={form.overdueSeconds} disabled={busy} aria-invalid={problemsOf('overdueSeconds').length > 0} aria-describedby={limit.overdueSeconds ? 'limit-overdueSeconds' : undefined} title={problemsOf('overdueSeconds').join(' ')} /></label>
          <label><span>Alarm and screenshots start after (seconds)</span>{@render hint('focusSeconds')}<input name="focusSeconds" type="number" min="1" max="600" step="1" bind:value={form.focusSeconds} disabled={busy} aria-invalid={problemsOf('focusSeconds').length > 0} aria-describedby={limit.focusSeconds ? 'limit-focusSeconds' : undefined} title={problemsOf('focusSeconds').join(' ')} /></label>
          <label><span>Alarm repeats every (seconds)</span><input name="alertSeconds" type="number" min="1" max="10" step="1" bind:value={form.alertSeconds} disabled={busy} aria-invalid={problemsOf('alertSeconds').length > 0} title={problemsOf('alertSeconds').join(' ')} /></label>
          <label><span>Screenshot every (seconds)</span><input name="captureSeconds" type="number" min="3" max="60" step="1" bind:value={form.captureSeconds} disabled={busy} aria-invalid={problemsOf('captureSeconds').length > 0} title={problemsOf('captureSeconds').join(' ')} /></label>
          <label><span>Lock after away for (seconds, 0 = off){#if said(form.lockAwaySeconds)} <small class="said" aria-hidden="true">{said(form.lockAwaySeconds)}</small>{/if}</span>{@render hint('lockAwaySeconds')}<input name="lockAwaySeconds" type="number" min="0" max="3600" step="1" bind:value={form.lockAwaySeconds} disabled={busy} aria-invalid={problemsOf('lockAwaySeconds').length > 0} aria-describedby={limit.lockAwaySeconds ? 'limit-lockAwaySeconds' : undefined} title={problemsOf('lockAwaySeconds').join(' ')} /></label>
          <label><span>Lock after no contact for (seconds, 0 = off){#if said(form.lockNoContactSeconds)} <small class="said" aria-hidden="true">{said(form.lockNoContactSeconds)}</small>{/if}</span>{@render hint('lockNoContactSeconds')}<input name="lockNoContactSeconds" type="number" min="0" max="3600" step="1" bind:value={form.lockNoContactSeconds} disabled={busy} aria-invalid={problemsOf('lockNoContactSeconds').length > 0} aria-describedby={limit.lockNoContactSeconds ? 'limit-lockNoContactSeconds' : undefined} title={problemsOf('lockNoContactSeconds').join(' ')} /></label>
          <label><span>Screenshots per absence</span><input name="incidentCaptures" type="number" min="1" max="500" step="1" bind:value={form.incidentCaptures} disabled={busy || form.lockAwaySeconds > 0} aria-invalid={problemsOf('incidentCaptures').length > 0} title={problemsOf('incidentCaptures').join(' ')} /></label>
          <label><span>Screenshots per student</span><input name="sessionCaptures" type="number" min="1" max="2000" step="1" bind:value={form.sessionCaptures} disabled={busy} aria-invalid={problemsOf('sessionCaptures').length > 0} title={problemsOf('sessionCaptures').join(' ')} /></label>
          <label><span>Default grace period (seconds)</span><input name="graceSeconds" type="number" min="1" max="1800" step="1" bind:value={form.graceSeconds} disabled={busy} aria-invalid={problemsOf('graceSeconds').length > 0} title={problemsOf('graceSeconds').join(' ')} /></label>
        </div>
        {#if form.lockAwaySeconds > 0}<p class="muted">With the away lock on, the screenshots per absence are worked out: {effective} per absence, so they cover the whole absence up to the lock.</p>{/if}
        {#if disabled}<p class="muted">Another action is running in the dashboard, so the settings cannot be saved until it finishes.</p>{/if}
        <div class="actions">
          <button type="button" onclick={restoreDefaults} disabled={busy || isDefault} title={isDefault ? 'These are already the defaults.' : 'Put the values the server starts with back in the form. Nothing is saved until you save.'}>Restore the defaults</button>
          <button class="primary" type="submit" disabled={busy || disabled || problems.length > 0}>{busy ? 'Saving…' : 'Save settings'}</button>
        </div>
      </form>
    {:else if problem}
      <div class="actions"><button type="button" onclick={read} disabled={busy}>Try again</button></div>
    {:else}
      <p>Loading the settings…</p>
    {/if}
  </Panel>
  {#if values}
    <Panel label="Preview" title="Preview" tone="info">
      <p>Up to {effective} screenshots per absence, at most {form.sessionCaptures} per student. Worst case about {megabytes} MB per student{sizeNote}.</p>
    </Panel>
  {/if}
</div>

<style>
  .settings { display: grid; grid-template-columns: minmax(0, 1fr); gap: var(--space-4); min-width: 0; }
  .grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(16rem, 1fr)); gap: var(--space-3); margin: var(--space-3) 0; }
  /* A label that wraps to two lines keeps its field level with the others in the row. */
  label { display: grid; align-content: end; gap: var(--space-1); font-weight: 600; color: var(--text-main); }
  input { font-weight: 400; }
  .problems { margin: var(--space-3) 0; padding-inline-start: 1.6rem; }
  .problems li { margin-bottom: var(--space-1); }
  input[aria-invalid="true"] { border-color: var(--alert); box-shadow: 0 0 0 3px var(--alert-halo); }
  .actions { display: flex; justify-content: space-between; gap: var(--space-3); margin-top: var(--space-3); }
  .actions button { margin: 0; }
  .muted { color: var(--text-muted); }
  .said { margin-inline-start: var(--space-1); color: var(--text-muted); font-weight: 400; }
  /* What the other values allow, between the name of a field and the field, so the fields of a row stay level. */
  .limit { color: var(--text-muted); font-weight: 400; font-size: var(--text-sm); line-height: 1.3; }
  /* A fix sits at the end of the problem it solves; it names what it will set. */
  .fix { min-height: 0; margin: 0 0 0 var(--space-2); padding: .1rem .6rem; font-size: var(--text-sm); }
  .fit { display: grid; justify-items: start; gap: var(--space-1); margin: var(--space-2) 0; }
  .fit button { margin: 0; }
  .note { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: var(--space-2); margin: var(--space-3) 0; padding: var(--space-2) var(--space-3); border: 1px solid var(--border-subtle); border-radius: var(--radius-sm); background: var(--bg-secondary); }
  .note p { margin: 0; }
  .note button { margin: 0; }
</style>
