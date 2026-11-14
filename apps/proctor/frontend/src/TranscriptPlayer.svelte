<script>
  import { onMount, untrack } from 'svelte';
  import { Terminal } from '@xterm/xterm';
  import '@xterm/xterm/css/xterm.css';
  import Icon from '../../../../shared/theme/Icon.svelte';
  import { parseCast, playbackPlan, plainText } from './workspace.js';

  // Replays a terminal's transcript the way it happened: what the program printed, with the pauses it had (a pause longer than two
  // seconds is shortened, so a student who sat thinking does not make the proctor wait). It can be paused, sped up, taken to any point
  // or shown whole. What is shown is text the student's programs printed: xterm.js draws it as text.
  let { title, text, onClose } = $props();
  let host;
  let terminal;
  let cast = $state(null);
  let problem = $state('');
  let playing = $state(false);
  let speed = $state(1);
  let position = $state(0); // how many events have been written
  let timer = 0;
  let plan = [];

  const total = $derived(cast?.events.length ?? 0);
  const clock = seconds => `${String(Math.floor(seconds / 60)).padStart(2, '0')}:${String(Math.floor(seconds % 60)).padStart(2, '0')}`;
  const shownAt = $derived(position === 0 || !cast ? 0 : cast.events[Math.min(position, total) - 1].at);

  const token = name => getComputedStyle(document.documentElement).getPropertyValue(name).trim() || undefined;
  function theme() {
    const colors = { background: token('--paper'), foreground: token('--text-main'), cursor: token('--text-main'), selectionBackground: token('--selection-fill') };
    for (const name of ['black', 'red', 'green', 'yellow', 'blue', 'magenta', 'cyan', 'white']) {
      colors[name] = token(`--terminal-${name}`);
      colors[`bright${name[0].toUpperCase()}${name.slice(1)}`] = token(`--terminal-bright-${name}`);
    }
    return colors;
  }

  // Everything up to an event, written at once (the terminal is reset first).
  function jump(index) {
    clearTimeout(timer);
    playing = false;
    terminal.reset();
    terminal.write(cast.events.slice(0, index).map(event => event.text).join(''));
    position = index;
  }
  function step() {
    if (!playing || position >= total) { playing = false; return; }
    const { delay, text: piece } = plan[position];
    timer = setTimeout(() => {
      terminal.write(piece);
      position += 1;
      step();
    }, (delay * 1000) / speed);
  }
  function play() {
    if (position >= total) jump(0);
    playing = true;
    step();
  }
  function pause() { playing = false; clearTimeout(timer); }

  onMount(() => {
    try {
      cast = parseCast(text);
    } catch (err) { problem = String(err.message ?? err); return; }
    plan = playbackPlan(cast.events);
    // The terminal is as big as the one the student had, within what a window can show.
    terminal = new Terminal({ cols: Math.min(cast.header.width, 200), rows: Math.min(cast.header.height, 60), disableStdin: true, cursorBlink: false, scrollback: 10000, theme: theme(),
      fontFamily: token('--font-mono') ?? 'monospace', fontSize: 13, convertEol: false });
    terminal.open(host);
    terminal.textarea?.setAttribute('aria-label', `${title} transcript (read-only)`);
    return () => { clearTimeout(timer); terminal.dispose(); terminal = null; };
  });
  $effect(() => { speed; untrack(() => { if (playing) { clearTimeout(timer); step(); } }); });
  const readable = $derived(cast ? plainText(cast.events) : '');
</script>
<section class="player" aria-label="Replay of {title}">
  <header>
    <h3>{title}</h3>
    {#if cast}<span class="muted">{cast.header.width}×{cast.header.height} · {total} piece{total === 1 ? '' : 's'} of output · {clock(cast.duration)} long</span>{/if}
    <button type="button" class="icon-btn close" aria-label="Close replay" title="Close replay" onclick={onClose}><Icon name="close" size={18} /></button>
  </header>
  {#if problem}<p role="alert">{problem}</p>{/if}
  {#if cast}
    <div class="controls" role="group" aria-label="Replay controls">
      {#if playing}<button type="button" onclick={pause}><Icon name="pause" size={16} />Pause</button>
      {:else if position >= total}<button type="button" onclick={play}><Icon name="replayPlay" size={16} />Play again</button>
      {:else}<button type="button" onclick={play}><Icon name="play" size={16} />Play</button>{/if}
      <label>Speed <select bind:value={speed} aria-label="Replay speed">{#each [1, 2, 4, 10] as value}<option {value}>{value}×</option>{/each}</select></label>
      <label class="seek">Position <input type="range" min="0" max={total} value={position} aria-label="Position in the transcript" aria-valuetext="{clock(shownAt)} of {clock(cast.duration)}"
        oninput={event => jump(Number(event.currentTarget.value))} /></label>
      <span class="time" aria-hidden="true">{clock(shownAt)} / {clock(cast.duration)}</span>
      <button type="button" onclick={() => jump(total)}><Icon name="goToEnd" size={16} />Show all</button>
    </div>
  {/if}
  <div class="screen" bind:this={host} hidden={!cast}></div>
  {#if cast}
    <details>
      <summary>The transcript as plain text</summary>
      <pre aria-label="Transcript of {title} as plain text">{readable}</pre>
    </details>
  {/if}
</section>
<style>
  .player { display: grid; gap: var(--space-2); margin-top: var(--space-2); padding: var(--space-3); background: var(--bg-primary); border: 1px solid var(--border-subtle); border-radius: var(--radius); min-width: 0; }
  header { display: flex; flex-wrap: wrap; align-items: baseline; gap: var(--space-2); }
  h3 { margin: 0; font-size: var(--text-base); }
  .close { margin: 0 0 0 auto; }
  /* A button with an icon and no text: a square with a hairline border, as in the owner's mock. */
  .icon-btn { display: inline-flex; align-items: center; justify-content: center; flex: none; width: 2.25rem; height: 2.25rem; padding: 0; border: 1px solid var(--control-border); border-radius: 8px; background: var(--transparent); box-shadow: none; color: var(--text-muted); }
  .icon-btn:hover:not(:disabled) { background: var(--hover-fill); color: var(--text-main); }
  .controls { display: flex; flex-wrap: wrap; align-items: center; gap: var(--space-2); }
  .controls button, .controls select { margin: 0; }
  .controls button { display: inline-flex; align-items: center; gap: .4rem; }
  .controls label { display: inline-flex; align-items: center; gap: .35rem; margin: 0; font-size: var(--text-sm); }
  .seek { flex: 1 1 12rem; }
  .seek input { flex: 1; min-width: 6rem; }
  .time { font-family: var(--font-mono); font-size: var(--text-sm); color: var(--text-muted); }
  .screen { max-width: 100%; overflow: auto; padding: .35rem; background: var(--paper); border: 1px solid var(--control-border); border-radius: var(--radius-ctl); }
  details summary { cursor: pointer; color: var(--action-hover); font-size: var(--text-sm); }
  pre { max-height: 16rem; margin: var(--space-1) 0 0; overflow: auto; white-space: pre-wrap; overflow-wrap: anywhere; font-family: var(--font-mono); font-size: var(--text-xs); }
  .muted { color: var(--text-muted); font-size: var(--text-sm); }
</style>
