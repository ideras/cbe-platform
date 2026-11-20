<script>
  import { tick } from 'svelte';
  import Icon from '../../../../../shared/theme/Icon.svelte';
  // A button that opens a short list of actions (the WAI-ARIA menu button). A click, Enter, Space or the down arrow open it with its first
  // action in focus, the up arrow with its last; in it the arrows, Home and End move, Enter or a click does the action, and Escape, Tab or a
  // click elsewhere close it (Escape puts the keyboard back on the button). The list is fixed to the window under the button, so the panel the
  // button sits in, which clips what overflows it, does not cut the list. `items` are { label, detail, disabled, run } or 'separator'; a detail
  // (a count, a key) is shown at the right of its action and is not part of its name.
  let { label, icon = 'dots', items = [], disabled = false } = $props();
  let open = $state(false);
  let button = $state(null);
  let list = $state(null);
  let place = $state({ top: 0, right: 0 });
  const actions = () => [...(list?.querySelectorAll('[role="menuitem"]:not(:disabled)') ?? [])];

  async function show(which = 'first') {
    if (disabled || !button) return;
    const box = button.getBoundingClientRect();
    place = { top: box.bottom + 4, right: Math.max(8, window.innerWidth - box.right) };
    open = true;
    await tick();
    const all = actions();
    (which === 'last' ? all.at(-1) : all[0])?.focus();
  }
  function hide(back) {
    if (!open) return;
    open = false;
    if (back) button?.focus();
  }
  function buttonKeys(event) {
    if (event.key === 'ArrowDown') { event.preventDefault(); show('first'); }
    else if (event.key === 'ArrowUp') { event.preventDefault(); show('last'); }
  }
  function listKeys(event) {
    const all = actions();
    const here = all.indexOf(document.activeElement);
    const go = index => { event.preventDefault(); all[(index + all.length) % all.length]?.focus(); };
    if (event.key === 'ArrowDown') go(here + 1);
    else if (event.key === 'ArrowUp') go(here < 0 ? all.length - 1 : here - 1);
    else if (event.key === 'Home') go(0);
    else if (event.key === 'End') go(all.length - 1);
    else if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); hide(true); }
    else if (event.key === 'Tab') hide(false);
  }
  // The keyboard goes back to the button first, so that an action that puts it elsewhere (or takes the button away) has the last word.
  function choose(item) { hide(true); item.run(); }
  // A click anywhere else, a change of the window's size or the window losing the focus close the list.
  $effect(() => {
    if (!open) return;
    const away = event => { if (!list?.contains(event.target) && !button?.contains(event.target)) hide(false); };
    const gone = () => hide(false);
    window.addEventListener('pointerdown', away, true);
    window.addEventListener('resize', gone);
    window.addEventListener('blur', gone);
    return () => { window.removeEventListener('pointerdown', away, true); window.removeEventListener('resize', gone); window.removeEventListener('blur', gone); };
  });
  $effect(() => { if (disabled) hide(false); });
</script>
<button type="button" class="menu-button" bind:this={button} aria-label={label} title={label} aria-haspopup="menu" aria-expanded={open} {disabled}
  onclick={() => (open ? hide(true) : show())} onkeydown={buttonKeys}><Icon name={icon} size={16} /></button>
{#if open}
  <div class="menu" role="menu" aria-label={label} tabindex="-1" bind:this={list} style:top="{place.top}px" style:right="{place.right}px" onkeydown={listKeys}>
    {#each items as item, index (index)}
      {#if item === 'separator'}
        <hr />
      {:else}
        <button type="button" role="menuitem" tabindex="-1" disabled={item.disabled} onclick={() => choose(item)}>
          <span class="label">{item.label}</span>{#if item.detail != null}<span class="detail" aria-hidden="true">{item.detail}</span>{/if}
        </button>
      {/if}
    {/each}
  </div>
{/if}
<style>
  .menu-button { flex: none; display: inline-flex; align-items: center; justify-content: center; margin: 0; padding: .25rem .45rem; border: 0; border-radius: 0; background: var(--transparent); box-shadow: none; color: var(--text-main); }
  .menu-button[aria-expanded="true"] { background: var(--bg-raised); }
  .menu-button:focus-visible { outline: 2px solid var(--action-primary); outline-offset: -2px; }
  .menu { position: fixed; z-index: 50; display: grid; min-width: 15rem; padding: .3rem; background: var(--bg-raised); border: 1px solid var(--border-subtle); border-radius: var(--radius-ctl); box-shadow: var(--shadow-dialog); }
  .menu button { display: flex; align-items: center; gap: var(--space-3); width: 100%; margin: 0; padding: .4rem .6rem; border: 0; border-radius: 6px; background: var(--transparent); box-shadow: none; color: var(--text-main); font-size: var(--text-sm); font-weight: 400; text-align: left; }
  .menu button:hover:not(:disabled), .menu button:focus-visible { background: var(--hover-fill); color: var(--action-hover); outline: none; }
  .menu button:disabled { opacity: .45; }
  .label { flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .detail { flex: none; color: var(--text-muted); font-family: var(--font-mono); font-size: var(--text-xs); }
  hr { margin: .25rem .2rem; border: 0; border-top: 1px solid var(--border-subtle); }
</style>
