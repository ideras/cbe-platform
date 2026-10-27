<script>
  // A white card with a soft shadow and an optional heading, the dashboard's basic container.
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
  .panel { min-width: 0; padding: var(--space-4) var(--space-5); background: var(--color-surface); border: 1px solid var(--panel-border); border-radius: var(--radius-card); box-shadow: none; }
  .panel.info { border-left: 4px solid var(--sea-ink); }
  .panel.warning { border-left: 4px solid var(--color-warning-border); }
  .panel.danger { border-left: 4px solid var(--color-danger); }
  .panel-head { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: var(--space-2) var(--space-3); margin-bottom: var(--space-3); }
  .panel-title { margin: 0; color: var(--title-ink); font-size: 1.15rem; font-weight: 700; line-height: 1.3; overflow-wrap: anywhere; }
  .panel-actions { display: flex; flex-wrap: wrap; align-items: center; gap: var(--space-2); }
  .panel-body > :global(:first-child) { margin-top: 0; }
  .panel-body > :global(:last-child) { margin-bottom: 0; }
</style>
