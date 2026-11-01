<script>
  import { onMount } from 'svelte';
  import Icon from '../../../../shared/theme/Icon.svelte';
  // The stack of notifications. It is fixed to the window, so a message is seen wherever the page is scrolled, and it never
  // takes the focus. Time stands still while the pointer or the keyboard is on a toast and while the window is not the
  // active one. Alt+Shift+N moves the keyboard here from anywhere, the calculator included; Escape goes back.
  let { toasts = [], onDismiss, onTick } = $props();
  let host = $state(null);
  let hovering = $state(false);
  let focusInside = $state(false);
  let active = $state(true);
  let returnTo = null;
  let entered = false;
  let paused = $derived(hovering || focusInside || !active);

  onMount(() => {
    active = document.hasFocus();
    const timer = setInterval(() => { if (!paused && toasts.length) onTick(250); }, 250);
    const on = () => { active = true; }; const off = () => { active = false; };
    window.addEventListener('focus', on); window.addEventListener('blur', off);
    // Capture phase: the calculator's own key handling must not be able to swallow the shortcut.
    document.addEventListener('keydown', shortcut, true);
    return () => { clearInterval(timer); window.removeEventListener('focus', on); window.removeEventListener('blur', off); document.removeEventListener('keydown', shortcut, true); };
  });

  const buttons = () => [...host.querySelectorAll('button')];
  function shortcut(event) {
    if (!(event.altKey && event.shiftKey && event.code === 'KeyN')) return;
    const first = buttons()[0];
    if (!first) return;
    event.preventDefault(); event.stopPropagation();
    if (!host.contains(document.activeElement)) returnTo = document.activeElement;
    entered = true;
    first.focus();
  }
  function leave() {
    entered = false;
    const target = returnTo; returnTo = null;
    if (target?.isConnected) target.focus();
  }
  // Tab stays on the toasts only when the keyboard was sent here with the shortcut: with the calculator open the next
  // thing in the page is out of reach behind it. Reached by tabbing, the toasts are an ordinary part of the page.
  function key(event) {
    if (event.key === 'Escape' && entered) { event.preventDefault(); event.stopPropagation(); leave(); return; }
    if (event.key !== 'Tab' || !entered) return;
    const list = buttons(); if (!list.length) return;
    const index = list.indexOf(document.activeElement);
    const next = event.shiftKey ? (index <= 0 ? list.length - 1 : index - 1) : (index === list.length - 1 ? 0 : index + 1);
    event.preventDefault(); list[next].focus();
  }
  function dismiss(toast) {
    const list = buttons(); const index = list.indexOf(document.activeElement);
    onDismiss(toast.id);
    // The button the keyboard was on is gone: go on to the next toast, or back to where the student was.
    if (index < 0) return;
    queueMicrotask(() => { const rest = buttons(); if (rest.length) rest[Math.min(index, rest.length - 1)].focus(); else leave(); });
  }
  const label = toast => toast.kind === 'error' ? 'Dismiss error' : 'Dismiss warning';
</script>

<!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
<div class="toasts" bind:this={host} aria-label="Notifications" role="region" aria-keyshortcuts="Alt+Shift+N" onkeydown={key}
  onmouseenter={() => hovering = true} onmouseleave={() => hovering = false}
  onfocusin={() => focusInside = true} onfocusout={event => { if (!host.contains(event.relatedTarget)) { focusInside = false; entered = false; } }}>
  {#each toasts as toast (toast.id)}
    <div class="toast {toast.kind}" role={toast.kind === 'error' ? 'alert' : 'status'}>
      <span class="icon" aria-hidden="true"><Icon name={toast.kind === 'error' ? 'error' : toast.kind === 'info' ? 'info' : 'warning'} size={16} /></span>
      <p>{toast.text}</p>
      <button type="button" class="close" aria-label={label(toast)} onclick={() => dismiss(toast)}><Icon name="close" size={18} /></button>
    </div>
  {/each}
</div>

<style>
  .toasts { position: fixed; z-index: 100; right: var(--space-3); bottom: var(--space-3); display: flex; flex-direction: column; gap: var(--space-2);
    width: min(26rem, calc(100vw - 2 * var(--space-3))); max-height: calc(100vh - 2 * var(--space-3)); overflow-y: auto; pointer-events: none; }
  /* A raised card with a status edge: warn by default, alert for errors, blue for information. */
  .toast { pointer-events: auto; display: grid; grid-template-columns: auto minmax(0, 1fr) auto; align-items: start; gap: var(--space-2);
    padding: var(--space-2) var(--space-2) var(--space-2) var(--space-3); background: var(--bg-raised); color: var(--text-main);
    border: 1px solid var(--border-subtle); border-left: 5px solid var(--warn); border-radius: var(--radius-ctl); box-shadow: var(--shadow-raised); }
  .toast.error { border-left-color: var(--alert); }
  .toast.info { border-left-color: var(--action-primary); }
  .icon { display: grid; place-items: center; flex: none; width: 1.6rem; height: 1.6rem; margin-top: 0; border-radius: 50%; background: var(--warn-fill); color: var(--warn-text); }
  .error .icon { background: var(--alert-fill); color: var(--alert-text); }
  .info .icon { background: var(--selection-fill); color: var(--action-hover); }
  p { margin: 0; padding-top: .1rem; overflow-wrap: anywhere; line-height: 1.4; }
  .close { margin: 0; min-width: 2rem; min-height: 2rem; padding: 0 var(--space-2); line-height: 1; background: var(--transparent); border-color: var(--transparent); color: var(--text-muted); }
  @media (max-width: 720px) { .toasts { right: var(--space-2); left: var(--space-2); bottom: var(--space-2); width: auto; } }
</style>
