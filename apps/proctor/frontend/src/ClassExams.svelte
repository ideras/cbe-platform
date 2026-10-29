<script>
  import { formatTime } from '../../../../shared/exam/timing.js';
  import { onDestroy } from 'svelte';
  import * as api from '$proctor-api';
  import Panel from './Panel.svelte';
  import Confirmation from './Confirmation.svelte';
  import ExamImport from './ExamImport.svelte';
  import DownloadDialog from './DownloadDialog.svelte';
  import PreviewDialog from './PreviewDialog.svelte';
  import { parseTime } from './dates.js';
  import { monitorExam } from './monitor.js';
  import Icon from './Icon.svelte';
  // The exams of one class, where the proctor prepares them: an exam is imported into a class and stays there, and publishing it
  // admits that class's students. Exam files are Markdown, compiled and checked in Go; this view shows the result and asks for
  // what the file leaves out: the PIN and whether students may go back to an exercise.
  let { view, entry: klass, disabled = false, onSelected, onRefresh } = $props();

  let examPreview = $state(null);
  let showPin = $state(false);
  let pin = $state('');
  let forwardOnly = $state(false);
  let status = $state('');
  let problem = $state('');
  let working = $state(false);
  let modal = $state(null);
  let modalBusy = $state(false);
  let modalError = $state('');
  // The exam whose submissions are being downloaded, and the folder of the last download for the Open folder button.
  let downloadExam = $state(null);
  let previewExamID = $state(null);
  let lastFolder = $state('');

  const exams = $derived((view.catalog?.exams ?? []).filter(exam => exam.class_id === klass.class_id));
  // Publishing replaces whatever is published, which may be an exam of another class.
  const current = $derived((view.catalog?.exams ?? []).find(exam => exam.published));
  // The server has not answered yet and the last attempt failed (wrong token, server down): say why instead of "Loading…".
  const unreachable = $derived(!!view.error && !view.catalog_loaded);
  const locked = $derived(disabled || working || view.busy);

  function plural(n, noun) { return `${n} ${noun}${n === 1 ? '' : 's'}`; }
  function when(value) { return parseTime(value)?.toLocaleString() ?? null; }
  // What an exam's calculator is, for the import preview and the exam row; null when the exam has none. It
  // takes both wire shapes: the preview's summary (angle, disabled count) and the catalog's tools object
  // (angle_mode, disabled_functions) — the server applies the defaults in both. The angle is named only when
  // scientific mode is on, since only its trigonometric functions read it.
  function calculatorText(calculator) {
    // The object is sent only when the calculator is on, in both shapes; a missing object means no calculator.
    if (!calculator) return null;
    const modes = calculator.modes?.length ? calculator.modes : ['scientific'];
    const parts = [`Calculator: ${modes.join(', ')}`];
    if (modes.includes('scientific')) parts.push({ deg: 'degrees', rad: 'radians' }[calculator.angle ?? calculator.angle_mode] ?? 'selectable angle');
    const disabled = calculator.disabled ?? calculator.disabled_functions?.length ?? 0;
    if (disabled) parts.push(`${disabled} function${disabled === 1 ? '' : 's'} disabled`);
    return parts.join(' · ');
  }
  function span(entry) {
    const opens = when(entry.opens_at), closes = when(entry.closes_at);
    return opens || closes ? `${opens ?? 'any time'} → ${closes ?? 'no end'}` : 'any time';
  }
  // Whether new students could log in to this exam right now, by the server's clock.
  function admission(entry) {
    const now = new Date(view.catalog.server_time);
    if (entry.opens_at && now < new Date(entry.opens_at)) return 'not open yet';
    if (entry.closes_at && now >= new Date(entry.closes_at)) return 'closed';
    return 'open';
  }
  async function run(work) {
    working = true; problem = ''; status = ''; lastFolder = '';
    try { await work(); } catch (err) { problem = String(err); } finally { working = false; }
  }
  const chooseExam = () => run(async () => {
    const preview = await api.ChooseExam(klass.class_id);
    if (!preview.cancelled) { examPreview = preview; showPin = false; pin = ''; forwardOnly = preview.navigation === 'forward_only'; }
  });
  const importExam = () => run(async () => {
    const title = examPreview.title;
    try { await api.ImportExam(klass.class_id, pin, forwardOnly); status = `Imported “${title}” into ${klass.name}. It is stored but not published: students cannot log in to it yet.`; }
    finally { examPreview = null; showPin = false; pin = ''; await onRefresh(); }
  });
  async function cancelExam() { examPreview = null; showPin = false; pin = ''; await api.DiscardImport().catch(() => {}); }
  // Leaving the class with a preview open forgets the file the native side was keeping for it.
  onDestroy(() => { if (examPreview) api.DiscardImport().catch(() => {}); });
  function contents(entry) {
    const parts = [plural(entry.exercise_count, 'exercise'), plural(entry.slot_count, 'answer field')];
    if (entry.points != null) parts.push(`${entry.points} points`);
    return parts.join(', ');
  }
  // Monitoring needs something to watch: the published exam, or one students have taken (closing admission does not end the
  // sessions already running).
  const monitorable = entry => entry.published || entry.total_sessions > 0;
  const monitor = exam => run(() => monitorExam(view, exam.exam_id, onSelected));

  // A download finished: show what was written and keep the folder for the Open folder button. Files that could not be
  // written are named so the proctor can retry just those.
  function downloaded(result) {
    const count = result.files.length;
    const skippedWorking = result.skipped.filter(item => item.status === 'working').length;
    const skippedFinishing = result.skipped.filter(item => item.status === 'finishing').length;
    if (result.failed.length) {
      const names = result.failed.map(item => item.student_name || item.account_number).filter(Boolean);
      status = `PARTIAL: downloaded ${count} of ${count + result.failed.length} submissions to ${result.folder}. Retry for ${names.join(', ')}.`;
    } else {
      status = `Downloaded ${plural(count, 'submission')} to ${result.folder}.`;
      if (skippedWorking) status += ` ${plural(skippedWorking, 'student')} ${skippedWorking === 1 ? 'is' : 'are'} still working and ${skippedWorking === 1 ? 'was' : 'were'} skipped.`;
      if (skippedFinishing) status += ` ${plural(skippedFinishing, 'student')} ${skippedFinishing === 1 ? 'was' : 'were'} still finishing and ${skippedFinishing === 1 ? 'was' : 'were'} skipped.`;
    }
    if (result.warnings?.length) status += ' ' + result.warnings.join(' ');
    // Only a download that wrote students' files has a folder worth opening; the dashboard refuses to open any other.
    lastFolder = count > 0 ? result.folder : '';
  }
  async function openFolder() {
    problem = '';
    try { await api.OpenDownloadFolder(); } catch (err) { problem = String(err); }
  }
  function ask(kind, exam = null) {
    modalError = '';
    modal = { kind, entry: exam, klass, current, currentClass: view.catalog?.current_class_name ?? '', admission: exam ? admission(exam) : 'open' };
  }
  async function confirm() {
    if (modalBusy) return;
    modalBusy = true; modalError = ''; lastFolder = '';
    const { kind, entry, current } = modal;
    const inUse = (current?.active_sessions ?? 0) > 0;
    try {
      if (kind === 'publish') {
        await api.Publish(entry.exam_id, current?.exam_id ?? '', inUse, true);
        status = `“${entry.title}” is published to ${klass.name}. Its students log in with the exam PIN. Use Monitor to watch them in the monitoring window.`;
        modal = null;
        await onSelected();
      } else if (kind === 'deleteExam') {
        await api.DeleteExam(entry.exam_id, true);
        status = `Exam “${entry.title}” deleted.`;
        modal = null;
        await onRefresh();
      } else if (kind === 'purge') {
        const value = await api.PurgeScreenshots(entry.exam_id, true);
        status = `Screenshots of “${entry.title}”: ${value.deleted} deleted. ${value.complete ? 'Complete; this exam accepts no more images.' : `PARTIAL: retry for image IDs ${value.failed_screenshot_ids.join(', ')}.`} Delete downloaded copies, exports and backups separately.`;
        modal = null;
        await onRefresh();
      } else {
        await api.Unpublish(current.exam_id, inUse, true);
        status = `Admission is closed. Students already in “${current.title}” keep working; nobody else can log in.`;
        modal = null;
        await onRefresh();
      }
    } catch (err) { modalError = String(err); await onRefresh().catch(() => {}); }
    finally { modalBusy = false; }
  }
