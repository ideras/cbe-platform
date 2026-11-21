<script>
  import { mount, unmount, untrack } from 'svelte';
  import { sanitizeHTML, slotElements } from '../../../../shared/exam/sanitize.js';
  import PreviewAnswer from './PreviewAnswer.svelte';

  let { html, slots = [], numbers, fieldNames = false, answers, onChange } = $props();
  let host;
  let fields = $state.raw([]);
  const safe = $derived(sanitizeHTML(html));

  // Mount only onto trusted placeholders after sanitizing the exam HTML. Updating a trial answer does
  // not rebuild the fragment (or move focus or the caret).
  $effect(() => {
    safe;
    const byID = new Map(slots.map(slot => [slot.id, slot]));
    const mounted = untrack(() => slotElements(host).flatMap(element => {
      const slot = byID.get(element.dataset.slot);
      if (!slot) return [];
      element.replaceChildren();
      const props = $state({ slot, number: numbers.get(slot.id), fieldNames, answers, onChange });
      return [{ props, component: mount(PreviewAnswer, { target: element, props }) }];
    }));
    fields = mounted;
    return () => mounted.forEach(field => unmount(field.component));
  });

  $effect(() => {
    for (const field of fields) field.props.fieldNames = fieldNames;
  });
</script>

<div class="rich" bind:this={host}>{@html safe}</div>

<style>
  .rich { overflow-wrap: anywhere; }
  .rich :global(> :first-child) { margin-top: 0; }
  .rich :global(> :last-child) { margin-bottom: 0; }
  .rich :global(pre) { margin: var(--space-2) 0; padding: var(--space-2); overflow: auto; background: var(--bg-primary); border: 1px solid var(--border-subtle); border-radius: var(--radius-sm); font-size: var(--text-sm); }
  .rich :global(code) { font-family: var(--font-mono); }
  .rich :global(:not(pre) > code:not(.field-tag)) { padding: .05rem .3rem; background: var(--bg-secondary); border-radius: var(--radius-sm); }
  .rich :global(table) { width: max-content; max-width: 100%; border-collapse: collapse; margin: var(--space-2) 0; }
  .rich :global(th), .rich :global(td) { padding: .25rem .55rem; border: 1px solid var(--border-subtle); }
  .rich :global(img) { display: block; max-width: 100%; height: auto; margin: .75rem auto; background: var(--figure-bg); -webkit-user-drag: none; user-select: none; }
</style>
