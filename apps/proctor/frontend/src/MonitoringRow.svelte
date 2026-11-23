<script>
  import Icon from '../../../../shared/theme/Icon.svelte';
  import { remainingTime } from '../../../../shared/exam/timing.js';
  import { localTime } from './dates.js';
  import { workspaceBadge, activityText, todoText, todoNote, useText, gradeLine } from './workspace.js';
  import { inProgress, canForce, needsAttention, duration, sound, networkBadge, focusBadge, lifecycleBadge, lockLabel, lowTime, loginGate, remoteFindings, virtualMachine, appRelease } from './roster.js';

  // null: no workspace for this student yet; undefined: a normal exam.
  let { row, workspace, serverTime, policy, serverVersion = '', expanded, onToggle, disabled = false, timingBusy = false, unlockBusy = false, actionBusy = false, releaseBusy = false,
    onReview, onExtend, onUnlock, onForce, onRelease } = $props();
  const network = $derived(networkBadge(row));
  const focus = $derived(focusBadge(row));
  const life = $derived(lifecycleBadge(row, policy, serverTime));
  const gate = $derived(loginGate(row));
  const remote = $derived(remoteFindings(row));
  const vm = $derived(virtualMachine(row));
  const release = $derived(appRelease(row, serverVersion));
  const space = $derived(workspace ? workspaceBadge(workspace) : null);
  const detailsId = $derived(`monitor-details-${row.session_id}`);
</script>

