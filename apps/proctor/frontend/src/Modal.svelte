<script>
  import { onMount } from 'svelte';
  import Icon from '../../../../shared/theme/Icon.svelte';
  // A modal dialog with a title, a close button and a body. While `busy` it cannot be dismissed, so an action in flight
  // is never abandoned by accident. Focus returns to whatever opened it. The tone `deep alarm` is the framed dialog of an
  // irreversible action: an alert-colored header, and a click on the backdrop nudges the dialog instead of doing nothing.
  let { title, titleId = 'modal-title', tone = '', subtitle = '', eyebrow = '', icon = '', busy = false, onClose, children, width = '34rem' } = $props();
  let dialog;
  onMount(() => {
    const previous = document.activeElement;
    dialog.showModal();
    // A click on the backdrop is delivered to the dialog element itself; one inside its box lands on something it holds.
    const nudge = event => {
      const box = dialog.getBoundingClientRect();
      const inside = event.clientX >= box.left && event.clientX <= box.right && event.clientY >= box.top && event.clientY <= box.bottom;
      if (inside || !tone.split(' ').includes('alarm')) return;
      dialog.classList.remove('nudge');
      void dialog.offsetWidth; // restart the animation when the backdrop is clicked again
      dialog.classList.add('nudge');
    };
    dialog.addEventListener('click', nudge);
    return () => { dialog.removeEventListener('click', nudge); previous?.focus?.(); };
  });
</script>
<dialog bind:this={dialog} class={tone} style:--modal-width={width} oncancel={event => { event.preventDefault(); if (!busy) onClose(); }} aria-labelledby={titleId}>
  <header>
    {#if icon}<span class="heading-icon"><Icon name={icon} size={20} /></span>{/if}
    <div class="heading">{#if eyebrow}<span class="eyebrow">{eyebrow}</span>{/if}<h2 id={titleId}>{title}</h2>{#if subtitle}<p>{subtitle}</p>{/if}</div>
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
  .heading { min-width: 0; }
  .heading p { margin: .1rem 0 0; color: var(--text-muted); font-size: var(--text-sm); }
  .heading-icon { display: grid; place-items: center; flex: none; width: 38px; height: 38px; border-radius: 10px; }
  .body { padding: var(--space-3) var(--space-5) var(--space-5); }
  /* Opt-in framed dialogs keep the default layout and focus behavior for every other modal. */
  dialog.deep::backdrop { backdrop-filter: blur(2px); }
  /* The header stays in view while a tall dialog scrolls, as the mockups' dialogs do. */
  .deep header { position: sticky; top: 0; z-index: 1; justify-content: flex-start; padding: 1rem 1.1rem; background: var(--header-strong); color: var(--on-dark); }
  .deep h2 { color: inherit; font-size: var(--text-lg); line-height: 1.2; overflow-wrap: anywhere; }
  .deep .heading p { color: var(--side-text); font-size: .82rem; overflow-wrap: anywhere; }
  .deep .heading-icon { background: var(--side-active); border: 1px solid var(--cyan-chip-border); color: var(--accent-cyan-soft); }
  .deep .close { flex: none; width: 34px; min-height: 34px; margin-left: auto; border: 1px solid var(--cyan-chip-border); border-radius: 9px; color: var(--side-text); }
  .deep .close:hover:not(:disabled) { background: var(--chip-on-dark); border-color: var(--accent-cyan-soft); color: var(--on-dark); }
  .deep .close:focus-visible { outline-color: var(--accent-cyan-soft); }
  .deep .body { padding: 0; }
  /* The irreversible variant: the header is the alert gradient, its marks and text the pale alert fill. */
  dialog.alarm { animation: pop .18s ease-out; }
  dialog.alarm.nudge { animation: nudge .3s; }
  @keyframes pop { from { opacity: 0; transform: translateY(8px) scale(.98); } }
  @keyframes nudge { 25% { transform: translateX(-6px); } 75% { transform: translateX(6px); } }
  .alarm header { background: var(--header-alarm); }
  .alarm .heading-icon { background: var(--chip-on-dark); border-color: var(--alarm-chip-border); color: var(--alert-fill); }
  .alarm .eyebrow { color: var(--alert-fill); font-size: .68rem; letter-spacing: .12em; }
  .alarm .close { border-color: var(--alarm-chip-border); color: var(--alert-fill); }
  .alarm .close:hover:not(:disabled) { background: var(--chip-on-dark); border-color: var(--on-dark); color: var(--on-dark); }
  .alarm .close:focus-visible { outline-color: var(--on-dark); }
  .alarm .close:disabled { opacity: .5; }
</style>
