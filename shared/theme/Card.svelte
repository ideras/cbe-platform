<script>
  // Reusable card: a colored title band over a raised body inside a thin border.
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
  /* Tones: deep is the primary navy header, info the secondary ice-blue one; success, warning and danger use the
   * status fills, with a colored left edge on the card for warning and danger. */
  .card {
    --band: var(--header-soft);
    --band-ink: var(--action-hover);
    --band-border: var(--cyan-border);
    background: var(--bg-raised); border: 1px solid var(--border-subtle);
    border-radius: var(--radius); box-shadow: var(--shadow-card); overflow: hidden;
  }
  .deep { --band: var(--header-strong); --band-ink: var(--on-dark); --band-border: var(--transparent); }
  .success { --band: var(--ok-fill); --band-ink: var(--ok-text); --band-border: var(--ok-border); }
  .warning { --band: var(--warn-fill); --band-ink: var(--warn-text); --band-border: var(--warn-border); border-left: 4px solid var(--warn); }
  .danger { --band: var(--alert-fill); --band-ink: var(--alert-text); --band-border: var(--alert-border); border-left: 4px solid var(--alert); }
  .card-head {
    display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: var(--space-2) var(--space-3);
    padding: .65rem var(--space-4); background: var(--band); color: var(--band-ink);
    border-bottom: 1px solid var(--band-border);
  }
  .card-title { margin: 0; color: inherit; font-size: var(--text-base); font-weight: 600; line-height: 1.3; letter-spacing: .01em; overflow-wrap: anywhere; }
  .card-actions { display: flex; flex-wrap: wrap; gap: var(--space-2); }
  .card-actions :global(button:not(:disabled)) { background: var(--bg-raised); border-color: var(--border-subtle); color: var(--text-main); }
  .card-actions :global(button:hover:not(:disabled)) { background: var(--selection-fill); border-color: var(--action-primary); color: var(--action-hover); }
  .deep .card-head :global(:focus-visible) { outline-color: var(--accent-cyan-soft); }
  .card-body { padding: var(--space-4) 1.1rem; }
  .card-body > :global(:first-child) { margin-top: 0; }
  .card-body > :global(:last-child) { margin-bottom: 0; }
</style>
