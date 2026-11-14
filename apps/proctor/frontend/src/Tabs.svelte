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
      {#if item.icon}<Icon name={item.icon} size={17} />{/if}{item.text}{#if item.count !== undefined}{' '}<span class="n">{item.count}</span>{/if}
    </button>
  {/each}
</div>
{#each tabs as item (item.id)}
  <div role="tabpanel" id="{id}-panel-{item.id}" aria-labelledby="{id}-tab-{item.id}" hidden={selected !== item.id}>{@render panel(item.id)}</div>
{/each}
<style>
  .tabs { display: inline-flex; max-width: 100%; flex-wrap: wrap; gap: 4px; margin: var(--space-3) 0; padding: 4px; border: 1px solid var(--border-subtle); border-radius: 12px; background: var(--bg-secondary); }
  .tabs button { position: relative; display: inline-flex; align-items: center; gap: .5rem; margin: 0; padding: .55rem 1rem; border: 0; border-radius: 9px; background: var(--transparent); color: var(--text-main); font-weight: 600; box-shadow: none; }
  .tabs button :global(svg) { color: var(--text-muted); }
  .tabs button:hover:not(:disabled) { color: var(--action-hover); }
  .tabs button[aria-selected="true"], .tabs button[aria-selected="true"]:hover:not(:disabled) { background: var(--bg-raised); color: var(--action-hover); box-shadow: var(--shadow-card); }
  .tabs button[aria-selected="true"] :global(svg) { color: var(--action-primary); }
  .tabs button[aria-selected="true"]::after { content: ''; position: absolute; left: 22%; right: 22%; bottom: 3px; height: 3px; border-radius: 3px; background: var(--bar-fill); }
  .n { padding: .02rem .45rem; border: 1px solid var(--border-subtle); border-radius: 999px; background: var(--bg-primary); color: var(--text-muted); font-family: var(--font-mono); font-size: .75rem; font-weight: 700; }
  .tabs button[aria-selected="true"] .n { background: var(--selection-fill); color: var(--action-hover); border-color: var(--action-border); }
  [role="tabpanel"][hidden] { display: none; }
</style>
