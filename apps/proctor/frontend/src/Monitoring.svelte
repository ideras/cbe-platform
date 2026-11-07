<script>
  import { untrack } from 'svelte';
  import * as api from '$proctor-api';
  import Confirmation from './Confirmation.svelte';
  import UnlockDialog from './UnlockDialog.svelte';
  import ReleaseLoginDialog from './ReleaseLoginDialog.svelte';
  import ExtendGraceDialog from './ExtendGraceDialog.svelte';
  import EndTimeDialog from './EndTimeDialog.svelte';
  import MonitoringRow from './MonitoringRow.svelte';
  import Icon from '../../../../shared/theme/Icon.svelte';
  import { localTime } from './dates.js';
  import { rosterFilters, rosterCounts, sortRoster, filterRoster, initials, duration, spoken, examLength, lockRule } from './roster.js';
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
  let filter = $state('all');
  const monitored = $derived(view.catalog?.exams?.find(entry => entry.exam_id === view.exam_id));
  const sessions = $derived(sortRoster(view.roster?.sessions ?? []));
  const visible = $derived(filterRoster(sessions, filter));
  const counts = $derived(rosterCounts(sessions));
  const missing = $derived(view.roster?.students_without_session ?? []);
  const policy = $derived(view.roster?.policy ?? {});
  const stale = $derived(Boolean(view.stale || bridgeError));
  // The exam's first roster is still on its way from the server. Nothing is displayed yet, so nothing is out of date: the proctor is
  // told it is loading, and the stale warning waits for a failure (view.error, or a native side that does not answer) to report.
  const waiting = $derived(Boolean(view.loading && !view.loaded));
  const quietStale = $derived(waiting && !view.error && !bridgeError);
  const lengthText = $derived(monitored ? examLength(monitored.duration_seconds) : 'unknown');
  const graceText = $derived(monitored ? `${spoken(monitored.grace_seconds * 1000)}${monitored.extra_grace_seconds ? ` + ${spoken(monitored.extra_grace_seconds * 1000)} added` : ''}` : 'unknown');
  const title = $derived(monitored ? `${monitored.title} (${monitored.class_name})` : view.exam_id);

  // Announce every lock once, regardless of which table filter is selected.
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
  const examKey = $derived(`${view.server}|${view.exam_id}`);
  $effect(() => { examKey; untrack(() => { outcome = ''; reviewError = ''; filter = 'all'; }); });
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
<div class="monitoring">
  <section class="panel" aria-label="Live roster">
    <header class="roster-head">
      <div class="heading"><div class="eyebrow">Live roster</div><h1 aria-label="Live roster: {title}">{monitored?.title ?? view.exam_id}{#if monitored}<span>{` · ${monitored.class_name}`}</span>{/if}</h1></div>
      <div class="live" class:historical={stale} title="Server observation: {localTime(view.roster?.server_time, 'unavailable')}"><span class="pulse" class:running={!stale && view.loaded} aria-hidden="true"></span>{stale ? 'Last confirmed' : 'Updated'} <strong>{localTime(view.last_success, 'not yet')}</strong></div>
    </header>
    <div class="panel-body">
      {#if stale && !quietStale}<p role="alert" class="callout warning stale">STALE / UNCONFIRMED — displayed values are historical observations, not current status. {view.error} {bridgeError}</p>{/if}
      {#if waiting}<p class="callout pending" role="status">Loading the roster from the server…</p>{/if}
      {#if reviewError}<p role="alert">{reviewError}</p>{/if}
      {#if outcome}<p role="status">{outcome}</p>{/if}
      {#if view.loaded}
        <dl class="stats" aria-label="Session totals">
          <div class="stat" data-stat="total"><dt>sessions</dt><dd class="num">{counts.total}</dd></div>
          <div class="stat active" data-stat="progress" title="Active and grace sessions, including locked students"><dt>in progress</dt><dd class="num">{counts.progress}</dd></div>
          <div class="stat attention" data-stat="attention" title="Network, focus, lock or pending-finalization warnings; each student counted once"><dt>need attention</dt><dd class="num">{counts.attention}</dd></div>
          <div class="stat locked" data-stat="locked"><dt>locked</dt><dd class="num">{counts.locked}</dd></div>
          <div class="stat done" data-stat="submitted"><dt>submitted</dt><dd class="num">{counts.submitted}</dd></div>
        </dl>
        <div class="settings">
          <span class="chip length"><Icon name="clock" size={15} /><span>Duration: <b>{lengthText}</b></span></span>
          <span class="chip time"><Icon name="clock" size={15} /><span>Grace: <b>{graceText}</b></span></span>
          <span class="chip"><Icon name="clock" size={15} /><b>{monitored?.ends_at ? `Ends ${localTime(monitored.ends_at)}` : 'No exam end time'}</b></span>
          <span class="chip">Not logged in <b>{missing.length}</b></span>
          {#if monitored && (monitored.published || monitored.active_sessions > 0)}
            <div class="settings-actions">
              <button class="outline" disabled={disabled || view.busy || timingBusy} onclick={() => askTiming('end')}><Icon name="clock" size={15} />Change end time…</button>
              {#if monitored.ends_at}<button class="outline" disabled={disabled || view.busy || timingBusy} onclick={() => askTiming('clear')}>Clear end time</button>{/if}
              <button class="primary" disabled={disabled || view.busy || timingBusy} onclick={() => askTiming('grace')}><Icon name="plus" size={15} />Extend grace for everyone…</button>
            </div>
          {/if}
        </div>
        <div class="policy">
          <p><Icon name="lock" size={13} /><span>Exam rules: alarm after {spoken(policy.focus_threshold_ms)} · screenshot every {spoken(policy.capture_interval_ms)} · {lockRule(policy)}.</span></p>
          <p><Icon name="server" size={13} /><span>Server policy: heartbeat {duration(policy.heartbeat_interval_ms)} · overdue strictly beyond {duration(policy.overdue_threshold_ms)}.</span></p>
        </div>
        <div class="table-frame">
          <div class="table-tools">
            <span>Locked first, then student name; warning columns are independent. Showing {visible.length} of {counts.total}.</span>
            <div class="filters" role="group" aria-label="Filter sessions">{#each rosterFilters as option}<button class="filter" class:on={filter === option.id} aria-pressed={filter === option.id} onclick={() => filter = option.id}>{option.label}</button>{/each}</div>
          </div>
          <!-- svelte-ignore a11y_no_noninteractive_tabindex (Keyboard users need to scroll this region in WebKit.) -->
          <div class="table-scroll" role="region" aria-label="Session table — scroll horizontally for controls" tabindex="0"><table>
            <caption class="visually-hidden">All sessions — locked first, then student name; warning columns are independent</caption>
            <colgroup><col class="student-column" /><col class="network-column" /><col class="focus-column" /><col class="time-column" /><col class="control-column" /></colgroup>
            <thead><tr><th scope="col">Student</th><th scope="col">Network</th><th scope="col">Focus</th><th scope="col">Lifecycle / remaining</th><th scope="col">Review / control</th></tr></thead>
            <tbody>{#each visible as row (row.session_id)}
              <MonitoringRow {row} serverTime={view.roster.server_time} {policy} disabled={disabled || view.busy} {timingBusy} {unlockBusy} {actionBusy} {releaseBusy}
                onReview={review} onExtend={row => askTiming('grace', row)} onUnlock={askUnlock} onForce={row => ask('force', row)} onRelease={askRelease} />
            {:else}<tr class="empty-row"><td colspan="5">{sessions.length ? 'No sessions match this filter.' : 'No sessions for this exam yet.'}</td></tr>{/each}</tbody>
          </table></div>
        </div>
      {/if}
    </div>
  </section>
  {#if view.loaded}
    <section class="panel" aria-label="Students not logged in">
      <header class="missing-head"><Icon name="classes" size={18} /><h2>Students not logged in</h2><span class="count">{missing.length}</span></header>
      <div class="panel-body">
        <p class="note"><Icon name="info" size={15} /><span>No session for this exam — not classified as overdue.</span></p>
        <ul class="people" role="list" aria-label="Students without a session">{#each missing as student (student.account_number)}<li class="person"><span class="avatar" aria-hidden="true">{initials(student.student_name)}</span><b>{student.student_name}</b><span class="account">{student.account_number}</span></li>{/each}</ul>
        {#if !missing.length}<p class="note">No students without a session.</p>{/if}
      </div>
    </section>
  {/if}
</div>
{#if modal}<Confirmation kind={modal.kind} student={modal.student} busy={actionBusy} error={actionError} onCancel={() => modal = null} onConfirm={action} />{/if}
{#if unlockModal}<UnlockDialog student={unlockModal} live={sessions.find(row => row.session_id === unlockModal.session_id) ?? null} serverTime={view.roster?.server_time} exam={monitored}
  busy={unlockBusy} error={unlockError} onReview={row => api.OpenReview(row.session_id)} onCancel={() => unlockModal = null} onConfirm={unlock} />{/if}
{#if releaseModal}<ReleaseLoginDialog student={releaseModal} busy={releaseBusy} error={releaseError} onCancel={() => releaseModal = null} onConfirm={release} />{/if}
{#if timingModal?.kind === 'grace'}<ExtendGraceDialog student={timingModal.student} {sessions} exam={monitored} busy={timingBusy} error={timingError} onCancel={() => timingModal = null} onConfirm={changeTiming} />{:else if timingModal}<EndTimeDialog exam={monitored} serverTime={view.catalog?.server_time} clear={timingModal.kind === 'clear'} busy={timingBusy} error={timingError} onCancel={() => timingModal = null} onConfirm={changeTiming} />{/if}

<style>
  .monitoring { display: grid; grid-template-columns: minmax(0, 1fr); gap: 1rem; min-width: 0; }
  .panel { min-width: 0; overflow: hidden; background: var(--bg-raised); border: 1px solid var(--border-subtle); border-radius: 14px; box-shadow: var(--shadow-card); }
  .roster-head { display: flex; align-items: center; flex-wrap: wrap; gap: .75rem; padding: .9rem 1.1rem; background: var(--header-strong); color: var(--on-dark); }
  .heading { min-width: 0; }
  .eyebrow { color: var(--accent-cyan-soft); font-size: .7rem; font-weight: 700; letter-spacing: .1em; text-transform: uppercase; }
  h1 { margin: 0; color: inherit; font-size: 1.15rem; line-height: 1.25; overflow-wrap: anywhere; }
  h1 span { color: var(--side-text); font-weight: 500; }
  .live { display: inline-flex; align-items: center; flex-wrap: wrap; gap: .5rem; margin-left: auto; padding: .25rem .75rem; background: var(--side-active); border: 1px solid var(--cyan-chip-border); border-radius: 999px; color: var(--side-text); font-size: .85rem; }
  .live strong { color: var(--on-dark); font-family: var(--font-mono); }
  .pulse { width: 9px; height: 9px; border-radius: 50%; background: var(--muted-dot); }
  .pulse.running { background: var(--accent-cyan); animation: live-pulse 1.8s infinite; }
  @keyframes live-pulse { 0%, 100% { box-shadow: 0 0 0 0 var(--focus-halo-strong); } 70% { box-shadow: 0 0 0 8px var(--transparent); } }
  .panel-body { display: flex; flex-direction: column; gap: 1rem; padding: 1rem 1.1rem 1.1rem; min-width: 0; }
  .panel-body > p { margin: 0; }
  .stale { font-weight: 600; }
  .stats { display: grid; grid-template-columns: repeat(5, minmax(0, 1fr)); gap: .6rem; margin: 0; }
  .stat { display: flex; flex-direction: column; gap: .15rem; padding: .6rem .8rem; background: var(--bg-primary); border: 1px solid var(--border-subtle); border-top: 3px solid var(--muted-dot); border-radius: 10px; }
  .stat dt { order: 2; color: var(--text-muted); font-size: .78rem; }
  .num { order: 1; margin: 0; font-family: var(--font-mono); font-size: 1.5rem; font-weight: 700; line-height: 1.1; }
  .stat.active { border-top-color: var(--accent-cyan); } .active .num { color: var(--action-primary); }
  .stat.attention { border-top-color: var(--warn); } .attention .num { color: var(--warn-text); }
  .stat.locked { border-top-color: var(--alert); } .locked .num { color: var(--alert-text); }
  .stat.done { border-top-color: var(--ok); } .done .num { color: var(--ok-text); }
  .settings { display: flex; align-items: center; flex-wrap: wrap; gap: .6rem; padding: .7rem .8rem; background: var(--bg-primary); border: 1px solid var(--border-subtle); border-radius: 10px; }
  .chip { display: inline-flex; align-items: center; gap: .35rem; padding: .2rem .6rem; border-radius: 7px; background: var(--bg-secondary); color: var(--text-main); font-size: .85rem; }
  .chip b { color: inherit; } .chip.time { background: var(--warn-fill); color: var(--warn-text); } .time b, .chip.length b { font-family: var(--font-mono); }
  .settings-actions { display: flex; flex-wrap: wrap; gap: .5rem; margin-left: auto; }
  .settings-actions button { display: inline-flex; align-items: center; justify-content: center; gap: .4rem; min-height: 36px; padding: .3rem .9rem; border-radius: 8px; font-size: .9rem; }
  .outline { background: var(--bg-raised); color: var(--action-hover); border-color: var(--action-border); }
  .policy { display: flex; flex-wrap: wrap; gap: .4rem 1.2rem; color: var(--text-muted); font-size: .8rem; }
  .policy p { display: flex; align-items: flex-start; gap: .35rem; margin: 0; }
  .policy :global(svg) { margin-top: .15rem; color: var(--action-primary); }
  .table-frame { min-width: 0; overflow: hidden; background: var(--bg-raised); border: 1px solid var(--border-subtle); border-radius: 12px; }
  .table-tools { display: flex; align-items: center; flex-wrap: wrap; gap: .6rem; padding: .6rem .8rem; border-bottom: 1px solid var(--border-subtle); color: var(--text-muted); font-size: .8rem; }
  .filters { display: flex; flex-wrap: wrap; gap: .3rem; margin-left: auto; }
  .filter { min-height: 0; padding: .2rem .65rem; border-radius: 999px; background: var(--bg-primary); color: var(--text-muted); font-size: .8rem; font-weight: 400; }
  .filter.on { background: var(--selection-fill); border-color: var(--action-primary); color: var(--action-hover); font-weight: 700; }
  .table-scroll { overflow-x: auto; }
  table { width: 100%; min-width: 980px; border-collapse: collapse; table-layout: fixed; }
  .student-column { width: 19%; } .network-column { width: 24%; } .focus-column { width: 19%; } .time-column { width: 20%; } .control-column { width: 18%; }
  th { text-align: left; padding: .6rem .8rem; background: var(--bg-secondary); color: var(--action-hover); border-bottom: 1px solid var(--border-subtle); font-size: .7rem; font-weight: 700; letter-spacing: .08em; text-transform: uppercase; }
  .empty-row td { padding: 1.5rem; text-align: center; color: var(--text-muted); }
  .visually-hidden { position: absolute; width: 1px; height: 1px; margin: -1px; overflow: hidden; clip-path: inset(50%); white-space: nowrap; }
  .missing-head { display: flex; align-items: center; flex-wrap: wrap; gap: .75rem; padding: .9rem 1.1rem; background: var(--header-soft); color: var(--action-hover); border-bottom: 1px solid var(--cyan-border); }
  h2 { margin: 0; color: inherit; font-size: 1.05rem; }
  .count { padding: .05rem .55rem; border-radius: 999px; background: var(--bg-raised); border: 1px solid var(--action-border); font-family: var(--font-mono); font-size: .8rem; font-weight: 700; }
  .note { display: flex; align-items: center; gap: .5rem; margin: 0; color: var(--text-muted); font-size: .88rem; }
  .note :global(svg) { color: var(--action-primary); }
  .people { display: flex; flex-wrap: wrap; gap: .5rem; margin: 0; padding: 0; list-style: none; }
  .person { display: inline-flex; align-items: center; flex-wrap: wrap; gap: .55rem; max-width: 100%; padding: .4rem .75rem .4rem .45rem; background: var(--bg-primary); border: 1px solid var(--border-subtle); border-radius: 999px; font-size: .88rem; overflow-wrap: anywhere; }
  .avatar { display: grid; place-items: center; flex: none; width: 26px; height: 26px; border-radius: 50%; background: var(--bg-secondary); color: var(--text-main); font-size: .72rem; font-weight: 700; }
  .account { color: var(--action-hover); font-family: var(--font-mono); font-size: .85rem; }
  @media (max-width: 900px) { .stats { grid-template-columns: repeat(3, minmax(0, 1fr)); } .settings-actions { margin-left: 0; width: 100%; } .live { margin-left: 0; } }
  @media (max-width: 520px) { .stats { grid-template-columns: repeat(2, minmax(0, 1fr)); } .filters { margin-left: 0; } .settings-actions button { flex: 1 1 auto; } }
</style>
