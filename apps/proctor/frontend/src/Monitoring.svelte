<script>
  import { remainingTime } from '../../../../shared/exam/timing.js';
  import { untrack } from 'svelte';
  import * as api from '$proctor-api';
  import Confirmation from './Confirmation.svelte';
  import UnlockDialog from './UnlockDialog.svelte';
  import Panel from './Panel.svelte';
  import { localTime } from './dates.js';
  // The lock alert is inlined as a data: URL on purpose: WebKitGTK cannot play media loaded through Wails' wails:// scheme.
  import alertURL from './assets/lock-alert.wav?inline';
  // The live view of the monitored exam: every session with its network and focus state, the students who have not logged in,
  // and the confirmed force-submit. Each student's history opens in a review window of its own; screenshots are purged from
  // the exam list after grading.
  let { view, bridgeError = '', disabled = false, onRefresh } = $props();
  let modal = $state(null);
  let actionBusy = $state(false);
  let actionError = $state('');
  let outcome = $state('');
  let unlockModal = $state(null);
  let unlockBusy = $state(false);
  let unlockError = $state('');
  // The locks already announced, so the alert is played once per lock. It is not reactive state: the effect below must
  // not depend on it, or it would run again and again.
  let announced = new Set();

  const monitored = $derived(view.catalog?.exams?.find(entry => entry.exam_id === view.exam_id));
  // The locked sessions come first: they are the ones the proctor has to act on.
  const sessions = $derived.by(() => {
    const rows = view.roster?.sessions ?? [];
    return [...rows].sort((a, b) => Number(Boolean(b.locked)) - Number(Boolean(a.locked)));
  });
  const policy = $derived(view.roster?.policy ?? {});

  // A short sound, once per new lock.
  let alertSound;
  $effect(() => {
    const rows = sessions.filter(row => row.locked);
    if (!rows.length) return;
    const fresh = rows.filter(row => !announced.has(`${row.session_id}:${row.locked.since}`));
    if (!fresh.length) return;
    for (const row of fresh) announced.add(`${row.session_id}:${row.locked.since}`);
    try {
      alertSound = alertSound ?? new Audio(alertURL);
      alertSound.currentTime = 0;
      alertSound.play().catch(() => {});
    } catch { /* the alert is a courtesy; the row still shows the lock */ }
  });

  // Another exam or another server makes the last outcome stale.
  const examKey = $derived(`${view.server}|${view.exam_id}`); // a string, so the effect below runs only when it really changes
  $effect(() => { examKey; untrack(() => { outcome = ''; reviewError = ''; }); });
  let reviewError = $state('');
  async function review(row) {
    reviewError = '';
    try { await api.OpenReview(row.session_id); } catch (err) { reviewError = String(err); }
  }
  function ask(kind, student = null) { modal = { kind, student, exam: view.exam_id }; actionError = ''; }
  async function action({ reason }) {
    if (actionBusy) return; actionBusy = true; actionError = ''; outcome = '';
    try {
      const value = await api.ForceSubmit(modal.student.session_id, reason, true);
      outcome = `Server confirmed ${value.status}; designated final version #${value.final_version_id} (sequence ${value.version_seq}).`;
      modal = null; await onRefresh();
    } catch (err) { actionError = String(err); await api.Refresh().catch(() => {}); }
    finally { actionBusy = false; }
  }
  function duration(ms) { return ms == null ? 'unknown' : `${(ms / 1000).toFixed(1)}s`; }
  // The level the student's computer last reported in a heartbeat; unknown when it could not be read.
  function sound(presence) {
    const level = presence.volume_percent == null ? '' : `${presence.volume_percent}%`;
    if (presence.volume_muted) return level ? `muted (${level})` : 'muted';
    return level || 'not reported';
  }
  function canForce(row) { return row.status === 'active' || row.status === 'grace' || row.finalization_pending; }
  // A time of the exam's rules in the words the header uses: seconds below a minute, whole minutes from there.
  function spoken(ms) { return ms < 60000 ? `${Math.round(ms / 1000)} s` : `${Math.round(ms / 60000)} min`; }
  // When the exam locks, or that it never does: a lock time of 0 is off.
  function lockRule(rules) {
    const away = rules.lock_away_after_ms, contact = rules.lock_no_contact_after_ms;
    if (away && contact) return `locks after ${spoken(away)} away or ${spoken(contact)} without contact`;
    if (away) return `locks after ${spoken(away)} away`;
    if (contact) return `locks after ${spoken(contact)} without contact`;
    return 'never locks';
  }
  // How the lock is shown: the reason and how long the exam has been locked.
  function lockLabel(row) {
    const reason = row.locked.reason === 'no_contact' ? 'no contact' : `away ${spoken(policy.lock_away_after_ms)}`;
    const seconds = row.locked.since ? Math.max(0, Math.floor((Date.now() - new Date(row.locked.since).getTime()) / 1000)) : 0;
    const minutes = Math.floor(seconds / 60);
    const ago = minutes < 1 ? 'just now' : minutes === 1 ? '1 min ago' : `${minutes} min ago`;
    return { reason, ago };
  }
  function askUnlock(row) { unlockModal = row; unlockError = ''; }
  async function unlock({ reason, giveBack }) {
    if (unlockBusy) return;
    unlockBusy = true; unlockError = '';
    try {
      const value = await api.Unlock(unlockModal.session_id, reason, giveBack, true);
      outcome = `Unlocked ${unlockModal.account_number}; ${value.seconds_added ? `${value.seconds_added} seconds added` : 'no time added'}.`;
      unlockModal = null;
      await onRefresh();
    } catch (err) { unlockError = String(err); await api.Refresh().catch(() => {}); }
    finally { unlockBusy = false; }
  }