<!-- The row click is a pointer shortcut for the real, keyboard-accessible disclosure button. -->
<!-- svelte-ignore a11y_click_events_have_key_events a11y_no_noninteractive_element_interactions -->
<tr data-session-id={row.session_id} class:expanded class:locked-row={!!row.locked} class:focus-row={row.focus_warning} class:attention-row={needsAttention(row)}
  onclick={event => { if (!event.target.closest('button')) onToggle(); }}>
  <td>
    <button class="student-toggle" aria-label="Details for {row.student_name} ({row.account_number})" aria-expanded={expanded} aria-controls={detailsId} onclick={onToggle}>
      <span class="chevron"><Icon name="chevron" size={14} /></span>
      <span><strong>{row.student_name}</strong><small class="account">{row.account_number}</small></span>
    </button>
    {#if release.version || release.differs}<span class="version" class:mismatch={release.differs} title={release.note || 'Application release'}>{#if release.differs}<Icon name="warning" size={12} />{/if}{release.version || 'Release not reported'}</span>{/if}
    {#if gate || remote.length || vm || !row.roster_active}
      <small class="flags">{[gate?.waiting ? `Login refused ×${gate.refusals}` : gate?.released ? 'Next login allowed' : '', remote.length && 'Remote access reported', vm && 'Virtual machine reported', !row.roster_active && 'Inactive roster record'].filter(Boolean).join(' · ')}</small>
    {/if}
  </td>
  <td>
    <span class="roster-badge {life.tone}">{life.text}</span>
    <small class="remaining" class:low={lowTime(row, serverTime)}>{row.status === 'grace' ? 'Grace remaining' : 'Remaining'}: <strong>{remainingTime(row, serverTime)}</strong></small>
    {#if row.locked}<small>Locked {lockLabel(row, policy, serverTime).ago}{#if inProgress(row)}{' · time keeps running'}{/if}</small>{/if}
    {#if row.finalization_pending}<small>Finalization pending — no confirmed final yet</small>{/if}
  </td>
  <td>
    <span class="roster-badge {network.tone}">{network.text}</span>
    <small>{row.presence.monitoring ? 'Heartbeat age' : 'Last heartbeat before the end'}: <b>{duration(row.presence.heartbeat_age_ms)}</b>{#if !row.presence.monitoring}{' earlier'}{/if}</small>
  </td>
  <td>
    <span class="roster-badge {focus.tone}">{focus.text}</span>
    <small>{row.presence.monitoring ? `Away now: ${duration(row.presence.focus.unfocused_duration_ms)}` : 'Not live monitored'}</small>
  </td>
  {#if workspace !== undefined}
    <td class="workspace">
      {#if workspace}
        <span class="roster-badge {space.tone}">{space.text}</span>
        <small>TODO: <b>{todoText(workspace)}</b> <span>({todoNote})</span></small>
        {#if workspace.todo_total > 0}<span class="mini-bar" aria-hidden="true"><span style:width={`${100 * workspace.todo_done / workspace.todo_total}%`}></span></span>{/if}
        {#if workspace.submissions}<small>{workspace.submissions} submission{workspace.submissions === 1 ? '' : 's'}{#if workspace.submitted_final}{' · final made'}{/if}</small>{:else}<small>Nothing submitted yet</small>{/if}
      {:else}<small>No workspace yet</small>{/if}
    </td>
  {/if}
  <td>
    <div class="controls">
      <button type="button" class="outline" aria-label="Review {row.account_number}" onclick={() => onReview(row)}><Icon name="preview" size={14} />Review</button>
      {#if row.locked && inProgress(row)}<button type="button" class="danger" aria-label="Unlock {row.account_number}…" disabled={disabled || unlockBusy} onclick={() => onUnlock(row)}><Icon name="lock" size={14} />Unlock…</button>{/if}
    </div>
  </td>
</tr>
<tr class="detail-row" id={detailsId} hidden={!expanded}>
  <td colspan={workspace === undefined ? 5 : 6}>
    {#if expanded}
      <div class="diagnostics">
        <div>
          <h3>Computer</h3>
          <div><b>{row.presence.hostname || 'Hostname not reported'}</b></div>
          <div>{row.presence.remote_addr || 'IP not reported'}{#if release.version}{` · app ${release.version}`}{/if}</div>
          {#if release.differs}<p class="warning-note">{release.note}</p>{/if}
          {#if gate}
            <p class="warning-note">{#if gate.released}Next login allowed {localTime(gate.releasedAt, '')} · waiting for the student
            {:else}Login refused ×{gate.refusals} · last {localTime(gate.lastAt, 'at an unknown time')}{gate.addr ? ` from ${gate.addr}` : ''}{/if}</p>
          {/if}
          {#if remote.length}<p class="warning-note">Remote access: {remote.join(' · ')}</p>{/if}
        </div>
        <div>
          <h3>Network</h3>
          <div>{row.presence.monitoring ? 'Missed intervals' : 'Missed intervals before the end'}: <b>{row.presence.missed_intervals}</b></div>
          <div>Sound: <b>{sound(row.presence)}</b></div>
          {#if row.presence.ever_overdue}<div>Historical overdue recorded</div>{/if}
          <div>{row.presence.monitoring ? 'Live monitored' : 'Not live monitored'}</div>
        </div>
        <div>
          <h3>Focus</h3>
          <div>Current focus: <b>{row.presence.focus.state}</b></div>
          <div>Current unfocused: <b>{duration(row.presence.focus.unfocused_duration_ms)}</b></div>
          <div>Last known capped loss: <b>{duration(row.last_known_unfocused_duration_ms)}</b></div>
          {#if vm}<p class="warning-note">Virtual machine: {vm}. The application cannot report activity or focus loss outside the virtual machine.</p>{/if}
        </div>
        <div>
          <h3>{workspace === undefined ? 'Session' : 'Workspace and session'}</h3>
          {#if workspace}
            <div>Student: <b>{workspace.connected ? 'connected' : 'not connected'}</b></div>
            <div>Last activity: <b>{activityText(workspace.last_activity, serverTime)}</b></div>
            <div>Container: <b>{useText(workspace)}</b></div>
            {#if workspace.last_submission_at}<div>Last submission: <b>{localTime(workspace.last_submission_at)}</b></div>{/if}
            {#if workspace.agent_restarts}<div>Workspace restarted {workspace.agent_restarts} time{workspace.agent_restarts === 1 ? '' : 's'}</div>{/if}
            {#if gradeLine(workspace.grade)}{@const grade = gradeLine(workspace.grade)}
              <div>Grade: <b>{grade.text}</b>{grade.note ? ` (${grade.note})` : ''}</div>
            {/if}
          {/if}
          <div>State: <b>{row.status}</b></div>
          {#if !row.finalization_pending && row.final_version}<div>Final #{row.final_version.final_version_id} · sequence {row.final_version.version_seq} · {row.final_version.reason}</div>{/if}
        </div>
      </div>
      <div class="controls">
        {#if inProgress(row)}<button type="button" class="outline" aria-label="Extend grace {row.account_number}…" disabled={disabled || timingBusy} onclick={() => onExtend(row)}><Icon name="clock" size={14} />Extend grace…</button>{/if}
        {#if gate?.waiting && inProgress(row)}<button type="button" class="outline" aria-label="Allow new login {row.account_number}…" disabled={disabled || releaseBusy} onclick={() => onRelease(row)}><Icon name="login" size={14} />Allow new login…</button>{/if}
        {#if !(row.locked && inProgress(row)) && canForce(row)}<button type="button" class="danger" aria-label="Force-submit {row.account_number}…" disabled={disabled || actionBusy} onclick={() => onForce(row)}>Force-submit…</button>{/if}
      </div>
    {/if}
  </td>
</tr>

<style>
  tr[data-session-id] { cursor: pointer; }
  tr[data-session-id]:hover { background: var(--hover-fill); }
  tr.expanded { background: var(--selection-fill); }
  .attention-row td:first-child { box-shadow: inset 4px 0 var(--warn); }
  .focus-row td:first-child, .locked-row td:first-child { box-shadow: inset 4px 0 var(--alert); }
  .student-toggle { display: inline-flex; align-items: center; gap: .4rem; padding: 0 .25rem 0 0; margin: 0; border: 0; border-radius: 5px; color: var(--text-main); background: var(--transparent); box-shadow: none; text-align: left; min-height: 30px; }
  .student-toggle:hover:not(:disabled) { background: var(--hover-fill); color: var(--action-hover); }
  .chevron { display: inline-flex; flex: none; color: var(--action-primary); }
  .expanded .chevron { transform: rotate(90deg); }
  .account { color: var(--action-hover); font-family: var(--font-mono); }
  .version { display: inline-flex; align-items: center; gap: .2rem; margin: .15rem 0 0 1.2rem; padding: 0 .35rem; border-radius: 5px; background: var(--bg-secondary); color: var(--text-muted); font: .72rem/1.5 var(--font-mono); }
  .version.mismatch, .warning-note { background: var(--warn-fill); color: var(--warn-text); }
  small { display: block; color: var(--text-muted); font-size: .76rem; }
  .flags { margin: .15rem 0 0 1.2rem; color: var(--warn-text); }
  .remaining strong { font-family: var(--font-mono); color: var(--text-main); }
  .remaining.low, .remaining.low strong { color: var(--alert-text); }
  .controls { display: flex; flex-wrap: wrap; gap: .35rem; }
  .controls:empty { display: none; }
  .detail-row td { background: var(--bg-primary); padding: .75rem; }
  .diagnostics { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: .65rem 1rem; color: var(--text-muted); font-size: .8rem; }
  .diagnostics + .controls:not(:empty) { margin-top: .65rem; }
  h3 { margin: 0 0 .2rem; font-size: .69rem; text-transform: uppercase; letter-spacing: .07em; color: var(--action-primary); }
  .diagnostics b { color: var(--text-main); }
  .warning-note { margin: .25rem 0 0; padding: .15rem .35rem; border-radius: 4px; }
  @media (max-width: 940px) { .diagnostics { width: min(calc(100vw - 4rem), 100%); grid-template-columns: repeat(2, minmax(0, 1fr)); } }
  @media (max-width: 560px) { .diagnostics { grid-template-columns: minmax(0, 1fr); } }
</style>
