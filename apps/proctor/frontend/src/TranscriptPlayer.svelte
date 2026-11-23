<script>
  import { onMount } from 'svelte';
  import { Terminal } from '@xterm/xterm';
  import '@xterm/xterm/css/xterm.css';
  import Icon from '../../../../shared/theme/Icon.svelte';
  import { parseCast, plainText } from './workspace.js';

  let { title, text, onClose, copy, speed = $bindable(1) } = $props();
  let host;
  let terminal;
  let cast = $state(null);
  let problem = $state('');
  let playing = $state(false);
  let plain = $state(false);
  let position = $state(0); // seconds in the original recording
  let rendered = $state(0);
  let frame = 0;
  let last = 0;
  let echoUntil = 0;
  // A PTY can timestamp consecutive key echoes within the same display frame.
  const shortEcho = /^[^\p{C}\p{Zl}\p{Zp}]{1,3}$/u;

  const total = $derived(cast?.events.length ?? 0);
  const clock = seconds => `${String(Math.floor(seconds / 60)).padStart(2, '0')}:${String(Math.floor(seconds % 60)).padStart(2, '0')}`;
  const readable = $derived(plain && cast ? plainText(cast.events) : '');
  const ended = $derived(!!cast && position >= cast.duration && rendered >= total);
  const token = name => getComputedStyle(document.documentElement).getPropertyValue(name).trim() || undefined;
  function theme() {
    const colors = { background: token('--term-ground'), foreground: token('--term-text'), cursor: token('--term-cursor'), selectionBackground: token('--term-selection') };
    for (const name of ['black', 'red', 'green', 'yellow', 'blue', 'magenta', 'cyan', 'white']) {
      colors[name] = token(`--term-${name}`);
      colors[`bright${name[0].toUpperCase()}${name.slice(1)}`] = token(`--term-bright-${name}`);
    }
    return colors;
  }
  function writeThrough(end, reset = false) {
    const first = reset ? 0 : rendered;
    if (reset || end > first) {
      // Queue the reset with the writes so a pending replay frame cannot paint over a seek.
      terminal.write((reset ? '\x1bc' : '') + cast.events.slice(first, end).map(event => event.text).join(''), () => terminal?.scrollToBottom());
    }
    rendered = end;
  }
  function draw(reset = false, now = null) {
    let end = reset ? 0 : rendered;
    while (end < total && cast.events[end].at <= position) {
      const event = cast.events[end++];
      if (now !== null && speed <= 1 && shortEcho.test(event.text)) {
        // Let this recorded echo paint before following output can overwrite it. Keep real
        // inter-key timing when it's longer, and don't invent characters inside a recorded chunk.
        if (end < total && cast.events[end].at - event.at < 0.05) {
          position = event.at;
          echoUntil = now + 50 / speed;
          break;
        }
      }
    }
    writeThrough(end, reset);
  }
  function pause() { playing = false; cancelAnimationFrame(frame); }
  function seek(seconds) { pause(); position = seconds; draw(true); }
  function advance(now) {
    if (!playing) return;
    const elapsed = last ? Math.min(now - last, 50) : 0;
    last = now;
    if (speed > 1 || now >= echoUntil) {
      // Keep ordinary thinking pauses at normal speeds. Only skip extended idle time
      // (or shorten waits in fast replay); the seek bar still represents recorded time.
      const next = cast.events[rendered]?.at ?? cast.duration;
      if (next - position > (speed <= 1 ? 10 : 2)) position = next - 2;
      position = Math.min(cast.duration, position + elapsed * speed / 1000);
      draw(false, now);
    }
    if (ended) pause(); else frame = requestAnimationFrame(advance);
  }
  function play() {
    if (ended) { position = 0; writeThrough(0, true); }
    playing = true; last = 0; echoUntil = 0;
    frame = requestAnimationFrame(advance);
  }
  function showPlain(value) { if (value) pause(); plain = value; }

  onMount(() => {
    try { cast = parseCast(text); }
    catch (err) { problem = String(err.message ?? err); return; }
    terminal = new Terminal({ cols: Math.min(cast.header.width, 200), rows: Math.min(cast.header.height, 60), disableStdin: true, cursorBlink: false, scrollback: 10000, theme: theme(),
      fontFamily: token('--font-mono') ?? 'monospace', fontSize: 13, convertEol: false });
    terminal.open(host);
    terminal.textarea?.setAttribute('aria-label', `${title} transcript (read-only)`);
    return () => { cancelAnimationFrame(frame); terminal.dispose(); terminal = null; };
  });