</script>
<div class="monitoring">
  <Panel label="Live roster" title="Live roster: {monitored ? `${monitored.title} (${monitored.class_name})` : view.exam_id}">
    <!-- The roster refreshes itself every couple of seconds; times are this computer's local time. -->
    <p class="summary" title="Server observation: {localTime(view.roster?.server_time, 'unavailable')}">Updated <strong>{localTime(view.last_success, 'not yet')}</strong>{#if view.loaded} · {view.roster.sessions.length} sessions · {view.roster.students_without_session.length} students not logged in{/if}</p>
    {#if view.stale || bridgeError}<p role="alert" class="callout warning stale">STALE / UNCONFIRMED — displayed values are historical observations, not current status. {view.error} {bridgeError}</p>{/if}
    {#if view.loading}<p>Loading roster…</p>{/if}
    {#if reviewError}<p role="alert">{reviewError}</p>{/if}
    {#if outcome}<p role="status">{outcome}</p>{/if}
    {#if view.loaded}
      <p class="muted">Exam rules: alarm after {spoken(policy.focus_threshold_ms)} · screenshot every {spoken(policy.capture_interval_ms)} · {lockRule(policy)}.</p>
      <p class="muted">Server policy: heartbeat {duration(policy.heartbeat_interval_ms)} · overdue strictly beyond {duration(policy.overdue_threshold_ms)}.</p>
      <div class="table-scroll"><table>
        <caption>All sessions — sorted by student name; warning columns are independent</caption>
        <thead><tr><th>Student</th><th>Network</th><th>Focus</th><th>Lifecycle / remaining</th><th>Review / control</th></tr></thead>
        <tbody>{#each sessions as row (row.session_id)}
          <tr class:network-row={row.network_warning} class:focus-row={row.focus_warning} class:locked-row={row.locked}>
            <td><strong>{row.student_name}</strong><div>{row.account_number}</div>{#if !row.roster_active}<small>Inactive roster record</small>{/if}<small>{row.presence.hostname || 'Hostname not reported'} · {row.presence.remote_addr || 'IP not reported'}</small></td>
            <td>{#if row.network_warning}<span class="badge network">⚠ Network overdue</span>{:else}<span>{row.presence.connectivity}</span>{/if}{#if row.presence.monitoring}<div>Heartbeat age: {duration(row.presence.heartbeat_age_ms)}</div><div>Missed intervals: {row.presence.missed_intervals}</div>{:else}<div>Last heartbeat before the end: {duration(row.presence.heartbeat_age_ms)} earlier</div><div>Missed intervals before the end: {row.presence.missed_intervals}</div>{/if}<div>Sound: {sound(row.presence)}</div>{#if row.presence.ever_overdue}<small>Historical overdue recorded</small>{/if}{#if !row.presence.monitoring}<small>Not live monitored</small>{/if}</td>
            <td>{#if row.focus_warning}<span class="badge focus">⚠ Sustained focus loss</span>{/if}<div>Current focus: {row.presence.focus.state}</div><div>Current unfocused: {duration(row.presence.focus.unfocused_duration_ms)}</div><small>Last known capped loss: {duration(row.last_known_unfocused_duration_ms)}</small></td>
            <td><strong>{row.status}</strong>{#if row.locked}<div><span class="badge lock">🔒 Locked · {lockLabel(row).reason}</span></div><div>Locked {lockLabel(row).ago}</div>{/if}<div>{row.status === 'grace' ? 'Grace remaining' : 'Remaining'}: {remainingTime(row, view.roster?.server_time)}</div>{#if row.finalization_pending}<span class="badge pending">Finalization pending — no confirmed final yet</span>{:else if row.final_version}<small>Final #{row.final_version.final_version_id} · sequence {row.final_version.version_seq} · {row.final_version.reason}</small>{/if}</td>
            <td><button onclick={() => review(row)}>Review {row.account_number}</button>{#if row.locked}<button disabled={disabled || unlockBusy || view.busy} onclick={() => askUnlock(row)}>Unlock {row.account_number}…</button>{:else if canForce(row)}<button class="danger" disabled={disabled || actionBusy || view.busy} onclick={() => ask('force', row)}>Force-submit {row.account_number}…</button>{/if}</td>
          </tr>
        {/each}</tbody>
      </table></div>
    {/if}
  </Panel>
  {#if view.loaded}
    <Panel title="Students not logged in" tone="info">
      <p>No session for this exam — not classified as overdue.</p>
      <ul class="roster-missing" role="list" aria-label="Students without a session">{#each view.roster.students_without_session as student (student.account_number)}<li><strong>{student.student_name}</strong> · {student.account_number}</li>{/each}</ul>
    </Panel>
  {/if}
</div>
{#if modal}<Confirmation kind={modal.kind} student={modal.student} exam={modal.exam} busy={actionBusy} error={actionError} onCancel={() => modal = null} onConfirm={action} />{/if}
{#if unlockModal}<UnlockDialog student={unlockModal} busy={unlockBusy} error={unlockError} onCancel={() => unlockModal = null} onConfirm={unlock} />{/if}

<style>
  .monitoring { display: grid; grid-template-columns: minmax(0, 1fr); gap: var(--space-4); min-width: 0; }
  .summary { margin: 0 0 var(--space-2); }
  small { display: block; margin-top: .15rem; color: var(--text-muted); }
  .stale { font-weight: 600; }
  .table-scroll { overflow-x: auto; margin-top: var(--space-3); border: 1px solid var(--border-subtle); border-radius: 10px; }
  table { width: 100%; border-collapse: collapse; min-width: 850px; background: var(--bg-raised); }
  caption { padding: var(--space-2) var(--space-3); color: var(--text-muted); font-size: var(--text-sm); text-align: left; border-bottom: 1px solid var(--border-subtle); }
  th { background: var(--bg-secondary); color: var(--text-main); font-size: .78rem; font-weight: 700; letter-spacing: .05em; text-transform: uppercase; border-bottom: 1px solid var(--border-subtle); }
  th, td { text-align: left; vertical-align: top; padding: .5rem .7rem; overflow-wrap: anywhere; }
  td { border-bottom: 1px solid var(--border-subtle); }
  tbody tr:hover { background: var(--hover-fill); }
  td button { display: block; margin: 0 0 .25rem; }
  td div, td small { color: var(--text-main); }
  td small { color: var(--text-muted); }
  tbody tr.network-row:hover { background: var(--hover-fill); }
  .focus-row td:first-child { box-shadow: inset 4px 0 0 var(--alert); }
  .badge { display: inline-block; padding: .1rem .55rem; margin-bottom: .2rem; font-size: var(--text-sm); font-weight: 700; border: 1px solid; border-radius: 999px; }
  .network { background: var(--warn-fill); color: var(--warn-text); border-color: var(--warn-border); }
  .focus { background: var(--alert-fill); color: var(--alert-text); border-color: var(--alert-border); }
  .pending { background: var(--bg-secondary); color: var(--text-main); border-color: var(--border-subtle); }
  .lock { background: var(--bg-raised); color: var(--alert-text); border-color: var(--alert); }
  tbody tr.locked-row:hover { background: var(--hover-fill); }
  tbody tr.locked-row td:first-child { box-shadow: inset 4px 0 0 var(--alert); }
  .roster-missing { display: grid; grid-template-columns: repeat(auto-fill, minmax(14rem, 1fr)); gap: var(--space-2); margin: var(--space-3) 0 0; padding: 0; list-style: none; }
  .roster-missing li { padding: .3rem .7rem; background: var(--bg-primary); border: 1px solid var(--border-subtle); border-radius: var(--radius-ctl); }
</style>
