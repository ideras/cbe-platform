<script>
  import Modal from './Modal.svelte';
  import { localTime } from './dates.js';
  let { student = null, sessions = [], exam, busy = false, error = '', onCancel, onConfirm } = $props();
  let minutes = $state(5);
  let reason = $state('');
  let confirmed = $state(false);
  const rows = $derived(student ? [student] : sessions);
  const running = $derived(rows.filter(row => ['active', 'grace'].includes(row.status) && !row.finalization_pending));
  const capped = $derived(exam?.ends_at && running.some(row => new Date(row.grace_until).getTime() + minutes * 60000 > new Date(exam.ends_at).getTime()));
  const newGrace = $derived(student ? new Date(Math.min(new Date(student.grace_until).getTime() + minutes * 60000, exam?.ends_at ? new Date(exam.ends_at).getTime() : Infinity)).toISOString() : null);
</script>
<Modal title={student ? 'Extend grace' : 'Extend grace for everyone'} titleId="extend-grace-title" {busy} onClose={onCancel}>
  <form onsubmit={event => { event.preventDefault(); if (confirmed && reason.trim() && minutes >= 1 && minutes <= 60) onConfirm({ minutes, reason }); }}>
    {#if student}<p><strong>{student.student_name} ({student.account_number})</strong></p>{/if}
    <p>Changes {running.length} sessions; skips {rows.length - running.length} already submitted. Submitted sessions are not reopened.</p>
    {#if !student}<p>Students who log in later also receive this extra grace.</p>{/if}
    <label>Minutes (1–60)<input type="number" min="1" max="60" step="1" required bind:value={minutes} disabled={busy} /></label>
    <label>Reason<input required maxlength="1024" bind:value={reason} disabled={busy} /></label>
    {#if newGrace}<p>New grace end: {localTime(newGrace)}</p>{/if}
    {#if capped}<p class="callout warning">The exam end time caps this extension. Move the end time to make all the added time available.</p>{/if}
    <label><input type="checkbox" bind:checked={confirmed} disabled={busy} /> I confirm this grace extension.</label>
    {#if error}<p role="alert">{error}</p>{/if}
    <div class="actions"><button type="button" onclick={onCancel} disabled={busy}>Cancel</button><button class="primary" disabled={busy || !confirmed || !reason.trim() || minutes < 1 || minutes > 60}>{busy ? 'Extending…' : 'Extend grace'}</button></div>
  </form>
</Modal>
<style>label { display: block; margin-block: var(--space-3); } .actions { display: flex; flex-wrap: wrap; gap: var(--space-2); justify-content: flex-end; margin-top: var(--space-4); }</style>