</script>

<div class="exams">
<Panel label="Exams">
  {#if unreachable}
    <p class="wide" role="alert">Cannot load the exams from the server. {view.error}</p>
  {:else if !view.catalog_loaded}<p class="muted">Loading exams…</p>{/if}
  <div class="toolbar">
    <button class:primary={!examPreview?.valid} disabled={locked} onclick={chooseExam}>Import exam…</button>
  </div>
  {#if status}<p role="status">{status}{#if lastFolder} <button type="button" class="open-folder" disabled={working} onclick={openFolder}>Open folder</button>{/if}</p>{/if}
  {#if problem}<p role="alert">{problem}</p>{/if}

  {#if examPreview}
    <ExamImport preview={examPreview} className={klass.name}
      contents={contents(examPreview)} duration={formatTime(examPreview.duration_seconds)} grace={formatTime(examPreview.grace_seconds)}
      calculator={calculatorText(examPreview.calculator)} logins={span(examPreview)}
      bind:pin bind:showPin bind:forwardOnly {locked} {working}
      onImport={importExam} onCancel={cancelExam} onChoose={chooseExam} />
  {/if}

  {#if view.catalog_loaded}
    {#if exams.length === 0}
      <p>This class has no exams yet. Import one from its Markdown file.</p>
    {:else}
      <div class="table-scroll"><table>
        <caption>Exams of {klass.name} — newest first. Monitoring an exam does not let students in; only publishing does.</caption>
        <thead><tr><th>Exam</th><th>Exercises</th><th>Time</th><th>Logins allowed</th><th>Sessions</th><th>Actions</th></tr></thead>
        <tbody>{#each exams as entry (entry.exam_id)}
          <tr class:published={entry.published}>
            <td><strong>{entry.title}</strong>
              {#if entry.published}<span class="badge open">Published</span>{/if}
              {#if entry.exam_id === view.exam_id}<span class="badge watching">Monitoring</span>{/if}
              {#if entry.navigation === 'forward_only'}<span class="badge forward" title="Students cannot go back to an exercise once they move on">Forward only</span>{/if}
              {#if calculatorText(entry.tools?.calculator)}<span class="badge calc" title={calculatorText(entry.tools.calculator)}>{calculatorText(entry.tools.calculator)}</span>{/if}
              <small>{entry.exam_id}</small></td>
            <td>{entry.exercise_count}</td>
            <td>{formatTime(entry.duration_seconds)} + {formatTime(entry.grace_seconds)} grace</td>
            <td>{span(entry)}{#if admission(entry) !== 'open'}<small class="late">Logins are {admission(entry)} now</small>{/if}</td>
            <td>{entry.active_sessions} working · {entry.total_sessions} total</td>
            <td class="row-cell"><div class="row-actions">
              <button disabled={locked || !monitorable(entry)} aria-label="Monitor {entry.title}" title={monitorable(entry) ? `Open the monitoring window for ${entry.title}` : 'Nobody has taken this exam yet; publish it first'} onclick={() => monitor(entry)}><Icon name="monitor" class="monitor" size="1.35rem" /></button>
              <button disabled={locked} aria-label="Preview {entry.title}" title={`Preview ${entry.title} as the students see it, calculator included`} onclick={() => { previewExamID = entry.exam_id; }}><Icon name="preview" size="1.35rem" /></button>
              {#if !entry.published}<button disabled={locked} aria-label="Publish {entry.title}…" title="Publish {entry.title}…" onclick={() => ask('publish', entry)}><Icon name="publish" size="1.35rem" /></button>{/if}
              {#if entry.published}<button disabled={locked} aria-label="Close admission to {entry.title}…" title="Close admission to {entry.title}…" onclick={() => ask('unpublish', entry)}><Icon name="closeAdmission" class="close-admission" size="1.35rem" /></button>{/if}
              {#if entry.total_sessions > 0}<button disabled={locked} aria-label="Download submissions of {entry.title}…" title="Download submissions of {entry.title}…" onclick={() => { downloadExam = entry; }}><Icon name="download" class="download" size="1.35rem" /></button>{/if}
              {#if entry.total_sessions > 0}<button class="purge" disabled={locked || entry.active_sessions > 0} aria-label="Purge screenshots of {entry.title}…" title={entry.active_sessions > 0 ? 'Students are still working in this exam; purge once grades are final' : `Delete the screenshots of ${entry.title} after grading`} onclick={() => ask('purge', entry)}><Icon name="purge" size="1.35rem" /></button>{/if}
              {#if entry.total_sessions === 0 && !entry.published}<button class="purge" disabled={locked} aria-label="Delete exam {entry.title}…" title="Delete exam {entry.title}…" onclick={() => ask('deleteExam', entry)}><Icon name="deleteExam" class="delete-exam" size="1.35rem" /></button>{/if}
            </div></td>
          </tr>
        {/each}</tbody>
      </table></div>
    {/if}
  {/if}
</Panel>
</div>
{#if modal}<Confirmation kind={modal.kind} exam={modal.entry?.exam_id ?? ''} details={modal} busy={modalBusy} error={modalError} onCancel={() => modal = null} onConfirm={confirm} />{/if}
{#if downloadExam}<DownloadDialog exam={downloadExam} onClose={() => downloadExam = null} onDownloaded={downloaded} />{/if}
{#if previewExamID}<PreviewDialog examID={previewExamID} onClose={() => previewExamID = null} />{/if}

<style>
  .exams { display: grid; grid-template-columns: minmax(0, 1fr); gap: var(--space-4); min-width: 0; align-content: start; overflow-wrap: anywhere; }
  .wide { white-space: pre-line; }
  .toolbar { display: flex; justify-content: flex-end; margin: 0 0 var(--space-3); }
  .toolbar button { margin: 0; }
  .row-cell { width: 1%; }
  .row-actions { display: flex; gap: var(--space-1); }
  /* Icon-only buttons: the tooltip and the accessible name say what each one does. Ghost by default. */
  .row-actions button { display: inline-flex; align-items: center; justify-content: center; width: 2.3rem; height: 2.3rem; min-height: 0; margin: 0; padding: 0; background: var(--transparent); border-color: var(--transparent); color: var(--ink-2); }
  .row-actions button:hover:not(:disabled) { background: var(--hover); border-color: var(--transparent); color: var(--ink); }
  .row-actions button:disabled { background: var(--transparent); border-color: var(--transparent); color: var(--ink-2); }
  .row-actions :global(svg) { flex: none; width: 1.35rem; height: 1.35rem; object-fit: contain; }
  .row-actions :global(svg.monitor), .row-actions :global(svg.close-admission), .row-actions :global(svg.delete-exam), .row-actions :global(svg.download) { width: 1.6rem; height: 1.6rem; }
  .row-actions .purge { color: var(--coral); }
  .row-actions .purge:hover:not(:disabled) { background: var(--color-danger-bg); border-color: var(--transparent); color: var(--color-danger-strong); }
  .row-actions button:disabled :global(svg) { opacity: .35; }
  .open-folder { margin: 0 0 0 var(--space-2); padding: .15rem .6rem; font-size: var(--text-sm); }
  .table-scroll { overflow-x: auto; margin-top: var(--space-3); border: 1px solid var(--panel-border); border-radius: var(--radius-card); }
  table { width: 100%; border-collapse: collapse; min-width: 760px; background: var(--color-surface); }
  caption { padding: var(--space-2) var(--space-3); color: var(--color-text-muted); font-size: var(--text-sm); text-align: left; border-bottom: 1px solid var(--panel-border); }
  th { color: var(--ink-2); font-size: var(--text-sm); letter-spacing: .02em; border-bottom: 1px solid var(--panel-border); }
  th, td { text-align: left; vertical-align: top; padding: .5rem .7rem; overflow-wrap: anywhere; }
  th { white-space: nowrap; }
  td { border-bottom: 1px solid var(--line); }
  small { display: block; margin-top: .15rem; color: var(--color-text-muted); }
  .late { display: inline-block; margin-top: .15rem; padding: .05rem .5rem; border-radius: 999px; background: var(--sand); color: var(--sand-ink); font-weight: 600; }
  .badge { display: inline-block; margin-left: var(--space-1); padding: .05rem .35rem; font-size: var(--text-sm); font-weight: 700; border: 1px solid; border-radius: var(--radius-sm); }
  .open { background: var(--color-success-bg); color: var(--color-success); border-color: var(--color-success); }
  .watching { background: var(--transparent); color: var(--ink-2); border-color: var(--ink-2); }
  .forward { background: var(--transparent); color: var(--ink-2); border-color: var(--ink-2); }
  /* The dash marks an optional enabled feature, independently of its color. */
  .calc { background: var(--sea); color: var(--sea-ink); border-color: color-mix(in srgb, var(--sea-ink) 45%, var(--transparent)); border-style: dashed; }
</style>
