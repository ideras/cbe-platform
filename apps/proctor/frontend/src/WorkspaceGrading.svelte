<script>
  import { onMount, tick } from 'svelte';
  import * as api from '$proctor-api';
  import Icon from '../../../../shared/theme/Icon.svelte';
  import { localDateTime } from './dates.js';
  import { gradeLine, gradeNote, pointsText, failureLabel, sizeText } from './workspace.js';

  // The grading of a student's submissions: the runs of the exam's hidden tests, newest first, as a grid whose chosen row's results are
  // read under it (what each TODO scored, with the log of the grade program), and the button that confirms a grade. The results, the
  // logs and what the run printed are text of the student's programs and the teacher's grade program: shown as text, never as markup.
  let { sessionId, workspace, gradings, onChanged } = $props();
  let selectedId = $state(null);
  let detail = $state(null);
  let loading = $state(false);
  let problem = $state('');
  let notice = $state('');
  let busy = $state(false);
  let submission = $state(0); // 0 is the latest
  let box;
  const rowNodes = {};
  let alive = true;
  let asked = 0;
  let first = true;

  const runs = $derived(gradings?.gradings ?? []);
  const todos = $derived(gradings?.todos ?? []);
  const submissions = $derived([...(workspace?.submissions ?? [])].reverse());
  const entry = $derived(runs.some(run => run.id === selectedId) ? selectedId : runs[0]?.id);
  const todoOf = id => todos.find(todo => todo.id === id);
  const stateOf = run => gradeLine(run);
  const going = $derived(runs.some(run => run.state === 'queued' || run.state === 'running'));

  // The latest run is read to begin with, so that the results are in front of the proctor without a click.
  $effect(() => { if (first && runs.length) { first = false; open(runs[0], false); } });
  // A run that was going and has ended is read again, so that its results replace the "being graded".
  $effect(() => {
    const run = runs.find(item => item.id === selectedId);
    if (run && detail && (detail.state !== run.state || detail.confirmed !== run.confirmed)) read(run.id);
  });

  async function read(id) {
    const mine = ++asked;
    try {
      const value = await api.WorkspaceGrading(id);
      if (alive && mine === asked) detail = value;
    } catch (err) { if (alive && mine === asked) problem = `The run could not be read: ${String(err)}`; }
  }
  async function open(run, reveal = true) {
    selectedId = run.id; problem = ''; notice = '';
    detail = null; loading = true;
    await read(run.id);
    if (alive) loading = false;
    if (reveal && alive && detail) { await tick(); requestAnimationFrame(() => box?.scrollIntoView({ block: box.getBoundingClientRect().height > window.innerHeight ? 'start' : 'nearest', behavior: window.matchMedia?.('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth' })); }
  }
  function keys(event, index) {
    const last = runs.length - 1;
    const to = { ArrowDown: Math.min(index + 1, last), ArrowUp: Math.max(index - 1, 0), Home: 0, End: last }[event.key];
    if (to !== undefined) { event.preventDefault(); rowNodes[runs[to].id]?.focus(); }
    else if (event.key === 'Enter' || event.key === ' ') { event.preventDefault(); open(runs[index]); }
  }

  async function grade() {
    busy = true; problem = ''; notice = '';
    try {
      const id = await api.GradeStudent(sessionId, Number(submission));
      notice = 'The run is queued. Its result appears here when it ends.';
      selectedId = id; detail = null;
      await onChanged();
      await read(id);
    } catch (err) { problem = String(err); }
    finally { busy = false; }
  }
  async function confirm(run, yes) {
    busy = true; problem = ''; notice = '';
    try {
      await api.ConfirmGrading(run.id, yes);
      notice = yes ? 'This grade is confirmed.' : 'The confirmation is taken back: this grade is advisory again.';
      await onChanged();
      await read(run.id);
    } catch (err) { problem = String(err); }
    finally { busy = false; }
  }
  onMount(() => () => { alive = false; });
</script>

<p class="muted">The exam's hidden tests are run on a submission in a fresh container with no network. A grade is <strong>{gradeNote}</strong>: confirm the run whose grade stands.</p>
<div class="start">
  <label>Submission <select bind:value={submission} aria-label="Submission to grade" disabled={busy || !submissions.length}>
    <option value={0}>The latest{submissions.length ? ` (${localDateTime(submissions[0].submitted_at)})` : ''}</option>
    {#each submissions as item (item.id)}<option value={item.id}>{item.final ? 'Final submission' : 'Submission'} at {localDateTime(item.submitted_at)} ({item.files} files)</option>{/each}
  </select></label>
  <button type="button" class="primary" disabled={busy || !submissions.length || going} onclick={grade}><Icon name="done" size={16} />Grade this student</button>
  {#if !submissions.length}<span class="muted">Nothing has been submitted yet.</span>{:else if going}<span class="muted">A run is going; it can be started again when it ends.</span>{/if}
</div>
{#if problem}<p role="alert" class="problem">{problem}</p>{/if}
{#if notice}<p role="status" class="notice">{notice}</p>{/if}

{#if runs.length}
  <table class="runs" role="grid" aria-label="Gradings of the student's submissions">
    <thead><tr><th scope="col">Run</th><th scope="col">Asked</th><th scope="col">Submission</th><th scope="col">Result</th><th scope="col">Grade</th></tr></thead>
    <tbody>{#each runs as run, index (run.id)}
      <tr bind:this={rowNodes[run.id]} class:chosen={selectedId === run.id} aria-selected={selectedId === run.id} tabindex={entry === run.id ? 0 : -1} onclick={() => open(run)} onkeydown={event => keys(event, index)}>
        <td>#{run.id}</td><td>{localDateTime(run.queued_at)}</td><td>{run.submission}</td>
        <td>{stateOf(run).text}</td>
        <td>{#if run.confirmed}<strong class="confirmed">Confirmed</strong>{:else if run.state === 'done'}Advisory{:else}—{/if}</td>
      </tr>
    {/each}</tbody>
  </table>
  {#if selectedId !== null}
    <section class="detail" bind:this={box} aria-label="Results of run {selectedId}">
      {#if loading}<p class="muted" role="status">Reading the run…</p>{/if}
      {#if detail}
        <header><h3>Run #{detail.id}</h3><span class="muted">submission {detail.submission}{#if detail.duration_ms} · {Math.round(detail.duration_ms / 100) / 10} s{/if}{#if detail.finished_at} · ended {localDateTime(detail.finished_at)}{/if}</span></header>
        {#if detail.state === 'queued' || detail.state === 'running'}
          <p role="status" class="muted">{detail.state === 'queued' ? 'Waiting for a place to run.' : 'Running now…'}</p>
        {:else if detail.state === 'failed'}
          <p role="alert" class="problem"><strong>Grading failed: {failureLabel(detail.failure)}.</strong> {detail.failure_text}</p>
        {:else}
          <p class="total"><strong>{pointsText(detail.points, detail.max_points)}</strong> <span class="muted">{detail.confirmed ? 'confirmed' : gradeNote}</span></p>
          <table class="results" aria-label="Results of each TODO">
            <thead><tr><th scope="col">TODO</th><th scope="col">Result</th><th scope="col">Points</th><th scope="col">Log of the grade program</th></tr></thead>
            <tbody>
              {#each todos as todo (todo.id)}
                {@const result = detail.results.find(item => item.todo_id === todo.id)}
                <tr><th scope="row"><code>{todo.id}</code> <span class="todo-text">{todo.text}</span></th>
                  {#if result}<td class:ok={result.passed}>{result.passed ? 'Passed' : 'Not passed'}</td><td>{pointsText(result.points, todo.points).replace(' points', '')}</td><td><pre>{result.log || '—'}</pre></td>
                  {:else}<td colspan="3" class="muted">The grade program said nothing of it.</td>{/if}</tr>
              {/each}
              {#each detail.results.filter(item => !item.known) as extra (extra.todo_id)}
                <tr><th scope="row"><code>{extra.todo_id}</code> <span class="muted">is not a TODO of this exam; it is not counted</span></th><td>{extra.passed ? 'Passed' : 'Not passed'}</td><td>{extra.points}</td><td><pre>{extra.log || '—'}</pre></td></tr>
              {/each}
            </tbody>
          </table>
        {/if}
        {#if detail.skipped?.length}<p class="muted">Not given to the container (a link out of the project, or a name it cannot hold): {detail.skipped.join(', ')}.</p>{/if}
        {#if detail.state === 'done' || detail.state === 'failed'}
          <details>
            <summary>What the run printed{detail.stdout_truncated || detail.stderr_truncated ? ' (cut at the limit)' : ''}</summary>
            <h4>Standard output <span class="muted">{sizeText(detail.stdout.length)}</span></h4><pre aria-label="Standard output of the run">{detail.stdout || '(nothing)'}</pre>
            <h4>Standard error <span class="muted">{sizeText(detail.stderr.length)}</span></h4><pre aria-label="Standard error of the run">{detail.stderr || '(nothing)'}</pre>
          </details>
        {/if}
        {#if detail.state === 'done'}
          <div class="confirm">
            {#if detail.confirmed}<button type="button" disabled={busy} onclick={() => confirm(detail, false)}>Take the confirmation back</button>
            {:else}<button type="button" class="primary" disabled={busy} onclick={() => confirm(detail, true)}><Icon name="check" size={16} />Confirm this grade</button>{/if}
          </div>
        {/if}
      {/if}
    </section>
  {/if}
{:else}<p class="muted">This student has not been graded.</p>{/if}

<style>
  .muted { color: var(--text-muted); font-size: var(--text-sm); }
  .problem { color: var(--alert-text); }
  .notice { margin: var(--space-2) 0; color: var(--ok-text); font-size: var(--text-sm); }
  .start { display: flex; flex-wrap: wrap; align-items: center; gap: var(--space-2); margin: var(--space-2) 0; }
  .start label { display: inline-flex; align-items: center; gap: var(--space-2); margin: 0; font-size: var(--text-sm); }
  .start button { display: inline-flex; align-items: center; gap: .35rem; margin: 0; }
  .runs, .results { width: 100%; border-collapse: collapse; font-size: var(--text-sm); }
  .runs th, .runs td, .results th, .results td { padding: .4rem .6rem; text-align: left; vertical-align: top; border-bottom: 1px solid var(--border-subtle); }
  .runs thead th, .results thead th { color: var(--action-hover); font-size: var(--text-xs); letter-spacing: .06em; text-transform: uppercase; }
  .runs tbody tr { cursor: pointer; }
  .runs tbody tr:hover { background: var(--hover-fill); }
  .runs tbody tr.chosen { background: var(--selection-fill); font-weight: 700; box-shadow: inset 4px 0 0 var(--action-primary); }
  .runs tbody tr:focus-visible { outline: 2px solid var(--action-primary); outline-offset: -2px; }
  .confirmed { color: var(--ok-text); }
  .detail { margin-top: var(--space-3); padding: var(--space-3); background: var(--bg-primary); border: 1px solid var(--border-subtle); border-radius: var(--radius); scroll-margin-bottom: var(--space-3); }
  .detail header { display: flex; flex-wrap: wrap; align-items: baseline; gap: var(--space-2); }
  .detail h3 { margin: 0; font-size: var(--text-base); }
  .detail h4 { margin: var(--space-2) 0 var(--space-1); font-size: var(--text-sm); }
  .total { margin: var(--space-2) 0; font-size: var(--text-base); }
  .results th[scope="row"] { font-weight: 400; }
  .todo-text { color: var(--text-main); }
  td.ok { color: var(--ok-text); font-weight: 600; }
  pre { max-height: 14rem; margin: 0; overflow: auto; white-space: pre-wrap; overflow-wrap: anywhere; font-family: var(--font-mono); font-size: var(--text-xs); }
  details summary { cursor: pointer; margin-top: var(--space-2); color: var(--action-hover); font-size: var(--text-sm); }
  .confirm { margin-top: var(--space-3); }
  .confirm button { display: inline-flex; align-items: center; gap: .35rem; margin: 0; }
</style>
