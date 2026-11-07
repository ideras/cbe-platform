<script>
  import Modal from './Modal.svelte';
  // kind: force | deleteClass, both irreversible. details carries what the proctor saw: the class (entry). Publishing, closing
  // admission and deleting an exam have their own dialogs; onConfirm receives { reason }.
  let { kind, student = null, details = null, busy, error, onCancel, onConfirm } = $props();
  let reason = $state('');
  let confirmed = $state(false);
  function plural(n, noun) { return `${n} ${noun}${n === 1 ? '' : 's'}`; }
  const titles = { force: 'Confirm force-submit', deleteClass: 'Delete class' };
  const action = $derived(kind === 'deleteClass' ? 'Delete class' : 'Confirm irreversible action');
  const ready = $derived(confirmed && !(kind === 'force' && !reason.trim()));
</script>
<Modal title={titles[kind]} titleId="confirmation-title" tone="danger" {busy} onClose={onCancel} width="38rem">
  <form onsubmit={event => { event.preventDefault(); if (ready && !busy) onConfirm({ reason }); }}>
    {#if kind === 'force'}
      <p><strong>{student.student_name} ({student.account_number})</strong></p>
      <p>Irreversible: finalize the latest committed answers and lock further editing. Unacknowledged local text may not be included. A concurrent student submission or expiry keeps its original final result.</p>
      <label>Operator reason <textarea bind:value={reason} required maxlength="1024" rows="3" disabled={busy}></textarea></label>
      <label class="check"><input type="checkbox" bind:checked={confirmed} disabled={busy} /> I confirm force-submit for this student.</label>
    {:else if kind === 'deleteClass'}
      <p>Delete <strong>{details.entry.name}</strong> ({plural(details.entry.student_count, 'student')})?</p>
      <p>The class and its roster are removed and this cannot be undone. It has no exams. Students who are in no other class and have never logged in are removed from the server; anyone who has logged in is kept, so the exam history still shows their name.</p>
      <label class="check"><input type="checkbox" bind:checked={confirmed} disabled={busy} /> I want to delete the class “{details.entry.name}”.</label>
    {/if}
    {#if error}<p role="alert">{error}</p>{/if}
    <div class="actions"><button type="button" onclick={onCancel} disabled={busy}>Cancel</button><button class="danger" type="submit" disabled={busy || !ready}>{busy ? 'Waiting for server…' : action}</button></div>
  </form>
</Modal>
<style>
  label { display: block; margin: var(--space-3) 0; font-weight: 600; color: var(--text-main); }
  .check { display: flex; align-items: flex-start; gap: var(--space-2); }
  textarea { display: block; width: 100%; margin-top: var(--space-1); font-weight: 400; }
  .actions { display: flex; flex-wrap: wrap; justify-content: flex-end; gap: var(--space-2); margin-top: var(--space-4); }
  .actions button { margin: 0; }
</style>
