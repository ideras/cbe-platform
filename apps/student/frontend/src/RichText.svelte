<script>
  // Shows a piece of exam text and puts an answer field wherever it marks a slot. The text is sanitized first; fields are
  // mounted once per text and then only told about new values and locking, so typing never rebuilds them.
  import { mount, unmount, untrack } from 'svelte';
  import { sanitizeHTML, slotElements } from '../../../../shared/exam/sanitize.js';
  import SlotField from './SlotField.svelte';

  let { html, slots = [], answers = {}, locked = false, onChange = () => {} } = $props();
  let host;
  let fields = $state.raw([]);
  const safe = $derived(sanitizeHTML(html));

  $effect(() => {
    safe;
    const mounted = untrack(() => slotElements(host).flatMap(element => {
      const slot = slots.find(candidate => candidate.id === element.dataset.slot);
      if (!slot) return [];
      element.replaceChildren();
      const props = $state({ slot, value: answers[slot.id] ?? '', locked, onChange });
      return [{ props, component: mount(SlotField, { target: element, props }) }];
    }));
    fields = mounted;
    return () => mounted.forEach(field => unmount(field.component));
  });

  $effect(() => {
    for (const field of fields) {
      field.props.value = answers[field.props.slot.id] ?? '';
      field.props.locked = locked;
    }
  });
</script>

<div class="rich" bind:this={host}>{@html safe}</div>

<style>
  .rich { overflow-wrap: anywhere; }
  .rich :global(> :first-child) { margin-top: 0; }
  .rich :global(> :last-child) { margin-bottom: 0; }
  .rich :global(pre) {
    margin: var(--space-3) 0; padding: var(--space-3); overflow: auto; background: var(--transparent); border: 1px solid var(--color-border-soft);
    border: 1px solid var(--color-border-soft); border-radius: var(--radius-sm); font-size: var(--text-sm); line-height: 1.45;
  }
  .rich :global(code) { font-family: var(--font-mono); font-size: .92em; }
  .rich :global(:not(pre) > code) { padding: .05rem .3rem; background: var(--color-surface-sunken); border-radius: var(--radius-sm); }
  .rich :global(table) { margin: var(--space-3) 0; border-collapse: collapse; max-width: 100%; }
  .rich :global(th), .rich :global(td) { padding: .35rem .7rem; border: 1px solid var(--color-border); vertical-align: middle; }
  .rich :global(th) { background: var(--transparent); color: var(--color-heading); text-align: left; }
  .rich :global(th[align="center"]), .rich :global(td[align="center"]) { text-align: center; }
  .rich :global(th[align="right"]), .rich :global(td[align="right"]) { text-align: right; }
  .rich :global(blockquote) { margin: var(--space-3) 0; padding-left: var(--space-3); border-left: 3px solid var(--color-border); color: var(--color-text-muted); }
  .rich :global(li + li) { margin-top: var(--space-1); }
  .rich :global(div.slot) { display: block; }
</style>
