<script>
  import { onMount } from 'svelte';
  import * as api from '$proctor-api';
  import Icon from '../../../../shared/theme/Icon.svelte';
  import SettingsTimeline from './SettingsTimeline.svelte';
  import { message } from './errors.js';
  import { spoken } from './roster.js';
  import { DEFAULTS, LABELS, RANGES, STEPS, SECTIONS, effectiveCaptures, findProblems, descriptions, fit, heldField, changeText, sectionOf } from './settings-rules.js';
  import { awayTimeline, connectionTimeline } from './settings-timeline.js';
  // The proctor's Settings (owner's mock settings-window.html, 2026-11-08): the monitoring values every student of the next published
  // exam follows. They are stored on the server, so every proctor computer sees the same ones. A published exam keeps the rules it was
  // published with. The page is four sections of fields, each with a timeline of what the values do, a list of the sections, and a side
  // column with what the values cannot be saved as (and the buttons that fix it) and what they mean for the students. A bar at the
  // bottom appears once a value differs from the server's and saves them all.
  let { disabled = false } = $props();
  const uid = $props.id();
  let values = $state(null);
  let busy = $state(false);
  let problem = $state('');
  let saved = $state('');
  let reload = $state(false);

  // The form works in seconds for every time but the screenshots' counts. The locks run from one second to an hour, so seconds say
  // them all; what a long one is in minutes is said beside the field.
  let form = $state({ ...DEFAULTS });
  // The values the server holds, in the form's units: what Discard puts back and what the changed marks compare with.
  let loaded = $state({ ...DEFAULTS });
  // The fields the proctor changed, the last one first: the one to hold when the others are fitted to it. And what the last fit, or
  // the restoring of the defaults, changed, with the values from before it so that it can be undone.
  let recent = $state([]);
  let note = $state(null);
  // The value a lock had when it was switched off, which switching it on brings back.
  let lastOn = {};

  const LOCKS = ['lockAwaySeconds', 'lockNoContactSeconds'];
  const COUNTS = ['incidentCaptures', 'sessionCaptures'];
  const SWITCHES = { lockAwaySeconds: 'Lock after away', lockNoContactSeconds: 'Lock after no contact' };
  // A field's accessible name says its unit, which the field shows as "s" beside the number.
  const nameOf = field => COUNTS.includes(field) ? LABELS[field] : `${LABELS[field]} (seconds)`;

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
    loaded = { ...form };
    recent = []; note = null; lastOn = {};
  }
  async function read() {
    busy = true; problem = '';
    try { const value = await api.Settings(); values = value; load(value); }
    catch (err) { problem = message(err); }
    finally { busy = false; }
  }
  onMount(read);

  const finite = value => typeof value === 'number' && Number.isFinite(value);
  const seconds = value => finite(value) ? spoken(value * 1000) : '—';
  // A lock of a minute or more in the words of the roster ("5 min", "1 min 30 s"); under a minute the number says it all.
  const said = value => Number.isInteger(value) && value >= 60 ? spoken(value * 1000) : '';

  // With the away lock on, the number of screenshots per absence is worked out: the screenshots cover the whole absence
  // up to the lock.
  let effective = $derived(effectiveCaptures(form));
  // How much room the students' screenshots take: the server's average size, or half a megabyte when it has none yet.
  let averageBytes = $derived(values?.average_screenshot_bytes ?? 0);
  let megabytes = $derived(finite(form.sessionCaptures) ? Math.round(form.sessionCaptures * (averageBytes > 0 ? averageBytes : 0.5 * 1024 * 1024) / (1024 * 1024)) : null);
  let averageText = $derived(averageBytes >= 1024 * 1024 ? `${(averageBytes / (1024 * 1024)).toFixed(1)} MB` : `${Math.max(1, Math.round(averageBytes / 1024))} KB`);
  // The bar under it is the share of the most screenshots a student may ever have (2000).
  let share = $derived(finite(form.sessionCaptures) ? Math.min(100, Math.max(0, form.sessionCaptures / RANGES.sessionCaptures[1] * 100)) : 0);

  // The problems of the current values, each naming the fields it is about, so the proctor knows what to change, and the buttons that
  // would fix it. The rules are in settings-rules.js.
  let problems = $derived(findProblems(form));
  // What each field is, or the limit the other values put on it right now, said under it.
  let about = $derived(descriptions(form));
  // What fitting the others to the field changed last, among those that decide a conflict, would do; nothing when it cannot.
  let held = $derived(heldField(problems, recent));
  let plan = $derived(held ? fit(form, held) : null);
  // The sections with a field that has a problem, marked in the list of sections.
  let troubled = $derived(new Set(problems.flatMap(entry => entry.fields).map(sectionOf)));
  let away = $derived(awayTimeline(form));
  let connection = $derived(connectionTimeline(form));
  // The screenshots per absence do not count while the away lock is on: the server works them out from the lock then, and the field
  // cannot be edited.
  const counts = (field, into) => !(field === 'incidentCaptures' && into.lockAwaySeconds > 0);
  // Whether the form already holds the defaults.
  let isDefault = $derived(Object.keys(DEFAULTS).every(field => !counts(field, form) || form[field] === DEFAULTS[field]));
  // The fields whose value differs from the server's.
  let changes = $derived(Object.keys(DEFAULTS).filter(field => counts(field, form) && form[field] !== loaded[field]));
  // The problems that are about one field, so the field can say so itself.
  const troubles = field => problems.some(entry => entry.fields.includes(field));

  const remember = field => { recent = [field, ...recent.filter(other => other !== field)]; };
  // Every edit of a field, typed, stepped or switched, makes it the one to hold, and ends the note about what the last fit changed.
  function edited(event) {
    const field = event.target?.name;
    if (field in DEFAULTS) { remember(field); note = null; saved = ''; }
  }
  function step(field, direction) {
    const [low, high] = RANGES[field];
    const from = finite(form[field]) ? Math.round(form[field]) : DEFAULTS[field];
    form[field] = Math.min(high, Math.max(low, from + direction * (STEPS[field] ?? 1)));
    remember(field); note = null; saved = '';
  }
  function toggle(field) {
    if (form[field] === 0) form[field] = lastOn[field] ?? (loaded[field] > 0 ? loaded[field] : DEFAULTS[field]);
    else { if (finite(form[field]) && form[field] > 0) lastOn[field] = form[field]; form[field] = 0; }
    remember(field); note = null; saved = '';
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
    form = { ...DEFAULTS }; recent = []; saved = '';
  }
  function undo() {
    form = note.before; recent = note.recent; note = null;
  }
  function discard() {
    form = { ...loaded }; recent = []; note = null; lastOn = {};
  }

  // The list of sections scrolls to one and follows the one being read.
  let sectionElements = {};
  let current = $state(SECTIONS[0].id);
  function show(id) {
    current = id;
    sectionElements[id]?.scrollIntoView({ behavior: 'smooth', block: 'start' });
    sectionElements[id]?.querySelector('h2')?.focus({ preventScroll: true });
  }
  $effect(() => {
    if (!values) return;
    const watcher = new IntersectionObserver(entries => {
      for (const entry of entries) if (entry.isIntersecting) current = entry.target.dataset.section;
    }, { rootMargin: '-20% 0px -60% 0px' });
    for (const element of Object.values(sectionElements)) if (element) watcher.observe(element);
    return () => watcher.disconnect();
  });

  // The saved note goes after a while; the values on the page say the rest.
  let savedTimer;
  $effect(() => () => clearTimeout(savedTimer));

  async function save() {
    if (busy || disabled || problems.length || !changes.length) return;
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
      clearTimeout(savedTimer);
      savedTimer = setTimeout(() => { saved = ''; }, 6000);
    } catch (err) {
      const text = message(err);
      problem = text;
      // Another proctor saved in between: the page is reloaded with what the server holds.
      if (text.includes('Another proctor')) { reload = true; await read(); }
    } finally { busy = false; }
  }
