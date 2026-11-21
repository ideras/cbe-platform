<script>
  import { onMount, tick } from 'svelte';
  import * as api from '$proctor-api';
  import TranscriptPlayer from './TranscriptPlayer.svelte';
  import { localDateTime } from './dates.js';
  import { kindLabel, endText, sizeText } from './workspace.js';

  // The transcripts of a student's terminals, as a grid whose rows are chosen: the chosen row's transcript is replayed under the grid,
  // and the window is scrolled to it (it is under a grid that may fill the window, and a replay that is out of sight looks like nothing
  // happened). What the programs printed is the student's text: the player draws it as text, never as markup.
  let { workspace } = $props();
  let selectedId = $state(null);
  let replay = $state(null); // { id, title, text }
  let loading = $state(false);
  let problem = $state('');
  let box = $state(); // the replay's place under the grid
  const rowNodes = {};
  let alive = true;
  let asked = 0;

  const terminals = $derived(workspace.terminals);
  // The row in the tab order: the chosen one, or the first.
  const entry = $derived(terminals.some(item => item.id === selectedId) ? selectedId : terminals[0]?.id);

  async function select(terminal) {
    selectedId = terminal.id; problem = '';
    if (replay?.id === terminal.id) { reveal(); return; }
    const mine = ++asked;
    replay = null; loading = true;
    try {
      const text = await api.WorkspaceTranscript(terminal.id);
      if (!alive || mine !== asked) return;
      replay = { id: terminal.id, title: `${kindLabel(terminal.kind)} at ${localDateTime(terminal.started_at)}`, text };
    } catch (err) {
      if (alive && mine === asked) problem = `The transcript could not be read: ${String(err)}`;
    } finally { if (alive && mine === asked) loading = false; }
    if (alive && mine === asked && replay) reveal();
  }
  // Once the player is drawn (its size is known only then) the window shows it: as little scrolling as will show all of it, or its top if
  // it is taller than the window.
  async function reveal() {
    await tick();
    requestAnimationFrame(() => {
      if (!box) return;
      const still = window.matchMedia?.('(prefers-reduced-motion: reduce)').matches;
      box.scrollIntoView({ block: box.getBoundingClientRect().height > window.innerHeight ? 'start' : 'nearest', behavior: still ? 'auto' : 'smooth' });
    });
  }
  function close() {
    const id = selectedId;
    asked += 1; selectedId = null; replay = null; loading = false; problem = '';
    tick().then(() => rowNodes[id]?.focus());
  }
  // Arrow keys, Home and End move between the rows; Enter or Space chooses one.
  function keys(event, index) {
    const last = terminals.length - 1;
    const to = { ArrowDown: Math.min(index + 1, last), ArrowUp: Math.max(index - 1, 0), Home: 0, End: last }[event.key];
    if (to !== undefined) { event.preventDefault(); rowNodes[terminals[to].id]?.focus(); }
    else if (event.key === 'Enter' || event.key === ' ') { event.preventDefault(); select(terminals[index]); }
  }
  onMount(() => () => { alive = false; });
</script>

{#if terminals.length}
  <p class="muted">Choose a terminal to replay what it showed.</p>
  <table class="terminals" role="grid" aria-label="Transcripts of the student's terminals">
    <thead><tr><th scope="col">Terminal</th><th scope="col">Started</th><th scope="col">How it ended</th><th scope="col">Output</th></tr></thead>
    <tbody>{#each terminals as terminal, index (terminal.id)}
      <!-- A row of a grid is chosen by click, Enter or Space; the arrow keys move between rows. -->
      <tr bind:this={rowNodes[terminal.id]} class:chosen={selectedId === terminal.id} aria-selected={selectedId === terminal.id} tabindex={entry === terminal.id ? 0 : -1}
        onclick={() => select(terminal)} onkeydown={event => keys(event, index)}>
        <td>{kindLabel(terminal.kind)}</td><td>{localDateTime(terminal.started_at)}</td><td>{endText(terminal)}</td><td>{sizeText(terminal.bytes)}{terminal.truncated ? ' (cut at the limit)' : ''}</td>
      </tr>
    {/each}</tbody>
  </table>
  {#if selectedId !== null}
    <div class="replay" bind:this={box}>
      {#if problem}<p role="alert" class="problem">{problem}</p>{/if}
      {#if loading}<p class="muted" role="status">Reading the transcript…</p>{/if}
      {#if replay}{#key replay.id}<TranscriptPlayer title={replay.title} text={replay.text} onClose={close} />{/key}{/if}
    </div>
  {/if}
{:else}<p class="muted">The student has not opened a terminal, or run anything.</p>{/if}

<style>
  .problem { color: var(--alert-text); }
  .terminals { width: 100%; border-collapse: collapse; font-size: var(--text-sm); }
  .terminals th, .terminals td { padding: .45rem .6rem; text-align: left; border-bottom: 1px solid var(--border-subtle); }
  .terminals th { color: var(--action-hover); font-size: var(--text-xs); letter-spacing: .06em; text-transform: uppercase; }
  .terminals tbody tr { cursor: pointer; }
  .terminals tbody tr:hover { background: var(--hover-fill); }
  .terminals tbody tr.chosen { background: var(--selection-fill); font-weight: 700; box-shadow: inset 4px 0 0 var(--action-primary); }
  .terminals tbody tr:focus-visible { outline: 2px solid var(--action-primary); outline-offset: -2px; }
  .replay { scroll-margin-bottom: var(--space-3); }
  .muted { color: var(--text-muted); font-size: var(--text-sm); }
</style>
