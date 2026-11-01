<script>
  import { onMount } from 'svelte';
  import Icon from './Icon.svelte';
  // A modal dialog with a title, a close button and a body. While `busy` it cannot be dismissed, so an action in flight
  // is never abandoned by accident. Focus returns to whatever opened it.
  let { title, titleId = 'modal-title', tone = '', busy = false, onClose, children, width = '34rem' } = $props();
  let dialog;
  onMount(() => { const previous = document.activeElement; dialog.showModal(); return () => previous?.focus?.(); });
</script>
<dialog bind:this={dialog} class={tone} style:--modal-width={width} oncancel={event => { event.preventDefault(); if (!busy) onClose(); }} aria-labelledby={titleId}>
  <header>
    <h2 id={titleId}>{title}</h2>
    <button type="button" class="close" aria-label="Close" title="Close" disabled={busy} onclick={onClose}><Icon name="close" size={20} /></button>
  </header>
  <div class="body">{@render children()}</div>
</dialog>
<style>
  dialog { width: min(var(--modal-width), calc(100% - 2rem)); max-height: calc(100% - 2rem); padding: 0; color: var(--text-main); background: var(--bg-raised); border: 1px solid var(--border-subtle); border-radius: 14px; box-shadow: var(--shadow-dialog); overflow: auto; }
  dialog::backdrop { background: var(--backdrop); }
  dialog.danger { border-top: 5px solid var(--alert); }
  dialog.warning { border-top: 5px solid var(--warn); }
  dialog.info { border-top: 5px solid var(--action-primary); }
  header { display: flex; align-items: center; justify-content: space-between; gap: var(--space-3); padding: var(--space-4) var(--space-5) 0; }
  h2 { margin: 0; color: var(--text-main); font-size: 1.3rem; font-weight: 750; }
  .close { display: inline-flex; align-items: center; justify-content: center; width: 2.2rem; min-height: 2.2rem; padding: 0; border-color: var(--transparent); background: var(--transparent); color: var(--text-muted); box-shadow: none; }
  .close:hover:not(:disabled) { background: var(--hover-fill); border-color: var(--transparent); color: var(--action-hover); }
  .close:disabled { background: var(--transparent); border-color: var(--transparent); color: var(--muted-dot); }
  .body { padding: var(--space-3) var(--space-5) var(--space-5); }
</style>
