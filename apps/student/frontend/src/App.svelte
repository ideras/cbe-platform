<script>
  import { onMount, onDestroy } from 'svelte';
  import * as api from '$student-api';
  import ExercisePage from './ExercisePage.svelte';
  import Calculator from '../../../../shared/theme/calculator/Calculator.svelte';
  import RichText from './RichText.svelte';
  import ConfirmDialog from './ConfirmDialog.svelte';
  import CaptureStatus from './CaptureStatus.svelte';
  import LockScreen from './LockScreen.svelte';
  import SoundStatus from './SoundStatus.svelte';
  import Card from '../../../../shared/theme/Card.svelte';
  import brandLogo from './assets/cbe-portal-logo.svg';
  // Inlined as a data: URL on purpose: WebKitGTK cannot play media loaded through Wails' wails:// scheme (NotSupportedError).
  import alertURL from './assets/unfocus-alert.wav?inline';
  import { UnfocusAlert, describeSoundFailure } from './unfocus-alert.js';
  import { AnswerQueue, saveLabel } from './answer-queue.js';
  import { answeredCount, exerciseSlots } from './exercises.js';

  let view = $state({ phase: 'login', acknowledged_revision: 0, timing_stale: true });
  let server = $state('');
  let account = $state('');
  let pin = $state('');
  let exam = $state(null);
  let answers = $state({});
  let revision = $state(0);
  let selected = $state(0);
  let busy = $state(false);
  let error = $state('');
  let bridgeError = $state('');
  let answerBridgeError = $state('');
  let observation = 0;
  let accepted = $state(false);
  let soundError = $state('');
  let sound;
  let closed = false;
  let capturing = $state(false);
  let captureTest = $state({ state: 'idle' });
  // The times the rules show: read from the server as soon as its address is known, with a plain fallback.
  let rules = $state(null);
  let pendingCapture = null;
  let captureBlocked = $derived(Boolean(view.capture?.required && view.capture.blocked));
  let admissionReady = $derived(view.capture?.ready === true && !captureBlocked);
  let canSubmit = $derived(view.phase === 'exam' && !view.submitting && !view.advancing && !busy && !capturing);
  let locked = $derived(!canSubmit || captureBlocked);
  // Forward-only exams show the exercise the server has the student on; earlier ones are closed for good.
  let forwardOnly = $derived(exam?.navigation === 'forward_only');
  let exercises = $derived(exam?.content.exercises ?? []);
  let page = $derived(forwardOnly ? Math.min(view.exercise ?? 0, exercises.length - 1) : selected);
  let exercise = $derived(exercises[page]);
  let lastPage = $derived(page >= exercises.length - 1);
  let confirmingAdvance = $state(false);
  let confirmingSubmit = $state(false);
  // What the submit confirmation reports: fields with an answer of their own, across every exercise.
  let answeredFields = $derived(exercises.reduce((sum, item) => sum + answeredCount(item, answers), 0));
  let totalFields = $derived(exercises.reduce((sum, item) => sum + exerciseSlots(item).length, 0));
  // Monitoring problems the student must know about; the routine monitoring details are not shown.
  let monitoringProblems = $derived(view.monitoring ? [view.monitoring.heartbeat_error, view.monitoring.event_error, view.monitoring.alert_error,
    view.monitoring.dropped_events ? `Unconfirmed/dropped events: ${view.monitoring.dropped_events}. History may be incomplete.` : ''].filter(Boolean) : []);
  const queue = new AnswerQueue(async (snapshot, sequence) => { await api.UpdateAnswers(snapshot, sequence); answerBridgeError = ''; }, problem => answerBridgeError = String(problem));
  // The calculator: the button and the modal exist only while the exam itself is shown (D11), and only when the
  // exam enables a mode this build implements. The allowed set and the results come from Go; the history lives
  // here, so a lock keeps it, and ends with the exam (Phase 4).
  const calcImplementedModes = ['scientific', 'neural', 'programmer'];
  let calcOpen = $state(false);
  let calcAllowed = $state([]);
  let calcHistory = $state([]);
  let calcButton = $state(null);
  let calcModes = $derived((exam?.tools?.calculator?.modes ?? []).filter(mode => calcImplementedModes.includes(mode)));
  let calcAvailable = $derived(calcModes.length > 0 && view.phase === 'exam' && !examLocked);
  let calcAngleSetting = $derived(exam?.tools?.calculator?.angle_mode ?? 'user');
  async function openCalculator() {
    try { calcAllowed = await api.CalcAllowedFunctions(); } catch { calcAllowed = []; }
    calcOpen = true;
  }
  function recordEvaluation(entry) { calcHistory = [...calcHistory, entry].slice(-20); }
  // A lock or the end of the exam closes the calculator; a lock keeps its history, the exam's end clears it (D11).
  $effect(() => {
    if (examLocked || view.phase === 'complete') calcOpen = false;
    if (view.phase === 'complete') calcHistory = [];
  });
  // The exam is locked: the questions and the answer fields are replaced by the lock screen.
  let examLocked = $derived(Boolean(view.locked));
  // How long a duration is, in the words the rules use.
  function duration(millis) {
    if (!millis || millis <= 0) return '';
    const seconds = Math.round(millis / 1000);
    if (seconds < 60) return `${seconds} second${seconds === 1 ? '' : 's'}`;
    const minutes = Math.round(seconds / 60);
    return `${minutes} minute${minutes === 1 ? '' : 's'}`;
  }
  let alarmTime = $derived(rules?.alarm_millis ? `more than ${duration(rules.alarm_millis)}` : 'a few seconds');
  let lockAway = $derived(rules?.lock_away_millis ? duration(rules.lock_away_millis) : 'several minutes');
  let lockNoContact = $derived(rules?.lock_no_contact_millis ? duration(rules.lock_no_contact_millis) : 'several minutes');
  // How much time the instructor gave back, in the words the note uses.
  function addedTime(seconds) {
    const minutes = Math.floor(seconds / 60);
    if (minutes <= 0) return seconds === 1 ? '1 second was added' : `${seconds} seconds were added`;
    return minutes === 1 ? '1 minute was added' : `${minutes} minutes were added`;
  }
  // The rules are read from the address in the form, once the student stops typing it.
  $effect(() => {
    const address = server.trim();
    if (!address) { rules = null; return; }
    let current = true;
    const timer = setTimeout(async () => {
      try { const value = await api.Rules(address); if (current) rules = value; }
      catch { if (current) rules = null; }
    }, 400);
    return () => { current = false; clearTimeout(timer); };
  });

  function apply(value) {
    if (closed) return;
    if (value.exam && !exam) { exam = value.exam; answers = { ...value.answers }; revision = value.revision; }
    if (value.observation < observation) return;
    observation = value.observation;
    view = value;
    if (!server) server = value.server;
  }
  async function refresh() {
    try { apply(await api.Snapshot()); bridgeError = ''; }
    catch { if (!closed) bridgeError = 'Native client connection unavailable. Text remains local; timing is stale.'; }
  }
  onMount(() => {
    sound = new UnfocusAlert(new Audio(alertURL), sequence => {
      soundError = 'Audible alert could not play. Check sound output; monitoring continues.';
      api.ReportAlertFailure(sequence).catch(() => {});
    });
    const unsubscribe = api.OnFocusAlert(incident => sound.play(incident));
    refresh();
    let reading = false;
    const timer = setInterval(async () => { if (reading) return; reading = true; try { await refresh(); } finally { reading = false; } }, 500);
    return () => { clearInterval(timer); unsubscribe(); sound.close(); };
  });
  onDestroy(() => { closed = true; queue.close(); pendingCapture?.cancel(); });

  async function login(event) {
    event.preventDefault(); if (!accepted || busy || capturing || !admissionReady) return;
    // Unlock audible alerts from this click, before anything awaits (see UnfocusAlert.prime).
    sound.prime().catch(problem => { if (!closed) soundError = `${describeSoundFailure(problem)} Audible alerts may not play; monitoring continues.`; });
    busy = true; error = '';
    const submittedPin = pin; pin = '';
    try { await api.AcknowledgeRules(); apply(await api.Login(server, account, submittedPin)); }
    catch (problem) { error = String(problem); await refresh(); }
    finally { busy = false; }
  }
  async function load() {
    busy = true; error = '';
    try { apply(await api.RetryLoad()); }
    catch (problem) { error = String(problem); await refresh(); }
    finally { busy = false; }
  }
  function edit(id, text) {
    if (locked) return;
    answers = { ...answers, [id]: text }; revision += 1;
    queue.stage(answers, revision);
  }
  async function submit() {
    if (!canSubmit) return;
    confirmingSubmit = false;
    busy = true; error = '';
    const snapshot = { ...answers }; const sequence = revision;
    try {
      if (sequence > 0) queue.stage(snapshot, sequence);
      await queue.flush();
      apply(await api.Submit(snapshot, sequence));
    } catch (problem) { error = String(problem); await refresh(); }
    finally { busy = false; }
  }
  function go(index) { selected = index; confirmingAdvance = false; confirmingSubmit = false; window.scrollTo?.({ top: 0 }); }
  async function advance() {
    if (!canSubmit || captureBlocked || !forwardOnly || lastPage) return;
    confirmingAdvance = false;
    busy = true; error = '';
    const snapshot = { ...answers }; const sequence = revision; const from = page;
    try {
      if (sequence > 0) queue.stage(snapshot, sequence);
      await queue.flush();
      apply(await api.Advance(snapshot, sequence, from));
      window.scrollTo?.({ top: 0 });
    } catch (problem) { error = String(problem); await refresh(); }
    finally { busy = false; }
  }
  async function closeWindow() { try { await api.CloseCompletedWindow(); } catch (problem) { error = String(problem); } }
  async function retry() { try { if (revision > 0 && view.phase === 'exam' && !view.submitting && !view.advancing) queue.stage(answers, revision); await api.Retry(); await refresh(); } catch (problem) { error = String(problem); } }
  async function testScreenshot() {
    if (capturing || busy || view.phase === 'complete' || view.submitting) return;
    capturing = true; captureTest = { state: 'running' };
    try {
      pendingCapture = api.TestScreenshot();
      const result = await pendingCapture;
      if (closed) return;
      if (!Number.isSafeInteger(result?.width) || !Number.isSafeInteger(result?.height) || result.width <= 0 || result.height <= 0 || result.width > 16384 || result.height > 16384 || result.width * result.height > 40000000) throw Error('The screenshot test returned an invalid result. Retry or notify the instructor.');
      captureTest = { state: 'passed', width: result.width, height: result.height };
      try { apply(await api.Snapshot()); }
      catch { captureTest = { ...captureTest, note: 'Native capture readiness could not be confirmed. Wait for a current status before continuing.' }; }
    } catch (problem) {
      if (!closed) captureTest = problem?.name === 'CancelError' ? { state: 'cancelled' } : { state: 'failed', error: String(problem) };
    } finally { pendingCapture = null; capturing = false; }
  }
