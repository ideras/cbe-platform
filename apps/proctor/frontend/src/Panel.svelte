<script>
  // A raised card with a soft shadow and an optional heading band, the dashboard's basic container.
  // `label` makes it a named region (aria-label); `tone` adds a colored edge for notices (info, warning, danger).
  let { title, label, tone = '', level = 2, titleId, class: extra = '', actions, children, ...rest } = $props();
</script>

{#snippet content()}
  {#if title || actions}
    <header class="panel-head">
      {#if title}
        {#if level === 1}<h1 class="panel-title" id={titleId}>{title}</h1>
        {:else if level === 3}<h3 class="panel-title" id={titleId}>{title}</h3>
        {:else if level === 4}<h4 class="panel-title" id={titleId}>{title}</h4>
        {:else}<h2 class="panel-title" id={titleId}>{title}</h2>{/if}
      {/if}
      {#if actions}<div class="panel-actions">{@render actions()}</div>{/if}
    </header>
  {/if}
  <div class="panel-body">{@render children?.()}</div>
{/snippet}

{#if label}
  <section class="panel {tone} {extra}" aria-label={label} {...rest}>{@render content()}</section>
{:else}
  <div class="panel {tone} {extra}" {...rest}>{@render content()}</div>
{/if}

<style>
  /* A titled panel has the secondary (ice-blue) header band; a tone adds a colored left edge. */
  .panel { min-width: 0; overflow: hidden; background: var(--bg-raised); border: 1px solid var(--border-subtle); border-radius: var(--radius-card); box-shadow: var(--shadow-card); }
  .panel.info { border-left: 4px solid var(--action-primary); }
  .panel.warning { border-left: 4px solid var(--warn); }
  .panel.danger { border-left: 4px solid var(--alert); }
  .panel-head { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: var(--space-2) var(--space-3); padding: .65rem 1.25rem; background: var(--header-soft); border-bottom: 1px solid var(--cyan-border); }
  .panel-title { margin: 0; color: var(--action-hover); font-size: 1.1rem; font-weight: 700; line-height: 1.3; overflow-wrap: anywhere; }
  .panel-actions { display: flex; flex-wrap: wrap; align-items: center; gap: var(--space-2); }
  .panel-body { padding: var(--space-4) 1.25rem; }
  .panel-body > :global(:first-child) { margin-top: 0; }
  .panel-body > :global(:last-child) { margin-bottom: 0; }
</style>
