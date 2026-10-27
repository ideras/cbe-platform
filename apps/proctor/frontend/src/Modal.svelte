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
  dialog { width: min(var(--modal-width), calc(100% - 2rem)); max-height: calc(100% - 2rem); padding: 0; color: var(--color-text); background: var(--color-surface); border: 1px solid var(--panel-border); border-radius: 16px; box-shadow: var(--shadow-dialog); overflow: auto; }
  dialog::backdrop { background: var(--backdrop-dialog); }
  dialog.danger { border-top: 5px solid var(--color-danger); }
  dialog.warning { border-top: 5px solid var(--color-warning-border); }
  dialog.info { border-top: 5px solid var(--sea-ink); }
  header { display: flex; align-items: center; justify-content: space-between; gap: var(--space-3); padding: var(--space-4) var(--space-5) 0; }
  h2 { margin: 0; color: var(--title-ink); font-size: 1.3rem; font-weight: 750; }
  .close { display: inline-flex; align-items: center; justify-content: center; width: 2.2rem; min-height: 2.2rem; padding: 0; border-color: var(--transparent); background: var(--transparent); color: var(--color-text-muted); }
  .close:hover:not(:disabled) { background: var(--brand-soft); border-color: var(--transparent); color: var(--brand-strong); }
  .body { padding: var(--space-3) var(--space-5) var(--space-5); }
</style>
