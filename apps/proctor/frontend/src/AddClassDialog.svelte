<script>
  import * as api from '$proctor-api';
  import Modal from './Modal.svelte';
  import Icon from './Icon.svelte';
  import { message } from './errors.js';
  // Adding a class takes its roster file, which the server checks before anything is stored, and a name. The file is read by the
  // native side; this window only sees what the server made of it.
  let { onClose, onCreated } = $props();
  let preview = $state(null);
  let name = $state('');
  let typed = false;
  let choosing = $state(false);
  let busy = $state(false);
  let problem = $state('');
  const ready = $derived(!!preview?.valid && name.trim() !== '');
  function plural(n, noun) { return `${n} ${noun}${n === 1 ? '' : 's'}`; }

  async function choose() {
    choosing = true; problem = '';
    try {
      const result = await api.ChooseClassFile();
      if (!result.cancelled) { preview = result; if (!typed) name = result.suggested_name; }
    } catch (err) { problem = message(err); }
    finally { choosing = false; }
  }
  async function discard() { if (preview) await api.DiscardClassFile().catch(() => {}); }
  async function close() { await discard(); onClose(); }
  async function create() {
    if (!ready || busy) return;
    busy = true; problem = '';
    try { const entry = await api.CreateClass(name.trim()); onCreated(entry); }
    catch (err) { problem = message(err); }
    finally { busy = false; }
  }
</script>
<Modal title="Add class" titleId="add-class-title" busy={busy || choosing} onClose={close}>
  <form onsubmit={event => { event.preventDefault(); create(); }}>
    <div class="label" id="class-file-label">Roster file</div>
    <div class="file" role="group" aria-labelledby="class-file-label">
      <span class:chosen={preview}>{preview ? preview.file : 'No file chosen (CSV with canvas_id, account_number, name, email, sis_user_id)'}</span>
      <button type="button" disabled={busy || choosing} onclick={choose}><Icon name="folder" size={18} />{preview ? 'Choose another…' : 'Browse…'}</button>
    </div>

    {#if preview?.valid}
      <p class="found"><strong>{plural(preview.students, 'student')}</strong> found in this file.</p>
      {#if preview.warning_count > 0}
        <details><summary>{plural(preview.warning_count, 'warning')}</summary>
          <ul>{#each preview.warnings as item}<li>Row {item.row}{item.account_number ? ` (${item.account_number})` : ''}: {item.message}</li>{/each}</ul>
          {#if preview.warning_count > preview.warnings.length}<p class="muted">And {preview.warning_count - preview.warnings.length} more.</p>{/if}
        </details>
      {/if}
    {:else if preview}
      <p role="alert">This file cannot be used. Fix these problems and choose it again:</p>
      <ul>{#each preview.problems as item}<li>{item}</li>{/each}</ul>
    {/if}

    <label for="class-name">Class name</label>
    <input id="class-name" bind:value={name} maxlength="120" autocomplete="off" placeholder="Mathematics 10A" disabled={busy} oninput={() => typed = true} />

    {#if problem}<p role="alert">{problem}</p>{/if}
    <div class="actions">
      <button type="button" disabled={busy || choosing} onclick={close}>Cancel</button>
      <button type="submit" class="primary" disabled={!ready || busy || choosing}>{busy ? 'Adding…' : 'Add class'}</button>
    </div>
  </form>
</Modal>
<style>
  label, .label { display: block; margin: var(--space-4) 0 var(--space-1); color: var(--title-ink); font-weight: 600; }
  .label:first-child { margin-top: 0; }
  input { width: 100%; }
  .file { display: flex; flex-wrap: wrap; align-items: center; gap: var(--space-2) var(--space-3); padding: .45rem .6rem; background: var(--color-control-bg); border: 1px solid var(--color-control-border); border-radius: var(--radius-control); }
  .file span { flex: 1 1 10rem; min-width: 0; overflow-wrap: anywhere; color: var(--color-text-muted); }
  .file span.chosen { color: var(--color-text); }
  .file button { display: inline-flex; align-items: center; gap: .4rem; margin: 0; }
  .found { margin-bottom: 0; }
  ul { margin-top: var(--space-1); }
  .actions { display: flex; flex-wrap: wrap; justify-content: flex-end; gap: var(--space-2); margin-top: var(--space-5); }
  .actions button { margin: 0; min-width: 6.5rem; }
</style>
