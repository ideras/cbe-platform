<script>
  // Reusable card: a colored title band over a white body inside a thin border.
  // `label` makes the card a named region (aria-label); without it the card is a plain container.
  // Plain {#if} branches (not <svelte:element>) keep the SVG namespace constant out of the bundles.
  let { title, label, tone = 'deep', level = 2, titleId, class: extra = '', actions, children, ...rest } = $props();
</script>

{#snippet content()}
  <header class="card-head">
    {#if level === 1}<h1 class="card-title" id={titleId}>{title}</h1>
    {:else if level === 3}<h3 class="card-title" id={titleId}>{title}</h3>
    {:else if level === 4}<h4 class="card-title" id={titleId}>{title}</h4>
    {:else}<h2 class="card-title" id={titleId}>{title}</h2>{/if}
    {#if actions}<div class="card-actions">{@render actions()}</div>{/if}
  </header>
  <div class="card-body">{@render children?.()}</div>
{/snippet}

{#if label}
  <section class="card {tone} {extra}" aria-label={label} {...rest}>{@render content()}</section>
{:else}
  <div class="card {tone} {extra}" {...rest}>{@render content()}</div>
{/if}

<style>
  .card {
    --band: var(--transparent);
    --band-ink: var(--ink);
    background: var(--color-surface); border: 1px solid var(--color-border);
    border-radius: var(--radius-md); box-shadow: none; overflow: hidden;
  }
  .info, .success { --band: var(--sea); --band-ink: var(--sea-ink); }
  .warning { --band: var(--transparent); --band-ink: var(--sand-ink); }
  .danger { --band: var(--transparent); --band-ink: var(--coral); }
  .card-head {
    display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: var(--space-2) var(--space-3);
    padding: .55rem var(--space-4); background: var(--band); color: var(--band-ink);
    border-bottom: 1px solid var(--line);
  }
  .card-title { margin: 0; color: inherit; font-size: var(--text-base); font-weight: 600; line-height: 1.3; letter-spacing: .01em; overflow-wrap: anywhere; }
  .card-actions { display: flex; flex-wrap: wrap; gap: var(--space-2); }
  .card-actions :global(button:not(:disabled)) { background: var(--surface); border-color: var(--ink-2); color: var(--ink); }
  .card-actions :global(button:hover:not(:disabled)) { background: var(--hover); border-color: var(--ink-2); }
  .card-head :global(:focus-visible) { outline-color: var(--ocean); }
  .card-body { padding: var(--space-4); }
  .card-body > :global(:first-child) { margin-top: 0; }
  .card-body > :global(:last-child) { margin-bottom: 0; }
</style>
