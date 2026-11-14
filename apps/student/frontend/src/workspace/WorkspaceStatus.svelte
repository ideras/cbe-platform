<script>
  import Icon from '../../../../../shared/theme/Icon.svelte';
  // What the student needs to know about the connection, told in the exam toolbar (where the time is) so that it takes no room from the
  // editor: the connection, the last save, and the last copy the server took. The connection is told in words and an icon, never by
  // color alone. The time left is the toolbar's own chip, not repeated here.
  let { connection = 'idle', unsaved = false, lastSave = null, lastSnapshotAt = null, restarts = 0 } = $props();
  const clock = value => new Date(value).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' });
  const label = $derived({ connected: 'Connected', connecting: 'Connecting…', reconnecting: 'Reconnecting…', locked: 'Locked', replaced: 'Open in another window', ended: 'Exam over', closed: 'Disconnected', idle: 'Not connected' }[connection] ?? connection);
  const tone = $derived(connection === 'connected' ? 'ok' : ['locked', 'closed', 'ended'].includes(connection) ? 'alarm' : 'warn');
</script>
<div class="status" role="group" aria-label="Workspace status">
  <!-- A live region, not role="status": the theme draws that one as a callout, which is not what a chip in the toolbar is. -->
  <span class="chip connection {tone}" aria-live="polite"><Icon name={connection === 'connected' ? 'wifi' : 'wifiOff'} size={16} />{label}</span>
  <span class="item" class:unsaved>{#if unsaved}Unsaved changes{:else if lastSave}Saved {clock(lastSave)}{:else}Nothing to save{/if}</span>
  <span class="item">{#if lastSnapshotAt}Server copy {clock(lastSnapshotAt)}{:else}No server copy yet{/if}</span>
  {#if restarts > 0}<span class="item">Workspace restarted {restarts} time{restarts === 1 ? '' : 's'}</span>{/if}
</div>
<style>
  .status { display: flex; flex-wrap: wrap; align-items: center; gap: .15rem .8rem; font-size: var(--text-sm); }
  .item { white-space: nowrap; }
  .chip { display: inline-flex; align-items: center; gap: .3rem; padding: .2rem .5rem; border-radius: 6px; background: var(--chip-on-dark); color: var(--on-dark); font-weight: 600; white-space: nowrap; }
  .chip.warn { background: var(--warn-fill); color: var(--warn-text); }
  .chip.alarm { background: var(--alert-fill); color: var(--alert-text); }
  .unsaved { padding: .2rem .5rem; border-radius: 6px; background: var(--warn-fill); color: var(--warn-text); font-weight: 600; }
</style>
