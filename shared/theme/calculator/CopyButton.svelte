<script>
  let { value, label = 'Copy answer' } = $props();
  let host;
  let message = $state('');
  $effect(() => { value; message = ''; });

  // The desktop WebView may not expose the secure-context clipboard API. The
  // selection-based fallback keeps copying inside the calculator and restores focus.
  async function copy() {
    const text = String(value);
    try {
      try {
        if (!navigator.clipboard?.writeText) throw Error('Clipboard API unavailable');
        await navigator.clipboard.writeText(text);
      } catch {
        const focused = document.activeElement;
        const field = document.createElement('textarea');
        field.value = text;
        field.setAttribute('aria-label', 'Answer to copy');
        field.style.cssText = 'position:fixed;opacity:0;pointer-events:none;';
        host.append(field);
        try {
          field.select();
          if (!document.execCommand('copy')) throw Error('Copy failed');
        } finally { field.remove(); focused?.focus(); }
      }
      message = 'Copied';
    } catch { message = 'Could not copy. Select the answer and copy it manually.'; }
  }
</script>

<span class="copy" bind:this={host}>
  <button type="button" aria-label={label} title={label} onclick={copy}>
    <!-- Lucide copy (ISC), bundled locally; notices ship in public/lucide-LICENSE.txt. -->
    <svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor"
      stroke-linecap="round" stroke-linejoin="round" stroke-width="2" aria-hidden="true" focusable="false">
      <rect width="14" height="14" x="8" y="8" rx="2" ry="2" />
      <path d="M4 16c-1.1 0-2-.9-2-2V4c0-1.1.9-2 2-2h10c1.1 0 2 .9 2 2" />
    </svg>
  </button>
  {#if message}<span class="feedback" role="status">{message}</span>{/if}
</span>

<style>
  .copy { display: inline-flex; flex-wrap: nowrap; min-width: 0; max-width: 100%; align-items: center; align-self: center; vertical-align: middle; gap: var(--space-1); margin-left: var(--space-2); font-family: var(--font-ui, sans-serif); font-size: var(--text-xs); }
  .feedback { display: inline-flex; align-items: center; min-height: 28px; margin: 0; padding: .1rem .4rem; border-left-width: 1px; line-height: 1.25; }
  button { display: inline-flex; align-items: center; justify-content: center; min-width: 28px; min-height: 28px; margin: 0; padding: .1rem .4rem; font-size: var(--text-xs); }
</style>
