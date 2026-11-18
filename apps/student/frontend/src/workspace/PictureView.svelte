<script>
  import { tick, untrack } from 'svelte';
  import { looksLikeSvg, svgDataUri, FIT, fitScale, stepPictureZoom, zoomLabel } from './picture.js';

  // An SVG file shown as the picture it draws (docs/workspace/student-ide.md, "Editor"): what Graphviz makes of an automaton, for one.
  // The picture is an <img> from a data: address and nothing else, never the SVG put into the page: the file comes from the student's
  // container, and an image runs no script and loads nothing from elsewhere. It starts at Fit; the zoom steps are of the picture's own
  // size, and a picture larger than the room is moved with the scroll bars, the arrow keys or the wheel. Nothing is written anywhere.
  //
  // A file that is being written while it is read (dot writes it, the IDE reads it about 0.3 s after the change) is a picture that cannot
  // be drawn for a moment: that is a short notice, never an error, and the next change of the file draws it again.
  let { name, text, onSource } = $props();
  let region = $state(null);
  let roomWidth = $state(0);
  let roomHeight = $state(0);
  let zoom = $state(FIT);
  const GAP = 12; // px around the picture

  const svg = $derived(looksLikeSvg(text));
  const address = $derived(svg ? svgDataUri(text) : '');
  // The picture shown is the last one that could be drawn: a new text is tried first in an image of its own, off the page, so the one on
  // the page changes only when the new one is ready (no blank moment, no broken image) and keeps its zoom and its place.
  let drawn = $state(null); // { address, width, height }
  let failed = $state(''); // the address that could not be drawn
  $effect(() => {
    const next = address;
    if (!next || untrack(() => drawn?.address) === next) return;
    let current = true;
    const probe = new Image();
    probe.onload = () => {
      if (!current) return;
      // An SVG with no size and no viewBox has none to draw at.
      if (probe.naturalWidth > 0 && probe.naturalHeight > 0) { drawn = { address: next, width: probe.naturalWidth, height: probe.naturalHeight }; failed = ''; }
      else failed = next;
    };
    probe.onerror = () => { if (current) failed = next; };
    probe.src = next;
    return () => { current = false; probe.onload = null; probe.onerror = null; };
  });
  const broken = $derived(!svg || failed === address);
  const shown = $derived(broken ? null : drawn);

  const scale = $derived(shown ? (zoom === FIT ? fitScale(shown.width, shown.height, roomWidth - 2 * GAP, roomHeight - 2 * GAP) : zoom) : 1);
  const width = $derived(shown ? Math.max(1, Math.floor(shown.width * scale)) : 0);
  const height = $derived(shown ? Math.max(1, Math.floor(shown.height * scale)) : 0);
  const canStep = delta => Boolean(shown) && stepPictureZoom(zoom, scale, delta) !== zoom;

  // A change of the zoom keeps the point under the anchor (the middle of the room, or the mouse) where it was.
  async function zoomTo(next, anchor) {
    if (!shown || next === zoom || !region) return;
    const x = anchor?.x ?? region.clientWidth / 2;
    const y = anchor?.y ?? region.clientHeight / 2;
    const across = (region.scrollLeft + x) / region.scrollWidth;
    const down = (region.scrollTop + y) / region.scrollHeight;
    zoom = next;
    await tick();
    region.scrollLeft = across * region.scrollWidth - x;
    region.scrollTop = down * region.scrollHeight - y;
  }
  const step = (delta, anchor) => zoomTo(stepPictureZoom(zoom, scale, delta), anchor);

  // + − and 0 when the picture or its buttons have the keyboard, with Ctrl too (which would otherwise zoom the whole window).
  function keys(event) {
    if (event.altKey) return;
    const delta = { '+': 1, '=': 1, '-': -1, '−': -1, '_': -1, '0': 0 }[event.key];
    if (delta === undefined) return;
    event.preventDefault();
    step(delta);
  }

  // Ctrl and the wheel zoom about the mouse, as in a picture viewer. A trackpad sends many small steps: they are gathered into one.
  $effect(() => {
    const node = region;
    if (!node) return;
    let gathered = 0;
    const wheel = event => {
      if (!event.ctrlKey) return;
      event.preventDefault();
      gathered += event.deltaMode === 0 ? event.deltaY : Math.sign(event.deltaY) * 100;
      if (Math.abs(gathered) < 50) return;
      const box = node.getBoundingClientRect();
      step(gathered < 0 ? 1 : -1, { x: event.clientX - box.left, y: event.clientY - box.top });
      gathered = 0;
    };
    node.addEventListener('wheel', wheel, { passive: false });
    return () => node.removeEventListener('wheel', wheel);
  });

  export function focus() { region?.focus(); }
