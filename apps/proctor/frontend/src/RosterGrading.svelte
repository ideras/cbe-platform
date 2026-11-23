<script>
  import { untrack } from 'svelte';
  import * as api from '$proctor-api';
  import Icon from '../../../../shared/theme/Icon.svelte';
  import GradeDialog from './GradeDialog.svelte';
  import { localTime } from './dates.js';
  import { gradeLine, gradeSheetNote } from './workspace.js';
  import { batchProgress, gradingRows } from './live-grading.js';

  let { examId, server, exam, sessions, missing, workspaces, summary, stale, disabled, onReview, onRefresh } = $props();
  let filter = $state('all');
  let preview = $state(false);
  let queueBusy = $state(false);
  let queueError = $state('');
  let exportBusy = $state(false);
  let actionError = $state('');
  let outcome = $state('');
  let batch = $state.raw(null);
  let finished = $state.raw(new Map());
  let retries = $state.raw(new Map());

  const rows = $derived(gradingRows(sessions, missing, workspaces));
  const status = {
    running: ['Being graded', 'active'], queued: ['Waiting', 'neutral'], advisory: ['Advisory', 'network'],
    confirmed: ['Confirmed', 'ok'], failed: ['Run failed', 'lock'],
    ungraded: ['Not graded yet', 'neutral'], none: ['No submission', 'neutral'],
  };
  const filterOptions = [
    ['all', 'All'], ['active', 'Being graded or waiting'], ['advisory', 'Advisory'], ['confirmed', 'Confirmed'],
    ['failed', 'Failed'], ['ungraded', 'Not graded'], ['none', 'No submission'],
  ];
  const matches = (row, option) => option === 'all' || (option === 'active' ? ['queued', 'running'].includes(row.status) : row.status === option);
  const counts = $derived(Object.fromEntries(filterOptions.map(([option]) => [option, rows.filter(row => matches(row, option)).length])));
  const visible = $derived(rows.filter(row => matches(row, filter)));
  const runStates = $derived(new Map([...workspaces.values()].flatMap(row => row.grade ? [[row.grade.grading, row.grade.state]] : [])));
  const progress = $derived(batch ? batchProgress(batch.queued, runStates, finished) : null);
  const gradeBlocked = $derived(disabled || queueBusy || !summary.gradable || [...retries.values()].some(request => request.sending));

  $effect(() => {
    const ids = batch?.queued;
    const states = runStates;
    if (!ids || stale) return;
    untrack(() => {
      const terminal = ids.filter(id => !finished.has(id) && ['done', 'failed'].includes(states.get(id)));
      if (terminal.length) finished = new Map([...finished, ...terminal.map(id => [id, states.get(id)])]);
    });
  });

  function setRetry(id, value) {
    const next = new Map(retries);
    if (value) next.set(id, value); else next.delete(id);
    retries = next;
  }

  async function refresh() {
    try { await onRefresh(); }
    catch (err) { actionError = `Could not refresh the roster: ${String(err)}`; }
  }

  async function gradeClass() {
    if (gradeBlocked) return;
    queueBusy = true; queueError = ''; actionError = ''; outcome = '';
    let accepted = false;
    try {
      // GradeExam acts on the native selection. Another window can switch it before this window's next poll.
      const selection = await api.Snapshot();
      if (!selection.connected || selection.exam_id !== examId || selection.server !== server) {
        throw new Error('The monitored exam changed. Open the grading preview for the exam you want.');
      }
      // Filters are visual only. The server chooses eligible submissions for the class.
      const result = await api.GradeExam([]);
      finished = new Map();
      batch = { ...result, queued: [...new Set(result.queued)] };
      accepted = true;
      await refresh();
    } catch (err) { queueError = String(err); }
    finally { queueBusy = false; if (accepted) preview = false; }
  }

  async function retry({ student, workspace }) {
    const id = student.session_id;
    const original = workspace.grade.grading;
    const request = retries.get(id);
    if (disabled || queueBusy || request?.sending || request?.original === original) return;
    setRetry(id, { original, sending: true });
    actionError = ''; outcome = '';
    try {
      const run = await api.GradeStudent(id, 0);
      setRetry(id, { original, sending: false });
      outcome = `Retry #${run} queued for ${student.student_name}.`;
      await refresh();
    } catch (err) {
      setRetry(id, null);
      actionError = `Could not retry grading for ${student.student_name}: ${String(err)}`;
    }
  }

  async function downloadGrades() {
    if (disabled || exportBusy || !summary.done) return;
    exportBusy = true; actionError = ''; outcome = '';
    const atRequest = summary;
    try {
      const path = await api.SaveWorkspaceGrades(examId);
      if (path) outcome = `${gradeSheetNote(path, atRequest)} Counts are from the roster when you requested the file.`;
    } catch (err) { actionError = String(err); }
    finally { exportBusy = false; }
  }
