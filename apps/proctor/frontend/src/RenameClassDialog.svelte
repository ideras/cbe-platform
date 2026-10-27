<script>
  import * as api from '$proctor-api';
  import Modal from './Modal.svelte';
  import { message } from './errors.js';
  // A class keeps its roster, its students and its exams when it is renamed; only the name changes.
  let { entry, onClose, onRenamed } = $props();
  // svelte-ignore state_referenced_locally
  let name = $state(entry.name);
  let busy = $state(false);
  let problem = $state('');
  const changed = $derived(name.trim() !== '' && name.trim() !== entry.name);
  async function save() {
    if (!changed || busy) return;
    busy = true; problem = '';
    try { onRenamed(await api.RenameClass(entry.class_id, name.trim())); }
    catch (err) { problem = message(err); }
    finally { busy = false; }
  }
</script>
<Modal title="Rename class" titleId="rename-class-title" {busy} {onClose}>
  <form onsubmit={event => { event.preventDefault(); save(); }}>
    <label for="rename-class-name">Class name</label>
    <input id="rename-class-name" bind:value={name} maxlength="120" autocomplete="off" disabled={busy} />
    <p class="hint">Its students, roster and exams stay as they are.</p>
    {#if problem}<p role="alert">{problem}</p>{/if}
    <div class="actions">
      <button type="button" disabled={busy} onclick={onClose}>Cancel</button>
      <button type="submit" class="primary" disabled={!changed || busy}>{busy ? 'Saving…' : 'Rename'}</button>
    </div>
  </form>
</Modal>
<style>
  label { display: block; margin: 0 0 var(--space-1); color: var(--title-ink); font-weight: 600; }
  input { width: 100%; }
  .hint { margin: var(--space-2) 0 0; color: var(--color-text-muted); font-size: var(--text-sm); }
  .actions { display: flex; flex-wrap: wrap; justify-content: flex-end; gap: var(--space-2); margin-top: var(--space-5); }
  .actions button { margin: 0; min-width: 6.5rem; }
</style>
