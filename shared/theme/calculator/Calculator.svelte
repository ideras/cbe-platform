<script>
  // The calculator, an in-window overlay of the host application (plan §3, amended 2026-10-23: the component
  // is embeddable — the student application hosts it during the exam, and the proctor dashboard embeds it in
  // the exam preview dialog). It never opens a window or a native dialog, and its keys and paste/drop events
  // stay inside the modal. Everything it computes goes through the injected `api` object — the host's Wails
  // bindings — so the same component runs on both apps and neither computes the allowed set itself. The
  // optional CalcOpened/CalcClosed/CalcModeChanged of the host are the student's session logging (D12); a host
  // without them, like the preview, simply omits them. `note` is the permanent line: the student's recording
  // notice, or the preview's nothing-is-recorded line.
  import { tick, untrack } from 'svelte';
  import { calcMessage, calcHint } from './calc-messages.js';
  import CopyButton from './CopyButton.svelte';
  import Programmer from './Programmer.svelte';

  let { open = false, angleSetting = 'user', modes = ['scientific'], allowed = [], disabledNames = [], history = [], note = '', floating = false, api, onEvaluated, onClose } = $props();
  // floating renders the calculator as a non-modal dialog of the host window (owner decision 2026-10-23): its
  // own close button and Escape work, but nothing else of the host is blocked.

  let position = $state(null);
  let drag = null;
  let input = $state('');
  let result = $state(null); // { display } on success, { message, hint } on failure, { error } when Go refused
  let angle = $state('deg');
  let dialog = $state(null);
  let opener = $state(null); // the button that opened the modal, for the focus to return to
  let activeMode = $state('scientific');
  let fixedAngle = $derived(angleSetting !== 'user');
  const MAX_INPUT = 200;
  const modeLabels = { scientific: 'Scientific', neural: 'Neural', programmer: 'Programmer' };
  // Scientific and neural mode share one evaluator and one panel (plan Phase 7, owner 2026-10-25): the panel is
  // named scientific when the exam enables scientific mode and neural when it enables neural alone, as Go names
  // it in the log (calc.ScientificPanel). Programmer mode is its own panel.
  const neural = $derived(modes.includes('neural'));
  const panels = $derived([
    ...(modes.includes('scientific') ? ['scientific'] : neural ? ['neural'] : []),
    ...(modes.includes('programmer') ? ['programmer'] : []),
  ]);
  const scientificPanel = $derived(activeMode === 'scientific' || activeMode === 'neural');
  // The angle choice starts at the exam's setting and follows it if the exam ever redefines it; the student's
  // toggle overrides it until then. The effect tracks angleSetting only, so the toggle survives it.
  $effect(() => { angle = angleSetting === 'rad' ? 'rad' : 'deg'; });
  // The active panel starts on, and stays within, the panels the exam enables.
  $effect(() => { if (!panels.includes(activeMode)) activeMode = panels[0] ?? 'scientific'; });

  // Match the programmer pad's seven columns: three number columns on the left, symbols on the right.
  // Explicit cells keep the number rows aligned when factorial or neural brackets are hidden.
  // A disallowed function stays hidden; typing it still answers the standard UNKNOWN_FUNCTION words.
  const pad = [
    { label: '7' }, { label: '8' }, { label: '9' }, { label: '÷', insert: '/' },
    { label: '(' }, { label: ')' }, { label: '!', name: 'factorial' },
    { label: '4' }, { label: '5' }, { label: '6' }, { label: '×', insert: '*' },
    { label: '^' }, { label: 'π', insert: 'pi' }, { label: 'e' },
    { label: '1' }, { label: '2' }, { label: '3' }, { label: 'Ans', insert: 'ans' },
    { label: ',' }, { label: '[', neural: true, aria: 'Open bracket' }, { label: ']', neural: true, aria: 'Close bracket' },
    { label: '0' }, { label: '.' },
  ];
  const functions = [
    { name: 'log10', label: 'log₁₀' }, { name: 'ln', label: 'ln' }, { name: 'log', label: 'log' },
    { name: 'sqrt', label: '√' }, { name: 'cbrt', label: '∛' }, { name: 'exp', label: 'eˣ' },
    { name: 'abs', label: '|x|' }, { name: 'floor', label: '⌊x⌋' }, { name: 'ceil', label: '⌈x⌉' },
    { name: 'round', label: 'round' }, { name: 'mod', label: 'mod' },
    { name: 'sin', label: 'sin' }, { name: 'cos', label: 'cos' }, { name: 'tan', label: 'tan' },
    { name: 'asin', label: 'sin⁻¹' }, { name: 'acos', label: 'cos⁻¹' }, { name: 'atan', label: 'tan⁻¹' },
    { name: 'atan2', label: 'atan₂' },
    { name: 'sinh', label: 'sinh' }, { name: 'cosh', label: 'cosh' }, { name: 'tanh', label: 'tanh' },
  ];
  // Neural mode's functions (Phase 7), each with its help text; bce's says it uses the natural log.
  const neuralFunctions = [
    { name: 'sigmoid', help: 'sigmoid(x), elementwise' }, { name: 'relu', help: 'relu(x), elementwise' },
    { name: 'leaky_relu', help: 'leaky_relu(x, a): slope a below 0, 0.01 when a is left out' },
    { name: 'dsigmoid', help: 'dsigmoid(x): the sigmoid’s derivative' }, { name: 'dtanh', help: 'dtanh(x): the tanh derivative' },
    { name: 'drelu', help: 'drelu(x): the ReLU derivative, 0 at 0' }, { name: 'dleaky_relu', help: 'dleaky_relu(x, a): the leaky ReLU derivative' },
    { name: 'softmax', help: 'softmax(v), of a vector' }, { name: 'sum', help: 'sum of all elements' }, { name: 'mean', help: 'mean of all elements' },
    { name: 'max', help: 'largest element' }, { name: 'min', help: 'smallest element' },
    { name: 'argmax', help: 'argmax(v): index of the largest element, from 0' }, { name: 'len', help: 'len(v): number of elements' },
    { name: 'norm', help: 'norm(v): length (L2 norm)' }, { name: 'dot', help: 'dot(a, b): dot product' },
    { name: 'matmul', help: 'matmul(A, B): matrix product; * is elementwise' }, { name: 'transpose', help: 'transpose(A), of a matrix' },
    { name: 'mse', help: 'mse(p, y): mean squared error' }, { name: 'bce', help: 'bce(p, y): binary cross-entropy, with the natural log (ln)' },
  ];

  $effect(() => {
    if (!open) return;
    position = null;
    drag = null;
    opener = document.activeElement;
    document.addEventListener('keydown', trap, true);
    tick().then(() => dialog?.querySelector('input')?.focus());
    return () => document.removeEventListener('keydown', trap, true);
  });

  // The usage events (D12): opening and closing are recorded by Go, with how long the modal was open. A lock
  // or the exam's end unmounts the modal, and the cleanup records the close even then. The mode is read
  // untracked: switching tabs later must not close and reopen the log.
  let openedAt = 0;
  $effect(() => {
    if (!open) return;
    openedAt = Date.now();
    const mode = untrack(() => activeMode);
    api.CalcOpened?.(mode)?.catch?.(() => {});
    return () => api.CalcClosed?.(mode, Math.max(0, Date.now() - openedAt))?.catch?.(() => {});
  });

  function switchMode(mode) {
    if (mode === activeMode) return;
    api.CalcModeChanged?.(activeMode, mode)?.catch?.(() => {});
    result = null;
    activeMode = mode;
    tick().then(() => dialog?.querySelector('input')?.focus());
  }

  // Capture phase: Escape closes from anywhere in the modal, Tab stays in it, and nothing reaches the page.
  function trap(event) {
    if (!dialog?.contains(event.target)) return;
    if (event.key === 'Escape') {
      event.preventDefault();
      event.stopPropagation();
      close();
      return;
    }
    if (event.key === 'Tab') {
      event.stopPropagation();
      const focusable = [...dialog.querySelectorAll('button:not(:disabled), input, [tabindex="0"]')].filter(node => node.getClientRects().length);
      if (!focusable.length) return;
      const index = focusable.indexOf(document.activeElement);
      event.preventDefault();
      focusable[event.shiftKey ? (index <= 0 ? focusable.length - 1 : index - 1) : (index === focusable.length - 1 || index < 0 ? 0 : index + 1)].focus();
    }
  }

  // Bubble phase: the keys a modal uses keep their default (typing, Backspace) but never leave the modal.
  function stopAll(event) { event.stopPropagation(); }

  function insert(text) {
    if (!open) return;
    if (input.length + text.length > MAX_INPUT) {
      result = { message: calcMessage('TOO_LONG'), hint: '' };
      return;
    }
    input += text;
    result = null;
    dialog?.querySelector('input')?.focus();
  }

  function backspace() {
    input = input.slice(0, -1);
    result = null;
    dialog?.querySelector('input')?.focus();
  }

  async function evaluate() {
    if (!open || !input.trim()) return;
    try {
      const answer = await api.CalcEvaluate(input, angle);
      if (answer.ok) {
        result = { display: answer.display, rows: answer.rows ?? null };
        onEvaluated?.({ expression: input, ok: true, display: answer.display, mode: activeMode, angle });
      } else {
        result = { message: calcMessage(answer.code), hint: calcHint(input, answer.code) };
        onEvaluated?.({ expression: input, ok: false, code: answer.code, message: result.message, mode: activeMode, angle });
      }
    } catch (problem) {
      // A Go error means the calculator itself is unavailable or the log queue is full (D12).
      result = { error: String(problem).replace(/^Error:\s*/, '') };
    }
    dialog?.querySelector('input')?.focus();
  }

  function toggleAngle() {
    angle = angle === 'deg' ? 'rad' : 'deg';
  }

  function close() {
    result = null;
    onClose?.();
    opener?.focus?.();
  }

  function moveTo(x, y) {
    if (!dialog) return;
    const rect = dialog.getBoundingClientRect();
    position = {
      x: Math.max(0, Math.min(x, window.innerWidth - rect.width)),
      y: Math.max(0, Math.min(y, window.innerHeight - rect.height)),
    };
  }
  function startDrag(event) {
    if (event.button !== 0) return;
    const rect = dialog.getBoundingClientRect();
    drag = { id: event.pointerId, x: event.clientX - rect.left, y: event.clientY - rect.top };
    event.currentTarget.setPointerCapture(event.pointerId);
    event.currentTarget.focus();
    event.preventDefault();
  }
  function moveDrag(event) {
    if (drag?.id === event.pointerId) moveTo(event.clientX - drag.x, event.clientY - drag.y);
  }
  function moveKey(event) {
    const delta = { ArrowLeft: [-1, 0], ArrowRight: [1, 0], ArrowUp: [0, -1], ArrowDown: [0, 1] }[event.key];
    if (!delta) return;
    event.preventDefault();
    const rect = dialog.getBoundingClientRect();
    const step = event.shiftKey ? 40 : 10;
    moveTo(rect.left + delta[0] * step, rect.top + delta[1] * step);
  }
  $effect(() => {
    if (!dialog) return;
    const clamp = () => untrack(() => { if (position) moveTo(position.x, position.y); });
    const observer = new ResizeObserver(clamp);
    observer.observe(dialog);
    window.addEventListener('resize', clamp);
    return () => { observer.disconnect(); window.removeEventListener('resize', clamp); };
  });

  const stop = event => event.preventDefault();
