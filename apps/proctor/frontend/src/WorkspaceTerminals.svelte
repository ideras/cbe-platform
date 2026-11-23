<script>
  import { onMount, tick } from 'svelte';
  import * as api from '$proctor-api';
  import Icon from '../../../../shared/theme/Icon.svelte';
  import TranscriptPlayer from './TranscriptPlayer.svelte';
  import { localDateTime, localTime } from './dates.js';
  import { kindLabel, endText, sizeText } from './workspace.js';

  // The transcripts of a student's terminals, as a grid whose rows are chosen: the chosen row's transcript is replayed under the grid,
  // and the window is scrolled to it (it is under a grid that may fill the window, and a replay that is out of sight looks like nothing
  // happened). What the programs printed is the student's text: the player draws it as text, never as markup.
  let { workspace, copy } = $props();
  let selectedId = $state(null);
  let replay = $state(null); // { id, title, text }
  let loading = $state(false);
  let problem = $state('');
  let box = $state(); // the replay's place under the grid
  const rowNodes = $state({});
  let alive = true;
  let asked = 0;
  let playerKey = $state(0);
  let speed = $state(1);

  const terminals = $derived(workspace.terminals);
  // The row in the tab order: the chosen one, or the first.
  const entry = $derived(terminals.some(item => item.id === selectedId) ? selectedId : terminals[0]?.id);

  async function select(terminal) {
    selectedId = terminal.id; problem = '';
    if (replay?.id === terminal.id) { playerKey += 1; reveal(); return; }
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
  <p class="review-hint"><Icon name="info" size={14} />Pick a terminal to replay what it showed.</p>
  <table class="terminals" role="grid" aria-label="Transcripts of the student's terminals">
    <thead><tr><th scope="col">Terminal</th><th scope="col">Started</th><th scope="col">How it ended</th><th scope="col">Output</th></tr></thead>
    <tbody>{#each terminals as terminal, index (terminal.id)}
      <!-- A row of a grid is chosen by click, Enter or Space; the arrow keys move between rows. -->
      <tr bind:this={rowNodes[terminal.id]} class:chosen={selectedId === terminal.id} aria-selected={selectedId === terminal.id} tabindex={entry === terminal.id ? 0 : -1}
        onclick={() => select(terminal)} onkeydown={event => keys(event, index)}>
        <td><span class="terminal-kind"><span class="kind-icon" class:check={terminal.kind === 'check'}><Icon name={terminal.kind === 'check' ? 'check' : 'terminal'} size={14} /></span>{kindLabel(terminal.kind)}</span></td>
        <td class="when mono" title={localDateTime(terminal.started_at)}>{localTime(terminal.started_at)}</td>
        <td><span class="review-pill {terminal.ended_at && terminal.exit_code != null ? terminal.exit_code === 0 ? 'ok' : 'alert' : ''}">{endText(terminal)}</span></td>
        <td class="size mono">{#if terminal.truncated}<span class="review-pill warn" title="Output was cut at the limit" aria-label="cut at the limit">cut</span>{/if} {sizeText(terminal.bytes)}</td>
      </tr>
    {/each}</tbody>
  </table>
  {#if selectedId !== null}
    <div class="replay" bind:this={box}>
      {#if problem}<p role="alert" class="problem">{problem}</p>{/if}
      {#if loading}<p class="muted" role="status">Reading the transcript…</p>{/if}
      {#if replay}{#key playerKey}<TranscriptPlayer title={replay.title} text={replay.text} {copy} bind:speed onClose={close} />{/key}{/if}
    </div>
  {/if}
{:else}<p class="muted">The student has not opened a terminal, or run anything.</p>{/if}

<style>
  .problem { color: var(--alert-text); }
  .terminals { width: 100%; border-collapse: separate; border-spacing: 0 5px; font-size: inherit; }
  thead { position: absolute; width: 1px; height: 1px; clip-path: inset(50%); overflow: hidden; }
  td { padding: .45rem .65rem; text-align: left; border-block: 1px solid var(--border-subtle); background: var(--bg-primary); }
  td:first-child { border-inline-start: 1px solid var(--border-subtle); border-radius: 9px 0 0 9px; }
  td:last-child { border-inline-end: 1px solid var(--border-subtle); border-radius: 0 9px 9px 0; }
  .terminals tbody tr { cursor: pointer; }
  .terminals tbody tr:hover td { background: var(--hover-fill); }
  .terminals tbody tr.chosen td { background: var(--selection-fill); border-color: var(--action-primary); }
  .terminals tbody tr:focus-visible { outline: 2px solid var(--action-primary); outline-offset: 1px; }
  .terminal-kind { display: flex; align-items: center; gap: .6rem; font-weight: 700; }
  .kind-icon { display: grid; place-items: center; flex: none; width: 26px; height: 26px; border-radius: 7px; background: var(--selection-fill); color: var(--action-hover); }
  .kind-icon.check { background: var(--ok-fill); color: var(--ok-text); }
  .when, .size { color: var(--text-muted); font-size: .75rem; }
  .size { text-align: right; }
  @media (max-width: 480px) { td { padding-inline: .25rem; } .terminal-kind { gap: .25rem; } .when, .size { font-size: .68rem; } }
  .replay { scroll-margin-bottom: var(--space-3); }
  .muted { color: var(--text-muted); font-size: var(--text-sm); }
</style>
