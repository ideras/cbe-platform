<script>
  import { onMount, untrack } from 'svelte';
  import * as api from '$proctor-api';
  import Icon from '../../../../shared/theme/Icon.svelte';
  import Modal from './Modal.svelte';
  import RunOutput from './RunOutput.svelte';
  import { localDateTime } from './dates.js';
  import { gradeNote, pointsText, failureLabel } from './workspace.js';

  let { sessionId, workspace, gradings, onChanged, onDownload, saving, copy, toast } = $props();
  let selectedId = $state(null);
  let detail = $state(null);
  let loading = $state(false);
  let problem = $state('');
  let notice = $state('');
  let busy = $state(false);
  let submission = $state(0); // The binding accepts 0 for the latest submission.
  let dialog = $state(null);
  let checked = $state(false);
  let confirmationError = $state('');
  let openGroups = $state({ todos: true });
  let openItems = $state({});
  let alive = true;
  let asked = 0;
  let previousRuns = new Map();
  let lastReadVersion = '';
  const version = run => run ? `${run.id}:${run.state}:${run.confirmed}` : '';

  const runs = $derived(gradings?.gradings ?? []);
  const todos = $derived(gradings?.todos ?? []);
  const submissions = $derived([...workspace.submissions].reverse());
  const going = $derived(runs.some(run => run.state === 'queued' || run.state === 'running') || detail?.state === 'queued' || detail?.state === 'running');
  const runState = run => ({ queued: 'Queued', running: 'Running', failed: 'Failed', done: run.confirmed ? 'Confirmed' : 'Advisory' })[run.state];
  const tone = run => ({ queued: 'blue', running: 'blue', failed: 'alert', done: run.confirmed ? 'ok' : 'warn' })[run.state];
  const numeric = value => Number(value.toFixed(2));
  const score = (points, max) => points != null && max != null ? `${numeric(points)} / ${numeric(max)}` : pointsText(points, max);
  const width = (points, max) => max > 0 ? Math.max(0, Math.min(100, (points ?? 0) * 100 / max)) : 0;
  const rows = $derived.by(() => {
    const results = new Map((detail?.results ?? []).map(item => [item.todo_id, item]));
    return todos.map(todo => {
      const result = results.get(todo.id);
      const full = !!result && (todo.points > 0 ? result.points >= todo.points : result.passed);
      return { ...todo, result, ticked: workspace.done?.[todo.id] === true, full, partial: !!result && !full && result.points > 0 };
    });
  });
  const extras = $derived((detail?.results ?? []).filter(item => !item.known).map(result => ({ id: result.todo_id, text: 'Not a TODO of this exam; it is not counted', result })));
  const groups = $derived([...(rows.length ? [{ id: 'todos', name: 'TODO items', rows }] : []), ...(extras.length ? [{ id: 'extras', name: 'Other output', rows: extras }] : [])]);
  const full = $derived(rows.filter(row => row.full).length);
  const partial = $derived(rows.filter(row => row.partial).length);
  const mismatch = $derived(rows.filter(row => row.ticked && !row.full).length);

  $effect(() => {
    const current = runs.find(run => run.id === selectedId);
    if (selectedId === null && runs.length) untrack(() => open(runs[0].id));
    else if (current && detail && version(current) !== version(detail) && version(current) !== lastReadVersion) untrack(() => read(current.id));
  });
  $effect(() => {
    const latest = runs;
    for (const run of latest) {
      if (!['queued', 'running'].includes(previousRuns.get(run.id))) continue;
      if (run.state === 'done') untrack(() => toast(`Run #${run.id} finished · ${score(run.points, run.max_points)}${run.confirmed ? ' (confirmed)' : ' (advisory)'}`));
      if (run.state === 'failed') untrack(() => toast(`Run #${run.id} failed`));
    }
    previousRuns = new Map(latest.map(run => [run.id, run.state]));
  });
  $effect(() => { if (submission && !submissions.some(item => item.id === submission)) submission = 0; });

  async function read(id) {
    lastReadVersion = version(runs.find(run => run.id === id));
    const mine = ++asked;
    try {
      const value = await api.WorkspaceGrading(id);
      if (alive && mine === asked) detail = value;
    } catch (err) { if (alive && mine === asked) problem = `The run could not be read: ${String(err)}`; }
    finally { if (alive && mine === asked) loading = false; }
  }
  function open(id) {
    selectedId = id; problem = ''; notice = ''; detail = null; loading = true;
    read(id);
  }
  function runKeys(event, index) {
    const next = { ArrowLeft: (index + runs.length - 1) % runs.length, ArrowRight: (index + 1) % runs.length, Home: 0, End: runs.length - 1 }[event.key];
    if (next === undefined) return;
    event.preventDefault();
    open(runs[next].id);
    document.getElementById(`review-run-${runs[next].id}`)?.focus();
  }
  function expandAll(on) {
    openGroups = Object.fromEntries(groups.map(group => [group.id, on]));
    openItems = Object.fromEntries(groups.flatMap(group => group.rows.map(row => [`${group.id}:${row.id}`, on])));
  }
  async function grade() {
    if (busy || going || !submissions.length) return;
    busy = true; problem = ''; notice = '';
    try {
      const id = await api.GradeStudent(sessionId, Number(submission));
      open(id);
      notice = 'The run is queued. Its result appears here when it ends.';
      await onChanged();
    } catch (err) { problem = String(err); }
    finally { busy = false; }
  }
  function ask(run, yes) {
    checked = false; confirmationError = '';
    dialog = { run, yes };
  }
  function closeDialog() { if (!busy) dialog = null; }
  async function confirm() {
    if (!dialog || !checked || busy) return;
    const { run, yes } = dialog;
    busy = true; confirmationError = ''; notice = '';
    try {
      await api.ConfirmGrading(run.id, yes);
      dialog = null;
      notice = yes ? `Grade confirmed: ${score(run.points, run.max_points)}` : 'The confirmation is taken back: this grade is advisory again.';
      await onChanged();
      if (selectedId === run.id) await read(run.id);
    } catch (err) { confirmationError = String(err); }
    finally { busy = false; }
  }
  onMount(() => () => { alive = false; });
