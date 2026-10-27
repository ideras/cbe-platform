<script>
  import { onMount } from 'svelte';
  import * as api from '$proctor-api';
  // A small protected screenshot that opens the image window, which can step through every capture of the same focus loss.
  let { image, set, index } = $props();
  let data = $state('');
  let error = $state('');
  let alive = true;
  async function load() {
    error = '';
    try { const value = await api.Image(image.screenshot_id, true); if (alive) data = value.data; }
    catch (err) { if (alive) error = String(err); }
  }
  async function open() {
    try { await api.OpenImage(set.map(item => item.screenshot_id), set.map(item => item.captured_at), index); }
    catch (err) { error = String(err); }
  }
  onMount(() => { load(); return () => { alive = false; data = ''; }; });
</script>
<button type="button" class="thumb" title="Screenshot #{image.screenshot_id} · {image.width}×{image.height}" aria-label="Open screenshot #{image.screenshot_id}" onclick={open}>
  {#if data}<img src={data} alt="Thumbnail of screenshot #{image.screenshot_id}" />{:else if error}<span class="failed">!</span>{:else}<span class="loading">…</span>{/if}
</button>
{#if error}<span class="error" role="alert">Image unavailable: {error}</span>{/if}
<style>
  .thumb { display: inline-grid; place-items: center; width: 4.5rem; height: 2.8rem; min-height: 0; margin: 0; padding: 0; overflow: hidden; background: var(--surface); border: 1px solid var(--line); border-radius: var(--radius-sm); }
  .thumb:hover:not(:disabled) { border-color: var(--brand); }
  img { display: block; width: 100%; height: 100%; object-fit: cover; }
  .failed { color: var(--color-danger); font-weight: 700; }
  .loading { color: var(--color-text-muted); }
  .error { display: block; color: var(--color-danger); font-size: var(--text-sm); }
</style>
