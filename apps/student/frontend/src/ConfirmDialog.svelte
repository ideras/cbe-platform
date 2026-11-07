<script>
  import { onMount } from 'svelte';
  import Icon from '../../../../shared/theme/Icon.svelte';
  // A modal confirmation for a step that cannot be undone. Focus starts on the safe choice, Escape cancels, and the page
  // behind it cannot be used until the student chooses. Focus returns to whatever opened it. It is the proctor's framed
  // dialog: a dark header with an icon, the title and what it is about, the content in cards and a footer with the two
  // choices, the safe one first. `tone="alarm"` is for the step that ends the exam (an alert-colored header and an eyebrow
  // that says so); with `danger` the confirming button is the red one and the safe choice is the primary. There is no X in the
  // header: it would be a second button with the name of the safe choice, and Escape is already that.
  // The content is the caller's. The classes it can use are styled here: .strip (from and to), .facts with .fact (.warn, .ok,
  // .info) and .two for side by side, .meter, .pills and .note.
  let { title, confirmLabel, cancelLabel, disabled = false, onConfirm, onCancel, icon = '', eyebrow = '', subtitle = '', tone = '', danger = false, children } = $props();
  let dialog, safe;
  onMount(() => {
    const previous = document.activeElement;
    dialog.showModal();
    safe.focus();
    return () => previous?.focus?.();
  });
