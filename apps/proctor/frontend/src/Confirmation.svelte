<script>
  import Modal from './Modal.svelte';
  // kind: force | purge | deleteExam | deleteClass (irreversible) or publish | unpublish. details carries what the proctor saw: the exam
  // (entry), its class (klass), and the exam published now (current) with its class (currentClass). Publishing admits the
  // students of the exam's class; onConfirm receives { reason }.
  let { kind, student = null, exam = '', details = null, busy, error, onCancel, onConfirm } = $props();
  let reason = $state('');
  let confirmed = $state(false);
  function plural(n, noun) { return `${n} ${noun}${n === 1 ? '' : 's'}`; }
  const titles = { force: 'Confirm force-submit', purge: 'Confirm screenshot purge', deleteExam: 'Delete exam', deleteClass: 'Delete class', publish: 'Publish exam', unpublish: 'Close admission' };
  const working = $derived(details?.current?.active_sessions ?? 0);
  const danger = $derived(kind === 'force' || kind === 'purge' || kind === 'deleteExam' || kind === 'deleteClass');
  const action = $derived(kind === 'deleteClass' ? 'Delete class' : kind === 'deleteExam' ? 'Delete exam' : danger ? 'Confirm irreversible action' : kind === 'publish' ? 'Publish exam' : 'Close admission');
  const ready = $derived(confirmed && !(kind === 'force' && !reason.trim()));
</script>
<Modal title={titles[kind]} titleId="confirmation-title" tone={danger ? 'danger' : working > 0 ? 'warning' : 'info'} {busy} onClose={onCancel} width="38rem">
  <form onsubmit={event => { event.preventDefault(); if (ready && !busy) onConfirm({ reason }); }}>
    {#if kind === 'force'}
      <p><strong>{student.student_name} ({student.account_number})</strong></p>
      <p>Irreversible: finalize the latest committed answers and lock further editing. Unacknowledged local text may not be included. A concurrent student submission or expiry keeps its original final result.</p>
      <label>Operator reason <textarea bind:value={reason} required maxlength="1024" rows="3" disabled={busy}></textarea></label>
      <label class="check"><input type="checkbox" bind:checked={confirmed} disabled={busy} /> I confirm force-submit for this student.</label>
    {:else if kind === 'purge'}
      <p>Exam <strong>{details?.entry?.title ?? exam}</strong> <code>{exam}</code></p>
      <p>Irreversible: permanently disable further images for this exam and delete its stored screenshots. Answers and events remain. Downloads, exports and backups require separate deletion.</p>
      <label class="check"><input type="checkbox" bind:checked={confirmed} disabled={busy} /> Grading is finalized and I confirm screenshot deletion for this exam.</label>
    {:else if kind === 'deleteClass'}
      <p>Delete <strong>{details.entry.name}</strong> ({plural(details.entry.student_count, 'student')})?</p>
      <p>The class and its roster are removed and this cannot be undone. It has no exams. Students who are in no other class and have never logged in are removed from the server; anyone who has logged in is kept, so the exam history still shows their name.</p>
      <label class="check"><input type="checkbox" bind:checked={confirmed} disabled={busy} /> I want to delete the class “{details.entry.name}”.</label>
    {:else if kind === 'deleteExam'}
      <p>Delete <strong>{details.entry.title}</strong> from <strong>{details.klass.name}</strong>?</p>
      <p>The exam is removed and this cannot be undone. Nobody has taken it, so no answers or screenshots are lost; its PIN stops working. To give this class the same exam again, import its file again.</p>
      <label class="check"><input type="checkbox" bind:checked={confirmed} disabled={busy} /> I want to delete the exam “{details.entry.title}”.</label>
    {:else if kind === 'publish'}
      <p>The <strong>{plural(details.klass.student_count, 'student')}</strong> of <strong>{details.klass.name}</strong> will log in to <strong>{details.entry.title}</strong> ({plural(details.entry.exercise_count, 'exercise')}{details.entry.navigation === 'forward_only' ? ', forward only' : ''}, {details.entry.duration_seconds / 60} min) with its PIN, starting now. Students of other classes cannot.</p>
      {#if details.admission !== 'open'}<p class="callout warning">Its login window is {details.admission} by the server’s clock, so students cannot start it yet.</p>{/if}
      {#if working > 0}
        <p class="callout warning"><strong>{plural(working, 'student')} still working in “{details.current.title}” ({details.currentClass}).</strong> They keep their sessions and can keep saving and submitting, but if their application closes or crashes they cannot log back in to it after this change.</p>
      {:else if details.current}<p>“{details.current.title}” ({details.currentClass}) stops admitting students; nobody is working in it.</p>{/if}
      <label class="check"><input type="checkbox" bind:checked={confirmed} disabled={busy} /> {working > 0 ? `I understand that ${plural(working, 'student')} still working in “${details.current.title}”, and I want to publish “${details.entry.title}”.` : `I want to publish “${details.entry.title}”.`}</label>
    {:else}
      <p>Nobody new will be able to log in. Students already in <strong>{details.current.title}</strong> keep their sessions.</p>
      {#if working > 0}<p class="callout warning">{plural(working, 'student')} still working. If their application closes or crashes they cannot log back in.</p>{/if}
      <label class="check"><input type="checkbox" bind:checked={confirmed} disabled={busy} /> {working > 0 ? `I understand that ${plural(working, 'student')} still working, and I want to close admission.` : 'I want to close admission.'}</label>
    {/if}
    {#if error}<p role="alert">{error}</p>{/if}
    <div class="actions"><button type="button" onclick={onCancel} disabled={busy}>Cancel</button><button class={danger ? 'danger' : 'primary'} type="submit" disabled={busy || !ready}>{busy ? 'Waiting for server…' : action}</button></div>
  </form>
</Modal>
<style>
  label { display: block; margin: var(--space-3) 0; font-weight: 600; color: var(--title-ink); }
  .check { display: flex; align-items: flex-start; gap: var(--space-2); }
  textarea { display: block; width: 100%; margin-top: var(--space-1); font-weight: 400; }
  .actions { display: flex; flex-wrap: wrap; justify-content: flex-end; gap: var(--space-2); margin-top: var(--space-4); }
  .actions button { margin: 0; }
</style>
