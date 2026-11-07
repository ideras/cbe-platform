<script>
  import Card from '../../../../shared/theme/Card.svelte';
  import ConfirmDialog from './ConfirmDialog.svelte';

  // The app cannot reach the server. It says so, says when the exam locks if that goes on, and, while the student has
  // requests left, offers to lock the exam now so they can go and reconnect to the exam network without the alarm. The
  // numbers come from the native side, which also does the locking: this component only shows them.
  let { monitoring, onLock } = $props();
  let confirming = $state(false);
  let busy = $state(false);

  // The time left in the minutes and seconds a clock shows.
  function clock(millis) {
    const total = Math.max(0, Math.ceil(millis / 1000));
    return `${Math.floor(total / 60)}:${String(total % 60).padStart(2, '0')}`;
  }
  let lockIn = $derived(monitoring.no_contact_lock_in_ms);
  let left = $derived(monitoring.network_locks_left ?? 0);

  async function lock() {
    if (busy) return;
    busy = true;
    try { await onLock(); } finally { busy = false; confirming = false; }
  }
</script>

<Card label="Connection" title="The app cannot reach the exam server" icon="warning" tone="warning" level={3}>
  <p role="status">Check that this computer is connected to the exam Wi-Fi network. Your answers stay in this window and are saved when the connection returns.</p>
  {#if lockIn != null}
    <p>If the connection is not restored, your exam locks in <strong class="time" aria-hidden="true">{clock(lockIn)}</strong><span class="sr-only">a few minutes</span>. Only your instructor can unlock it, and the exam time keeps running.</p>
  {/if}
  {#if monitoring.network_lock_available}
    <p>Has your computer connected to another network? Lock your exam first, then change the network: the alarm stays quiet while it is locked, but your screens are still captured.</p>
    <button type="button" onclick={() => confirming = true}>I need to change network</button>
  {/if}
</Card>

{#if confirming}
  <ConfirmDialog title="Lock your exam to change network?" confirmLabel="Lock my exam" cancelLabel="Keep working" disabled={busy} onConfirm={lock} onCancel={() => confirming = false}>
    <p>Your exam locks now and the alarm stays quiet while you reconnect to the exam network. The window stops being full screen, so you can reach your network settings; it goes back to full screen when your instructor unlocks the exam.</p>
    <p><strong>Only your instructor can unlock it, and the exam time keeps running while it is locked.</strong> Tell your instructor when you are connected and back in this window.</p>
    <p>While it is locked, screenshots of all your screens are taken, as when you leave the exam window. Only your instructor sees them.</p>
    <p>You can do this {left === 1 ? 'one more time' : `${left} more times`}.</p>
  </ConfirmDialog>
{/if}

<style>
  /* The time left is a monospace chip on the warm fill, as the remaining time is on the lock screen. */
  .time { display: inline-block; padding: 0 .45rem; font-family: var(--font-mono); color: var(--warn-text); background: var(--warn-fill); border: 1px solid var(--warn-border); border-radius: 5px; }
  /* What a screen reader hears instead of a clock that changes every second. */
  .sr-only { position: absolute; width: 1px; height: 1px; margin: -1px; padding: 0; overflow: hidden; clip-path: inset(50%); white-space: nowrap; border: 0; }
</style>