</script>
<!-- svelte-ignore a11y_no_static_element_interactions -->
<div class="picture" onkeydown={keys}>
  <!-- The names start with what is drawn on the buttons (WCAG's "label in name"). A button at the end of the steps says so and keeps the focus. -->
  <div class="bar" role="group" aria-label="Picture zoom">
    <button type="button" aria-pressed={zoom === FIT} aria-disabled={!shown} title="The whole picture in view (0)" onclick={() => zoomTo(FIT)}>Fit</button>
    <button type="button" aria-label="− Zoom out" aria-disabled={!canStep(-1)} title="Zoom out (−)" onclick={() => step(-1)}>−</button>
    <button type="button" aria-label="+ Zoom in" aria-disabled={!canStep(1)} title="Zoom in (+)" onclick={() => step(1)}>+</button>
    <button type="button" aria-label="100 % Actual size" aria-disabled={!shown || (zoom !== FIT && scale === 1)} title="The picture at its own size" onclick={() => zoomTo(1)}>100 %</button>
    {#if shown}<span class="level">{zoomLabel(scale)}</span>{/if}
  </div>
  <!-- The region takes the keyboard so that the arrow keys scroll a picture larger than the room (a scrolling region a keyboard cannot
       reach is an accessibility fault of its own). -->
  <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
  <div class="region" role="region" aria-label="Picture of {name}" tabindex="0" style:padding="{GAP}px" bind:this={region} bind:clientWidth={roomWidth} bind:clientHeight={roomHeight}>
    {#if broken}
      <div class="cannot" role="status">
        <p>This file cannot be drawn as a picture yet.</p>
        <button type="button" onclick={() => onSource?.()}>Show the source</button>
      </div>
    {:else if shown}
      <img src={shown.address} alt="Picture drawn by {name}" draggable="false" style:width="{width}px" style:height="{height}px" />
    {/if}
  </div>
</div>
<style>
  .picture { display: flex; flex-direction: column; min-height: 0; min-width: 0; height: 100%; background: var(--bg-raised); }
  .bar { display: flex; flex: none; align-items: stretch; gap: 1px; border-bottom: 1px solid var(--border-subtle); background: var(--bg-raised); }
  .bar button { margin: 0; padding: .2rem .6rem; border: 0; border-radius: 0; background: var(--transparent); box-shadow: none; color: var(--text-main); font-size: var(--text-sm); font-weight: 700; white-space: nowrap; }
  .bar button[aria-pressed="true"] { background: var(--selection-fill); }
  .bar button[aria-disabled="true"] { opacity: .45; cursor: default; }
  .bar button[aria-disabled="true"]:hover { background: var(--transparent); border-color: var(--transparent); color: var(--text-main); }
  .bar button:focus-visible { outline: 2px solid var(--action-primary); outline-offset: -2px; }
  .level { align-self: center; margin-left: auto; padding: 0 .75rem; color: var(--text-main); font-family: var(--font-mono); font-size: var(--text-sm); white-space: nowrap; }
  /* The picture is centred while it is smaller than the room (the auto margins), and scrolls from its edges when it is larger. */
  .region { display: flex; flex: 1 1 0; min-height: 0; overflow: auto; background: var(--bg-secondary); }
  .region:focus-visible { outline: 2px solid var(--action-primary); outline-offset: -2px; }
  /* A white ground, as the exam's pictures have (--figure-bg): a drawing in black on nothing would sit on the page's ground otherwise. Not
     draggable, so the picture is not dragged out of the window or into a terminal. */
  img { display: block; flex: none; max-width: none; margin: auto; background: var(--figure-bg); box-shadow: 0 0 0 1px var(--border-subtle); -webkit-user-drag: none; user-select: none; }
  .cannot { display: flex; flex-direction: column; align-items: center; gap: var(--space-2); margin: auto; color: var(--text-main); text-align: center; }
  .cannot p { margin: 0; }
  .cannot button { margin: 0; }
</style>