</script>

<div class="panel-body">
  <div class="action-bar" role="group" aria-label="Grading actions">
    <button class="primary" disabled={gradeBlocked} onclick={() => { preview = true; queueError = ''; }}><Icon name="play" size={15} />Grade submissions…</button>
    <button class="outline" disabled={disabled || exportBusy || !summary.done} onclick={downloadGrades} title="Save a CSV with separate columns for confirmed and advisory grades">
      <Icon name="download" size={15} />{exportBusy ? 'Saving grades…' : 'Download grades…'}
    </button>
    <span class="tool-note">A grade is <strong>advisory</strong> until you confirm it in Student Review.</span>
  </div>
  {#if actionError}<p class="callout warning" role="alert">{actionError}</p>{/if}
  {#if outcome}<p class="callout" role="status">{outcome}</p>{/if}
  {#if batch}
    <section class="batch" aria-label="Class grading request">
      <header>
        <Icon name={progress.complete && progress.total ? 'done' : 'fileCheck'} size={16} />
        <strong>{!progress.total ? 'No new runs queued' : progress.complete ? `Class grading finished · ${progress.done} graded` : 'Class grading · queue receipt'}</strong>
        <button type="button" class="outline" aria-label="Hide" title="Hide grading progress" onclick={() => batch = null}><Icon name="close" size={16} /></button>
      </header>
      <div class="batch-body">
        {#if !progress.complete}
          <p role="status">{progress.total} {progress.total === 1 ? 'submission was' : 'submissions were'} queued for grading.</p>
        {/if}
        {#if progress.total}
          <progress aria-label="Finished runs from this request" max={progress.total} value={progress.done + progress.failed}></progress>
          <p aria-live="polite" aria-atomic="true"><strong>{progress.done + progress.failed} of {progress.total} finished</strong> · {progress.done} graded · {progress.failed} failed{!progress.complete ? ` · ${progress.running} running · ${progress.queued} waiting` : ''}</p>
          {#if progress.unseen}<p class="tool-note">{progress.unseen} {progress.unseen === 1 ? 'run not shown' : 'runs not shown'} in the roster yet. A confirmed grade can hide a newer run; open Student Review for run history.</p>{/if}
          {#if stale && !progress.complete}<p class="tool-note">These are last observed states, not current run status.</p>{/if}
        {/if}
        {#if batch.skipped.length}
          <details><summary>{batch.skipped.length} {batch.skipped.length === 1 ? 'student' : 'students'} left out — see why</summary><ul>{#each batch.skipped as skip}<li>{skip.account_number || skip.session_id}: {skip.reason}</li>{/each}</ul></details>
        {/if}
      </div>
    </section>
  {/if}
  <dl class="stats" aria-label="Class grade totals">
    {#each [['active', 'Being graded or waiting', 'active'], ['advisory', 'Advisory', 'attention'], ['confirmed', 'Confirmed', 'done'], ['failed', 'Failed', 'locked'], ['ungraded', 'Submitted, not graded', ''], ['none', 'No submission', '']] as [id, label, tone]}
      <div class="stat {tone}"><dt>{label}</dt><dd class="num">{counts[id]}</dd></div>
    {/each}
  </dl>
  <div class="tools">
    <div class="filters" role="group" aria-label="Filter grades">
      {#each filterOptions as [id, label]}
        <button class="filter" aria-pressed={filter === id} onclick={() => filter = id}>{label} <span class="filter-count">{counts[id]}</span></button>
      {/each}
    </div>
    <span class="tool-note">Running first, then waiting, then by name · Showing {visible.length} of {rows.length}.</span>
  </div>
  <div class="table-frame">
    <!-- svelte-ignore a11y_no_noninteractive_tabindex (Keyboard users need to scroll this region in WebKit.) -->
    <div class="table-scroll" role="region" aria-label="Grading table — scroll horizontally for controls" tabindex="0">
      <table>
        <caption class="visually-hidden">Class grading</caption>
        <colgroup><col style="width:25%" /><col style="width:21%" /><col style="width:21%" /><col style="width:18%" /><col style="width:15%" /></colgroup>
        <thead><tr><th scope="col">Student</th><th scope="col">Latest submission</th><th scope="col">Grade status</th><th scope="col">Score</th><th scope="col">Review</th></tr></thead>
        <tbody>
          {#each visible as item (item.student.session_id ?? `missing-${item.student.account_number}`)}
            {@const { student, workspace, status: id } = item}
            {@const grade = gradeLine(workspace?.grade)}
            {@const request = retries.get(student.session_id)}
            {@const retryPending = request && (request.sending || request.original === workspace?.grade?.grading)}
            <tr class:running={id === 'running'} class:failed={id === 'failed'}>
              <td><strong>{student.student_name}</strong><small class="account">{student.account_number}</small></td>
              <td>{#if workspace?.submissions}
                {workspace.last_submission_at ? localTime(workspace.last_submission_at) : 'Time not reported'}
                <small>{workspace.submissions} total{workspace.submitted_final ? ' · final made' : ''}</small>
              {:else}<span class="tool-note">{student.session_id ? 'Nothing submitted yet' : 'Not logged in'}</span>{/if}</td>
              <td><span class="roster-badge {status[id][1]}">{status[id][0]}</span>
                {#if id === 'failed'}<small>{grade.text}</small>{:else if id === 'advisory'}<small>Confirm in Review</small>{/if}</td>
              <td>{#if id === 'advisory' || id === 'confirmed'}<strong class="score">{grade.text}</strong>
                {#if workspace.grade.points != null && workspace.grade.max_points > 0}
                  <span class="mini-bar" aria-hidden="true"><span style:width={`${Math.min(100, 100 * workspace.grade.points / workspace.grade.max_points)}%`}></span></span>
                {/if}
                {:else if id === 'failed' && workspace.submissions > 0}
                  <button class="outline" aria-label="Retry grading {student.account_number}" disabled={disabled || queueBusy || retryPending} onclick={() => retry(item)}>{retryPending ? (request.sending ? 'Queuing…' : 'Retry queued') : 'Retry'}</button>
                {:else}<span class="tool-note">—</span>{/if}</td>
              <td>{#if student.session_id}<button class="outline" aria-label="Review {student.account_number}" onclick={() => onReview(student)}><Icon name="preview" size={14} />Review</button>{:else}<span class="tool-note">—</span>{/if}</td>
            </tr>
          {:else}<tr class="empty-row"><td colspan="5">{rows.length ? 'No students match this filter.' : 'No students for this exam yet.'}</td></tr>{/each}
        </tbody>
      </table>
    </div>
  </div>
</div>

{#if preview}
  <GradeDialog {summary} {exam} noSubmission={counts.none} busy={queueBusy} {disabled} error={queueError} onCancel={() => preview = false} onConfirm={gradeClass} />
{/if}

<style>
  .action-bar { display: flex; align-items: center; flex-wrap: wrap; gap: .5rem; padding: .55rem .7rem; border: 1px solid var(--border-subtle); border-radius: 10px; background: var(--bg-primary); }
  .action-bar > span { margin-left: auto; }
  .batch { border: 1px solid var(--action-border); border-radius: 10px; overflow: hidden; }
  .batch header { display: flex; align-items: center; flex-wrap: wrap; gap: .45rem; padding: .45rem .7rem; background: var(--header-soft); color: var(--action-hover); }
  .batch header button { margin-left: auto; min-width: 31px; padding-inline: .35rem; }
  .batch-body { display: grid; gap: .4rem; padding: .65rem .8rem; font-size: .86rem; }
  .batch-body p, .batch-body ul { margin: 0; overflow-wrap: anywhere; }
  .batch-body summary { cursor: pointer; color: var(--action-hover); }
  progress { width: 100%; height: .65rem; accent-color: var(--action-primary); }
  .running { background: var(--hover-fill); }
  .running td:first-child { box-shadow: inset 4px 0 var(--accent-cyan); }
  .failed td:first-child { box-shadow: inset 4px 0 var(--alert); }
  small { display: block; color: var(--text-muted); font-size: .76rem; }
  .account, .score { font-family: var(--font-mono); }
  .account { color: var(--action-hover); }
  @media (max-width: 560px) { .action-bar > span { margin-left: 0; width: 100%; } }
</style>
