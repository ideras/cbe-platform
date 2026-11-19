<script>
  import Icon from '../../../../../shared/theme/Icon.svelte';
  import RichText from '../RichText.svelte';
  // The exam's instructions and its TODO list. The ticks are the student's own record of their progress (the server keeps them and
  // shows them to the instructor); nothing is checked. The text is the exam's, sanitized like every exam text. `onWiden` is the Wider
  // button, which gives the column half the window (`wide`) and takes it back; without it there is no button.
  let { instructions = '', todos = [], done = {}, disabled = false, onToggle, wide = false, onWiden = null } = $props();
  const total = $derived(todos.length);
  const ticked = $derived(todos.filter(item => done[item.id]).length);
</script>
{#snippet widen()}
  <!-- A toggle keeps its name; pressed, it is the wide column, and a second press gives the editor its room back. -->
  <button type="button" class="widen" aria-label="Wider instructions" aria-pressed={wide} onclick={onWiden}
    title={wide ? 'Give the editor its room back' : 'Give the instructions and the files half of the window'}><Icon name={wide ? 'back' : 'arrowRight'} size={13} />Wider</button>
{/snippet}
<section class="todo" aria-label="Instructions and TODO list">
  {#if instructions}
    <!-- The button sits on the line of the summary, at its right, where the summary has nothing: it takes no room of the text, and it stays at the top
         of the panel while the instructions are scrolled. -->
    {#if onWiden}<div class="head">{@render widen()}</div>{/if}
    <details class="instructions" open>
      <summary>Instructions</summary>
      <RichText html={instructions} />
    </details>
  {/if}
  <h3 id="todo-heading">TODO <span class="end"><span class="count" aria-label="{ticked} of {total} done">{ticked}/{total}</span>{#if onWiden && !instructions}{@render widen()}{/if}</span></h3>
  <ul aria-labelledby="todo-heading">
    {#each todos as item (item.id)}
      <li class:ticked={done[item.id]}>
        <label>
          <input type="checkbox" checked={!!done[item.id]} {disabled} onchange={event => onToggle(item.id, event.currentTarget.checked)} />
          <span class="text"><RichText html={item.text_html} />{#if item.points != null}<small class="points">{item.points} pts</small>{/if}</span>
        </label>
      </li>
    {/each}
  </ul>
</section>
<style>
  .todo { display: grid; gap: var(--space-2); min-width: 0; }
  .instructions { padding: var(--space-2) var(--space-3); background: var(--bg-raised); border: 1px solid var(--border-subtle); border-radius: var(--radius-ctl); font-size: var(--text-sm); }
  .instructions summary { cursor: pointer; color: var(--action-hover); font-weight: 600; }
  .instructions[open] summary { margin-bottom: var(--space-1); }
  .instructions :global(p) { margin: 0 0 var(--space-1); }
  h3 { display: flex; align-items: baseline; justify-content: space-between; margin: var(--space-1) 0 0; font-size: var(--text-base); }
  .count { font-family: var(--font-mono); font-size: var(--text-xs); color: var(--text-muted); }
  .end { display: inline-flex; align-items: center; gap: var(--space-2); }
  .head { display: flex; justify-content: flex-end; height: 0; position: sticky; top: 0; z-index: 1; }
  .head .widen { margin: .3rem .4rem 0 0; }
  .widen { display: inline-flex; align-items: center; gap: .25rem; margin: 0; padding: .1rem .45rem; font-size: var(--text-xs); font-weight: 400; line-height: 1.4; }
  .widen[aria-pressed="true"] { background: var(--selection-fill); }
  /* In a narrow window the column is the whole width already. */
  @media (max-width: 900px) { .head, .widen { display: none; } }
  ul { display: grid; gap: .1rem; margin: 0; padding: 0; list-style: none; }
  li label { display: flex; align-items: flex-start; gap: var(--space-2); margin: 0; padding: .25rem .3rem; border-radius: var(--radius-ctl); cursor: pointer; }
  li label:hover { background: var(--hover-fill); }
  li input { flex: none; width: 1.05rem; height: 1.05rem; margin: .15rem 0 0; accent-color: var(--ok-text); }
  .text { min-width: 0; font-size: var(--text-sm); overflow-wrap: anywhere; }
  .text :global(p) { margin: 0; display: inline; }
  .ticked .text { color: var(--text-muted); text-decoration: line-through; }
  .points { margin-left: .4rem; color: var(--text-muted); font-family: var(--font-mono); }
</style>