</script>
<h1 class="visually-hidden">CBE Portal</h1>
<main>
  {#if error}<p role="alert">{error}</p>{/if}
  {#if bridgeError}<p role="alert">{bridgeError}</p>{/if}
  {#if answerBridgeError}<p role="alert">Local answer transfer failed. Your visible text is retained here. {answerBridgeError}</p>{/if}
  {#if view.phase === 'login'}
    <div class="login-layout">
      <section class="rules" aria-label="Participation rules">
        <div class="rules-intro">
          <span class="eyebrow">Before you start</span>
          <h2>Exam rules</h2>
        </div>
        <Card title="During the exam" level={3}>
          <ul class="rule-list">
            <li><strong>Stay in this window until you submit.</strong> If you leave it for {alarmTime}, an alarm sounds until you come back.</li>
            <li><strong>While you are away, screenshots of all your screens are taken</strong>, including anything else open on them. Only your instructor sees them, and they are deleted after grading.</li>
            <li><strong>Keep your sound on.</strong> The app turns it up during the exam and restores it afterwards.</li>
            <li>Your instructor sees whether you are connected, your computer's name and when you leave this window.</li>
            <li><strong>If you stay away from this window for {lockAway}, or the app cannot reach the server for {lockNoContact}, your exam locks.</strong> Only your instructor can unlock it, and the exam time keeps running.</li>
          </ul>
        </Card>
        <CaptureStatus capture={view.capture} admission testable {capturing} {captureTest} {busy} onTest={testScreenshot} onCancel={() => pendingCapture?.cancel()} />
        <SoundStatus sound={view.sound} />
        <label class="accept"><input type="checkbox" bind:checked={accepted} disabled={busy || capturing} required form="student-login" />I have read these rules</label>
      </section>
      <div class="login-side">
        <div class="brand">
          <img src={brandLogo} alt="Computer Based Evaluation desktop logo" width="310" height="295" />
          <span>CBE Portal</span>
        </div>
        <Card label="Login" title="Login" tone="info">
          <form id="student-login" onsubmit={login}>
            <p class="login-intro">Start or resume your exam.</p>
            <label>Server address <input bind:value={server} required disabled={busy || capturing} autocomplete="off" spellcheck="false" /></label>
            <label>Account number <input bind:value={account} required disabled={busy || capturing} autocomplete="off" spellcheck="false" placeholder="Your account number" /></label>
            <label>Exam PIN <input type="password" bind:value={pin} required disabled={busy || capturing} autocomplete="off" placeholder="Provided by your instructor" /></label>
            <p class="login-hint">Read the rules and pass the screenshot test on the left before continuing.</p>
            <button class="primary login-button" disabled={busy || capturing || !accepted || !admissionReady}>{busy ? 'Logging in…' : 'Login / resume'}</button>
          </form>
        </Card>
      </div>
    </div>
  {:else if view.phase === 'loading'}
    <div class="exam-main">
      <Card title="Session started — exam not fully loaded" tone="warning">
        <p>The server’s timer is running. Editing remains disabled until questions, saved answers and authoritative status are loaded.</p>
        <button class="primary" onclick={load} disabled={busy}>Retry loading exam</button>
      </Card>
    </div>
  {:else if examLocked}
    <h2 class="exam-title">{exam?.title || 'Exam'}</h2>
    <div class="exam-main">
      <LockScreen lock={view.locked} policy={view.monitoring?.policy} remainingSeconds={view.status?.seconds_remaining ?? 0} />
      <p class="muted">Unacknowledged text is retained only while this process stays open. Closing or crashing can lose it. Only server-acknowledged work is durable.</p>
    </div>
  {:else}
    <div class="title-row">
      <h2 class="exam-title">{exam?.title || 'Exam finished'}</h2>
      {#if calcAvailable}<button class="calc-open" bind:this={calcButton} onclick={openCalculator}>Calculator</button>{/if}
    </div>
    {#if view.phase === 'complete'}
      <Card label="Submission closed — read-only" title="Submission closed — read-only" level={3} tone="success">
        <p>Server state: {view.final?.status || view.status?.status || 'submitted'}.</p>
        {#if view.final}<p>Selected final version: {view.final.version_seq}.</p>{/if}
        <p>Displayed text is the local view, not a retrieval of the selected final answer. It may include unsaved work; expiry or proctor submission can select an earlier server version.</p>
        {#if view.capture?.pending}<p role="status" class="callout pending">A screenshot is still being uploaded ({view.capture.pending} pending). Keep this window open until it finishes; closing cancels it.</p>{/if}
        <p>Fullscreen and always-on-top have been released. You can close this window; closing cancels any pending screenshot upload.</p>
        <button class="primary" onclick={closeWindow}>Close exam window</button>
      </Card>
    {:else if view.status}
      <Card label="Server timing" title="Server timing" level={3} tone="info">
        <div class="timing">
          <p>Remaining time (last server report): <strong>{view.status.seconds_remaining} seconds</strong>.</p>
          <p>State: {view.status.status}. Server report: {view.status.server_time}.</p>
          <p>Deadline: {view.status.deadline_at}. Grace ends: {view.status.grace_until}.</p>
        </div>
        {#if view.status.status === 'grace'}<p role="alert">Grace period — continue editing and saving, or submit now. Remaining exam time is zero.</p>{/if}
      </Card>
    {/if}
    {#if view.timing_stale || bridgeError}<p role="alert">Timing stale / disconnected — no local clock is used to infer expiry.</p>{/if}
    {#if view.status_error}<p role="alert">{view.status_error}</p>{/if}
    <div class="exam-main">
        {#if view.phase === 'exam'}{#each monitoringProblems as problem}<p role="alert">{problem}</p>{/each}{/if}
        {#if view.phase === 'exam' && captureBlocked}
          <p role="alert">{view.capture.block_reason || 'Screenshot retest required: editing is paused until a screenshot test succeeds.'} The server timer continues.</p>
          <CaptureStatus capture={view.capture} testable {capturing} {captureTest} {busy} onTest={testScreenshot} onCancel={() => pendingCapture?.cancel()} />
        {/if}
        <p aria-label="Save status" role="status" class="callout" class:danger={!!view.save_error}
          class:pending={!view.save_error && (view.save_state === 'saving' || revision > view.acknowledged_revision)}>{saveLabel(view, revision)}</p>
        {#if view.unlock_note}<p role="status">{view.unlock_note}{view.added_seconds ? ` — ${addedTime(view.added_seconds)}` : ''}.</p>{/if}
        {#if view.submitting}<p role="status" class="callout pending">Submission awaiting server confirmation. Editing is locked; ambiguous failures retry the same snapshot.</p>{/if}
        {#if view.submit_error}<p role="alert">{view.submit_error}</p>{/if}
        {#if view.advancing}<p role="status" class="callout pending">Moving to the next exercise — waiting for the server to confirm. Editing is locked.</p>{/if}
        {#if view.advance_error}<p role="alert">Could not move to the next exercise. {view.advance_error}</p>{/if}
        {#if view.save_error || view.submit_error || view.advance_error || view.timing_stale || answerBridgeError}<button onclick={retry}>Retry connection / pending work</button>{/if}
        {#if exam && exercise}
          {#if exam.content.instructions}
            <details class="instructions" open>
              <summary>General instructions</summary>
              <RichText html={exam.content.instructions} />
            </details>
          {/if}
          <nav aria-label="Exercises">
            {#each exercises as item, index (item.id)}
              {@const total = exerciseSlots(item).length}
              {@const done = answeredCount(item, answers)}
              {@const closed = forwardOnly && index < page}
              <button aria-current={page === index ? 'step' : undefined} disabled={forwardOnly && index !== page} class:closed title={item.title}
                aria-label="Exercise {index + 1}: {item.title}. {closed ? 'Closed' : `${done} of ${total} answered`}" onclick={() => go(index)}>
                {index + 1}<small>{closed ? 'closed' : `${done}/${total}`}</small>
              </button>
            {/each}
          </nav>
          {#if forwardOnly}<p class="muted">Exercise {page + 1} of {exercises.length}. Once you continue to the next exercise you cannot come back to this one.</p>{/if}
          {#key exercise.id}<ExercisePage {exercise} {answers} {locked} onChange={edit} />{/key}
          <div class="pager">
            {#if !forwardOnly}
              <button disabled={page === 0} onclick={() => go(page - 1)}>← Previous exercise</button>
              <button disabled={lastPage} onclick={() => go(page + 1)}>Next exercise →</button>
            {:else if !lastPage && view.phase === 'exam'}
              <button class="primary" onclick={() => { confirmingSubmit = false; confirmingAdvance = true; }} disabled={!canSubmit || captureBlocked}>Continue to exercise {page + 2} →</button>
            {:else if view.phase === 'exam'}
              <p class="muted">This is the last exercise. Submit when you are done.</p>
            {/if}
          </div>
        {/if}
        {#if view.phase === 'exam'}<button class="submit" class:primary={!forwardOnly || lastPage} onclick={() => { confirmingAdvance = false; confirmingSubmit = true; }} disabled={!canSubmit}>Submit current answers — final</button>{/if}
        <p class="muted">Unacknowledged text is retained only while this process stays open. Closing or crashing can lose it. Only server-acknowledged work is durable.</p>
    </div>
  {/if}
  {#if view.phase === 'complete' && view.capture?.pending_reports}<p role="alert">Capture status reports are still awaiting server acknowledgment. Keep this app open; closing loses unconfirmed memory-only reports.</p>{/if}
  {#if soundError}<p role="alert">{soundError}</p>{/if}
</main>
{#if confirmingAdvance && view.phase === 'exam' && forwardOnly && !lastPage}
  <ConfirmDialog title="Leave exercise {page + 1} for good?" confirmLabel="Continue to exercise {page + 2}" cancelLabel="Stay on exercise {page + 1}"
    disabled={!canSubmit || captureBlocked} onConfirm={advance} onCancel={() => confirmingAdvance = false}>
    <p>You will not be able to see or change it again. Your answers to it are saved as they are now.</p>
  </ConfirmDialog>
{/if}
{#if confirmingSubmit && view.phase === 'exam'}
  <ConfirmDialog title="Submit your exam now?" confirmLabel="Submit my exam" cancelLabel="Keep working" disabled={!canSubmit} onConfirm={submit} onCancel={() => confirmingSubmit = false}>
    <p>This is final: you will not be able to change any answer afterwards.</p>
    <p>You have answered {answeredFields} of {totalFields} fields.</p>
  </ConfirmDialog>
{/if}
{#if calcAvailable || calcOpen}
  <Calculator open={calcOpen} angleSetting={calcAngleSetting} modes={calcModes} allowed={calcAllowed}
    disabledNames={exam?.tools?.calculator?.disabled_functions ?? []} history={calcHistory}
    note="Every calculation you make is recorded and is part of your exam." {api}
    onEvaluated={recordEvaluation} onClose={() => { calcOpen = false; calcButton?.focus(); }} />
{/if}

<style>
  /* The page keeps its top-level heading for screen readers; there is no visible title bar. */
  .visually-hidden { position: absolute; width: 1px; height: 1px; margin: -1px; padding: 0; overflow: hidden; clip-path: inset(50%); white-space: nowrap; border: 0; }
  main { max-width: 78rem; margin: 0 auto; padding: var(--space-4) clamp(var(--space-3), 3vw, var(--space-5)) var(--space-5); }
  label { display: grid; gap: var(--space-1); margin: var(--space-3) 0; }
  input { width: 100%; min-width: 0; font-weight: 400; }
  button { margin: var(--space-1) var(--space-1) var(--space-1) 0; }
  .exam-title { margin: 0 0 var(--space-3); }
  .title-row { display: flex; align-items: center; gap: var(--space-3); flex-wrap: wrap; }
  .title-row .exam-title { margin: 0; }
  .title-row .calc-open { margin: 0; }

  /* Login: rules stack on the left, brand + credentials on the right */
  .login-layout { display: grid; grid-template-columns: minmax(0, 1.3fr) minmax(19rem, 1fr); gap: var(--space-4); align-items: start; }
  .rules { display: grid; gap: var(--space-3); }
  .rules-intro h2 { margin: var(--space-1) 0 0; font-size: 1.4rem; }
  .rule-list { display: grid; gap: var(--space-2); margin: 0; padding-inline-start: 1.15rem; }
  .rule-list li::marker { color: var(--ink-2); }
  .accept { display: flex; align-items: flex-start; gap: var(--space-2); margin: 0; padding: var(--space-3) var(--space-4); background: var(--color-surface); border: 1px solid var(--ink-2); border-left: 4px solid var(--ink-2); border-radius: var(--radius-md); box-shadow: var(--shadow-card); font-weight: 600; color: var(--color-heading); }
  .accept input { width: 1.1rem; }
  .login-side { position: sticky; top: var(--space-4); display: grid; gap: var(--space-3); }
  .brand { display: flex; align-items: center; gap: var(--space-3); padding: var(--space-3) var(--space-4); background: var(--transparent); border-bottom: 1px solid var(--line); border-radius: var(--radius-md); box-shadow: none; }
  .brand img { display: block; flex: none; width: 5.5rem; height: auto; }
  .brand span { color: var(--ink); font-size: 1.75rem; font-weight: 600; letter-spacing: .01em; line-height: 1.1; overflow-wrap: anywhere; }
  #student-login label { color: var(--color-heading); font-size: var(--text-sm); font-weight: 600; }
  .login-intro { margin: 0 0 var(--space-2); color: var(--color-text-muted); }
  .login-hint { color: var(--color-text-muted); font-size: var(--text-sm); }
  .login-hint { margin-top: var(--space-3); }
  .login-button { width: 100%; margin: var(--space-1) 0 0; }

  /* Exam: editor column plus status column */
  .exam-main { display: grid; gap: var(--space-3); min-width: 0; margin-top: var(--space-3); }
  .exam-main > :global(*) { margin-top: 0; margin-bottom: 0; }
  .exam-main > button { justify-self: start; }
  .timing p { margin: var(--space-1) 0; color: var(--color-text-muted); font-size: var(--text-sm); }
  .timing p:first-child { color: var(--color-text); font-size: var(--text-base); }
  .timing p:first-child strong { font-size: 1.5rem; }
  nav { display: flex; flex-wrap: wrap; gap: var(--space-2); }
  nav button { display: inline-flex; align-items: baseline; gap: var(--space-2); margin: 0; border-radius: 999px; padding-inline: var(--space-4); font-weight: 600; }
  nav button small { font-weight: 400; font-size: var(--text-xs); opacity: .85; }
  nav button[aria-current] { background: var(--ink); border-color: var(--ink); color: var(--surface); }
  nav button[aria-current]:hover { background: var(--ink); }
  nav button[aria-current]:disabled { opacity: 1; }
  nav button.closed { text-decoration: line-through; }
  .instructions { padding: var(--space-3) var(--space-4); background: var(--color-surface); border: 1px solid var(--color-border); border-radius: var(--radius-md); }
  .instructions summary { cursor: pointer; color: var(--color-heading); font-weight: 600; }
  .instructions[open] summary { margin-bottom: var(--space-2); }
  .pager { display: flex; flex-wrap: wrap; align-items: center; gap: var(--space-2); }
  .pager > button { margin: 0; }
  .submit { justify-self: start; margin: 0; padding-inline: var(--space-5); }

  @media (max-width: 720px) {
    .login-layout { grid-template-columns: minmax(0, 1fr); gap: var(--space-3); }
    .login-side { position: static; }
    .login-hint { display: none; }
  }
</style>
