<script>
  import { untrack } from 'svelte';
  import Modal from './Modal.svelte';
  // kind: force | deleteClass (irreversible) or unpublish. details carries what the proctor saw: the exam (entry), its class
  // (klass), and the exam published now (current). Publishing and deleting an exam have their own dialogs; onConfirm receives
  // { reason, allowActive }.
  // working is how many students are working in the published exam as the dashboard last heard it; without it the count in details,
  // taken when the dialog opened, is used.
  let { kind, student = null, details = null, working: live = null, busy, error, onCancel, onConfirm } = $props();
  let reason = $state('');
  let confirmed = $state(false);
  // Closing admission while students are still working needs its own tick, apart from the plain confirmation.
  let override = $state(false);
  function plural(n, noun) { return `${n} ${noun}${n === 1 ? '' : 's'}`; }
  const titles = { force: 'Confirm force-submit', deleteClass: 'Delete class', unpublish: 'Close admission' };
  const working = $derived(live ?? details?.current?.active_sessions ?? 0);
  // The override is given for the students working when it is ticked. If the count changes, the tick is asked for again.
  let counted = untrack(() => working);
  $effect(() => {
    const now = working;
    if (now !== counted) { counted = now; override = false; }
  });
  const danger = $derived(kind === 'force' || kind === 'deleteClass');
  const action = $derived(kind === 'deleteClass' ? 'Delete class' : danger ? 'Confirm irreversible action' : 'Close admission');
  const allowActive = $derived(kind === 'unpublish' && working > 0 && override);
  const ready = $derived(confirmed && !(kind === 'force' && !reason.trim()) && !(kind === 'unpublish' && working > 0 && !override));
</script>
<Modal title={titles[kind]} titleId="confirmation-title" tone={danger ? 'danger' : working > 0 ? 'warning' : 'info'} {busy} onClose={onCancel} width="38rem">
  <form onsubmit={event => { event.preventDefault(); if (ready && !busy) onConfirm({ reason, allowActive }); }}>
    {#if kind === 'force'}
      <p><strong>{student.student_name} ({student.account_number})</strong></p>
      <p>Irreversible: finalize the latest committed answers and lock further editing. Unacknowledged local text may not be included. A concurrent student submission or expiry keeps its original final result.</p>
      <label>Operator reason <textarea bind:value={reason} required maxlength="1024" rows="3" disabled={busy}></textarea></label>
      <label class="check"><input type="checkbox" bind:checked={confirmed} disabled={busy} /> I confirm force-submit for this student.</label>
    {:else if kind === 'deleteClass'}
      <p>Delete <strong>{details.entry.name}</strong> ({plural(details.entry.student_count, 'student')})?</p>
      <p>The class and its roster are removed and this cannot be undone. It has no exams. Students who are in no other class and have never logged in are removed from the server; anyone who has logged in is kept, so the exam history still shows their name.</p>
      <label class="check"><input type="checkbox" bind:checked={confirmed} disabled={busy} /> I want to delete the class “{details.entry.name}”.</label>
    {:else}
      <p>Nobody new will be able to log in. Students already in <strong>{details.current.title}</strong> keep their sessions.</p>
      {#if working > 0}
        <p class="callout warning">{plural(working, 'student')} still working. If their application closes or crashes they cannot log back in.</p>
        <label class="check override"><input type="checkbox" bind:checked={override} disabled={busy} /> <span><strong>Override:</strong> close admission anyway, although {plural(working, 'student')} {working === 1 ? 'is' : 'are'} still working in “{details.current.title}”.</span></label>
      {/if}
      <label class="check"><input type="checkbox" bind:checked={confirmed} disabled={busy} /> I want to close admission.</label>
    {/if}
    {#if error}<p role="alert">{error}</p>{/if}
    <div class="actions"><button type="button" onclick={onCancel} disabled={busy}>Cancel</button><button class={danger ? 'danger' : 'primary'} type="submit" disabled={busy || !ready}>{busy ? 'Waiting for server…' : action}</button></div>
  </form>
</Modal>
<style>
  label { display: block; margin: var(--space-3) 0; font-weight: 600; color: var(--text-main); }
  .check { display: flex; align-items: flex-start; gap: var(--space-2); }
  textarea { display: block; width: 100%; margin-top: var(--space-1); font-weight: 400; }
  .actions { display: flex; flex-wrap: wrap; justify-content: flex-end; gap: var(--space-2); margin-top: var(--space-4); }
  .actions button { margin: 0; }
</style>