</script>

{#snippet field(name)}
  {@const lock = LOCKS.includes(name)}
  {@const off = lock && form[name] === 0}
  {@const auto = name === 'incidentCaptures' && form.lockAwaySeconds > 0}
  {@const bad = troubles(name)}
  {@const [low, high] = RANGES[name]}
  {@const fixed = busy || off || auto}
  <div class="field" class:bad>
    <div class="f-top">
      <label for="{uid}-{name}">{LABELS[name]}</label>
      {#if lock && said(form[name])}<span class="said" aria-hidden="true">{said(form[name])}</span>{/if}
      {#if counts(name, form) && form[name] !== loaded[name]}<span class="changed" title="Changed, not saved yet"></span>{/if}
      {#if lock}
        <label class="switch">
          <input type="checkbox" role="switch" aria-label={SWITCHES[name]} checked={!off} disabled={busy} onchange={() => toggle(name)} />
          <span class="track" aria-hidden="true"></span><span aria-hidden="true">{off ? 'Off' : 'On'}</span>
        </label>
      {/if}
      {#if auto}<span class="auto-tag" title="Worked out from the away lock">Auto</span>{/if}
    </div>
    <div class="stepper" class:quiet={off || auto}>
      <button type="button" tabindex="-1" aria-label="Decrease {LABELS[name]}" disabled={fixed || (finite(form[name]) && form[name] <= low)} onclick={() => step(name, -1)}>−</button>
      {#if off}
        <input id="{uid}-{name}" name={name} type="number" aria-label={nameOf(name)} aria-describedby="{uid}-hint-{name}" value="" placeholder="off" disabled />
      {:else if auto}
        <input id="{uid}-{name}" name={name} type="number" aria-label={nameOf(name)} aria-describedby="{uid}-hint-{name}" value={effective} disabled />
      {:else}
        <input id="{uid}-{name}" name={name} type="number" min={lock ? 0 : low} max={high} step="1" bind:value={form[name]} disabled={busy}
          aria-label={nameOf(name)} aria-describedby="{uid}-hint-{name}" aria-invalid={bad} />
      {/if}
      <span class="unit" aria-hidden="true">{COUNTS.includes(name) ? 'shots' : 's'}</span>
      <button type="button" tabindex="-1" aria-label="Increase {LABELS[name]}" disabled={fixed || (finite(form[name]) && form[name] >= high)} onclick={() => step(name, 1)}>+</button>
    </div>
    <p class="hint" id="{uid}-hint-{name}">{about[name]}</p>
  </div>
{/snippet}

<section class="settings" aria-labelledby="{uid}-title">
  <header class="page-head">
    <div>
      <span class="eyebrow">Settings</span>
      <h1 id="{uid}-title">Monitoring rules</h1>
    </div>
    {#if values}
      <button type="button" class="ghost" onclick={restoreDefaults} disabled={busy || isDefault}
        title={isDefault ? 'These are already the defaults.' : 'Put the values the server starts with back in the form. Nothing is saved until you save.'}>
        <Icon name="refresh" size={16} />Restore the defaults
      </button>
    {/if}
  </header>
  <p class="scope"><Icon name="server" size={16} /><span>Stored on the server, so every proctor computer sees the same values. <strong>Changes apply to exams published from now on;</strong> an exam already published keeps the rules it was published with.</span></p>
  {#if problem}<p role="alert">{problem}</p>{/if}
  {#if reload}<p role="alert">Another proctor saved the settings first. The values below were reloaded; check them and save again.</p>{/if}

  {#if values}
    <form class="layout" onsubmit={event => { event.preventDefault(); save(); }} oninput={edited}>
      <nav class="sections-nav" aria-label="Settings sections">
        {#each SECTIONS as section (section.id)}
          <button type="button" class:on={current === section.id} aria-current={current === section.id ? 'true' : undefined} onclick={() => show(section.id)}>
            <Icon name={section.icon} size={17} />{section.short}{#if troubled.has(section.id)}<span class="sr-only">(a value needs fixing)</span><span class="err-dot" aria-hidden="true" title="A value here needs fixing"></span>{/if}
          </button>
        {/each}
      </nav>

      <div class="blocks">
        {#each SECTIONS as section (section.id)}
          <section class="block" data-section={section.id} bind:this={sectionElements[section.id]} aria-labelledby="{uid}-{section.id}">
            <header class="block-head">
              <span class="block-icon"><Icon name={section.icon} size={17} /></span>
              <div><h2 id="{uid}-{section.id}" tabindex="-1">{section.title}</h2><p>{section.about}</p></div>
            </header>
            <div class="block-body">
              {#if section.id === 'away'}<SettingsTimeline model={away} />{/if}
              {#if section.id === 'connection'}<SettingsTimeline model={connection} />{/if}
              <div class="fields">{#each section.fields as name (name)}{@render field(name)}{/each}</div>
            </div>
          </section>
        {/each}
      </div>

      <aside class="side">
        {#if problems.length}
          <div class="issues" role="alert" aria-label="What the settings cannot be saved as">
            <p class="issues-title"><Icon name="warning" size={16} />{problems.length === 1 ? '1 problem to fix before saving' : `${problems.length} problems to fix before saving`}</p>
            <ul>
              {#each problems as entry}
                <li>{entry.message}{#if entry.fixes.length}<span class="fixes">{#each entry.fixes as fix}<button type="button" class="fix" onclick={() => apply(fix)} disabled={busy}>{fix.label}</button>{/each}</span>{/if}</li>
              {/each}
            </ul>
            {#if plan}
              <div class="fit">
                <button type="button" onclick={fitTheOthers} disabled={busy}>Fit the other values to “{LABELS[held]}”</button>
                <small>It keeps that value and changes only what conflicts with it, as little as it takes: {plan.changes.map(changeText).join('; ')}.</small>
              </div>
            {/if}
          </div>
        {/if}
        {#if note}
          <div class="note" role="status">
            <p>{note.text}{#if note.changes.length}: {note.changes.join('; ')}.{/if}</p>
            <button type="button" onclick={undo} disabled={busy}>Undo</button>
          </div>
        {/if}
        <section class="summary" aria-labelledby="{uid}-summary">
          <header class="summary-head"><Icon name="eye" size={17} /><h2 id="{uid}-summary">What students get</h2></header>
          <div class="summary-body">
            <p class="big"><span class="n">{megabytes ?? '—'}</span> <span class="u">MB per student, worst case</span></p>
            <div class="meter" class:heavy={share > 50} title="Screenshots per student, out of the 2000 a student may ever have">
              <span class="meter-bar" style:width="{share}%"></span>
            </div>
            <p class="s-note">{averageBytes > 0 ? `At ${averageText} a screenshot, the average of those stored so far.` : 'An estimate, at about 0.5 MB a screenshot.'}</p>
            <dl>
              <div><dt>Alarm after leaving</dt> <dd>{seconds(form.focusSeconds)}</dd></div>
              <div><dt>Exam locks when away</dt> <dd class:never={form.lockAwaySeconds === 0}>{form.lockAwaySeconds === 0 ? 'never' : seconds(form.lockAwaySeconds)}</dd></div>
              <div><dt>Exam locks without contact</dt> <dd class:never={form.lockNoContactSeconds === 0}>{form.lockNoContactSeconds === 0 ? 'never' : seconds(form.lockNoContactSeconds)}</dd></div>
              <div><dt>Screenshots per absence</dt> <dd>{finite(effective) ? `up to ${effective}` : '—'}</dd></div>
              <div><dt>Screenshots per student</dt> <dd>{finite(form.sessionCaptures) ? `up to ${form.sessionCaptures}` : '—'}</dd></div>
              <div><dt>Default grace period</dt> <dd>{seconds(form.graceSeconds)}</dd></div>
            </dl>
          </div>
        </section>
      </aside>

      {#if changes.length}
        <div class="savebar" role="region" aria-label="Unsaved changes">
          <p><strong>{changes.length === 1 ? '1 unsaved change' : `${changes.length} unsaved changes`}</strong>
            <small>{problems.length ? 'Fix the problems first, then save.' : disabled ? 'Another action is running in the dashboard; save when it finishes.' : 'Applies to exams published from now on.'}</small></p>
          <button type="button" class="ghost" onclick={discard} disabled={busy}>Discard</button>
          <button class="primary" type="submit" disabled={busy || disabled || problems.length > 0}><Icon name="check" size={16} />{busy ? 'Saving…' : 'Save settings'}</button>
        </div>
      {:else if saved}
        <p class="saved" role="status"><Icon name="check" size={16} />{saved}</p>
      {/if}
    </form>
  {:else if problem}
    <div><button type="button" onclick={read} disabled={busy}>Try again</button></div>
  {:else}
    <p class="muted">Loading the settings…</p>
  {/if}
</section>

<style>
  .settings { container: settings / inline-size; display: grid; grid-template-columns: minmax(0, 1fr); gap: var(--space-4); min-width: 0; }
  .page-head { display: flex; flex-wrap: wrap; align-items: flex-end; justify-content: space-between; gap: var(--space-3) var(--space-5); }
  h1 { margin: 0; font-size: clamp(1.9rem, 3.4vw, 2.3rem); font-weight: 800; letter-spacing: -.02em; line-height: 1.15; }
  .ghost { display: inline-flex; align-items: center; gap: .45rem; margin: 0; background: var(--transparent); }
  .scope { display: flex; gap: .6rem; align-items: flex-start; margin: 0; padding: .65rem .85rem; background: var(--hover-fill); border: 1px solid var(--action-border); border-left: 4px solid var(--action-primary); border-radius: var(--radius-md); font-size: var(--text-sm); color: var(--action-hover); }
  .scope :global(svg) { margin-top: .12rem; color: var(--action-primary); }
  .scope strong { color: var(--text-main); }
  .muted { color: var(--text-muted); }
  .sr-only { position: absolute; width: 1px; height: 1px; overflow: hidden; clip-path: inset(50%); white-space: nowrap; }

  /* One column on a narrow page: the sections' list as a row, what cannot be saved first, the sections, then the summary. Wider, the
   * side column stands beside the sections; wider still, the list of sections stands on their left. */
  .layout { display: grid; grid-template-columns: minmax(0, 1fr); gap: var(--space-4) 1.25rem; align-items: start; }
  .sections-nav { order: 0; display: flex; gap: var(--space-1); overflow-x: auto; }
  .side { display: contents; }
  .issues { order: 1; }
  .note { order: 2; }
  .blocks { order: 3; display: grid; gap: var(--space-4); min-width: 0; }
  .summary { order: 4; }
  .savebar, .saved { order: 5; }
  @container settings (min-width: 46rem) {
    .layout { grid-template-columns: minmax(0, 1fr) 17.5rem; grid-template-areas: "nav nav" "main side" "bar bar"; }
    .sections-nav { grid-area: nav; }
    .blocks { grid-area: main; }
    .side { grid-area: side; display: flex; flex-direction: column; gap: var(--space-4); position: sticky; top: var(--space-4); }
    .savebar, .saved { grid-area: bar; }
  }
  @container settings (min-width: 68rem) {
    .layout { grid-template-columns: 12rem minmax(0, 1fr) 18.5rem; grid-template-areas: "nav main side" "bar bar bar"; }
    .sections-nav { flex-direction: column; position: sticky; top: var(--space-4); overflow: visible; }
  }

  .sections-nav button { display: flex; align-items: center; gap: .6rem; min-height: 0; margin: 0; padding: .55rem .7rem; border: 0; border-radius: var(--radius-md); background: var(--transparent); color: var(--text-muted); font-size: .92rem; text-align: left; white-space: nowrap; box-shadow: none; }
  .sections-nav button :global(svg) { color: var(--muted-dot); }
  .sections-nav button:hover:not(:disabled) { background: var(--hover-fill); color: var(--action-hover); }
  .sections-nav button:hover :global(svg), .sections-nav button.on :global(svg) { color: var(--action-primary); }
  .sections-nav button.on { background: var(--bg-raised); color: var(--action-hover); box-shadow: var(--shadow-card); }
  .err-dot { width: 8px; height: 8px; margin-left: auto; border-radius: 50%; background: var(--alert); }

  .block { min-width: 0; overflow: hidden; background: var(--bg-raised); border: 1px solid var(--border-subtle); border-radius: var(--radius-card, 14px); box-shadow: var(--shadow-card); scroll-margin-top: var(--space-4); }
  .block-head { display: flex; align-items: center; gap: .7rem; padding: .8rem var(--space-4); background: var(--header-soft); border-bottom: 1px solid var(--cyan-border); }
  .block-icon { display: grid; place-items: center; width: 32px; height: 32px; border-radius: 9px; background: var(--bg-raised); color: var(--action-primary); border: 1px solid var(--cyan-chip-border); }
  .block-head h2 { margin: 0; font-size: 1.05rem; color: var(--action-hover); }
  .block-head h2:focus { outline: none; }
  .block-head p { margin: 0; font-size: .8rem; color: var(--text-main); }
  .block-body { display: grid; gap: var(--space-4); padding: var(--space-4); }

  /* Two columns at most, as in the mock; one when a column would be too narrow for a field and its switch. */
  .fields { display: grid; grid-template-columns: repeat(auto-fit, minmax(max(14rem, calc(50% - var(--space-2))), 1fr)); gap: .9rem var(--space-4); }
  .field { display: grid; gap: .3rem; align-content: start; min-width: 0; }
  .f-top { display: flex; align-items: center; gap: var(--space-2); min-height: 1.4rem; }
  .f-top > label:first-child { overflow: hidden; font-size: .88rem; font-weight: 600; color: var(--text-main); white-space: nowrap; text-overflow: ellipsis; }
  .said { color: var(--text-muted); font-size: var(--text-xs); white-space: nowrap; }
  .changed { flex: none; width: 8px; height: 8px; border-radius: 50%; background: var(--action-primary); box-shadow: 0 0 0 3px var(--selection-fill); }
  .auto-tag { margin-left: auto; padding: .02rem .5rem; border-radius: 999px; background: var(--bg-secondary); color: var(--text-main); font-size: .72rem; font-weight: 700; }

  .stepper { display: flex; align-items: stretch; height: 40px; overflow: hidden; background: var(--bg-primary); border: 1px solid var(--control-border); border-radius: 9px; transition: border-color .2s, box-shadow .2s; }
  .stepper:focus-within { border-color: var(--action-primary); box-shadow: 0 0 0 3px var(--focus-halo); }
  .field.bad .stepper { border-color: var(--alert); box-shadow: 0 0 0 3px var(--alert-halo); }
  .stepper.quiet { border-style: dashed; }
  .stepper button { width: 36px; min-height: 0; margin: 0; padding: 0; border: 0; border-radius: 0; background: var(--bg-secondary); color: var(--text-main); font-size: 1.1rem; font-weight: 700; box-shadow: none; }
  .stepper button:hover:not(:disabled) { background: var(--selection-fill); color: var(--action-hover); }
  .stepper button:disabled { background: var(--bg-secondary); color: var(--muted-dot); }
  .stepper input { flex: 1; min-width: 0; min-height: 0; margin: 0; padding: 0; border: 0; border-radius: 0; background: var(--transparent); box-shadow: none; text-align: center; font-family: var(--font-mono); font-size: 1rem; font-weight: 700; color: var(--text-main); -moz-appearance: textfield; appearance: textfield; }
  .stepper input:focus-visible { border: 0; box-shadow: none; outline: none; }
  .stepper input::-webkit-outer-spin-button, .stepper input::-webkit-inner-spin-button { -webkit-appearance: none; margin: 0; }
  .stepper input:disabled { color: var(--text-muted); }
  .stepper input::placeholder { color: var(--text-muted); font-weight: 600; }
  .unit { display: grid; place-items: center; padding: 0 .6rem; border-left: 1px solid var(--border-subtle); color: var(--text-muted); font-size: .8rem; }
  .hint { margin: 0; color: var(--text-muted); font-size: .78rem; line-height: 1.35; }
  .field.bad .hint { color: var(--alert-text); }

  .switch { position: relative; display: inline-flex; align-items: center; gap: .4rem; margin-left: auto; color: var(--text-muted); font-size: .78rem; font-weight: 600; cursor: pointer; user-select: none; }
  /* The real checkbox lies over the drawn track, unseen, so a click on the track is a click on it. */
  .switch input { position: absolute; inset: 0; z-index: 1; width: 100%; height: 100%; margin: 0; opacity: 0; cursor: pointer; }
  .track { position: relative; width: 34px; height: 20px; border-radius: 999px; background: var(--control-border); transition: background-color .15s; }
  .track::after { content: ""; position: absolute; top: 2px; left: 2px; width: 16px; height: 16px; border-radius: 50%; background: var(--paper); box-shadow: var(--shadow-card); transition: transform .15s; }
  .switch input:checked + .track { background: var(--action-primary); }
  .switch input:checked + .track::after { transform: translateX(14px); }
  .switch input:focus-visible + .track { box-shadow: 0 0 0 3px var(--focus-halo-strong); }
  .switch input:disabled + .track { opacity: .6; }

  .issues { display: grid; gap: var(--space-2); padding: .65rem .8rem; background: var(--alert-fill); border: 1px solid var(--alert-border); border-left: 4px solid var(--alert); border-radius: var(--radius-md); color: var(--alert-text); font-size: .85rem; font-weight: 500; }
  .issues-title { display: flex; align-items: center; gap: .4rem; margin: 0; font-weight: 700; }
  .issues ul { display: grid; gap: var(--space-2); margin: 0; padding-inline-start: 1.1rem; }
  .fixes { display: flex; flex-wrap: wrap; gap: var(--space-1); margin-top: .3rem; }
  .fix { min-height: 0; margin: 0; padding: .1rem .6rem; font-size: var(--text-sm); }
  .fit { display: grid; justify-items: start; gap: var(--space-1); padding-top: var(--space-2); border-top: 1px solid var(--alert-border); }
  .fit button { margin: 0; }
  .note { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: var(--space-2); margin: 0; padding: var(--space-2) var(--space-3); border: 1px solid var(--border-subtle); border-radius: var(--radius-md); background: var(--bg-raised); color: var(--text-main); font-weight: 400; }
  .note p { margin: 0; }
  .note button { margin: 0; }

  .summary { overflow: hidden; background: var(--bg-raised); border: 1px solid var(--border-subtle); border-radius: var(--radius-card, 14px); box-shadow: var(--shadow-card); }
  .summary-head { display: flex; align-items: center; gap: .6rem; padding: .8rem var(--space-4); background: var(--header-strong); color: var(--on-dark); }
  .summary-head :global(svg) { color: var(--accent-cyan-soft); }
  .summary-head h2 { margin: 0; font-size: 1rem; color: inherit; }
  .summary-body { display: grid; gap: var(--space-2); padding: .9rem var(--space-4); }
  .big { display: flex; align-items: baseline; gap: .4rem; margin: 0; }
  .big .n { font-family: var(--font-mono); font-size: 1.8rem; font-weight: 700; line-height: 1; color: var(--action-hover); }
  .big .u { color: var(--text-muted); font-size: .85rem; }
  .meter { height: 8px; overflow: hidden; border-radius: 999px; background: var(--bg-secondary); }
  .meter-bar { display: block; height: 100%; border-radius: 999px; background: var(--bar-fill); transition: width .2s; }
  .meter.heavy .meter-bar { background: var(--bar-alarm); }
  .s-note { margin: 0; color: var(--text-muted); font-size: .75rem; }
  dl { display: grid; margin: var(--space-1) 0 0; }
  dl div { display: flex; justify-content: space-between; gap: var(--space-3); padding: .5rem 0 0; margin-top: .5rem; border-top: 1px solid var(--border-subtle); font-size: .85rem; }
  dt { color: var(--text-muted); }
  dd { margin: 0; font-family: var(--font-mono); font-weight: 700; text-align: right; color: var(--text-main); }
  dd.never { color: var(--text-muted); }

  /* The bar of unsaved changes stays at the bottom of the window while the page scrolls under it. */
  .savebar { position: sticky; bottom: var(--space-4); z-index: 5; justify-self: center; display: flex; flex-wrap: wrap; align-items: center; gap: var(--space-3); max-width: 100%;
    padding: .6rem .6rem .6rem var(--space-4); background: var(--text-main); color: var(--bg-primary); border-radius: var(--radius); box-shadow: var(--shadow-dialog); }
  .savebar p { display: grid; margin: 0; font-size: .9rem; }
  .savebar strong { color: var(--bg-primary); }
  .savebar small { color: var(--side-text); font-size: .78rem; }
  .savebar button { margin: 0; }
  .savebar .ghost { color: var(--side-text); border-color: var(--side-border); }
  .savebar .ghost:hover:not(:disabled) { background: var(--chip-on-dark); border-color: var(--side-border); color: var(--on-dark); }
  .savebar .primary { display: inline-flex; align-items: center; gap: .45rem; }
  .saved { position: sticky; bottom: var(--space-4); z-index: 5; justify-self: center; display: flex; align-items: center; gap: var(--space-2); margin: 0;
    --callout-bg: var(--ok-fill); --callout-border: var(--ok-border); --callout-edge: var(--ok); --callout-text: var(--ok-text); font-weight: 600; box-shadow: var(--shadow-raised); }
</style>
