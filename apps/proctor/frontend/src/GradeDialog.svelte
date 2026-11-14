<script>
  import Modal from './Modal.svelte';
  import { gradeNote } from './workspace.js';
  // Asks before the class is graded: what will be queued, and what it means. `summary` is gradingSummary of the roster.
  let { summary, busy, error, onCancel, onConfirm } = $props();
  const plural = (n, noun) => `${n} ${noun}${n === 1 ? '' : 's'}`;
</script>
<Modal title="Grade the class" titleId="grade-title" tone="info" icon="fileCheck" subtitle="The exam's hidden tests, on each student's latest submission" {busy} onClose={onCancel} width="38rem">
  <form onsubmit={event => { event.preventDefault(); if (summary.gradable && !busy) onConfirm(); }}>
    <p><strong>{plural(summary.gradable, 'submission')}</strong> will be graded, one run for each, in a container with no network.
      {summary.waiting + summary.running ? `${plural(summary.waiting + summary.running, 'student')} already waiting or being graded ${summary.waiting + summary.running === 1 ? 'is' : 'are'} left out. ` : ''}Students who have submitted nothing are left out too.</p>
    <p>A few runs go at once, so a class takes a few minutes. You can keep working: each student's grade appears in the roster as their run ends, <strong>{gradeNote}</strong>. A student who was graded before is graded again, and the new run is kept next to the old one.</p>
    {#if error}<p role="alert">{error}</p>{/if}
    <div class="actions"><button type="button" onclick={onCancel} disabled={busy}>Cancel</button><button class="primary" type="submit" disabled={busy || !summary.gradable}>{busy ? 'Waiting for server…' : `Grade ${plural(summary.gradable, 'submission')}`}</button></div>
  </form>
</Modal>
<style>
  .actions { display: flex; flex-wrap: wrap; justify-content: flex-end; gap: var(--space-2); margin-top: var(--space-4); }
  .actions button { margin: 0; }
</style>
