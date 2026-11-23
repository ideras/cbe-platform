<script>
  import Icon from '../../../../shared/theme/Icon.svelte';
  // A row of tabs and their panels, as in the tabs pattern: arrow keys, Home and End move between the tabs, and only the chosen one is
  // in the tab order. Every panel stays in the page, the others hidden, so what was chosen or started in one (a file being read, a
  // replay) is still there when its tab is chosen again. `tabs` is [{ id, text, icon?, count? }]; `panel(id)` draws a panel.
  let { tabs, selected = $bindable(), label, id = 'tabs', panel } = $props();
  function move(event) {
    const index = tabs.findIndex(item => item.id === selected);
    const next = { ArrowRight: (index + 1) % tabs.length, ArrowLeft: (index + tabs.length - 1) % tabs.length, Home: 0, End: tabs.length - 1 }[event.key];
    if (next === undefined) return;
    event.preventDefault();
    selected = tabs[next].id;
    document.getElementById(`${id}-tab-${selected}`)?.focus();
  }
</script>
<div class="tabs" role="tablist" aria-label={label}>
  {#each tabs as item (item.id)}
    <button type="button" role="tab" id="{id}-tab-{item.id}" aria-selected={selected === item.id} aria-controls="{id}-panel-{item.id}" tabindex={selected === item.id ? 0 : -1} onkeydown={move} onclick={() => { selected = item.id; }}>
      {#if item.icon}<Icon name={item.icon} size={17} />{/if}{item.text}{#if item.count !== undefined}{' '}<span class="n {item.tone ?? ''}">{item.count}</span>{/if}
    </button>
  {/each}
</div>
{#each tabs as item (item.id)}
  <div class="review-panel" role="tabpanel" id="{id}-panel-{item.id}" aria-labelledby="{id}-tab-{item.id}" hidden={selected !== item.id}>{@render panel(item.id)}</div>
{/each}
<style>
  .tabs { display: flex; gap: .15rem; padding: 0 .75rem; overflow-x: auto; border-bottom: 1px solid var(--border-subtle); background: var(--bg-raised); }
  .tabs button { position: relative; display: inline-flex; align-items: center; gap: .4rem; margin: 0; padding: .6rem .75rem; border: 0; border-radius: 0; background: none; color: var(--text-muted); font-size: inherit; font-weight: 650; box-shadow: none; white-space: nowrap; }
  .tabs button :global(svg) { color: var(--text-muted); }
  .tabs button:hover:not(:disabled), .tabs button[aria-selected="true"] { color: var(--action-hover); background: none; }
  .tabs button[aria-selected="true"] :global(svg) { color: var(--action-primary); }
  .tabs button[aria-selected="true"]::after { content: ''; position: absolute; inset: auto .5rem 0; height: 3px; border-radius: 3px 3px 0 0; background: var(--bar-fill); }
  .tabs button:focus-visible { outline-offset: -2px; }
  .n { padding: 0 .4rem; border-radius: 999px; background: var(--bg-secondary); color: var(--text-muted); font: 700 .72rem/1.45 var(--font-mono); }
  .tabs button[aria-selected="true"] .n, .n.blue { background: var(--selection-fill); color: var(--action-hover); }
  .tabs .n.warn { background: var(--warn-fill); color: var(--warn-text); }
  .tabs .n.ok { background: var(--ok-fill); color: var(--ok-text); }
  .tabs .n.alert { background: var(--alert-fill); color: var(--alert-text); }
  [role="tabpanel"][hidden] { display: none; }
</style>
