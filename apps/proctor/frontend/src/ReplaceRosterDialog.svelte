<script>
  import * as api from '$proctor-api';
  import Modal from './Modal.svelte';
  import Icon from './Icon.svelte';
  import { message } from './errors.js';
  // Replacing a roster means a new file takes the place of the whole list. The server compares the file with the class first;
  // students who are not in it lose their place in the class, and nothing changes until the proctor confirms.
  let { entry, onClose, onReplaced } = $props();
  let preview = $state(null);
  let confirmed = $state(false);
  let choosing = $state(false);
  let busy = $state(false);
  let problem = $state('');
  const removed = $derived(preview?.removed ?? 0);
  const ready = $derived(!!preview?.valid && (removed === 0 || confirmed));
  function plural(n, noun) { return `${n} ${noun}${n === 1 ? '' : 's'}`; }

  async function choose() {
    choosing = true; problem = ''; confirmed = false;
    try { const result = await api.ChooseRosterFile(entry.class_id); if (!result.cancelled) preview = result; }
    catch (err) { problem = message(err); }
    finally { choosing = false; }
  }
  async function close() { if (preview) await api.DiscardRosterFile().catch(() => {}); onClose(); }
  async function replace() {
    if (!ready || busy) return;
    busy = true; problem = '';
    try { onReplaced(await api.ReplaceClassRoster(entry.class_id, true)); }
    catch (err) { problem = message(err); }
    finally { busy = false; }
  }
</script>
<Modal title="Replace roster" titleId="replace-roster-title" tone={removed > 0 ? 'warning' : 'info'} busy={busy || choosing} onClose={close}>
  <form onsubmit={event => { event.preventDefault(); replace(); }}>
    <p class="lead">The students of <strong>{entry.name}</strong> become exactly the students in the file you choose.</p>
    <div class="label" id="replace-file-label">New roster file</div>
    <div class="file" role="group" aria-labelledby="replace-file-label">
      <span class:chosen={preview}>{preview ? preview.file : 'No file chosen (CSV with canvas_id, account_number, name, email, sis_user_id)'}</span>
      <button type="button" disabled={busy || choosing} onclick={choose}><Icon name="folder" size={18} />{preview ? 'Choose another…' : 'Browse…'}</button>
    </div>

    {#if preview?.valid}
      <p><strong>{plural(preview.students, 'student')}</strong> in the file: <strong>{preview.added}</strong> new to this class, <strong>{removed}</strong> in the class now but not in the file.</p>
      {#if removed > 0}
        <p class="callout warning"><strong>{plural(removed, 'student')}</strong> will no longer be able to log in through this class. Sessions they already have keep working.{#if preview.admitting} <strong>This class is the one admitted to the published exam right now</strong>, so they cannot log in or resume it.{/if}</p>
      {/if}
      {#if preview.warning_count > 0}
        <details><summary>{plural(preview.warning_count, 'warning')}</summary>
          <ul>{#each preview.warnings as item}<li>Row {item.row}{item.account_number ? ` (${item.account_number})` : ''}: {item.message}</li>{/each}</ul>
          {#if preview.warning_count > preview.warnings.length}<p class="muted">And {preview.warning_count - preview.warnings.length} more.</p>{/if}
        </details>
      {/if}
      <p class="muted">The server checked this file. Nothing changes until you replace the roster.</p>
      {#if removed > 0}
        <label class="check"><input type="checkbox" bind:checked={confirmed} disabled={busy} /> I understand that {plural(removed, 'student')} will no longer be able to log in through this class.</label>
      {/if}
    {:else if preview}
      <p role="alert">This file cannot be used. Fix these problems and choose it again:</p>
      <ul>{#each preview.problems as item}<li>{item}</li>{/each}</ul>
    {/if}

    {#if problem}<p role="alert">{problem}</p>{/if}
    <div class="actions">
      <button type="button" disabled={busy || choosing} onclick={close}>Cancel</button>
      <button type="submit" class={removed > 0 ? 'danger' : 'primary'} disabled={!ready || busy || choosing}>{busy ? 'Replacing…' : 'Replace roster'}</button>
    </div>
  </form>
</Modal>
<style>
  .lead { margin-top: 0; }
  .label { display: block; margin: var(--space-4) 0 var(--space-1); color: var(--title-ink); font-weight: 600; }
  .file { display: flex; flex-wrap: wrap; align-items: center; gap: var(--space-2) var(--space-3); padding: .45rem .6rem; background: var(--color-control-bg); border: 1px solid var(--color-control-border); border-radius: var(--radius-control); }
  .file span { flex: 1 1 10rem; min-width: 0; overflow-wrap: anywhere; color: var(--color-text-muted); }
  .file span.chosen { color: var(--color-text); }
  .file button { display: inline-flex; align-items: center; gap: .4rem; margin: 0; }
  .check { display: flex; align-items: flex-start; gap: var(--space-2); margin: var(--space-3) 0; font-weight: 600; color: var(--title-ink); }
  ul { margin-top: var(--space-1); }
  .actions { display: flex; flex-wrap: wrap; justify-content: flex-end; gap: var(--space-2); margin-top: var(--space-5); }
  .actions button { margin: 0; min-width: 7.5rem; }
</style>
