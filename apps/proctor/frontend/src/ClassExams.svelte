<script>
  import { formatTime } from '../../../../shared/exam/timing.js';
  import { onDestroy } from 'svelte';
  import * as api from '$proctor-api';
  import ExamCard from './ExamCard.svelte';
  import CloseAdmissionDialog from './CloseAdmissionDialog.svelte';
  import PublishDialog from './PublishDialog.svelte';
  import ExamImport from './ExamImport.svelte';
  import DownloadDialog from './DownloadDialog.svelte';
  import PurgeDialog from './PurgeDialog.svelte';
  import DeleteExamDialog from './DeleteExamDialog.svelte';
  import PreviewDialog from './PreviewDialog.svelte';
  import { parseTime } from './dates.js';
  import { monitorExam } from './monitor.js';
  import Icon from '../../../../shared/theme/Icon.svelte';
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
  // The exam whose screenshots are being purged.
  let purgeExam = $state(null);
  // The exam being deleted, and what the Download submissions dialog opened from there last wrote, which the delete dialog repeats.
  let deleteExam = $state(null);
  let deleteDownload = $state('');
  let previewExamID = $state(null);
  let lastFolder = $state('');

  const exams = $derived((view.catalog?.exams ?? []).filter(exam => exam.class_id === klass.class_id));
  // The exam being deleted as the server reports it now, or null once it is gone. The dialog keeps the exam it was opened for.
  const deleting = $derived(deleteExam ? (view.catalog?.exams ?? []).find(exam => exam.exam_id === deleteExam.exam_id) ?? null : null);
  // Publishing replaces whatever is published, which may be an exam of another class.
  const current = $derived((view.catalog?.exams ?? []).find(exam => exam.published));
  // The students working in the exam an open dialog is about to replace or close, as the server reports them now. The dialog keeps the
  // exam it was opened for (the server refuses if another one has been published since); only the count follows the polling, so a
  // student who logs in while the dialog is open is seen before the proctor decides.
  const workingNow = $derived.by(() => {
    const seen = modal?.current;
    if (!seen) return 0;
    return (view.catalog?.exams ?? []).find(exam => exam.exam_id === seen.exam_id)?.active_sessions ?? seen.active_sessions ?? 0;
  });
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
    if (entry.ends_at && now >= new Date(entry.ends_at)) return 'ended';
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
    const noun = result.format === 'answers-only' ? 'answers-only submission' : 'submission';
    const skippedWorking = result.skipped.filter(item => item.status === 'working').length;
    const skippedFinishing = result.skipped.filter(item => item.status === 'finishing').length;
    if (result.failed.length) {
      const names = result.failed.map(item => item.student_name || item.account_number).filter(Boolean);
      status = `PARTIAL: downloaded ${count} of ${count + result.failed.length} ${noun}s to ${result.folder}. Retry for ${names.join(', ')}.`;
    } else {
      status = `Downloaded ${plural(count, noun)} to ${result.folder}.`;
      if (skippedWorking) status += ` ${plural(skippedWorking, 'student')} ${skippedWorking === 1 ? 'is' : 'are'} still working and ${skippedWorking === 1 ? 'was' : 'were'} skipped.`;
      if (skippedFinishing) status += ` ${plural(skippedFinishing, 'student')} ${skippedFinishing === 1 ? 'was' : 'were'} still finishing and ${skippedFinishing === 1 ? 'was' : 'were'} skipped.`;
    }
    if (result.warnings?.length) status += ' ' + result.warnings.join(' ');
    // Only a download that wrote students' files has a folder worth opening; the dashboard refuses to open any other.
    lastFolder = count > 0 ? result.folder : '';
    // A download made from the delete dialog is told there too, where the proctor is deciding whether the copies are enough.
    if (deleteExam?.exam_id === downloadExam?.exam_id) deleteDownload = status;
  }
  async function openFolder() {
    problem = '';
    try { await api.OpenDownloadFolder(); } catch (err) { problem = String(err); }
  }
  // A purge finished: the status line, which stays after the dialog closes, says what was deleted and, if some images could not
  // be, which to retry. The exam list is read again to show the exam as it is now.
  async function purged(result) {
    lastFolder = '';
    status = `Screenshots of “${purgeExam.title}”: ${result.deleted} deleted. ${result.complete ? 'Complete; this exam accepts no more images.' : `PARTIAL: retry for image IDs ${result.failed_screenshot_ids.join(', ')}.`} Delete downloaded copies, exports and backups separately.`;
    await onRefresh().catch(() => {});
  }
  // A deletion finished: the status line, which stays after the dialog closes, says what went and what did not. The exam list is
  // read again, so the exam is gone from it.
  async function deleted(result) {
    lastFolder = '';
    if (result.sessions > 0) {
      status = `Exam “${result.title}” deleted with its ${plural(result.sessions, 'submission')} and ${plural(result.screenshots, 'screenshot')}. Copies you downloaded, exports and backups are not touched.`;
      if (result.files_pending) status += ` ${plural(result.files_pending, 'screenshot file')} could not be removed from the server yet; it keeps trying.`;
    } else status = `Exam “${result.title}” deleted.`;
    await onRefresh().catch(() => {});
  }
  function ask(kind, exam = null) {
    modalError = '';
    modal = { kind, entry: exam, klass, current, currentClass: view.catalog?.current_class_name ?? '', serverTime: view.catalog?.server_time, admission: exam ? admission(exam) : 'open' };
  }
  // A publish went through. The dialog stays open with the PIN and the way to the live roster; the status line, which stays after it
  // closes, says what is published, and the exam list is read again to show the exam as it is now.
  async function published() {
    lastFolder = '';
    const { entry } = modal;
    status = `“${entry.title}” is published to ${klass.name}. Its students log in with the exam PIN. Use Monitor to watch them in the monitoring window.`;
    try { await onSelected(); } catch {}
  }
  // allowActive is the proctor's override: closing admission while students are still working in the exam needs it.
  async function confirm({ allowActive = false } = {}) {
    if (modalBusy) return;
    modalBusy = true; modalError = ''; lastFolder = '';
    const { current } = modal;
    try {
      await api.Unpublish(current.exam_id, allowActive, true);
      status = `Admission is closed. Students already in “${current.title}” keep working; nobody else can log in.`;
      modal = null;
      await onRefresh();
    } catch (err) { modalError = String(err); await onRefresh().catch(() => {}); }
    finally { modalBusy = false; }
  }
