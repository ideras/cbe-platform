<script>
  import RichText from '../RichText.svelte';
  // The exam's instructions and its TODO list. The ticks are the student's own record of their progress (the server keeps them and
  // shows them to the instructor); nothing is checked. The text is the exam's, sanitized like every exam text.
  let { instructions = '', todos = [], done = {}, disabled = false, onToggle } = $props();
  const total = $derived(todos.length);
  const ticked = $derived(todos.filter(item => done[item.id]).length);
</script>
<section class="todo" aria-label="Instructions and TODO list">
  {#if instructions}
    <details class="instructions" open>
      <summary>Instructions</summary>
      <RichText html={instructions} />
    </details>
  {/if}
  <h3 id="todo-heading">TODO <span class="count" aria-label="{ticked} of {total} done">{ticked}/{total}</span></h3>
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
  ul { display: grid; gap: .1rem; margin: 0; padding: 0; list-style: none; }
  li label { display: flex; align-items: flex-start; gap: var(--space-2); margin: 0; padding: .25rem .3rem; border-radius: var(--radius-ctl); cursor: pointer; }
  li label:hover { background: var(--hover-fill); }
  li input { flex: none; width: 1.05rem; height: 1.05rem; margin: .15rem 0 0; accent-color: var(--ok-text); }
  .text { min-width: 0; font-size: var(--text-sm); overflow-wrap: anywhere; }
  .text :global(p) { margin: 0; display: inline; }
  .ticked .text { color: var(--text-muted); text-decoration: line-through; }
  .points { margin-left: .4rem; color: var(--text-muted); font-family: var(--font-mono); }
</style>
