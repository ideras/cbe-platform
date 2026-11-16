<script>
  import { onMount } from 'svelte';
  import { nameProblem } from './paths.js';
  // Asks for a name: a new file, a new folder, or a new name for one. The name is checked here for what the agent would refuse, and
  // what the workspace says against it (a name that is taken) is shown under the field, where the student can change it.
  let { title, label, initial = '', confirmLabel, where = '', onSubmit, onCancel } = $props();
  let dialog, input;
  let value = $state(initial);
  let problem = $state('');
  let busy = $state(false);
  onMount(() => {
    const previous = document.activeElement;
    dialog.showModal();
    input.focus();
    // A name that has an extension is selected up to the dot, as every file manager does.
    const dot = initial.lastIndexOf('.');
    input.setSelectionRange(0, dot > 0 ? dot : initial.length);
    return () => previous?.focus?.();
  });
  async function submit(event) {
    event.preventDefault();
    if (busy) return;
    const name = value.trim();
    problem = nameProblem(name);
    if (problem) return;
    busy = true;
    try { problem = (await onSubmit(name)) || ''; } finally { busy = false; }
  }
</script>
<dialog bind:this={dialog} aria-labelledby="name-title" oncancel={event => { event.preventDefault(); onCancel(); }}>
  <form onsubmit={submit}>
    <h2 id="name-title">{title}</h2>
    {#if where}<p class="where">in <code>{where}</code></p>{/if}
    <label>{label}
      <input bind:this={input} bind:value autocomplete="off" spellcheck="false" aria-invalid={problem ? 'true' : undefined} aria-describedby={problem ? 'name-problem' : undefined} />
    </label>
    <p id="name-problem" class="problem" role="alert">{problem}</p>
    <div class="foot">
      <button type="button" onclick={onCancel}>Cancel</button>
      <button class="primary" disabled={busy}>{confirmLabel}</button>
    </div>
  </form>
</dialog>
<style>
  dialog { width: min(26rem, calc(100% - 2rem)); padding: 0; color: var(--text-main); background: var(--bg-raised); border: 1px solid var(--border-subtle); border-radius: 14px; box-shadow: var(--shadow-dialog); }
  dialog::backdrop { background: var(--backdrop); backdrop-filter: blur(2px); }
  form { display: grid; gap: var(--space-2); padding: 1.1rem; }
  h2 { margin: 0; font-size: var(--text-lg); }
  .where { margin: 0; color: var(--text-muted); font-size: var(--text-sm); }
  label { display: grid; gap: var(--space-1); margin: 0; font-weight: 600; }
  input { width: 100%; font-family: var(--font-mono); font-weight: 400; }
  .problem { min-height: 1.3em; margin: 0; color: var(--alert-text); font-size: var(--text-sm); }
  .foot { display: flex; justify-content: flex-end; gap: var(--space-2); }
  .foot button { margin: 0; }
</style>
