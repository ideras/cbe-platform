<script>
  import Icon from '../../../../shared/theme/Icon.svelte';
  import { remainingTime } from '../../../../shared/exam/timing.js';
  import { inProgress, canForce, needsAttention, duration, sound, networkBadge, focusBadge, lifecycleBadge, lockLabel, lowTime } from './roster.js';
  let { row, serverTime, policy, disabled = false, timingBusy = false, unlockBusy = false, actionBusy = false,
    onReview, onExtend, onUnlock, onForce } = $props();
  const network = $derived(networkBadge(row));
  const focus = $derived(focusBadge(row));
  const life = $derived(lifecycleBadge(row, policy, serverTime));
</script>
<tr data-session-id={row.session_id} class:network-row={row.network_warning} class:focus-row={row.focus_warning} class:locked-row={!!row.locked} class:attention-row={needsAttention(row)}>
  <td>
    <div class="name">{row.student_name}</div><div class="account">{row.account_number}</div>
    {#if !row.roster_active}<small>Inactive roster record</small>{/if}
    <div class="host">{row.presence.hostname || 'Hostname not reported'} · {row.presence.remote_addr || 'IP not reported'}</div>
  </td>
  <td>
    <span class="badge {network.tone}">{network.text}</span>
    <div class="kv">
      {#if row.presence.monitoring}
        <div><span class="key">Heartbeat age:{' '}</span><span class="value" class:warn-value={row.network_warning}>{duration(row.presence.heartbeat_age_ms)}</span></div>
        <div><span class="key">Missed intervals:{' '}</span><span class="value" class:warn-value={row.network_warning}>{row.presence.missed_intervals}</span></div>
      {:else}
        <div><span class="key">Last heartbeat before the end:{' '}</span><span class="value">{duration(row.presence.heartbeat_age_ms)} earlier</span></div>
        <div><span class="key">Missed intervals before the end:{' '}</span><span class="value">{row.presence.missed_intervals}</span></div>
      {/if}
      <div><span class="key">Sound:{' '}</span><span class="value">{sound(row.presence)}</span></div>
    </div>
    {#if row.presence.ever_overdue}<small>Historical overdue recorded</small>{/if}
    <small>{row.presence.monitoring ? 'Live monitored' : 'Not live monitored'}</small>
  </td>
  <td>
    <span class="badge {focus.tone}">{focus.text}</span>
    <div class="kv">
      <div><span class="key">Current focus:{' '}</span><span class="value" class:alert-value={row.focus_warning}>{row.presence.focus.state}</span></div>
      <div><span class="key">Current unfocused:{' '}</span><span class="value" class:alert-value={row.focus_warning}>{duration(row.presence.focus.unfocused_duration_ms)}</span></div>
    </div>
    <small>Last known capped loss: {duration(row.last_known_unfocused_duration_ms)}</small>
  </td>
  <td>
    <span class="badge {life.tone}">{life.text}</span>
    <div class="remaining" class:low={lowTime(row, serverTime)}><span class="key">{row.status === 'grace' ? 'Grace remaining' : 'Remaining'}:{' '}</span><strong>{remainingTime(row, serverTime)}</strong></div>
    <small>State: {row.status}</small>
    {#if row.locked}<small>Locked {lockLabel(row, policy, serverTime).ago}{#if inProgress(row)}{' · time keeps running'}{/if}</small>{/if}
    {#if row.finalization_pending}<small class="pending">Finalization pending — no confirmed final yet</small>
    {:else if row.final_version}<small>Final #{row.final_version.final_version_id} · sequence {row.final_version.version_seq} · {row.final_version.reason}</small>{/if}
  </td>
  <td>
    <div class="controls">
      <button type="button" class="outline" onclick={() => onReview(row)}><Icon name="preview" size={14} />Review {row.account_number}</button>
      {#if inProgress(row)}<button type="button" class="outline" aria-label="Extend grace {row.account_number}…" disabled={disabled || timingBusy} onclick={() => onExtend(row)}><Icon name="clock" size={14} />Extend grace…</button>{/if}
      {#if row.locked && inProgress(row)}<button type="button" class="danger" aria-label="Unlock {row.account_number}…" disabled={disabled || unlockBusy} onclick={() => onUnlock(row)}><Icon name="lock" size={14} />Unlock…</button>
      {:else if canForce(row)}<button type="button" class="danger" aria-label="Force-submit {row.account_number}…" disabled={disabled || actionBusy} onclick={() => onForce(row)}>Force-submit…</button>{/if}
    </div>
  </td>
</tr>
<style>
  td { text-align: left; vertical-align: top; padding: .65rem .8rem; border-bottom: 1px solid var(--border-subtle); font-size: .88rem; overflow-wrap: anywhere; }
  tr:last-child td { border-bottom: none; }
  tr:hover { background: var(--hover-fill); }
  .attention-row td:first-child { box-shadow: inset 4px 0 0 var(--warn); }
  .focus-row td:first-child, .locked-row td:first-child { box-shadow: inset 4px 0 0 var(--alert); }
  .name { font-weight: 700; }
  .account { color: var(--action-hover); font-family: var(--font-mono); font-size: .85rem; }
  .host { margin-top: .15rem; color: var(--text-muted); font-family: var(--font-mono); font-size: .78rem; }
  .kv { display: flex; flex-direction: column; gap: .2rem; }
  .key { color: var(--text-muted); }
  .value { color: var(--text-main); font-family: var(--font-mono); font-weight: 600; }
  .value.warn-value { color: var(--warn-text); }
  .value.alert-value { color: var(--alert-text); }
  small { display: block; margin-top: .2rem; color: var(--text-muted); font-size: .76rem; }
  .badge { display: inline-flex; align-items: center; gap: .35rem; padding: .15rem .55rem; margin-bottom: .35rem; border-radius: 999px; font-size: .75rem; font-weight: 700; letter-spacing: .04em; text-transform: uppercase; }
  .badge::before { content: ''; flex: none; width: 7px; height: 7px; border-radius: 50%; background: currentColor; }
  .ok { background: var(--ok-fill); color: var(--ok-text); }
  .active { background: var(--selection-fill); color: var(--action-hover); }
  .network { background: var(--warn-fill); color: var(--warn-text); }
  .focus, .lock { background: var(--alert-fill); color: var(--alert-text); }
  .neutral { background: var(--bg-secondary); color: var(--text-main); }
  .remaining { margin-top: .1rem; font-family: var(--font-mono); font-size: 1rem; }
  .remaining .key { font-size: .76rem; }
  .remaining strong { color: inherit; font-weight: 700; }
  .remaining.low { color: var(--alert-text); }
  .controls { display: flex; flex-direction: column; align-items: flex-start; gap: .35rem; }
  button { display: inline-flex; align-items: center; justify-content: flex-start; gap: .4rem; min-height: 30px; margin: 0; padding: .2rem .7rem; border-radius: 8px; font-size: .82rem; }
  .outline { background: var(--bg-raised); border-color: var(--action-border); color: var(--action-hover); }
</style>