</script>
<section class="player" aria-label="Replay of {title}">
  <header>
    <h3>{title}</h3>
    {#if cast}
      <span class="meta">{cast.header.width}×{cast.header.height} · {total} piece{total === 1 ? '' : 's'} of output · {clock(cast.duration)} long</span>
      <span class="review-spacer"></span>
      <button type="button" class="player-button" aria-pressed={!plain} onclick={() => showPlain(false)}>Replay</button>
      <button type="button" class="player-button" aria-pressed={plain} onclick={() => showPlain(true)}>Plain text</button>
      <button type="button" class="player-button" aria-label="Copy transcript" onclick={() => copy(plain ? readable : plainText(cast.events.slice(0, rendered)), 'Transcript')}>Copy</button>
    {:else}<span class="review-spacer"></span>{/if}
    <button type="button" class="player-button close" aria-label="Close replay" title="Close replay" onclick={onClose}><Icon name="close" size={15} /></button>
  </header>
  {#if problem}<p role="alert">{problem}</p>{/if}
  {#if cast && !plain}
    <div class="controls" role="group" aria-label="Replay controls">
      <button type="button" class="player-button play" onclick={playing ? pause : play}><Icon name={playing ? 'pause' : ended ? 'replayPlay' : 'play'} size={14} />{playing ? 'Pause' : ended ? 'Play again' : 'Play'}</button>
      <div class="speeds" role="group" aria-label="Replay speed">
        {#each [0.5, 1, 2, 4] as value}<button type="button" class="player-button" aria-pressed={speed === value} onclick={() => { speed = value; }}>{value === 0.5 ? '½' : value}×</button>{/each}
      </div>
      <input class="seek" type="range" min="0" max={cast.duration} step="any" value={position} aria-label="Position in the transcript" aria-valuetext="{clock(position)} of {clock(cast.duration)}"
        oninput={event => seek(Number(event.currentTarget.value))} />
      <span class="time mono" aria-hidden="true">{clock(position)} / {clock(cast.duration)}</span>
      <button type="button" class="player-button" onclick={() => seek(cast.duration)}><Icon name="goToEnd" size={14} />Show all</button>
    </div>
  {/if}
  <div class="screen" bind:this={host} hidden={!!problem || plain}></div>
  {#if plain}<pre class="screen plain" aria-label="Transcript of {title} as plain text">{readable || '(no output)'}</pre>{/if}
</section>
<style>
  .player { min-width: 0; background: var(--term-ground); color: var(--term-text); border: 1px solid var(--term-head); border-radius: 10px; overflow: hidden; }
  header, .controls { display: flex; flex-wrap: wrap; align-items: center; gap: .35rem; padding: .45rem .65rem; background: var(--term-head); }
  header { border-bottom: 1px solid var(--cyan-chip-border); }
  h3 { margin: 0; color: inherit; font-size: .84rem; }
  .meta, .time { color: var(--term-dim); font-size: .74rem; }
  .player-button { display: inline-flex; align-items: center; justify-content: center; gap: .3rem; margin: 0; padding: .15rem .45rem; min-height: 26px; border: 1px solid var(--cyan-chip-border); border-radius: 6px; background: var(--chip-on-dark); color: var(--term-text); font-size: .74rem; box-shadow: none; }
  .player-button:hover:not(:disabled), .player-button[aria-pressed='true'] { background: var(--term-selection); color: var(--on-dark); }
  .player-button:focus-visible { outline-color: var(--accent-cyan-soft); }
  .close { padding: 0; width: 26px; }
  .play { min-width: 70px; }
  .speeds { display: inline-flex; }
  .speeds button { border-radius: 0; }
  .speeds button + button { border-inline-start: 0; }
  .speeds button:first-child { border-radius: 6px 0 0 6px; }
  .speeds button:last-child { border-radius: 0 6px 6px 0; }
  .seek { flex: 1 1 8rem; min-width: 4rem; margin: 0; accent-color: var(--accent-cyan-soft); }
  .screen { max-height: 22rem; overflow: auto; padding: .65rem; }
  /* Fill the pane so xterm anchors its scrollbar at the right. If the recorded grid
     is wider than the pane, keep its columns intact and let the outer screen scroll. */
  .screen :global(.xterm) { width: max-content; min-width: 100%; }
  .plain { min-height: 8rem; margin: 0; border: 0; white-space: pre-wrap; overflow-wrap: anywhere; }
  .player p { margin: .65rem; }
</style>
