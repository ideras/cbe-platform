<script>
  import Modal from './Modal.svelte';
  import EndTimeField from './EndTimeField.svelte';
  let { exam, serverTime, clear = false, busy = false, error = '', onCancel, onConfirm } = $props();
  let value = $state('');
  let confirmed = $state(false);
</script>
<Modal title={clear ? 'Clear end time' : 'Change end time'} titleId="end-time-title" {busy} onClose={onCancel}>
  <form onsubmit={event => { event.preventDefault(); if (confirmed && (clear || value)) onConfirm(clear ? '' : value); }}>
    <p>{exam.title}: changes affect running sessions and later logins. Finished submissions are never reopened.</p>
    {#if clear}<p>Restore each student's own deadline and grace end.</p>{:else}<EndTimeField bind:value {exam} {serverTime} />{/if}
    <label><input type="checkbox" bind:checked={confirmed} disabled={busy} /> I confirm this time change.</label>
    {#if error}<p role="alert">{error}</p>{/if}
    <div class="actions"><button type="button" onclick={onCancel} disabled={busy}>Cancel</button><button class="primary" disabled={busy || !confirmed || (!clear && !value)}>{busy ? 'Saving…' : clear ? 'Clear end time' : 'Change end time'}</button></div>
  </form>
</Modal>
<style>.actions { display: flex; flex-wrap: wrap; gap: var(--space-2); margin-top: var(--space-4); justify-content: flex-end; }</style>