</script>
<dialog bind:this={dialog} class={tone} aria-labelledby="confirm-title" aria-describedby="confirm-body" oncancel={event => { event.preventDefault(); onCancel(); }}>
  <header>
    {#if icon}<span class="heading-icon"><Icon name={icon} size={20} /></span>{/if}
    <div class="heading">{#if eyebrow}<span class="eyebrow">{eyebrow}</span>{/if}<h2 id="confirm-title">{title}</h2>{#if subtitle}<p>{subtitle}</p>{/if}</div>
  </header>
  <div id="confirm-body" class="content">{@render children()}</div>
  <div class="foot">
    <button type="button" class:primary={danger} bind:this={safe} onclick={onCancel}>{cancelLabel}</button>
    <button type="button" class:primary={!danger} class:danger {disabled} onclick={onConfirm}>{confirmLabel}</button>
  </div>
</dialog>
<style>
  dialog { width: min(34rem, calc(100% - 2rem)); max-height: calc(100% - 2rem); padding: 0; color: var(--text-main); background: var(--bg-raised); border: 1px solid var(--border-subtle); border-radius: 14px; box-shadow: var(--shadow-dialog); overflow: auto; }
  dialog::backdrop { background: var(--backdrop); backdrop-filter: blur(2px); }

  /* The header stays in view while a tall dialog scrolls. */
  header { position: sticky; top: 0; z-index: 1; display: flex; align-items: center; gap: var(--space-3); padding: 1rem 1.1rem; background: var(--header-strong); color: var(--on-dark); }
  h2 { margin: 0; color: inherit; font-size: var(--text-lg); font-weight: 750; line-height: 1.2; overflow-wrap: anywhere; }
  .heading { min-width: 0; }
  .heading p { margin: .1rem 0 0; color: var(--side-text); font-size: .85rem; overflow-wrap: anywhere; }
  .heading-icon { display: grid; place-items: center; flex: none; width: 38px; height: 38px; border-radius: 10px; background: var(--side-active); border: 1px solid var(--cyan-chip-border); color: var(--accent-cyan-soft); }
  .eyebrow { display: block; margin-bottom: .1rem; color: var(--side-text); font-size: .68rem; font-weight: 700; letter-spacing: .12em; text-transform: uppercase; }
  /* The step that ends the exam: the header is the alert gradient, its marks and text the pale alert fill. */
  .alarm header { background: var(--header-alarm); }
  .alarm .eyebrow { color: var(--alert-fill); }
  .alarm .heading-icon { background: var(--chip-on-dark); border-color: var(--alarm-chip-border); color: var(--alert-fill); }

  .content { display: flex; flex-direction: column; gap: .9rem; padding: 1.1rem; font-size: .95rem; }
  .content :global(p) { margin: 0; overflow-wrap: anywhere; }
  .content :global(strong) { color: inherit; }

  /* From one thing to the next: the exercise being left and the one that opens. */
  .content :global(.strip) { display: flex; flex-wrap: wrap; align-items: center; gap: .7rem; padding: .75rem .9rem; border: 1px solid var(--border-subtle); border-radius: 12px; background: var(--bg-primary); }
  .content :global(.strip .step) { display: flex; flex: 1 1 10rem; flex-direction: column; min-width: 0; }
  .content :global(.strip .k) { color: var(--text-muted); font-size: .68rem; font-weight: 700; letter-spacing: .08em; text-transform: uppercase; }
  .content :global(.strip .v) { font-weight: 700; overflow-wrap: anywhere; }
  .content :global(.strip .next .v) { color: var(--action-hover); }
  .content :global(.strip svg) { flex: none; color: var(--action-primary); }

  /* What changes and what does not: a marker, a heading and what it means. */
  .content :global(.facts) { display: grid; grid-template-columns: minmax(0, 1fr); gap: .6rem; }
  .content :global(.facts.two) { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .content :global(.fact) { display: flex; align-items: flex-start; gap: .65rem; min-width: 0; padding: .7rem .8rem; border: 1px solid var(--action-border); border-radius: 10px; background: var(--hover-fill); color: var(--action-hover); font-size: .9rem; }
  .content :global(.fact > div) { flex: 1; min-width: 0; }
  .content :global(.fact .mk) { display: grid; place-items: center; flex: none; width: 26px; height: 26px; margin-top: .05rem; border-radius: 50%; background: var(--action-hover); color: var(--hover-fill); }
  .content :global(.fact .h) { margin-bottom: .15rem; font-weight: 700; }
  .content :global(.fact p + p) { margin-top: .4rem; }
  .content :global(.fact.warn) { border-color: var(--warn-border); background: var(--warn-fill); color: var(--warn-text); }
  .content :global(.fact.warn .mk) { background: var(--warn-text); color: var(--warn-fill); }
  .content :global(.fact.ok) { border-color: var(--ok-border); background: var(--ok-fill); color: var(--ok-text); }
  .content :global(.fact.ok .mk) { background: var(--ok-text); color: var(--ok-fill); }

  /* How much of the exam is answered: the sentence says it, the bar shows it. */
  .content :global(.meter) { height: 8px; margin-top: .5rem; border-radius: 4px; background: var(--bg-secondary); overflow: hidden; }
  .content :global(.meter span) { display: block; height: 100%; border-radius: 4px; background: var(--bar-fill); }
  .content :global(.pills) { display: flex; flex-wrap: wrap; gap: .35rem; margin: .4rem 0 0; padding: 0; list-style: none; }
  .content :global(.pills li) { padding: .15rem .6rem; border: 1px solid var(--warn-border); border-radius: 999px; background: var(--warn-fill); color: var(--warn-text); font-size: .82rem; }
  .content :global(.pills li b) { margin-left: .25rem; font-family: var(--font-mono); font-weight: 700; }
  .content :global(.pills li.more) { border-color: var(--border-subtle); background: var(--bg-secondary); color: var(--text-main); }
  .content :global(.fact .k) { margin-top: .6rem; }
  .content :global(.k) { color: var(--text-muted); font-size: .72rem; font-weight: 700; letter-spacing: .08em; text-transform: uppercase; }

  /* Notices: what the student should know before choosing. */
  .content :global(.note) { display: flex; align-items: flex-start; gap: .55rem; padding: .6rem .75rem; border: 1px solid var(--action-border); border-left: 4px solid var(--action-primary); border-radius: 9px; background: var(--hover-fill); color: var(--action-hover); font-size: .9rem; }
  .content :global(.note svg) { flex: none; margin-top: .15rem; }
  .content :global(.note.warn) { border-color: var(--warn-border); border-left-color: var(--warn); background: var(--warn-fill); color: var(--warn-text); }
  .content :global(.note.alert) { border-color: var(--alert-border); border-left-color: var(--alert); background: var(--alert-fill); color: var(--alert-text); }

  /* The footer stays at the bottom of the dialog while the content scrolls. The safe choice is the first; in a narrow window the
     choices are one above the other with the primary on top, as in the proctor's dialogs. */
  .foot { position: sticky; bottom: 0; display: flex; align-items: center; justify-content: flex-end; gap: .6rem; padding: .85rem 1.1rem; border-top: 1px solid var(--border-subtle); background: var(--bg-primary); }
  .foot button { display: inline-flex; align-items: center; justify-content: center; gap: .45rem; min-height: 40px; margin: 0; padding: 0 1.1rem; border-radius: 9px; font-weight: 700; }

  /* The dialog is 34rem wide and starts to shrink with the window at 36rem. */
  @media (max-width: 560px) {
    .content :global(.facts.two) { grid-template-columns: minmax(0, 1fr); }
    .content :global(.strip) { flex-direction: column; align-items: flex-start; gap: .4rem; }
    .content :global(.strip svg) { transform: rotate(90deg); }
    .foot { flex-direction: column-reverse; }
    .foot button { width: 100%; white-space: normal; }
  }
</style>