</script>

<div class="review-toolbar">
  <select class="review-select" bind:value={submission} aria-label="Submission to grade" disabled={busy || !submissions.length}>
    <option value={0}>{submissions.length ? `Submission #${submissions.length} · the latest · ${localDateTime(submissions[0].submitted_at)}` : 'No submissions'}</option>
    {#each submissions.slice(1) as item, index (item.id)}<option value={item.id}>Submission #{submissions.length - index - 1}{item.final ? ' · final' : ''} · {localDateTime(item.submitted_at)}</option>{/each}
  </select>
  <button type="button" class="review-button primary" disabled={busy || !submissions.length || going} title={going ? 'It can be started again when the current run ends' : ''} onclick={grade}>
    {#if going}<span class="review-spinner" aria-hidden="true"></span>A run is going…{:else}<Icon name="play" size={14} />Grade this student{/if}
  </button>
  <span class="review-spacer"></span>
  <button type="button" class="review-button outline" disabled={!submissions.length || !!saving} onclick={onDownload}><Icon name="download" size={14} />Download report (.md)…</button>
</div>
<p class="review-hint"><Icon name="info" size={14} /><span>Hidden tests run on a submission in a fresh container with no network. A grade is <strong>{gradeNote}</strong>.</span></p>
{#if problem}<p role="alert" class="problem">{problem}</p>{/if}
{#if notice}<p role="status" class="notice">{notice}</p>{/if}

{#if runs.length}
  <div class="runs" role="tablist" aria-label="Grading runs">
    {#each runs as run, index (run.id)}
      <button class="run" type="button" role="tab" id="review-run-{run.id}" aria-selected={selectedId === run.id} aria-controls="review-run-panel-{run.id}" tabindex={selectedId === run.id || selectedId === null && index === 0 ? 0 : -1} onkeydown={event => runKeys(event, index)} onclick={() => open(run.id)}>
        <span class="run-id mono">#{run.id}</span><strong class="mono">{run.state === 'done' ? score(run.points, run.max_points) : '—'}</strong><span class="review-pill {tone(run)}">{runState(run)}</span><span class="review-hint">{localDateTime(run.queued_at)}</span>
      </button>
    {/each}
  </div>
{/if}
{#if selectedId !== null}
  <div role="tabpanel" id="review-run-panel-{selectedId}" aria-labelledby="review-run-{selectedId}" class="run-panel">
    <section class="run-card" aria-label="Results of run {selectedId}">
      {#if loading}<p class="pending review-hint" role="status">Reading run #{selectedId}…</p>{/if}
      {#if detail}
        <header class="run-head">
          {#if detail.state === 'done'}
            <div class="score mono">{score(detail.points, detail.max_points)}</div>
            {#if detail.max_points > 0}<span class="bar overall" aria-hidden="true"><i style:width={`${width(detail.points, detail.max_points)}%`}></i></span>{/if}
          {/if}
          <span class="review-pill {tone(detail)}">{#if detail.confirmed}<Icon name="check" size={12} />{/if}{runState(detail)}</span>
          <span class="meta">Run <b>#{detail.id}</b> · submission <b>{detail.submission}</b>{detail.duration_ms ? ` · ${numeric(detail.duration_ms / 1000)} s` : ''}{detail.finished_at ? ` · ended ${localDateTime(detail.finished_at)}` : ` · asked ${localDateTime(detail.queued_at)}`}</span>
          {#if detail.state === 'done' || detail.state === 'failed'}
            <div class="review-toolbar">
              <button class="review-button" onclick={() => { dialog = { output: detail }; }}><Icon name="terminal" size={14} />What the run printed</button>
              {#if detail.state === 'done'}
                {#if detail.confirmed}<button class="review-button outline" disabled={busy} onclick={() => ask(detail, false)}>Take the confirmation back</button>
                {:else}<button class="review-button primary" disabled={busy} onclick={() => ask(detail, true)}><Icon name="check" size={14} />Confirm {score(detail.points, detail.max_points)}</button>{/if}
              {/if}
            </div>
          {/if}
        </header>
        {#if detail.state === 'queued' || detail.state === 'running'}
          <div class="pending" role="status">
            <strong class="review-progress"><span class="review-spinner" aria-hidden="true"></span>{detail.state === 'queued' ? 'Queued. The result appears here when it ends.' : 'Running the hidden tests in a fresh container…'}</strong>
            <div class="steps" aria-label="Run progress"><span class="review-pill {detail.state === 'queued' ? 'blue' : 'ok'}">{detail.state === 'running' ? '✓ ' : ''}Queued</span><span class="review-pill {detail.state === 'running' ? 'blue' : ''}">Running tests</span><span class="review-pill">Done</span></div>
          </div>
        {:else if detail.state === 'failed'}
          <p role="alert" class="problem pending"><strong>Grading failed: {failureLabel(detail.failure)}.</strong> {detail.failure_text}</p>
        {:else}
          <div class="review-tiles stats">
            <div class="review-tile ok"><span class="key">Full marks</span><strong class="mono">{full}</strong><span class="sub">of {rows.length} TODO items</span></div>
            <div class="review-tile warn"><span class="key">Partial</span><strong class="mono">{partial}</strong><span class="sub">some points earned</span></div>
            <div class="review-tile alert"><span class="key">Zero</span><strong class="mono">{rows.length - full - partial}</strong><span class="sub">no points earned</span></div>
            <div class="review-tile alert"><span class="key">Ticked, not passed</span><strong class="mono">{mismatch}</strong><span class="sub">student said done</span></div>
          </div>
          {#if mismatch}<p class="insight"><Icon name="warning" size={15} /><span>The student ticked <b>{mismatch}</b> TODO {mismatch === 1 ? 'item' : 'items'} that didn’t get full marks. Ticks are self-reported; this is an observation, not proof of misconduct.</span></p>{/if}
          {#if groups.length}
            <div class="groups">
              <div class="group-tools"><button onclick={() => expandAll(true)}>Expand all</button><button onclick={() => expandAll(false)}>Collapse all</button></div>
              {#each groups as group (group.id)}
                <div class="group">
                  <button class="group-head" aria-expanded={!!openGroups[group.id]} onclick={() => { openGroups[group.id] = !openGroups[group.id]; }}>
                    <span class="review-caret"><Icon name="chevron" size={14} /></span><strong>{group.name}</strong><span class="meta">{group.rows.length} {group.rows.length === 1 ? 'item' : 'items'}</span>
                    {#if group.id === 'todos' && detail.max_points > 0}
                      <span class="review-spacer"></span><span class="bar" aria-hidden="true"><i style:width={`${width(detail.points, detail.max_points)}%`}></i></span><strong class="mono">{score(detail.points, detail.max_points)}</strong>
                    {/if}
                  </button>
                  {#if openGroups[group.id]}
                    {#each group.rows as row (row.id)}
                      {@const key = `${group.id}:${row.id}`}
                      {@const mark = (row.full ?? row.result?.passed) ? 'ok' : row.partial ? 'warn' : 'alert'}
                      <div class="item">
                        <button class="item-head" aria-expanded={!!openItems[key]} onclick={() => { openItems[key] = !openItems[key]; }}>
                          <span class="review-caret"><Icon name="chevron" size={13} /></span><strong class="todo-id mono">{row.id}</strong><span class="desc" title={row.text}>{row.text}</span>
                          {#if group.id === 'todos'}<span class="tick {row.ticked ? row.full ? 'ok' : 'warn' : ''}">{row.ticked ? '☑ ticked' : '☐ not ticked'}</span>{/if}
                          {#if row.result && row.points > 0}<span class="bar {mark}" aria-hidden="true"><i style:width={`${width(row.result.points, row.points)}%`}></i></span>{/if}
                          <span class="review-pill {row.result ? mark : ''} mono">{row.result ? score(row.result.points, row.points) : '—'}</span>
                        </button>
                        {#if openItems[key]}
                          <div class="item-log">
                            <p>{row.text}</p>
                            {#if group.id === 'todos'}<p class="mobile-tick">{row.ticked ? '☑ Ticked' : '☐ Not ticked'} · self-reported</p>{/if}
                            {#if row.result}<span class="review-pill {row.result.passed ? 'ok' : 'alert'}">{row.result.passed ? '✓ Passed' : '✕ Not passed'}</span><pre>{row.result.log || 'No log was reported.'}</pre>
                            {:else}<p class="review-hint">The grade program said nothing of it.</p>{/if}
                          </div>
                        {/if}
                      </div>
                    {/each}
                  {/if}
                </div>
              {/each}
            </div>
          {/if}
        {/if}
        {#if detail.skipped?.length}<p class="pending review-hint">Not given to the container (a link out of the project or an unsupported name): {detail.skipped.join(', ')}.</p>{/if}
      {/if}
    </section>
  </div>
{:else}<p class="review-hint">This student has not been graded.</p>{/if}

{#if dialog?.output}<RunOutput run={dialog.output} {copy} onClose={closeDialog} />
{:else if dialog}
  <Modal title={dialog.yes ? 'Confirm grade' : 'Take the confirmation back'} titleId="review-confirm-title" subtitle="This changes the grade that stands for the student." icon="check" tone="deep" width="29rem" {busy} lightDismiss onClose={closeDialog}>
    <div class="review-modal confirm-body">
      <div class="subject"><Icon name="fileCheck" size={22} /><div><strong>Run #{dialog.run.id}</strong><span class="review-hint">Submission {dialog.run.submission} · ended {localDateTime(dialog.run.finished_at)}</span></div><strong class="mono">{score(dialog.run.points, dialog.run.max_points)}</strong></div>
      <div class="scope"><div><Icon name="check" size={15} />{dialog.yes ? 'This run’s grade stands for the student.' : 'This run becomes advisory again.'}</div><div><Icon name="clock" size={15} />You can confirm another run later.</div></div>
      <!-- svelte-ignore a11y_autofocus (The dialog opens from a deliberate user action.) -->
      <label class="gate" class:checked><input type="checkbox" bind:checked disabled={busy} autofocus /> <span>{dialog.yes ? 'I confirm' : 'I take back the confirmation of'} <strong class="mono">{pointsText(dialog.run.points, dialog.run.max_points)}</strong> from run #{dialog.run.id}.</span></label>
      {#if confirmationError}<p role="alert" class="problem">{confirmationError}</p>{/if}
    </div>
    {#snippet footer()}
      <div class="review-modal review-toolbar review-footer">
        <button class="review-button" disabled={busy} onclick={closeDialog}>Cancel</button>
        <button class="review-button primary" disabled={!checked || busy} onclick={confirm}>{busy ? 'Saving…' : dialog.yes ? `Confirm ${score(dialog.run.points, dialog.run.max_points)}` : 'Take confirmation back'}</button>
      </div>
    {/snippet}
  </Modal>
{/if}

<style>
  .problem { margin: 0; color: var(--alert-text); overflow-wrap: anywhere; }
  .notice { margin: 0; color: var(--ok-text); font-size: .78rem; }
  .runs { display: flex; flex-wrap: wrap; gap: .45rem; }
  .run { display: flex; align-items: center; flex-wrap: wrap; gap: .4rem; min-height: 32px; padding: .25rem .55rem; margin: 0; border-radius: 8px; background: var(--bg-primary); color: var(--text-main); box-shadow: none; }
  .run:hover, .run[aria-selected='true'] { background: var(--selection-fill); border-color: var(--action-primary); }
  .run-id { color: var(--action-hover); font-size: .75rem; }
  .run-panel { min-width: 0; }
  .run-card { border: 1px solid var(--border-subtle); border-radius: 10px; overflow: hidden; min-width: 0; }
  .run-head { display: flex; flex-wrap: wrap; align-items: center; gap: .55rem; padding: .65rem .85rem; background: var(--bg-primary); border-bottom: 1px solid var(--border-subtle); }
  .run-head .review-toolbar { margin-left: auto; }
  .score { font-size: 1.35rem; font-weight: 750; color: var(--action-hover); }
  .meta { color: var(--text-muted); font-size: .75rem; }
  .meta b { font-family: var(--font-mono); font-weight: 650; }
  .bar { display: block; width: 68px; height: 6px; background: var(--bg-secondary); border-radius: 999px; overflow: hidden; }
  .bar.overall { width: 88px; height: 8px; }
  .bar i { display: block; height: 100%; border-radius: inherit; background: var(--bar-fill); }
  .bar.ok i { background: var(--ok); } .bar.warn i { background: var(--warn); } .bar.alert i { background: var(--alert); }
  .pending { padding: .75rem .85rem; }
  .steps { display: flex; flex-wrap: wrap; gap: .4rem; margin-top: .55rem; }
  div.stats { padding-bottom: .35rem; }
  .insight { display: flex; align-items: flex-start; gap: .45rem; padding: .45rem .6rem; margin: .35rem .85rem; background: var(--warn-fill); color: var(--warn-text); border-left: 4px solid var(--warn); border-radius: 6px; }
  .insight :global(svg) { flex: none; }
  .groups { display: grid; gap: .4rem; padding: .45rem .85rem .85rem; }
  .group-tools { display: flex; justify-content: flex-end; gap: .6rem; }
  .group-tools button { background: none; color: var(--action-hover); font-size: .75rem; box-shadow: none; border: 0; min-height: 24px; margin: 0; padding: .15rem .25rem; }
  .group-tools button:hover { text-decoration: underline; }
  .group { border: 1px solid var(--border-subtle); border-radius: 8px; min-width: 0; overflow: hidden; }
  .group-head, .item-head { display: flex; align-items: center; gap: .5rem; width: 100%; min-height: 34px; font: inherit; text-align: left; padding: .35rem .6rem; margin: 0; border: 0; border-radius: 0; box-shadow: none; }
  .group-head { background: var(--bg-primary); }
  .group-head:focus-visible, .item-head:focus-visible { outline-offset: -2px; }
  .group-head[aria-expanded='true'] .review-caret, .item-head[aria-expanded='true'] .review-caret { transform: rotate(90deg); }
  .item { border-top: 1px solid var(--border-subtle); }
  .item-head { background: var(--bg-raised); }
  .item-head[aria-expanded='true'] { background: var(--hover-fill); }
  .desc { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--text-muted); }
  .todo-id { color: var(--action-hover); max-width: 25%; overflow-wrap: anywhere; }
  .tick { color: var(--text-muted); font-size: .73rem; white-space: nowrap; }
  .tick.ok { color: var(--ok-text); } .tick.warn { color: var(--warn-text); }
  .item-log { padding: .5rem .65rem .6rem 1.9rem; background: var(--bg-primary); overflow-wrap: anywhere; }
  .item-log p { margin: 0 0 .35rem; white-space: pre-wrap; }
  .item-log pre { margin: .35rem 0 0; max-height: 12rem; overflow: auto; white-space: pre-wrap; overflow-wrap: anywhere; font-size: .78rem; }
  .confirm-body { display: grid; gap: .8rem; padding: 1.05rem; }
  .subject { display: flex; align-items: center; flex-wrap: wrap; gap: .6rem; padding: .55rem .65rem; background: var(--bg-primary); border: 1px solid var(--border-subtle); border-radius: 9px; }
  .subject :global(svg) { color: var(--action-primary); }
  .subject > :last-child { margin-inline-start: auto; color: var(--action-hover); }
  .scope { display: grid; grid-template-columns: 1fr 1fr; gap: .45rem; font-size: .8rem; }
  .scope div { display: flex; align-items: center; gap: .4rem; padding: .5rem; border-radius: 7px; background: var(--bg-secondary); }
  .scope div:first-child { background: var(--hover-fill); color: var(--action-hover); }
  .scope :global(svg) { flex: none; }
  .gate { display: flex; align-items: center; gap: .5rem; margin: 0; padding: .55rem .65rem; border: 1px solid var(--border-subtle); border-left: 4px solid var(--warn); border-radius: 8px; font-size: .86rem; }
  .gate.checked { color: var(--ok-text); background: var(--ok-fill); border-left-color: var(--ok); }
  .gate input { flex: none; accent-color: var(--ok-text); }
  .review-footer { justify-content: flex-end; }
  .mobile-tick { display: none; color: var(--text-muted); font-size: .75rem; }
  @media (max-width: 540px) {
    .item-head .bar, .item-head .tick, .group-head .bar { display: none; }
    .item-head { gap: .3rem; }
    .mobile-tick { display: block; }
    .scope { grid-template-columns: 1fr; }
  }
  @media (max-width: 480px) { .review-footer { flex-direction: column-reverse; align-items: stretch; } }
</style>
