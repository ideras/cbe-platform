<script>
  import { onMount } from 'svelte';
  import Icon from '../../../../../shared/theme/Icon.svelte';
  // Connection, the last save, the last snapshot the server took, and the time left. The connection is told in words and an icon,
  // never by color alone.
  let { connection = 'idle', unsaved = false, lastSave = null, lastSnapshotAt = null, deadline = null, secondsLeft = null, observed = 0, restarts = 0 } = $props();
  let now = $state(Date.now());
  onMount(() => { const timer = setInterval(() => { now = Date.now(); }, 1000); return () => clearInterval(timer); });
  const clock = value => new Date(value).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' });
  const label = $derived({ connected: 'Connected', connecting: 'Connecting…', reconnecting: 'Reconnecting…', locked: 'Locked', replaced: 'Open in another window', ended: 'Exam over', closed: 'Disconnected', idle: 'Not connected' }[connection] ?? connection);
  // The server says how long is left every few seconds; between its reports the clock runs here.
  const left = $derived(secondsLeft === null ? null : Math.max(0, secondsLeft - Math.floor((now - observed) / 1000)));
  const leftText = $derived(left === null ? '' : `${Math.floor(left / 3600)}:${String(Math.floor(left / 60) % 60).padStart(2, '0')}:${String(left % 60).padStart(2, '0')}`);
</script>
<div class="status" role="group" aria-label="Workspace status">
  <span class="item connection {connection}" role="status"><Icon name={connection === 'connected' ? 'wifi' : 'wifiOff'} size={16} />{label}</span>
  <span class="item">{#if unsaved}Unsaved changes{:else if lastSave}Saved {clock(lastSave)}{:else}Nothing to save{/if}</span>
  <span class="item">{#if lastSnapshotAt}Server copy {clock(lastSnapshotAt)}{:else}No server copy yet{/if}</span>
  {#if restarts > 0}<span class="item">Workspace restarted {restarts} time{restarts === 1 ? '' : 's'}</span>{/if}
  {#if left !== null}<span class="item time" aria-label="Time left">Time left {leftText}</span>{/if}
</div>
<style>
  .status { display: flex; flex-wrap: wrap; align-items: center; gap: .15rem 1.1rem; padding: .35rem .75rem; background: var(--bg-raised); border: 1px solid var(--border-subtle); border-radius: var(--radius-ctl); color: var(--text-muted); font-size: var(--text-xs); }
  .item { display: inline-flex; align-items: center; gap: .3rem; white-space: nowrap; }
  .connection { color: var(--ok-text); font-weight: 600; }
  .connection:not(.connected) { color: var(--warn-text); }
  .connection.locked, .connection.closed { color: var(--alert-text); }
  .time { margin-left: auto; font-family: var(--font-mono); color: var(--text-main); }
</style>
