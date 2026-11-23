<script>
  import * as api from '$proctor-api';
  import Confirmation from './Confirmation.svelte';
  import UnlockDialog from './UnlockDialog.svelte';
  import ReleaseLoginDialog from './ReleaseLoginDialog.svelte';
  import ExtendGraceDialog from './ExtendGraceDialog.svelte';
  import EndTimeDialog from './EndTimeDialog.svelte';
  import MonitoringRow from './MonitoringRow.svelte';
  import RosterGrading from './RosterGrading.svelte';
  import Tabs from './Tabs.svelte';
  import Icon from '../../../../shared/theme/Icon.svelte';
  import { todoNote, gradingSummary } from './workspace.js';
  import { localTime } from './dates.js';
  import { rosterFilters, rosterCounts, sortRoster, filterRoster, initials, duration, spoken, examLength, lockRule, inProgress, appRelease } from './roster.js';
  import './live-roster.css';
  // Inlined on purpose: WebKitGTK cannot play audio through the wails:// scheme.
  import alertURL from './assets/lock-alert.wav?inline';

  let { view, bridgeError = '', disabled = false, onRefresh } = $props();
  let modal = $state(null);
  let actionBusy = $state(false);
  let actionError = $state('');
  let outcome = $state('');
  let unlockModal = $state(null);
  let unlockBusy = $state(false);
  let unlockError = $state('');
  let releaseModal = $state(null);
  let releaseBusy = $state(false);
  let releaseError = $state('');
  let timingModal = $state(null);
  let timingBusy = $state(false);
  let timingError = $state('');
  let reviewError = $state('');
  let refreshBusy = $state(false);
  let refreshError = $state('');
  let filter = $state('all');
  let selected = $state('monitoring');
  let openRow = $state(null);
  let detailsOpen = $state(false);
  let missingOpen = $state(true);

  const monitored = $derived(view.catalog?.exams?.find(entry => entry.exam_id === view.exam_id));
  const sessions = $derived(sortRoster(view.roster?.sessions ?? []));
  const visible = $derived(filterRoster(sessions, filter));
  const counts = $derived(rosterCounts(sessions));
  const filterCounts = $derived({ all: counts.total, attention: counts.attention, progress: counts.progress, submitted: counts.submitted });
  const missing = $derived(view.roster?.students_without_session ?? []);
  const workspaces = $derived(new Map((view.workspace?.students ?? []).map(student => [student.session_id, student])));
  const hasWorkspace = $derived(Boolean(view.workspace));
  const grading = $derived(gradingSummary(view.workspace?.students));
  const activeGrades = $derived(grading.waiting + grading.running);
  const advisoryGrades = $derived(grading.done - grading.confirmed);
  const tabs = $derived([
    { id: 'monitoring', text: 'Monitoring', icon: 'classes', count: counts.total },
    { id: 'grading', text: 'Grading', icon: 'fileCheck',
      count: activeGrades ? `${activeGrades} active` : advisoryGrades ? `${advisoryGrades} advisory` : grading.confirmed ? `${grading.confirmed} confirmed` : undefined,
      tone: activeGrades ? 'blue' : advisoryGrades ? 'warn' : 'ok' },
  ]);
  $effect(() => { if (!hasWorkspace && selected !== 'monitoring') selected = 'monitoring'; });

  const policy = $derived(view.roster?.policy ?? {});
  const serverVersion = $derived(view.catalog?.server_version ?? '');
  const otherRelease = $derived(sessions.filter(row => inProgress(row) && appRelease(row, serverVersion).differs));
  const otherNames = $derived(otherRelease.slice(0, 5).map(row => row.student_name).join(', ') + (otherRelease.length > 5 ? ` and ${otherRelease.length - 5} more` : ''));
  const stale = $derived(Boolean(view.stale || bridgeError));
  const waiting = $derived(Boolean(view.loading && !view.loaded));
  const quietStale = $derived(waiting && !view.error && !bridgeError);
  const blocked = $derived(disabled || view.busy);
  const scheduled = $derived(monitored && (monitored.published || monitored.active_sessions > 0));
  const lengthText = $derived(monitored ? examLength(monitored.duration_seconds) : 'unknown');
  const graceText = $derived(monitored ? `${spoken(monitored.grace_seconds * 1000)}${monitored.extra_grace_seconds ? ` + ${spoken(monitored.extra_grace_seconds * 1000)} added` : ''}` : 'unknown');
  const title = $derived(monitored ? `${monitored.title} (${monitored.class_name})` : view.exam_id);

  // Announce every lock once, regardless of which tab or filter is selected.
  const announced = new Set();
  let alertSound;
  $effect(() => {
    const fresh = sessions.filter(row => row.locked && !announced.has(`${row.session_id}:${row.locked.since}`));
    if (!fresh.length) return;
    for (const row of fresh) announced.add(`${row.session_id}:${row.locked.since}`);
    try {
      alertSound = alertSound ?? new Audio(alertURL);
      alertSound.currentTime = 0;
      alertSound.play().catch(() => {});
    } catch { /* The row still shows the lock if the courtesy alert cannot play. */ }
  });

  async function requestRefresh() {
    if (blocked || refreshBusy) return;
    refreshBusy = true; refreshError = '';
    try { await onRefresh(); }
    catch (err) { refreshError = `Could not refresh the roster: ${String(err)}`; }
    finally { refreshBusy = false; }
  }
  async function review(row) {
    reviewError = '';
    try { await api.OpenReview(row.session_id); } catch (err) { reviewError = String(err); }
  }
  function ask(kind, student = null) { modal = { kind, student }; actionError = ''; }
  async function action({ reason }) {
    if (actionBusy) return;
    actionBusy = true; actionError = ''; outcome = '';
    try {
      const value = await api.ForceSubmit(modal.student.session_id, reason, true);
      outcome = `Server confirmed ${value.status}; designated final version #${value.final_version_id} (sequence ${value.version_seq}).`;
      modal = null; await onRefresh();
    } catch (err) { actionError = String(err); await api.Refresh().catch(() => {}); }
    finally { actionBusy = false; }
  }
  function askUnlock(row) { unlockModal = row; unlockError = ''; }
  async function unlock({ reason, giveBack }) {
    if (unlockBusy) return;
    unlockBusy = true; unlockError = '';
    try {
      const value = await api.Unlock(unlockModal.session_id, reason, giveBack, true);
      outcome = `Unlocked ${unlockModal.account_number}; ${value.seconds_added ? `${value.seconds_added} seconds added` : 'no time added'}.`;
      unlockModal = null; await onRefresh();
    } catch (err) { unlockError = String(err); await api.Refresh().catch(() => {}); }
    finally { unlockBusy = false; }
  }
  function askRelease(row) { releaseModal = row; releaseError = ''; }
  async function release({ reason }) {
    if (releaseBusy) return;
    releaseBusy = true; releaseError = '';
    try {
      await api.ReleaseLogin(releaseModal.session_id, reason, true);
      outcome = `The next login of ${releaseModal.account_number} will be accepted.`;
      releaseModal = null; await onRefresh();
    } catch (err) { releaseError = String(err); await api.Refresh().catch(() => {}); }
    finally { releaseBusy = false; }
  }
  function askTiming(kind, student = null) { timingModal = { kind, student }; timingError = ''; }
  async function changeTiming(value) {
    if (timingBusy) return;
    timingBusy = true; timingError = '';
    try {
      if (timingModal.kind === 'grace') {
        const result = timingModal.student
          ? await api.ExtendGrace(timingModal.student.session_id, value.minutes, value.reason, true)
          : await api.ExtendExamGrace(view.exam_id, value.minutes, value.reason, true);
        outcome = `Grace extended: ${result.changed} sessions changed, ${result.skipped} submitted sessions skipped.${result.capped ? ' Capped by the exam end time.' : ''}`;
      } else { await api.SetEndTime(view.exam_id, value, true); outcome = value ? 'Exam end time changed.' : 'Exam end time cleared.'; }
      timingModal = null; await onRefresh();
    } catch (err) { timingError = String(err); }
    finally { timingBusy = false; }
  }