</script>

<div class="exams">
<section class="panel" aria-label="Exams">
  <header class="panel-head"><h2>Exams</h2><span class="sub">Newest first</span><div class="tools"><button class:primary={!examPreview?.valid} disabled={locked} onclick={chooseExam}><Icon name="download" size={16} />Import exam…</button></div></header>
  <div class="panel-body">
  {#if unreachable}
    <p class="wide" role="alert">Cannot load the exams from the server. {view.error}</p>
  {:else if !view.catalog_loaded}<p class="muted">Loading exams…</p>{/if}
  <div class="info"><Icon name="info" size={15} /><span>Monitoring an exam does not let students in; only publishing does.</span></div>
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
      <div class="exam-list">{#each exams as entry (entry.exam_id)}
        <ExamCard {entry} monitored={entry.exam_id === view.exam_id} {locked} deemphasized={!!examPreview?.valid}
          monitorable={monitorable(entry)} calculator={calculatorText(entry.tools?.calculator)} logins={span(entry)} admission={admission(entry)}
          onMonitor={monitor} onPreview={entry => previewExamID = entry.exam_id} onPublish={entry => ask('publish', entry)} onUnpublish={entry => ask('unpublish', entry)}
          onDownload={entry => downloadExam = entry} onPurge={entry => purgeExam = entry} onDelete={entry => { deleteDownload = ''; deleteExam = entry; }} />
      {/each}</div>
    {/if}
  {/if}
  </div>
</section>
</div>
{#if modal?.kind === 'publish'}<PublishDialog details={modal} working={workingNow} onClose={() => modal = null} onPublished={published} onFailed={() => onRefresh().catch(() => {})} onMonitor={() => monitorExam(view, modal.entry.exam_id, onSelected)} />
{:else if modal}<CloseAdmissionDialog details={modal} working={workingNow} busy={modalBusy} error={modalError} onCancel={() => modal = null} onConfirm={confirm} />{/if}
{#if downloadExam}<DownloadDialog exam={downloadExam} onClose={() => downloadExam = null} onDownloaded={downloaded} />{/if}
{#if purgeExam}<PurgeDialog exam={purgeExam} onClose={() => purgeExam = null} onPurged={purged} onFailed={() => onRefresh().catch(() => {})} />{/if}
{#if deleteExam}<DeleteExamDialog exam={deleteExam} live={deleting} className={klass.name} downloadNote={deleteDownload} onDownload={entry => downloadExam = entry} onClose={() => deleteExam = null} onDeleted={deleted} onFailed={() => onRefresh().catch(() => {})} />{/if}
{#if previewExamID}<PreviewDialog examID={previewExamID} onClose={() => previewExamID = null} />{/if}

<style>
  .exams { display: grid; grid-template-columns: minmax(0, 1fr); gap: var(--space-4); min-width: 0; align-content: start; overflow-wrap: anywhere; }
  .wide { white-space: pre-line; }
  .panel { min-width: 0; overflow: hidden; background: var(--bg-raised); border: 1px solid var(--border-subtle); border-radius: 14px; box-shadow: var(--shadow-card); }
  .panel-head { display: flex; flex-wrap: wrap; align-items: center; gap: .6rem; padding: .8rem 1rem; background: var(--header-soft); border-bottom: 1px solid var(--cyan-border); }
  h2 { margin: 0; color: var(--action-hover); font-size: 1.05rem; }
  .sub { color: var(--text-muted); font-size: .82rem; }
  .tools { margin-left: auto; }
  .tools button { display: inline-flex; align-items: center; gap: .45rem; min-height: 38px; margin: 0; padding: .3rem 1rem; border-radius: 9px; font-size: .92rem; }
  .panel-body { display: flex; flex-direction: column; gap: .9rem; padding: 1rem; min-width: 0; }
  .panel-body > p { margin: 0; }
  .info { display: flex; align-items: flex-start; gap: .5rem; padding: .5rem .7rem; border: 1px dashed var(--border-subtle); border-radius: 9px; background: var(--bg-primary); color: var(--text-muted); font-size: .85rem; }
  .info :global(svg) { color: var(--action-primary); margin-top: .12rem; }
  .exam-list { display: grid; grid-template-columns: minmax(0, 1fr); gap: .9rem; }
  .open-folder { margin: 0 0 0 var(--space-2); padding: .15rem .6rem; font-size: var(--text-sm); }
</style>
