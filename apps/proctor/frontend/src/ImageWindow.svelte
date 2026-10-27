<script>
  import { onMount } from 'svelte';
  import * as api from '$proctor-api';
  // A protected screenshot in a window of its own, with zoom, panning, and the other captures of the same focus loss.
  // Saving writes the server's original file; that copy is outside the protected store and outlives a purge.
  let { ids, times = [], start = 0 } = $props();
  let index = $state(0);
  let data = $state('');
  let natural = $state({ width: 0, height: 0 });
  let zoom = $state('fit');
  let error = $state('');
  let status = $state('');
  let busy = $state(false);
  let viewport;
  let drag = null;
  let epoch = 0;
  let alive = true;
  const steps = [0.1, 0.25, 0.5, 0.75, 1, 1.5, 2, 3, 4, 6, 8];
  const id = $derived(ids[index]);
  // The strip of the whole series under the image, with the current one marked.
  let thumbs = $state([]);
  let thumbsLoaded = false;
  const taken = $derived(times[index] ? new Date(times[index]).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' }) : '');

  async function show(at) {
    const mine = ++epoch;
    index = at; data = ''; error = ''; status = ''; busy = true;
    try { const value = await api.Image(ids[at], false); if (alive && mine === epoch) data = value.data; }
    catch (err) { if (alive && mine === epoch) error = `Image unavailable: ${String(err)}`; }
    finally { if (alive && mine === epoch) busy = false; }
  }
  function current() {
    if (zoom !== 'fit' || !viewport || !natural.width) return zoom === 'fit' ? 1 : zoom;
    return Math.min(viewport.clientWidth / natural.width, viewport.clientHeight / natural.height, 1);
  }
  function step(direction) {
    const now = current();
    const next = direction > 0 ? steps.find(value => value > now + 1e-6) : [...steps].reverse().find(value => value < now - 1e-6);
    if (next) zoom = next;
  }
  async function save() {
    status = ''; error = '';
    try { const path = await api.SaveImage(id); if (path) status = `Saved to ${path}. This copy is not removed by purging the exam's screenshots; delete it when grading is final.`; }
    catch (err) { error = String(err); }
  }
  function key(event) {
    if (event.target instanceof HTMLInputElement) return;
    if (event.key === 'ArrowLeft' && index > 0) show(index - 1);
    else if (event.key === 'ArrowRight' && index < ids.length - 1) show(index + 1);
    else if (event.key === '+' || event.key === '=') step(1);
    else if (event.key === '-') step(-1);
    else if (event.key === '0') zoom = 'fit';
    else if (event.key === '1') zoom = 1;
    else return;
    event.preventDefault();
  }
  function wheel(event) {
    if (!event.ctrlKey) return;
    event.preventDefault();
    step(event.deltaY < 0 ? 1 : -1);
  }
  function down(event) {
    if (zoom === 'fit' || event.button !== 0) return;
    drag = { x: event.clientX, y: event.clientY, left: viewport.scrollLeft, top: viewport.scrollTop };
    viewport.setPointerCapture(event.pointerId);
  }
  function move(event) {
    if (!drag) return;
    viewport.scrollLeft = drag.left - (event.clientX - drag.x);
    viewport.scrollTop = drag.top - (event.clientY - drag.y);
  }
  async function loadThumbs() {
    if (thumbsLoaded) return;
    thumbsLoaded = true;
    for (let at = 0; at < ids.length; at++) {
      if (!alive) return;
      try {
        const value = await api.Image(ids[at], true);
        if (!alive) return;
        thumbs = [...thumbs, { id: ids[at], data: value.data }];
      } catch { thumbs = [...thumbs, { id: ids[at], data: '' }]; }
    }
  }
  onMount(() => {
    show(Math.min(Math.max(start, 0), ids.length - 1));
    loadThumbs();
    return () => { alive = false; data = ''; };
  });
</script>

<svelte:window onkeydown={key} />
<div class="image-window">
  <header class="toolbar" aria-label="Screenshot controls">
    <strong>Screenshot #{id}</strong>
    {#if ids.length > 1}
      <span class="group"><button disabled={index === 0} onclick={() => show(index - 1)} aria-label="Previous screenshot">← Previous</button>
        <span class="muted">{index + 1} of {ids.length}{taken ? ` · ${taken}` : ''}</span>
        <button disabled={index === ids.length - 1} onclick={() => show(index + 1)} aria-label="Next screenshot">Next →</button></span>
    {/if}
    <span class="group">
      <button onclick={() => step(-1)} aria-label="Zoom out">−</button>
      <span class="zoom" aria-live="polite">{zoom === 'fit' ? 'Fit' : `${Math.round(zoom * 100)}%`}</span>
      <button onclick={() => step(1)} aria-label="Zoom in">+</button>
      <button aria-pressed={zoom === 'fit'} onclick={() => zoom = 'fit'}>Fit</button>
      <button aria-pressed={zoom === 1} onclick={() => zoom = 1}>100%</button>
    </span>
    <button class="primary" disabled={!data} onclick={save}>Save as…</button>
  </header>
  {#if error}<p role="alert">{error}</p>{/if}
  {#if status}<p role="status">{status}</p>{/if}
  <div class="viewport" role="region" aria-label="Screenshot, drag to pan when zoomed" tabindex="-1" class:fit={zoom === 'fit'} class:pannable={zoom !== 'fit'} bind:this={viewport} onwheel={wheel} onpointerdown={down} onpointermove={move} onpointerup={() => drag = null} onpointercancel={() => drag = null}>
    {#if data}
      <img src={data} alt="Screenshot #{id}" draggable="false" style:width={zoom === 'fit' ? null : `${natural.width * zoom}px`}
        onload={event => natural = { width: event.currentTarget.naturalWidth, height: event.currentTarget.naturalHeight }} />
    {:else if busy}<p class="muted">Loading protected image…</p>{/if}
  </div>
  {#if ids.length > 1}
    <div class="strip" role="group" aria-label="Screenshots of this focus loss">
      {#each thumbs as thumb (thumb.id)}
        <button type="button" class="thumb" class:current={ids[index] === thumb.id} aria-label="Screenshot #{thumb.id}" aria-current={ids[index] === thumb.id ? 'true' : undefined} onclick={() => show(ids.indexOf(thumb.id))}>
          {#if thumb.data}<img src={thumb.data} alt="" />{:else}<span class="loading">…</span>{/if}
        </button>
      {/each}
    </div>
  {/if}
  <p class="muted note">{natural.width ? `${natural.width}×${natural.height} px · ` : ''}Server accepted time is not proven acquisition time. Keys: ← → captures, + − zoom, 0 fit, 1 actual size; Ctrl+wheel zooms, drag pans.</p>
</div>

<style>
  .image-window { display: flex; flex-direction: column; height: 100vh; box-sizing: border-box; padding: var(--space-2) var(--space-3); gap: var(--space-2); }
  .toolbar { display: flex; flex-wrap: wrap; align-items: center; gap: var(--space-2) var(--space-4); }
  .group { display: inline-flex; align-items: center; gap: var(--space-1); }
  .toolbar button { margin: 0; }
  .zoom { min-width: 3.5rem; text-align: center; font-variant-numeric: tabular-nums; }
  .viewport { flex: 1; min-height: 0; overflow: auto; background: var(--sidebar); border-radius: var(--radius-sm); }
  .viewport.fit { display: grid; place-items: center; overflow: hidden; }
  .viewport.fit img { max-width: 100%; max-height: 100%; }
  .viewport.pannable { cursor: grab; }
  .viewport.pannable:active { cursor: grabbing; }
  img { display: block; user-select: none; }
  .viewport p { color: var(--sidebar-ink); padding: var(--space-3); }
  p { margin: 0; }
  .note { font-size: var(--text-sm); }
  .strip { display: flex; gap: .25rem; overflow-x: auto; padding-bottom: .2rem; }
  .strip .thumb { flex: none; width: 5rem; height: 3rem; min-height: 0; margin: 0; padding: 0; overflow: hidden; background: var(--surface); border: 1px solid var(--line); border-radius: var(--radius-sm); }
  .strip .thumb img { display: block; width: 100%; height: 100%; object-fit: cover; }
  .strip .thumb.current { border-color: var(--ink); box-shadow: 0 0 0 2px var(--current-halo); }
  .strip .loading { color: var(--color-text-muted); }
</style>
