<script>
  import { onMount } from 'svelte';
  import * as api from '$proctor-api';
  import Panel from './Panel.svelte';
  import { message } from './errors.js';
  import { spoken } from './roster.js';
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
  let form = $state({
    heartbeatSeconds: 15, overdueSeconds: 30, focusSeconds: 5, alertSeconds: 2, captureSeconds: 5,
    lockAwaySeconds: 300, lockNoContactSeconds: 300, incidentCaptures: 30, sessionCaptures: 200, graceSeconds: 30,
  });

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
  let effectiveCaptures = $derived.by(() => {
    if (!form.lockAwaySeconds) return form.incidentCaptures;
    const span = form.lockAwaySeconds - form.focusSeconds;
    return span < 0 ? 1 : Math.floor(span / form.captureSeconds) + 1;
  });
  // How much room the students' screenshots take: the server's average size, or half a megabyte when it has none yet.
  let averageBytes = $derived(values?.average_screenshot_bytes ?? 0);
  let worstCase = $derived.by(() => {
    const each = averageBytes > 0 ? averageBytes : 0.5 * 1024 * 1024;
    return form.sessionCaptures * each;
  });
  let megabytes = $derived.by(() => Math.round(worstCase / (1024 * 1024)));
  let sizeNote = $derived(averageBytes > 0 ? '' : ' (estimate)');

  // The problems of the current values, each naming the fields it is about, so the proctor knows what to change.
  let problems = $derived.by(() => {
    const found = [];
    const add = (fields, message) => found.push({ fields, message });
    const { heartbeatSeconds: beat, overdueSeconds: lost, focusSeconds: start, alertSeconds: repeat, captureSeconds: capture,
      lockAwaySeconds: away, lockNoContactSeconds: noContact, incidentCaptures: perAbsence, sessionCaptures: perStudent, graceSeconds: grace } = form;
    // An empty field is a value nobody chose: it is never sent as a number.
    const empty = value => value === null || value === undefined || Number.isNaN(value);
    const show = value => empty(value) ? 'empty' : value;
    const fields = [['heartbeatSeconds', beat, 'Heartbeat every'], ['overdueSeconds', lost, 'Connection lost after'],
      ['focusSeconds', start, 'Alarm and screenshots start after'], ['alertSeconds', repeat, 'Alarm repeats every'],
      ['captureSeconds', capture, 'Screenshot every'], ['lockAwaySeconds', away, 'Lock after away for'],
      ['lockNoContactSeconds', noContact, 'Lock after no contact for'], ['incidentCaptures', perAbsence, 'Screenshots per absence'],
      ['sessionCaptures', perStudent, 'Screenshots per student'], ['graceSeconds', grace, 'Default grace period']];
    for (const [field, value, label] of fields) {
      if (empty(value)) add([field], `${label} is empty: type the value you want.`);
    }
    if (!empty(beat) && (beat < 5 || beat > 60)) add(['heartbeatSeconds'], `Heartbeat every must be between 5 and 60 seconds (it is ${beat}).`);
    if (!empty(beat) && !empty(lost) && lost < beat * 2) add(['overdueSeconds', 'heartbeatSeconds'], `Connection lost after must be at least two heartbeats: it is ${lost} s and the heartbeat is ${beat} s.`);
    if (!empty(lost) && lost > 600) add(['overdueSeconds'], `Connection lost after must be at most 10 minutes (it is ${lost} s).`);
    if (!empty(start) && (start < 1 || start > 600)) add(['focusSeconds'], `Alarm and screenshots start after must be between 1 second and 10 minutes (it is ${start} s).`);
    if (!empty(repeat) && (repeat < 1 || repeat > 10)) add(['alertSeconds'], `Alarm repeats every must be between 1 and 10 seconds (it is ${repeat} s).`);
    if (!empty(capture) && (capture < 3 || capture > 60)) add(['captureSeconds'], `Screenshot every must be between 3 and 60 seconds (it is ${capture} s).`);
    // A lock is off at zero, otherwise a whole number of seconds from one to an hour. The other values it has to come after are checked
    // only for a lock that is in range, so one wrong value is one problem.
    for (const [field, value, label] of [['lockAwaySeconds', away, 'Lock after away'], ['lockNoContactSeconds', noContact, 'Lock after no contact']]) {
      if (empty(value)) continue;
      if (!Number.isInteger(value)) add([field], `${label} must be a whole number of seconds (it is ${value}).`);
      else if (value !== 0 && (value < 1 || value > 3600)) add([field], `${label} must be off or between 1 second and 60 minutes (it is ${value} s).`);
    }
    const lock = value => !empty(value) && Number.isInteger(value) && value >= 1 && value <= 3600;
    if (lock(away) && !empty(start) && !empty(capture) && away < start + capture) add(['lockAwaySeconds', 'focusSeconds', 'captureSeconds'],
      `Lock after away (${away} s) must come after the alarm starts (${start} s) plus one screenshot interval (${capture} s): raise the lock or lower the alarm start.`);
    if (lock(noContact) && !empty(lost) && noContact <= lost) add(['lockNoContactSeconds', 'overdueSeconds'],
      `Lock after no contact (${noContact} s) must be longer than the connection lost threshold (${lost} s).`);
    if (!empty(perAbsence) && !away && (perAbsence < 1 || perAbsence > 500)) add(['incidentCaptures'], `Screenshots per absence must be between 1 and 500 (it is ${perAbsence}).`);
    if (!empty(perStudent) && (perStudent < effectiveCaptures || perStudent > 2000)) add(['sessionCaptures'],
      `Screenshots per student (${perStudent}) must be at least the per-absence count (${effectiveCaptures}) and at most 2000.`);
    if (!empty(grace) && (grace < 1 || grace > 1800 || !Number.isInteger(grace))) add(['graceSeconds'], `Default grace period must be a whole number of seconds between 1 second and 30 minutes (it is ${grace} s).`);
    return found;
  });
  // The problems that are about one field, so the field can say so itself.
  function problemsOf(field) { return problems.filter(entry => entry.fields.includes(field)).map(entry => entry.message); }

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
            {#each problems as entry}<li>{entry.message}</li>{/each}
          </ul>
        {/if}
        <div class="grid">
          <label>Heartbeat every (seconds)<input type="number" min="5" max="60" step="1" bind:value={form.heartbeatSeconds} disabled={busy} aria-invalid={problemsOf('heartbeatSeconds').length > 0} title={problemsOf('heartbeatSeconds').join(' ')} /></label>
          <label>Connection lost after (seconds)<input type="number" min="1" max="600" step="1" bind:value={form.overdueSeconds} disabled={busy} aria-invalid={problemsOf('overdueSeconds').length > 0} title={problemsOf('overdueSeconds').join(' ')} /></label>
          <label>Alarm and screenshots start after (seconds)<input type="number" min="1" max="600" step="1" bind:value={form.focusSeconds} disabled={busy} aria-invalid={problemsOf('focusSeconds').length > 0} title={problemsOf('focusSeconds').join(' ')} /></label>
          <label>Alarm repeats every (seconds)<input type="number" min="1" max="10" step="1" bind:value={form.alertSeconds} disabled={busy} aria-invalid={problemsOf('alertSeconds').length > 0} title={problemsOf('alertSeconds').join(' ')} /></label>
          <label>Screenshot every (seconds)<input type="number" min="3" max="60" step="1" bind:value={form.captureSeconds} disabled={busy} aria-invalid={problemsOf('captureSeconds').length > 0} title={problemsOf('captureSeconds').join(' ')} /></label>
          <label><span>Lock after away for (seconds, 0 = off){#if said(form.lockAwaySeconds)} <small class="said" aria-hidden="true">{said(form.lockAwaySeconds)}</small>{/if}</span><input type="number" min="0" max="3600" step="1" bind:value={form.lockAwaySeconds} disabled={busy} aria-invalid={problemsOf('lockAwaySeconds').length > 0} title={problemsOf('lockAwaySeconds').join(' ')} /></label>
          <label><span>Lock after no contact for (seconds, 0 = off){#if said(form.lockNoContactSeconds)} <small class="said" aria-hidden="true">{said(form.lockNoContactSeconds)}</small>{/if}</span><input type="number" min="0" max="3600" step="1" bind:value={form.lockNoContactSeconds} disabled={busy} aria-invalid={problemsOf('lockNoContactSeconds').length > 0} title={problemsOf('lockNoContactSeconds').join(' ')} /></label>
          <label>Screenshots per absence<input type="number" min="1" max="500" step="1" bind:value={form.incidentCaptures} disabled={busy || form.lockAwaySeconds > 0} aria-invalid={problemsOf('incidentCaptures').length > 0} title={problemsOf('incidentCaptures').join(' ')} /></label>
          <label>Screenshots per student<input type="number" min="1" max="2000" step="1" bind:value={form.sessionCaptures} disabled={busy} aria-invalid={problemsOf('sessionCaptures').length > 0} title={problemsOf('sessionCaptures').join(' ')} /></label>
          <label>Default grace period (seconds)<input type="number" min="1" max="1800" step="1" bind:value={form.graceSeconds} disabled={busy} aria-invalid={problemsOf('graceSeconds').length > 0} title={problemsOf('graceSeconds').join(' ')} /></label>
        </div>
        {#if form.lockAwaySeconds > 0}<p class="muted">With the away lock on, the screenshots per absence are worked out: {effectiveCaptures} per absence, so they cover the whole absence up to the lock.</p>{/if}
        {#if disabled}<p class="muted">Another action is running in the dashboard, so the settings cannot be saved until it finishes.</p>{/if}
        <div class="actions"><button class="primary" type="submit" disabled={busy || disabled || problems.length > 0}>{busy ? 'Saving…' : 'Save settings'}</button></div>
      </form>
    {:else if problem}
      <div class="actions"><button type="button" onclick={read} disabled={busy}>Try again</button></div>
    {:else}
      <p>Loading the settings…</p>
    {/if}
  </Panel>
  {#if values}
    <Panel label="Preview" title="Preview" tone="info">
      <p>Up to {effectiveCaptures} screenshots per absence, at most {form.sessionCaptures} per student. Worst case about {megabytes} MB per student{sizeNote}.</p>
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
  .actions { display: flex; justify-content: flex-end; margin-top: var(--space-3); }
  .actions button { margin: 0; }
  .muted { color: var(--text-muted); }
  .said { margin-inline-start: var(--space-1); color: var(--text-muted); font-weight: 400; }
</style>
