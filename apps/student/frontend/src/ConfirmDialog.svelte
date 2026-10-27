<script>
  import { onMount } from 'svelte';
  // A modal confirmation for a step that cannot be undone. Focus starts on the safe choice, Escape cancels, and the page
  // behind it cannot be used until the student chooses. Focus returns to whatever opened it.
  let { title, confirmLabel, cancelLabel, disabled = false, onConfirm, onCancel, children } = $props();
  let dialog, safe;
  onMount(() => {
    const previous = document.activeElement;
    dialog.showModal();
    safe.focus();
    return () => previous?.focus?.();
  });
</script>
<dialog bind:this={dialog} aria-labelledby="confirm-title" aria-describedby="confirm-body" oncancel={event => { event.preventDefault(); onCancel(); }}>
  <h2 id="confirm-title">{title}</h2>
  <div id="confirm-body">{@render children()}</div>
  <div class="actions">
    <button type="button" class="primary" {disabled} onclick={onConfirm}>{confirmLabel}</button>
    <button type="button" bind:this={safe} onclick={onCancel}>{cancelLabel}</button>
  </div>
</dialog>
<style>
  dialog { width: min(32rem, calc(100% - 2rem)); padding: var(--space-5); color: var(--color-text); background: var(--color-surface); border: 1px solid var(--color-border); border-top: 5px solid var(--color-warning-border); border-radius: var(--radius-lg); box-shadow: var(--shadow-raised); }
  dialog::backdrop { background: var(--backdrop-confirm); }
  h2 { margin: 0 0 var(--space-3); color: var(--color-heading); font-size: var(--text-xl); }
  #confirm-body :global(p) { margin: 0 0 var(--space-2); }
  .actions { display: flex; flex-wrap: wrap; gap: var(--space-2); margin-top: var(--space-4); }
  .actions button { margin: 0; }
</style>