</script>

<section class="live-roster" aria-label="Live roster">
  <header class="roster-head">
    <span class="heading-icon"><Icon name="monitor" size={21} /></span>
    <div class="heading"><div class="eyebrow">Live roster</div><h1 aria-label="Live roster: {title}">{monitored?.title ?? view.exam_id}{#if monitored}<span>{` · ${monitored.class_name}`}</span>{/if}</h1></div>
    <div class="heading-actions">
      <span class="live"><span class="pulse" class:running={!stale && view.loaded} aria-hidden="true"></span>{stale ? 'Last confirmed' : 'Updated'} <strong>{view.loaded ? localTime(view.roster?.server_time ?? view.last_success, 'not yet') : 'not yet'}</strong></span>
      <button class="header-button" disabled={blocked || refreshBusy} onclick={requestRefresh}><Icon name="refresh" size={15} />Refresh</button>
      {#if view.loaded}<button class="header-button" aria-expanded={detailsOpen} aria-controls="exam-details" onclick={() => detailsOpen = !detailsOpen}>Exam details <span class="chevron"><Icon name="chevron" size={14} /></span></button>{/if}
    </div>
  </header>

  {#if view.loaded}
    <section class="details" id="exam-details" aria-label="Exam details" hidden={!detailsOpen}>
      {#if detailsOpen}
      <div class="settings">
        <span class="chip"><Icon name="clock" size={15} /><span>Duration: <b>{lengthText}</b></span></span>
        <span class="chip time"><Icon name="clock" size={15} /><span>Grace: <b>{graceText}</b></span></span>
        <span class="chip"><Icon name="clock" size={15} /><b>{monitored?.ends_at ? `Ends ${localTime(monitored.ends_at)}` : 'No exam end time'}</b></span>
        <span class="chip">Not logged in <b>{missing.length}</b></span>
      </div>
      <div class="policy">
        <p><Icon name="lock" size={14} /><span>Exam rules: alarm after {spoken(policy.focus_threshold_ms)} · screenshot every {spoken(policy.capture_interval_ms)} · {lockRule(policy)}.</span></p>
        <p><Icon name="server" size={14} /><span>Server policy: heartbeat {duration(policy.heartbeat_interval_ms)} · overdue strictly beyond {duration(policy.overdue_threshold_ms)}{#if serverVersion}{` · release ${serverVersion}`}{/if}.</span></p>
      </div>
      {/if}
    </section>
    {#if scheduled}
      <div class="timing" role="group" aria-label="Exam timing controls">
        <span class="tool-note"><strong>Exam timing</strong>{#if !detailsOpen} · <span>{monitored.ends_at ? `Ends ${localTime(monitored.ends_at)}` : 'No exam end time'}</span> · <span>Grace: {graceText}</span>{/if}</span>
        <div class="timing-actions">
          <button class="outline" disabled={blocked || timingBusy} onclick={() => askTiming('end')}><Icon name="clock" size={15} />Change end time…</button>
          {#if monitored.ends_at}<button class="outline" disabled={blocked || timingBusy} onclick={() => askTiming('clear')}>Clear end time</button>{/if}
          <button class="primary" disabled={blocked || timingBusy} onclick={() => askTiming('grace')}><Icon name="plus" size={15} />Extend grace for everyone…</button>
        </div>
      </div>
    {/if}
  {/if}

  {#if (stale && !quietStale) || waiting || reviewError || refreshError || outcome}
    <div class="notices">
      {#if stale && !quietStale}<p role="alert" class="callout warning stale">STALE / UNCONFIRMED — displayed values are historical observations, not current status. {view.error} {bridgeError}</p>{/if}
      {#if waiting}<p class="callout pending" role="status">Loading the roster from the server…</p>{/if}
      {#if reviewError || refreshError}<p class="callout warning" role="alert">{reviewError || refreshError}</p>{/if}
      {#if outcome}<p role="status">{outcome}</p>{/if}
    </div>
  {/if}
  {#if view.loaded}
    {#if hasWorkspace}
      <Tabs id="roster" label="Roster sections" {tabs} bind:selected>
        {#snippet panel(id)}
          {#if id === 'monitoring'}{@render monitoring()}
          {:else}<RosterGrading examId={view.exam_id} server={view.server} exam={monitored?.title ?? view.exam_id} {sessions} {missing} {workspaces} summary={grading} {stale} disabled={blocked} onReview={review} {onRefresh} />{/if}
        {/snippet}
      </Tabs>
    {:else}{@render monitoring()}{/if}
  {/if}
</section>

{#snippet monitoring()}
  <div class="panel-body">
    <dl class="stats" aria-label="Session totals">
      <div class="stat" data-stat="total"><dt>sessions</dt><dd class="num">{counts.total}</dd></div>
      <div class="stat active" data-stat="progress" title="Active and grace sessions, including locked students"><dt>in progress</dt><dd class="num">{counts.progress}</dd></div>
      <div class="stat attention" data-stat="attention" title="Network, focus, lock, login, remote access, VM or finalization warnings; each student counted once"><dt>need attention</dt><dd class="num">{counts.attention}</dd></div>
      <div class="stat locked" data-stat="locked"><dt>locked</dt><dd class="num">{counts.locked}</dd></div>
      <div class="stat done" data-stat="submitted"><dt>submitted</dt><dd class="num">{counts.submitted}</dd></div>
      <div class="stat" data-stat="missing"><dt>not logged in</dt><dd class="num">{missing.length}</dd></div>
    </dl>
    {#if otherRelease.length}
      <p class="callout warning release"><Icon name="warning" size={16} /><span>{otherRelease.length === 1 ? '1 student still working runs' : `${otherRelease.length} students still working run`} another release of the application than the server ({serverVersion}): {otherNames}. Their rows say which.</span></p>
    {/if}
    <div class="tools">
      <div class="filters" role="group" aria-label="Filter sessions">
        {#each rosterFilters as { id, label }}<button class="filter" aria-pressed={filter === id} onclick={() => filter = id}>{label} <span class="filter-count">{filterCounts[id]}</span></button>{/each}
      </div>
      <span class="tool-note">Locked first, then by name · Showing {visible.length} of {counts.total}.</span>
    </div>
    <div class="table-frame">
      <!-- svelte-ignore a11y_no_noninteractive_tabindex (Keyboard users need to scroll this region in WebKit.) -->
      <div class="table-scroll" role="region" aria-label="Session table — scroll horizontally for controls" tabindex="0">
        <table class:workspace-table={hasWorkspace}>
          <caption class="visually-hidden">All sessions — locked first, then student name; warning columns are independent</caption>
          <colgroup><col class="student-column" /><col /><col /><col />{#if hasWorkspace}<col class="workspace-column" />{/if}<col class="control-column" /></colgroup>
          <thead><tr><th scope="col">Student</th><th scope="col">Lifecycle</th><th scope="col">Network</th><th scope="col">Focus</th>{#if hasWorkspace}<th scope="col">Workspace <small>· TODO is {todoNote}</small></th>{/if}<th scope="col">Review / control</th></tr></thead>
          <tbody>
            {#each visible as row (row.session_id)}
              <MonitoringRow {row} workspace={hasWorkspace ? (workspaces.get(row.session_id) ?? null) : undefined} serverTime={view.roster.server_time} {policy} {serverVersion} disabled={blocked} {timingBusy} {unlockBusy} {actionBusy} {releaseBusy}
                expanded={openRow === row.session_id} onToggle={() => openRow = openRow === row.session_id ? null : row.session_id}
                onReview={review} onExtend={row => askTiming('grace', row)} onUnlock={askUnlock} onForce={row => ask('force', row)} onRelease={askRelease} />
            {:else}<tr class="empty-row"><td colspan={hasWorkspace ? 6 : 5}>{sessions.length ? 'No sessions match this filter.' : 'No sessions for this exam yet.'}</td></tr>{/each}
          </tbody>
        </table>
      </div>
    </div>
    <section class="missing" aria-label="Students not logged in">
      <h2><button class="missing-head" aria-expanded={missingOpen} aria-controls="missing-students" onclick={() => missingOpen = !missingOpen}>
        <span class="chevron"><Icon name="chevron" size={14} /></span>Not logged in <span class="count">{missing.length}</span>
        <small>No session for this exam — not classified as overdue.</small>
      </button></h2>
      <div class="missing-body" id="missing-students" hidden={!missingOpen}>
        {#if missing.length}
          <ul class="people" role="list" aria-label="Students without a session">{#each missing as student (student.account_number)}<li class="person"><span class="avatar" aria-hidden="true">{initials(student.student_name)}</span><b>{student.student_name}</b><span class="account">{student.account_number}</span></li>{/each}</ul>
        {:else}<p class="tool-note">No students without a session.</p>{/if}
      </div>
    </section>
  </div>
{/snippet}

{#if modal}<Confirmation kind={modal.kind} student={modal.student} busy={actionBusy} error={actionError} onCancel={() => modal = null} onConfirm={action} />{/if}
{#if unlockModal}<UnlockDialog student={unlockModal} live={sessions.find(row => row.session_id === unlockModal.session_id) ?? null} serverTime={view.roster?.server_time} exam={monitored}
  busy={unlockBusy} error={unlockError} onReview={row => api.OpenReview(row.session_id)} onCancel={() => unlockModal = null} onConfirm={unlock} />{/if}
{#if releaseModal}<ReleaseLoginDialog student={releaseModal} busy={releaseBusy} error={releaseError} onCancel={() => releaseModal = null} onConfirm={release} />{/if}
{#if timingModal?.kind === 'grace'}<ExtendGraceDialog student={timingModal.student} {sessions} exam={monitored} busy={timingBusy} error={timingError} onCancel={() => timingModal = null} onConfirm={changeTiming} />{:else if timingModal}<EndTimeDialog exam={monitored} serverTime={view.catalog?.server_time} clear={timingModal.kind === 'clear'} busy={timingBusy} error={timingError} onCancel={() => timingModal = null} onConfirm={changeTiming} />{/if}

<style>
  .live-roster { min-width: 0; background: var(--bg-raised); border: 1px solid var(--border-subtle); border-radius: 14px; box-shadow: var(--shadow-card); }
  .roster-head { display: flex; align-items: center; flex-wrap: wrap; gap: .6rem .75rem; padding: .75rem 1rem; border-radius: 13px 13px 0 0; background: var(--header-strong); color: var(--on-dark); }
  .heading-icon { display: grid; place-items: center; flex: none; width: 38px; height: 38px; border-radius: 10px; background: var(--side-active); border: 1px solid var(--cyan-chip-border); color: var(--accent-cyan-soft); }
  .heading { flex: 1 1 15rem; min-width: 0; }
  .eyebrow { color: var(--accent-cyan-soft); font-size: .68rem; font-weight: 700; letter-spacing: .1em; }
  h1 { margin: 0; color: inherit; font-size: 1.1rem; line-height: 1.25; overflow-wrap: anywhere; }
  h1 span { color: var(--side-text); font-weight: 500; }
  .heading-actions { display: flex; align-items: center; flex-wrap: wrap; gap: .4rem; }
  .live, .header-button { display: inline-flex; align-items: center; gap: .4rem; margin: 0; padding: .22rem .6rem; background: var(--side-active); border: 1px solid var(--cyan-chip-border); color: var(--side-text); font-size: .8rem; }
  .live { border-radius: 999px; }
  .live strong { color: var(--on-dark); font-family: var(--font-mono); }
  .header-button { min-height: 30px; border-radius: 8px; }
  .header-button:hover:not(:disabled) { background: var(--chip-on-dark); color: var(--on-dark); }
  .header-button:focus-visible { outline-color: var(--accent-cyan-soft); }
  .chevron { display: inline-flex; }
  [aria-expanded='true'] > .chevron { transform: rotate(90deg); }
  .pulse { flex: none; width: 8px; height: 8px; border-radius: 50%; background: var(--muted-dot); }
  .pulse.running { background: var(--accent-cyan); animation: live-pulse 1.8s infinite; }
  @keyframes live-pulse { 0%, 100% { box-shadow: 0 0 0 0 var(--focus-halo-strong); } 70% { box-shadow: 0 0 0 7px var(--transparent); } }
  @media (prefers-reduced-motion: reduce) { .pulse.running { animation: none; } }
  .details, .timing { padding: .5rem 1rem; background: var(--bg-primary); border-bottom: 1px solid var(--border-subtle); }
  .settings, .policy, .timing, .timing-actions { display: flex; align-items: center; flex-wrap: wrap; gap: .35rem .55rem; }
  .policy { margin-top: .4rem; gap: .3rem 1rem; color: var(--text-muted); font-size: .78rem; }
  .policy p { display: flex; align-items: flex-start; gap: .3rem; margin: 0; }
  .policy :global(svg), .release :global(svg) { flex: none; }
  .policy :global(svg) { color: var(--action-primary); margin-top: .08rem; }
  .chip { display: inline-flex; align-items: center; gap: .3rem; padding: .15rem .5rem; border-radius: 7px; background: var(--bg-secondary); font-size: .8rem; }
  .chip b { font-family: var(--font-mono); }
  .chip.time { background: var(--warn-fill); color: var(--warn-text); }
  .timing { justify-content: space-between; }
  .timing :global(.tool-note) { overflow-wrap: anywhere; }
  .notices { display: grid; gap: .4rem; padding: .7rem 1rem; }
  .notices p { margin: 0; overflow-wrap: anywhere; }
  .release { display: flex; align-items: center; gap: .5rem; font-size: .84rem; overflow-wrap: anywhere; }
  .release::before { display: none; }
  .student-column { width: 23%; } .workspace-column { width: 19%; } .control-column { width: 115px; }
  .missing { border: 1px solid var(--border-subtle); border-radius: 10px; background: var(--bg-raised); }
  h2 { margin: 0; }
  .missing-head { display: flex; align-items: center; flex-wrap: wrap; justify-content: flex-start; gap: .35rem .5rem; width: 100%; margin: 0; padding: .4rem .7rem; border: 0; border-radius: 9px; color: var(--action-hover); background: var(--bg-primary); font-size: .88rem; text-align: left; }
  .missing-head small { color: var(--text-muted); font-size: .76rem; font-weight: 500; }
  .count { padding: 0 .4rem; border-radius: 999px; background: var(--bg-secondary); font: 700 .75rem/1.5 var(--font-mono); }
  .missing-body { padding: .55rem .7rem; border-top: 1px solid var(--border-subtle); }
  .missing-body p { margin: 0; }
  .people { display: flex; flex-wrap: wrap; gap: .4rem; margin: 0; padding: 0; list-style: none; }
  .person { display: inline-flex; align-items: center; flex-wrap: wrap; gap: .4rem; max-width: 100%; padding: .2rem .6rem .2rem .3rem; background: var(--bg-primary); border: 1px solid var(--border-subtle); border-radius: 999px; font-size: .82rem; overflow-wrap: anywhere; }
  .avatar { display: grid; place-items: center; flex: none; width: 24px; height: 24px; border-radius: 50%; background: var(--bg-secondary); font: 700 .68rem/1 var(--font-mono); }
  .account { color: var(--action-hover); font: .75rem/1 var(--font-mono); }
  @media (max-width: 560px) { .roster-head, .details, .timing, .notices { padding-inline: .7rem; } .heading { flex-basis: calc(100% - 55px); } }
</style>
