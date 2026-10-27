<script>
  import { onMount } from 'svelte';
  import Modal from './Modal.svelte';
  import { localTime } from './dates.js';
  // Unlocking a student's exam. The reason is always recorded, and the time given back is the proctor's explicit choice:
  // without it no time is added.
  let { student, busy = false, error = '', onCancel, onConfirm } = $props();
  let reason = $state('');
  let giveBack = $state(false);
  // The time locked keeps growing while the dialog is open, and so does the time that would be given back.
  let now = $state(Date.now());
  onMount(() => { const timer = setInterval(() => now = Date.now(), 1000); return () => clearInterval(timer); });
  let lockedMillis = $derived(student?.locked?.since ? Math.max(0, now - new Date(student.locked.since).getTime()) : 0);
  let lockedFor = $derived.by(() => {
    const minutes = Math.max(1, Math.floor(lockedMillis / 60000));
    return minutes === 1 ? '1 minute' : `${minutes} minutes`;
  });
  let reasonText = $derived(student?.locked?.reason === 'no_contact' ? 'the app could not reach the server' : 'the student was away from the exam window');
  let newDeadline = $derived(student?.deadline_at ? localTime(new Date(new Date(student.deadline_at).getTime() + lockedMillis).toISOString(), '') : '');
  let ready = $derived(reason.trim().length > 0);
</script>

<Modal title="Unlock exam" titleId="unlock-title" tone="warning" {busy} onClose={onCancel} width="34rem">
  <form onsubmit={event => { event.preventDefault(); if (ready && !busy) onConfirm({ reason: reason.trim(), giveBack }); }}>
    <p><strong>{student?.student_name} ({student?.account_number})</strong></p>
    <p>Locked because {reasonText}, {lockedFor} ago.</p>
    <label>Reason<input type="text" bind:value={reason} required maxlength="1024" disabled={busy} /></label>
    <label class="check"><input type="checkbox" bind:checked={giveBack} disabled={busy} /> Give back the {lockedFor} the exam was locked (new deadline {newDeadline})</label>
    <p class="muted">Without this box no time is added: the deadline does not move while the exam is locked.</p>
    {#if error}<p role="alert">{error}</p>{/if}
    <div class="actions"><button type="button" onclick={onCancel} disabled={busy}>Cancel</button><button class="primary" type="submit" disabled={busy || !ready}>{busy ? 'Unlocking…' : 'Unlock'}</button></div>
  </form>
</Modal>

<style>
  label { display: block; margin: var(--space-3) 0; font-weight: 600; color: var(--title-ink); }
  .check { display: flex; align-items: flex-start; gap: var(--space-2); font-weight: 400; }
  .muted { color: var(--color-text-muted); }
  .actions { display: flex; flex-wrap: wrap; justify-content: flex-end; gap: var(--space-2); margin-top: var(--space-4); }
  .actions button { margin: 0; }
</style>
