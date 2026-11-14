<script>
  import { onMount, tick } from 'svelte';
  import { Terminal } from '@xterm/xterm';
  import { FitAddon } from '@xterm/addon-fit';
  import '@xterm/xterm/css/xterm.css';

  // One terminal of the student's workspace, drawn with xterm.js. What the program prints arrives through the model (which keeps it
  // until this view exists); what the student types goes back through it. A terminal with no keyboard (an exam without terminals,
  // a program that ended, a connection that is down) shows its output and takes no keys.
  let { model, info, active, readOnly, keyboard } = $props();
  let host;
  let xterm;
  let fit;

  // xterm.js draws with real colors, not CSS variables: the page reads the theme's tokens (--terminal-*, and the panel's own colors) and
  // hands it the values. A token the theme does not have is left out, and xterm's own default stands.
  const names = ['black', 'red', 'green', 'yellow', 'blue', 'magenta', 'cyan', 'white'];
  function theme() {
    const css = getComputedStyle(document.documentElement);
    const token = name => css.getPropertyValue(name).trim() || undefined;
    const colors = { background: token('--paper'), foreground: token('--text-main'), cursor: token('--text-main'), cursorAccent: token('--paper'),
      selectionBackground: token('--selection-fill'), selectionForeground: token('--text-main') };
    for (const name of names) {
      colors[name] = token(`--terminal-${name}`);
      colors[`bright${name[0].toUpperCase()}${name.slice(1)}`] = token(`--terminal-bright-${name}`);
    }
    return colors;
  }

  function refit() {
    if (!xterm || !host || host.offsetParent === null) return;
    try { fit.fit(); } catch { /* the panel has no size yet */ }
  }

  onMount(() => {
    xterm = new Terminal({ fontFamily: getComputedStyle(document.documentElement).getPropertyValue('--font-mono').trim() || 'monospace', fontSize: 14, scrollback: 5000,
      cursorBlink: true, theme: theme(), disableStdin: !keyboard || readOnly, allowProposedApi: false, macOptionIsMeta: true });
    fit = new FitAddon();
    xterm.loadAddon(fit);
    xterm.open(host);
    xterm.textarea?.setAttribute('aria-label', `${info.title} input`);
    // F6 moves between the panes of the workspace, as in a desktop application; the terminal must not take it for a key.
    xterm.attachCustomKeyEventHandler(event => !(event.key === 'F6'));
    xterm.onData(data => model.type(info.term, data));
    xterm.onResize(({ cols, rows }) => model.resizeTerminal(info.term, cols, rows));
    const sink = { write: bytes => xterm.write(bytes), reset: () => xterm.reset() };
    model.registerSink(info.term, sink);
    // Fitting changes the size the observer watches: it is done on the next frame, outside the observer's own notification.
    let frame = 0;
    const watcher = new ResizeObserver(() => { cancelAnimationFrame(frame); frame = requestAnimationFrame(refit); });
    watcher.observe(host);
    refit();
    return () => { cancelAnimationFrame(frame); watcher.disconnect(); model.unregisterSink(info.term, sink); xterm.dispose(); xterm = null; };
  });

  $effect(() => { if (xterm) xterm.options.disableStdin = !keyboard || readOnly; });
  $effect(() => { if (active) tick().then(refit); });

  export function focus() { xterm?.focus(); }
</script>
<div class="terminal" class:active hidden={!active} bind:this={host} role="region" aria-label="{info.title} output"></div>
<style>
  .terminal { height: 100%; min-height: 0; padding: .25rem .4rem; background: var(--paper); overflow: hidden; }
  .terminal :global(.xterm) { height: 100%; }
  .terminal :global(.xterm-viewport) { background-color: var(--paper) !important; }
  .terminal:focus-within { box-shadow: inset 0 0 0 2px var(--focus-halo); }
</style>
