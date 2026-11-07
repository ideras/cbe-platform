<script>
  import ExamToolbar from './ExamToolbar.svelte';
  import { onMount, onDestroy, untrack } from 'svelte';
  import * as api from '$student-api';
  import ExercisePage from './ExercisePage.svelte';
  import Calculator from '../../../../shared/theme/calculator/Calculator.svelte';
  import RichText from './RichText.svelte';
  import ConfirmDialog from './ConfirmDialog.svelte';
  import { spokenSpan, addedText } from './spans.js';
  import CaptureStatus from './CaptureStatus.svelte';
  import LockScreen from './LockScreen.svelte';
  import ConnectionWarning from './ConnectionWarning.svelte';
  import SoundStatus from './SoundStatus.svelte';
  import Toasts from './Toasts.svelte';
  import Card from '../../../../shared/theme/Card.svelte';
  import Icon from '../../../../shared/theme/Icon.svelte';
  import brandLogo from './assets/cbe-portal-logo.svg';
  // Inlined as a data: URL on purpose: WebKitGTK cannot play media loaded through Wails' wails:// scheme (NotSupportedError).
  import alertURL from './assets/unfocus-alert.wav?inline';
  import { UnfocusAlert, describeSoundFailure } from './unfocus-alert.js';
  import { AnswerQueue, saveLabel } from './answer-queue.js';
  import { Notifier, describeProblem, volumeNotice } from './notifier.js';
  import { answeredCount, exerciseSlots } from './exercises.js';

  let view = $state({ phase: 'login', acknowledged_revision: 0, timing_stale: true });
  let server = $state('');
  let account = $state('');
  let pin = $state('');
  let exam = $state(null);
  let answers = $state({});
  let revision = $state(0);
  let selected = $state(0);
  const scales = [0.9, 1, 1.15, 1.3, 1.5];
  let scaleIndex = $state(1);
  function changeScale(delta) { scaleIndex = Math.max(0, Math.min(scales.length - 1, scaleIndex + delta)); requestAnimationFrame(() => window.dispatchEvent(new Event('exam-text-size'))); }
  let busy = $state(false);
  let bridgeError = $state('');
  let answerBridgeError = $state('');
  let observation = 0;
  let accepted = $state(false);
  let sound;
  let closed = false;
  let capturing = $state(false);
  let captureTest = $state({ state: 'idle' });
  // The times the rules show: read from the server as soon as its address is known, with a plain fallback.
  let rules = $state(null);
  let pendingCapture = null;
  let pendingLogin = null;
  let captureBlocked = $derived(Boolean(view.capture?.required && view.capture.blocked));
  // The login runs the screenshot check first; the native side reports capture ready once it has passed.
  let checkingCapture = $derived(busy && view.phase === 'login' && Boolean(view.capture?.required) && !view.capture.ready);
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
  let incomplete = $derived(exercises.map((item, index) => ({ index, done: answeredCount(item, answers), total: exerciseSlots(item).length }))
    .filter(item => item.done < item.total && (!forwardOnly || item.index === page)));
  // Errors and warnings are toasts, fixed to the window so they are seen wherever the page is scrolled. One-off results
  // (a failed login, a volume correction) are posted when they happen; the conditions that last are listed here and synced
  // on every snapshot, which shows each once. What the student must do about them stays on the page: the save status, the
  // retry button, the screenshot retest.
  let toasts = $state([]);
  const notifier = new Notifier({ onChange: () => { toasts = notifier.visible().map(toast => ({ ...toast })); } });
  let conditions = $derived.by(() => {
    const list = [];
    const add = (key, text, kind = 'warning', sticky = false) => { if (text) list.push({ key, text, kind, sticky }); };
    const monitoring = view.monitoring;
    add('grace', view.status?.status === 'grace' && view.phase !== 'complete' && 'Time is up. You are in the grace period: save and submit now.', 'warning', true);
    add('bridge', bridgeError, 'error');
    add('answer-transfer', answerBridgeError && `Local answer transfer failed. Your visible text is retained here. ${answerBridgeError}`, 'error', true);
    if (view.phase === 'exam') {
      add('monitor-heartbeat', monitoring?.heartbeat_error);
      add('monitor-event', monitoring?.event_error);
      add('monitor-alert', monitoring?.alert_error);
      add('monitor-dropped', monitoring?.dropped_events && `Unconfirmed/dropped events: ${monitoring.dropped_events}. History may be incomplete.`);
      add('timing', (view.timing_stale || bridgeError) && 'Timing stale / disconnected — no local clock is used to infer expiry.');
      add('status', view.status_error, 'error');
      add('save', view.save_error && `Save failed — your text remains here. ${view.save_error}`, 'error', true);
      add('submit', view.submit_error, 'error', true);
      add('advance', view.advance_error && `Could not move to the next exercise. ${view.advance_error}`, 'error', true);
      add('capture-blocked', captureBlocked && `${view.capture.block_reason || 'Screenshot retest required: editing is paused until a screenshot test succeeds.'} The server timer continues.`, 'error', true);
      add('capture-error', view.capture?.error, 'error');
      add('capture-failures', view.capture?.failures && `Unconfirmed/failed screenshot incidents: ${view.capture.failures}. Instructor follow-up is required.`);
      add('capture-report', view.capture?.report_error, 'error');
    }
    add('capture-reports-complete', view.phase === 'complete' && view.capture?.pending_reports && 'Capture status reports are still awaiting server acknowledgment. Keep this app open; closing loses unconfirmed memory-only reports.');
    return list;
  });
  $effect(() => { const current = conditions; untrack(() => notifier.sync(current)); });
  // The native side numbers every outcome of a volume correction; each is shown once, however often the snapshot repeats it.
  let lastVolumeNotice = 0;
  $effect(() => {
    const notice = view.sound_notice;
    if (!notice || !(notice.sequence > lastVolumeNotice)) return;
    lastVolumeNotice = notice.sequence;
    const { text, options } = volumeNotice(notice);
    notifier.post('volume', text, options);
  });
  let lastTimeNotice = 0;
  $effect(() => {
    for (const notice of view.time_notices ?? []) {
      if (!(notice.sequence > lastTimeNotice)) continue;
      lastTimeNotice = notice.sequence;
      const time = notice.ends_at ? new Date(notice.ends_at).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }) : '';
      const text = notice.kind === 'grace' ? `Your instructor extended the grace period by ${notice.added_seconds / 60} minutes.`
        : time ? `The exam now ends at ${time}.` : 'The exam no longer has an end time.';
      notifier.post(`time-change-${notice.sequence}`, text, { kind: 'info' });
    }
  });
  let lastUnlockNote = '';
  $effect(() => {
    const note = view.unlock_note || '';
    if (note && note !== lastUnlockNote) notifier.post('unlock', `${note}${view.added_seconds ? ` — ${addedText(view.added_seconds)}` : ''}.`, { kind: 'info' });
    lastUnlockNote = note;
  });
  function fail(problem) { notifier.post('action', describeProblem(problem), { kind: 'error', sticky: true }); }
  // The student locks the exam to go and reconnect to the exam network; the native side decides whether it may be done.
  async function lockForNetwork() {
    try { apply(await api.RequestNetworkLock()); } catch (problem) { fail(problem); }
  }
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
  let alarmTime = $derived(rules?.alarm_millis ? `more than ${spokenSpan(rules.alarm_millis)}` : 'a few seconds');
  let lockAway = $derived(rules?.lock_away_millis ? spokenSpan(rules.lock_away_millis) : 'several minutes');
  let lockNoContact = $derived(rules?.lock_no_contact_millis ? spokenSpan(rules.lock_no_contact_millis) : 'several minutes');
  // How many times the student may lock the exam themselves to reconnect; an older server says nothing, and that is none.
  let networkLocks = $derived(rules?.network_lock_limit > 0 ? rules.network_lock_limit : 0);
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
      notifier.post('alert-sound', 'Audible alert could not play. Check sound output; monitoring continues.');
      api.ReportAlertFailure(sequence).catch(() => {});
    });
    const unsubscribe = api.OnFocusAlert(incident => sound.play(incident));
    refresh();
    let reading = false;
    const timer = setInterval(async () => { if (reading) return; reading = true; try { await refresh(); } finally { reading = false; } }, 500);
    return () => { clearInterval(timer); unsubscribe(); sound.close(); };
  });
  onDestroy(() => { closed = true; queue.close(); pendingCapture?.cancel(); pendingLogin?.cancel?.(); });

  async function login(event) {
    event.preventDefault(); if (!accepted || busy) return;
    // Unlock audible alerts from this click, before anything awaits (see UnfocusAlert.prime).
    sound.prime().catch(problem => { if (!closed) notifier.post('alert-sound', `${describeSoundFailure(problem)} Audible alerts may not play; monitoring continues.`); });
    busy = true; notifier.clear('action');
    const submittedPin = pin; pin = '';
    try {
      // The native login checks screen capture first, with a real screenshot, and only then asks the server for the session.
      await api.AcknowledgeRules();
      pendingLogin = api.Login(server, account, submittedPin);
      apply(await pendingLogin);
    } catch (problem) {
      fail(problem); await refresh();
      // The check failed before the server saw the PIN: keep it, so the student need not type it again.
      if (view.capture?.required && !view.capture.ready) pin = submittedPin;
    } finally { pendingLogin = null; busy = false; }
  }
  async function load() {
    busy = true; notifier.clear('action');
    try { apply(await api.RetryLoad()); }
    catch (problem) { fail(problem); await refresh(); }
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
    busy = true; notifier.clear('action');
    const snapshot = { ...answers }; const sequence = revision;
    try {
      if (sequence > 0) queue.stage(snapshot, sequence);
      await queue.flush();
      apply(await api.Submit(snapshot, sequence));
    } catch (problem) { fail(problem); await refresh(); }
    finally { busy = false; }
  }
  function go(index) { selected = index; confirmingAdvance = false; confirmingSubmit = false; window.scrollTo?.({ top: 0 }); }
  async function advance() {
    if (!canSubmit || captureBlocked || !forwardOnly || lastPage) return;
    confirmingAdvance = false;
    busy = true; notifier.clear('action');
    const snapshot = { ...answers }; const sequence = revision; const from = page;
    try {
      if (sequence > 0) queue.stage(snapshot, sequence);
      await queue.flush();
      apply(await api.Advance(snapshot, sequence, from));
      window.scrollTo?.({ top: 0 });
    } catch (problem) { fail(problem); await refresh(); }
    finally { busy = false; }
  }
  async function closeWindow() { try { await api.CloseCompletedWindow(); } catch (problem) { fail(problem); } }
  async function retry() { try { if (revision > 0 && view.phase === 'exam' && !view.submitting && !view.advancing) queue.stage(answers, revision); await api.Retry(); await refresh(); } catch (problem) { fail(problem); } }
  // What was found, in the words the student knows: a program by its name, a Remote Desktop session by where it comes from.
  const remoteNames = found => found.map(item => item.kind === 'remote_session'
    ? (item.detail ? `a Remote Desktop session from ${item.detail}` : 'a Remote Desktop session') : item.name).join(', ');
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
<!-- The screenshot retest, shown as soon as a capture has failed: on the exam page, and on the lock screen too, so that a capture
     that failed during a lock (as when another user has the screen) can be tested the moment the student is back, without
     waiting for the unlock to bring the exam page back. -->
{#snippet retest()}
  <p class="callout danger">{view.capture.block_reason || 'Screenshot retest required: editing is paused until a screenshot test succeeds.'} The server timer continues.</p>
  <CaptureStatus capture={view.capture} testable {capturing} {captureTest} {busy} onTest={testScreenshot} onCancel={() => pendingCapture?.cancel()} />
{/snippet}
<main>
  {#if view.phase !== 'login' && view.phase !== 'loading'}
    <ExamToolbar title={exam?.title || 'Exam'} position={exercises.length ? `Exercise ${page + 1} of ${exercises.length}` : ''}
      status={view.status} statusObservation={view.status_observation} ageMillis={view.status_age_millis ?? 0}
      stale={view.timing_stale || !!bridgeError} locked={examLocked} complete={view.phase === 'complete'}
      saveText={view.save_error ? 'Not saved' : view.save_state === 'saving' || revision > view.acknowledged_revision ? 'Saving…' : 'Saved'}
      {scaleIndex} onSmaller={() => changeScale(-1)} onLarger={() => changeScale(1)} calculator={calcAvailable} onCalculator={openCalculator} bind:calcButton
      onWarning={notice => notifier.post(notice.key, notice.text, { kind: 'warning' })} />
    {#if view.status && view.phase !== 'complete'}<details class="timing-details"><summary>Timing details</summary><p>State: {view.status.status}. Server report: {view.status.server_time}.</p><p>Deadline: {view.status.deadline_at}. Grace ends: {view.status.grace_until}.</p></details>{/if}
  {/if}
  {#if view.phase === 'login'}
    <div class="login-layout">
      <section class="rules" aria-label="Participation rules">
        <div class="rules-intro">
          <span class="eyebrow">Before you start</span>
          <h2>Exam rules</h2>
        </div>
        <Card title="During the exam" icon="clock" level={3}>
          <ul class="rule-list">
            <li><strong>Stay in this window until you submit.</strong> If you leave it for {alarmTime}, an alarm sounds until you come back. Switching to a text console or to another user counts as leaving it.</li>
            <li><strong>While you are away, screenshots of all your screens are taken</strong>, including anything else open on them. Only your instructor sees them, and they are deleted after grading.</li>
            <li><strong>Keep your sound on.</strong> The app turns it up during the exam and restores it afterwards.</li>
            <li>Your instructor sees whether you are connected, your computer's name and when you leave this window.</li>
            <li>The app tells your instructor if it finds that this computer is being used through Remote Desktop, or that remote-control software (such as TeamViewer or AnyDesk) is running on it. Only the name of the program is sent.</li>
            <li class="critical"><strong>If you stay away from this window for {lockAway}, or the app cannot reach the server for {lockNoContact}, your exam locks.</strong> Only your instructor can unlock it, and the exam time keeps running. If it locked because the app could not reach the server, the window also stops being full screen so you can reach your network settings, and screenshots of all your screens are taken until your instructor unlocks it.</li>
            {#if networkLocks}<li>If the app cannot reach the server, it warns you and shows how long is left. To change network, you can lock your exam yourself, up to {networkLocks} time{networkLocks === 1 ? '' : 's'}: the alarm stays quiet while it is locked, but screenshots of all your screens are taken. The window stops being full screen so you can reach your network settings, and goes back to full screen when your instructor unlocks the exam. Only your instructor can unlock it, and the exam time keeps running.</li>{/if}
          </ul>
        </Card>
        <CaptureStatus capture={view.capture} admission checking={checkingCapture} />
        <SoundStatus sound={view.sound} />
        <label class="accept"><input type="checkbox" bind:checked={accepted} disabled={busy} required form="student-login" />I have read these rules</label>
      </section>
      <div class="login-side">
        <div class="brand">
          <img src={brandLogo} alt="Computer Based Evaluation desktop logo" width="310" height="295" />
          <span>CBE Portal</span>
        </div>
        <Card label="Login" title="Login" icon="login" tone="deep">
          <form id="student-login" onsubmit={login}>
            <p class="login-intro">Start or resume your exam.</p>
            <label>Server address <input class="mono" bind:value={server} required disabled={busy} autocomplete="off" spellcheck="false" /></label>
            <label>Account number <input bind:value={account} required disabled={busy} autocomplete="off" spellcheck="false" placeholder="Your account number" /></label>
            <label>Exam PIN <input class="mono" type="password" bind:value={pin} required disabled={busy} autocomplete="off" placeholder="Provided by your instructor" /></label>
            <p class="login-hint">Read the rules first. Logging in also checks that your screen can be captured.</p>
            <button class="primary login-button" disabled={busy || !accepted}>{checkingCapture ? 'Checking screen capture…' : busy ? 'Logging in…' : 'Login / resume'}</button>
            {#if checkingCapture}
              <p class="callout pending" role="status">Checking screen capture… If your system asks for permission to record or share your screen, allow it.</p>
              <button type="button" class="login-button" onclick={() => pendingLogin?.cancel?.()}>Cancel</button>
            {/if}
          </form>
        </Card>
      </div>
    </div>
  {:else if view.phase === 'loading'}
    <div class="exam-main">
      <Card title="Session started — exam not fully loaded" icon="warning" tone="warning">
        <p>The server’s timer is running. Editing remains disabled until questions, saved answers and authoritative status are loaded.</p>
        <button class="primary" onclick={load} disabled={busy}>Retry loading exam</button>
      </Card>
    </div>
  {:else if examLocked}
    <div class="exam-main">
      <LockScreen lock={view.locked} policy={view.monitoring?.policy} status={view.status} timingStale={view.timing_stale || !!bridgeError} />
      {#if view.phase === 'exam' && captureBlocked}{@render retest()}{/if}
      <p class="muted">Unacknowledged text is retained only while this process stays open. Closing or crashing can lose it. Only server-acknowledged work is durable.</p>
    </div>
  {:else}
    {#if view.phase === 'complete'}
      <Card label="Submission closed" title="Submission closed" icon="done" level={3} tone="success">
        <p>Server state: {view.final?.status || view.status?.status || 'submitted'}.</p>
        {#if view.final}<p>Selected final version: {view.final.version_seq}.</p>{/if}
        {#if view.status?.reason === 'end_time_reached'}<p>Submitted at the exam's end time ({new Date(view.status.ends_at).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })}).</p>{/if}
        <p>Your exam is finished. Questions and answers are no longer shown.</p>
        {#if view.capture?.pending}<p role="status" class="callout pending">A screenshot is still being uploaded ({view.capture.pending} pending). Keep this window open until it finishes; closing cancels it.</p>{/if}
        <p>Fullscreen and always-on-top have been released. You can close this window; closing cancels any pending screenshot upload.</p>
        <button class="primary" onclick={closeWindow}>Close exam window</button>
      </Card>
    {/if}
    {#if view.phase === 'exam'}
    <div class="exam-main">
        {#if view.monitoring?.contact_lost}<ConnectionWarning monitoring={view.monitoring} onLock={lockForNetwork} />{/if}
        <!-- The window can be in front and still not the active one, as when the instructor unlocks the exam and the computer does
             not hand it the focus. The alarm goes on until it is, so the student is told what to do about it. -->
        {#if view.monitoring?.focus === 'unfocused'}<p role="status" class="callout warning">This window is not the active one. Click anywhere in it to continue: the alarm stops when it is active.</p>{/if}
        <!-- What the app found that shows this computer is watched or controlled from elsewhere is told to the instructor, and to the
             student, so that it is no surprise and so that a mistake (a program they did not know was running) is put right. -->
        {#if view.monitoring?.remote?.length}<p role="status" class="callout warning">Your instructor has been told that this computer is being used remotely or has remote-control software running: {remoteNames(view.monitoring.remote)}. If this is a mistake, close it and tell your instructor.</p>{/if}
        {#if view.phase === 'exam' && captureBlocked}{@render retest()}{/if}
        <p aria-label="Save status" role="status" class="callout" class:danger={!!view.save_error}
          class:pending={!view.save_error && (view.save_state === 'saving' || revision > view.acknowledged_revision)}>{saveLabel(view, revision)}</p>
        {#if view.submitting}<p role="status" class="callout pending">Submission awaiting server confirmation. Editing is locked; ambiguous failures retry the same snapshot.</p>{/if}
        {#if view.advancing}<p role="status" class="callout pending">Moving to the next exercise — waiting for the server to confirm. Editing is locked.</p>{/if}
        {#if view.save_error || view.submit_error || view.advance_error || view.timing_stale || answerBridgeError}<button onclick={retry}><Icon name="refresh" size={18} />Retry connection / pending work</button>{/if}
        <div class="exam-content" style:--exam-scale={scales[scaleIndex]}>
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
                {index + 1}{#if !closed && total > 0 && done === total}<Icon name="check" size={16} />{/if}<small>{closed ? 'closed' : `${done}/${total}`}</small>
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
        </div>
        <p class="muted">Unacknowledged text is retained only while this process stays open. Closing or crashing can lose it. Only server-acknowledged work is durable.</p>
    </div>
    {/if}
  {/if}
</main>
<Toasts {toasts} onDismiss={id => notifier.dismiss(id)} onTick={ms => notifier.tick(ms)} />
{#if confirmingAdvance && view.phase === 'exam' && forwardOnly && !lastPage}
  <ConfirmDialog icon="arrowRight" title="Leave exercise {page + 1} for good?" subtitle={exam?.title} confirmLabel="Continue to exercise {page + 2}" cancelLabel="Stay on exercise {page + 1}"
    disabled={!canSubmit || captureBlocked} onConfirm={advance} onCancel={() => confirmingAdvance = false}>
    <div class="strip">
      <div class="step"><span class="k">Leaving · exercise {page + 1} of {exercises.length}</span><span class="v">{exercise?.title ?? `Exercise ${page + 1}`}</span></div>
      <Icon name="arrowRight" size={18} />
      <div class="step next"><span class="k">Opening · exercise {page + 2} of {exercises.length}</span><span class="v">{exercises[page + 1]?.title ?? `Exercise ${page + 2}`}</span></div>
    </div>
    <div class="facts two">
      <div class="fact warn"><span class="mk"><Icon name="eyeOff" size={13} /></span><div><p class="h">Closed for good</p><p>You will not be able to see or change it again.</p></div></div>
      <div class="fact ok"><span class="mk"><Icon name="check" size={13} /></span><div><p class="h">Saved as it is</p><p>Your answers to it are saved as they are now.</p></div></div>
    </div>
  </ConfirmDialog>
{/if}
{#if confirmingSubmit && view.phase === 'exam'}
  <ConfirmDialog tone="alarm" eyebrow="Final step" icon="fileCheck" danger title="Submit your exam now?" subtitle={exam?.title} confirmLabel="Submit my exam" cancelLabel="Keep working" disabled={!canSubmit} onConfirm={submit} onCancel={() => confirmingSubmit = false}>
    <div class="fact {answeredFields < totalFields ? 'warn' : 'ok'}">
      <span class="mk"><Icon name={answeredFields < totalFields ? 'warning' : 'check'} size={13} /></span>
      <div>
        <p class="h">You have answered {answeredFields} of {totalFields} fields.</p>
        <div class="meter" aria-hidden="true"><span style:width="{totalFields ? Math.round(100 * answeredFields / totalFields) : 0}%"></span></div>
        {#if incomplete.length}
          <p class="k">Still unanswered</p>
          <ul class="pills">
            {#each incomplete.slice(0, 5) as item}<li>Exercise {item.index + 1} <b>({item.done} of {item.total})</b></li>{/each}
            {#if incomplete.length > 5}<li class="more">and {incomplete.length - 5} more</li>{/if}
          </ul>
        {/if}
      </div>
    </div>
    <p class="note alert"><Icon name="warning" size={16} /><span><strong>This is final:</strong> you will not be able to change any answer afterwards.</span></p>
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
  /* Addresses and PINs are monospace. */
  input.mono { font-family: var(--font-mono); }
  button { margin: var(--space-1) var(--space-1) var(--space-1) 0; }
  :global(html) { scroll-padding-top: calc(var(--toolbar-h, 7rem) + 1rem); }
  .exam-content { display: grid; gap: var(--space-3); min-width: 0; font-size: calc(1rem * var(--exam-scale)); --text-base: calc(1rem * var(--exam-scale)); --text-sm: calc(.875rem * var(--exam-scale)); --text-xs: calc(.75rem * var(--exam-scale)); }
  .exam-content :global(input), .exam-content :global(textarea), .exam-content :global(.cm-editor) { font-size: var(--text-sm); }
  .timing-details { margin-top: var(--space-2); color: var(--text-muted); font-size: var(--text-sm); overflow-wrap: anywhere; }
  .timing-details summary { cursor: pointer; }

  /* Login: rules stack on the left, brand + credentials on the right */
  .login-layout { display: grid; grid-template-columns: minmax(0, 1.3fr) minmax(19rem, 1fr); gap: 1.25rem; align-items: start; max-width: 72rem; margin: 0 auto; }
  .rules { display: grid; gap: .9rem; }
  .rules-intro h2 { margin: .15rem 0 0; font-size: 1.5rem; }
  /* Rules are marked with blue dots; the rule that locks the exam is the critical one, on the alert fill. */
  .rule-list { display: flex; flex-direction: column; gap: .7rem; margin: 0; padding: 0; list-style: none; }
  .rule-list li { position: relative; padding-left: 1.3rem; }
  .rule-list li::before { content: ''; position: absolute; left: .2rem; top: .55rem; width: 7px; height: 7px; border-radius: 50%; background: var(--action-primary); }
  .rule-list li.critical { padding: .6rem .75rem .6rem 1.9rem; background: var(--alert-fill); border: 1px solid var(--alert-border); border-radius: var(--radius-ctl); }
  .rule-list li.critical::before { left: .8rem; top: 1.05rem; background: var(--alert); }
  /* The acknowledgement: a warm edge until it is checked, then the success state. */
  .accept { display: flex; align-items: center; gap: .75rem; margin: 0; padding: .9rem 1rem; background: var(--bg-raised); border: 1px solid var(--border-subtle); border-left: 5px solid var(--warn); border-radius: var(--radius); box-shadow: var(--shadow-card); font-weight: 600; color: var(--text-main); cursor: pointer; transition: background-color .2s ease, border-color .2s ease, color .2s ease; }
  .accept:hover { background: var(--hover-fill); }
  .accept:has(input:checked) { border-left-color: var(--ok); background: var(--ok-fill); color: var(--ok-text); }
  .accept input { width: 1.15rem; height: 1.15rem; accent-color: var(--ok-text); }
  .login-side { position: sticky; top: var(--space-4); display: grid; gap: .9rem; }
  .brand { display: flex; align-items: center; gap: 1rem; padding: .6rem .75rem; background: var(--bg-raised); border: 1px solid var(--border-subtle); border-radius: var(--radius); box-shadow: var(--shadow-card); }
  .brand img { display: block; flex: none; width: 5.1rem; height: auto; padding: .35rem; background: var(--tile-fill); border: 1px solid var(--cyan-chip-border); border-radius: 14px; }
  .brand span { color: var(--text-main); font-size: 1.6rem; font-weight: 700; letter-spacing: -.01em; line-height: 1.1; overflow-wrap: anywhere; }
  #student-login label { margin: 0 0 var(--space-3); color: var(--text-main); font-size: var(--text-sm); font-weight: 600; }
  .login-intro { margin: 0 0 var(--space-3); color: var(--text-muted); }
  .login-hint { margin: var(--space-1) 0 var(--space-3); color: var(--text-muted); font-size: var(--text-sm); }
  .login-button { width: 100%; min-height: 2.6rem; margin: var(--space-1) 0 0; }

  /* Exam: editor column plus status column */
  .exam-main { display: grid; gap: var(--space-3); min-width: 0; margin-top: var(--space-3); }
  /* Not a dialog: it is centered by its automatic margins, and the network lock's dialog is one of the children of this grid. */
  .exam-main > :global(:not(dialog)) { margin-top: 0; margin-bottom: 0; }
  .exam-main > button { display: inline-flex; align-items: center; justify-content: center; gap: .45rem; justify-self: start; }
  nav { display: flex; flex-wrap: wrap; gap: var(--space-2); }
  nav button { display: inline-flex; align-items: baseline; gap: var(--space-2); margin: 0; border-radius: 999px; padding-inline: var(--space-4); font-weight: 600; }
  nav button small { font-family: var(--font-mono); font-weight: 400; font-size: var(--text-xs); opacity: .85; }
  /* The current exercise is the selected step: deep blue with cream text. */
  nav button[aria-current], nav button[aria-current]:hover, nav button[aria-current]:disabled { background: var(--action-hover); border-color: var(--action-hover); color: var(--bg-primary); box-shadow: var(--shadow-action); opacity: 1; }
  nav button.closed { text-decoration: line-through; }
  .instructions { padding: var(--space-3) var(--space-4); background: var(--bg-raised); border: 1px solid var(--border-subtle); border-radius: var(--radius); box-shadow: var(--shadow-card); }
  .instructions summary { cursor: pointer; color: var(--action-hover); font-weight: 600; }
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
