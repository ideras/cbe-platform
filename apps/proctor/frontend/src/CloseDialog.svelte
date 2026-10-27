<script>
  import Modal from './Modal.svelte';
  // Asks before the dashboard closes, from its Close button or the window's own. Closing the dashboard changes nothing on
  // the server: the exams go on without it.
  let { onConfirm, onCancel } = $props();
</script>
<Modal title="Close the dashboard?" titleId="close-dialog-title" tone="warning" onClose={onCancel}>
  <p>All of its windows close and this computer stops monitoring. Students keep working: their exams, answers and timers live on the server, and you can open the dashboard again at any time.</p>
  <div class="actions"><button type="button" onclick={onCancel}>Keep it open</button><button type="button" class="danger" onclick={onConfirm}>Close the dashboard</button></div>
</Modal>
<style>
  p { margin: 0; }
  .actions { display: flex; flex-wrap: wrap; justify-content: flex-end; gap: var(--space-2); margin-top: var(--space-4); }
  .actions button { margin: 0; }
</style>
