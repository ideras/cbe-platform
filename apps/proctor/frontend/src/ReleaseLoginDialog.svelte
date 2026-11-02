<script>
  import Modal from './Modal.svelte';
  import { localTime } from './dates.js';
  import { loginGate } from './roster.js';
  // Allowing the next login of a student's account. The account is open on one computer at a time; this lets another computer
  // take it over, and the one that has it now loses it. The reason is always recorded.
  let { student, busy = false, error = '', onCancel, onConfirm } = $props();
  let reason = $state('');
  const gate = $derived(loginGate(student));
  let ready = $derived(reason.trim().length > 0);
</script>

<Modal title="Allow a new login" titleId="release-title" tone="warning" {busy} onClose={onCancel} width="36rem">
  <form onsubmit={event => { event.preventDefault(); if (ready && !busy) onConfirm({ reason: reason.trim() }); }}>
    <p><strong>{student?.student_name} ({student?.account_number})</strong></p>
    <dl class="facts">
      <div><dt>Open now on</dt><dd>{student?.presence?.hostname || 'a computer that gave no name'} · {student?.presence?.remote_addr || 'address not reported'}</dd></div>
      {#if gate?.refusals}<div><dt>Refused</dt><dd>{gate.refusals} {gate.refusals === 1 ? 'login' : 'logins'}, the last at {localTime(gate.lastAt, 'an unknown time')}{gate.addr ? ` from ${gate.addr}` : ''}</dd></div>{/if}
    </dl>
    <p>The next login for this account will be accepted at once. The computer that has the account open now will lose it: it can no longer save, and it tells the student to call you.</p>
    <p class="muted">Use this when the student's own computer failed or was replaced. If the address above is not the student's computer, do not allow it.</p>
    <label>Reason<input type="text" bind:value={reason} required maxlength="1024" disabled={busy} /></label>
    {#if error}<p role="alert">{error}</p>{/if}
    <div class="actions"><button type="button" onclick={onCancel} disabled={busy}>Cancel</button><button class="primary" type="submit" disabled={busy || !ready}>{busy ? 'Allowing…' : 'Allow new login'}</button></div>
  </form>
</Modal>

<style>
  label { display: block; margin: var(--space-3) 0; font-weight: 600; color: var(--text-main); }
  .facts { margin: var(--space-2) 0 var(--space-3); display: grid; gap: .25rem; }
  .facts div { display: grid; grid-template-columns: 8rem minmax(0, 1fr); gap: var(--space-2); }
  dt { color: var(--text-muted); }
  dd { margin: 0; font-family: var(--font-mono); font-size: .88rem; overflow-wrap: anywhere; }
  .muted { color: var(--text-muted); }
  .actions { display: flex; flex-wrap: wrap; justify-content: flex-end; gap: var(--space-2); margin-top: var(--space-4); }
  .actions button { margin: 0; }
</style>