</script>

{#snippet calcBody()}
      <div class="calc-head">
        <button type="button" class="calc-move" aria-label="Move calculator" title="Drag to move, or use the arrow keys when focused"
          onpointerdown={startDrag} onpointermove={moveDrag} onpointerup={() => drag = null}
          onpointercancel={() => drag = null} onlostpointercapture={() => drag = null} onkeydown={moveKey}>⠿ Calculator</button>
        <button type="button" class="calc-close" aria-label="Close the calculator" onclick={close}>✕</button>
      </div>
      {#if panels.length > 1}
      <div class="calc-head">
          <span class="calc-tabs" role="tablist" aria-label="Calculator mode">
            {#each panels as mode (mode)}
              <button type="button" role="tab" aria-selected={activeMode === mode} onclick={() => switchMode(mode)}>{modeLabels[mode] ?? mode}</button>
            {/each}
          </span>
      </div>
      {/if}
      {#if scientificPanel}
        {#if activeMode === 'scientific'}
          <!-- The angle mode matters only to the trigonometric functions, which neural mode alone does not have. -->
          <div class="calc-head">
            <span class="calc-angle" aria-label="Angle mode">
              <strong>{angle === 'deg' ? 'DEG' : 'RAD'}</strong>
              {#if !fixedAngle}
                <button type="button" onclick={toggleAngle} aria-label="Switch the angle mode to {angle === 'deg' ? 'radians' : 'degrees'}">deg/rad</button>
              {/if}
            </span>
          </div>
        {/if}
        <input class="calc-input" bind:value={input} maxlength={MAX_INPUT} spellcheck="false" autocomplete="off"
          aria-label="Expression" placeholder="Type an expression" onkeydown={event => { if (event.key === 'Enter') { event.preventDefault(); evaluate(); } }} />
        <div class="calc-output">
        <p class="calc-result" aria-live="polite">
          {#if result?.error}<span class="error">{result.error}</span>
          {:else if result?.message}<span class="error">{result.message}</span>
            {#if result.hint}<span class="hint">{result.hint}</span>{/if}
          {:else if result?.rows}
            <!-- A matrix: one row per line, the columns aligned. Screen readers get the one-line form. -->
            <span class="matrix-line" role="img" aria-label="= {result.display}">
              <span aria-hidden="true">=</span>
              <span class="matrix" aria-hidden="true" style:grid-template-columns="repeat({result.rows[0].length}, auto)">
                {#each result.rows as row, i (i)}{#each row as cell, j (j)}<span>{cell}</span>{/each}{/each}
              </span>
            </span>
          {:else if result?.display}<span class="display">= {result.display}</span>
          {/if}
        </p>
        {#if result?.display != null}<CopyButton value={result.display} />{/if}
        </div>
        <div class="calc-pad">
          {#each pad as button, index (button.label)}
            {#if (!button.name || allowed.includes(button.name)) && (!button.neural || neural)}
              <button type="button" class:fn={!!button.name} class:sym={!button.name && !/^\d$/.test(button.label)}
                style:grid-column={index % 7 + 1} style:grid-row={Math.floor(index / 7) + 1}
                aria-label={button.aria} onclick={() => insert(button.insert ?? button.label)}>{button.label}</button>
            {/if}
          {/each}
        </div>
        <div class="calc-functions">
          {#each functions as item (item.name)}
            {#if allowed.includes(item.name)}
              <button type="button" onclick={() => insert(`${item.name}(`)}>{item.label}</button>
            {/if}
          {/each}
        </div>
        {#if neural}
          <div class="calc-functions calc-neural" role="group" aria-label="Neural functions">
            {#each neuralFunctions as item (item.name)}
              {#if allowed.includes(item.name)}
                <button type="button" title={item.help} onclick={() => insert(`${item.name}(`)}>{item.name}</button>
              {/if}
            {/each}
          </div>
        {/if}
        <div class="calc-actions">
          <button type="button" class="equals" onclick={evaluate} aria-label="Evaluate">=</button>
          <button type="button" onclick={backspace} aria-label="Delete the last character">⌫</button>
        </div>
      {:else if activeMode === 'programmer'}
        <Programmer {allowed} disabledNames={disabledNames} {api} {onEvaluated} />
      {/if}
      {#if history.length}
        <!-- A scrollable region must be keyboard-reachable (WCAG 2.1, focusable with tabindex=0); Svelte's lint
             rule is stricter than the guideline here, so the exception is deliberate. -->
        <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
        <div class="calc-history" aria-label="Recent calculations" tabindex="0">
          {#each history as entry, index (history.length - index)}
            <p>{entry.expression} {entry.ok ? `= ${entry.display}` : `→ ${entry.message}`}</p>
          {/each}
        </div>
      {/if}
      {#if note}<p class="calc-note">{note}</p>{/if}
{/snippet}

{#if open}
  {#if floating}
    <div class="calc-modal calc-floating" bind:this={dialog} style:position={position ? 'fixed' : undefined} style:left={position ? `${position.x}px` : undefined}
      style:top={position ? `${position.y}px` : undefined} style:right={position ? 'auto' : undefined} role="dialog" aria-label="Calculator" tabindex="-1"
      onkeydown={stopAll} onpaste={stopAll} ondrop={stop} ondragover={stop}>
      {@render calcBody()}
    </div>
  {:else}
    <div class="calc-backdrop">
      <div class="calc-modal" bind:this={dialog} style:position={position ? 'fixed' : undefined} style:left={position ? `${position.x}px` : undefined}
      style:top={position ? `${position.y}px` : undefined} style:right={position ? 'auto' : undefined} role="dialog" aria-modal="true" aria-label="Calculator" tabindex="-1"
        onkeydown={stopAll} onpaste={stopAll} ondrop={stop} ondragover={stop}>
        {@render calcBody()}
      </div>
    </div>
  {/if}
{/if}

<style>
  .calc-floating { position: fixed; top: 1rem; right: 1rem; width: min(30rem, 44vw); z-index: 60; box-shadow: var(--shadow-dialog); }
  .calc-backdrop { position: fixed; inset: 0; background: var(--backdrop-calculator); display: grid; place-items: center; z-index: 40; }
  .calc-modal { width: min(34rem, 94vw); max-height: 90vh; overflow-y: auto; padding: var(--space-3) var(--space-4); background: var(--color-surface); border: 1px solid var(--color-border); border-radius: var(--radius-md); box-shadow: var(--shadow-card); display: grid; gap: var(--space-2); }
  .calc-move { flex: 1; margin: 0 var(--space-2) 0 0; padding: .15rem .4rem; text-align: left; cursor: grab; touch-action: none; user-select: none; }
  .calc-move:active { cursor: grabbing; }
  .calc-head { display: flex; align-items: center; justify-content: space-between; }
  .calc-tabs { display: inline-flex; gap: var(--space-1); }
  .calc-tabs button { margin: 0; padding: .1rem .55rem; font-size: var(--text-sm); }
  .calc-tabs button[aria-selected='true'] { background: var(--ocean); border-color: var(--ocean); color: var(--surface); }
  .calc-angle { display: inline-flex; align-items: center; gap: var(--space-2); color: var(--color-heading); font-weight: 600; }
  .calc-angle button { margin: 0; padding: .1rem .5rem; font-size: var(--text-sm); }
  .calc-close { margin: 0; padding: .1rem .55rem; }
  .calc-input { background: var(--page); font-family: var(--font-mono, monospace); font-size: 1.05rem; }
  .calc-output { display: flex; align-items: center; }
  .calc-result { flex: 1; min-width: 0; background: var(--page); min-height: 1.6rem; margin: 0; overflow-wrap: anywhere; }
  .calc-result .display { font-family: var(--font-mono, monospace); font-weight: 600; color: var(--color-heading); }
  .calc-result .matrix-line { display: inline-flex; align-items: center; gap: var(--space-2); font-family: var(--font-mono, monospace); font-weight: 600; color: var(--color-heading); }
  .calc-result .matrix { display: inline-grid; column-gap: 1.5ch; padding: 0 .4rem; border-inline: 2px solid currentColor; border-radius: 4px; }
  .calc-result .matrix span { text-align: right; }
  .calc-result .error { color: var(--coral); }
  .calc-result .hint { display: block; color: var(--color-text-muted); font-size: var(--text-sm); }
  .calc-pad { display: grid; grid-template-columns: repeat(7, minmax(0, 1fr)); gap: var(--space-1); }
  .calc-functions { display: flex; flex-wrap: wrap; gap: var(--space-1); }
  .calc-pad button, .calc-functions button, .calc-actions button { margin: 0; min-width: 2.6rem; padding: .25rem .45rem; }
  .calc-pad button { min-width: 0; padding-inline: 0; }
  .calc-functions button { font-size: var(--text-sm); background: var(--sea); color: var(--sea-ink); }
  .calc-pad button.fn { background: var(--sea); color: var(--sea-ink); }
  .calc-pad button.sym { background: var(--transparent); }
  .calc-actions button:not(.equals) { color: var(--coral); }
  .calc-actions button:not(.equals):hover { background: var(--coral-tint); }
  .calc-neural { padding-top: var(--space-1); border-top: 1px dashed var(--color-border); }
  .calc-actions { display: flex; gap: var(--space-2); }
  .calc-actions .equals { flex: 1; font-weight: 700; background: var(--ocean); border-color: var(--ocean); color: var(--surface); }
  .calc-actions .equals:hover, .calc-tabs button[aria-selected='true']:hover { background: var(--ocean-strong); border-color: var(--ocean-strong); }
  .calc-history { max-height: 7rem; overflow-y: auto; border-top: 1px solid var(--color-border); padding-top: var(--space-2); font-size: var(--text-sm); }
  .calc-history p { margin: 0 0 var(--space-1); font-family: var(--font-mono, monospace); overflow-wrap: anywhere; }
  .calc-note { margin: 0; padding-top: var(--space-2); border-top: 1px solid var(--color-border); color: var(--color-text-muted); font-size: var(--text-sm); }
</style>
