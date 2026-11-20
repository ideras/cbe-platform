<script>
  import RichText from '../RichText.svelte';
  // The exam's TODO list, in the left column above the files. The ticks are the student's own record of their progress (the server keeps
  // them and shows them to the instructor); nothing is checked. The heading, with how many are ticked, stays in view however long the list
  // is: the list scrolls under it (before, the list was under the instructions, and most students never saw it). The text is the exam's,
  // sanitized like every exam text.
  let { todos = [], done = {}, disabled = false, onToggle } = $props();
  const total = $derived(todos.length);
  const ticked = $derived(todos.filter(item => done[item.id]).length);
</script>
<section class="todo" aria-labelledby="todo-heading">
  <h3 id="todo-heading">TODO <span class="count" class:all={ticked === total} aria-label="{ticked} of {total} done">{ticked}/{total}</span>
    <span class="progress" aria-hidden="true"><span class="bar" style:width="{total ? (ticked / total) * 100 : 0}%"></span></span></h3>
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
  .todo { display: grid; grid-template-rows: auto minmax(0, 1fr); min-height: 0; max-height: 100%; min-width: 0; }
  h3 { display: flex; align-items: center; gap: var(--space-2); margin: 0; padding: .4rem .75rem; border-bottom: 1px solid var(--border-subtle); color: var(--action-primary); font-size: var(--text-xs); font-weight: 700; letter-spacing: .08em; text-transform: uppercase; }
  .count { padding: 0 .45rem; border-radius: 999px; background: var(--warn-fill); color: var(--warn-text); font-family: var(--font-mono); font-size: var(--text-xs); letter-spacing: 0; }
  .count.all { background: var(--ok-fill); color: var(--ok-text); }
  .progress { flex: 1; height: 4px; border-radius: 999px; background: var(--bg-secondary); overflow: hidden; }
  .progress .bar { display: block; height: 100%; background: linear-gradient(90deg, var(--action-primary), var(--accent-cyan)); transition: width .2s; }
  ul { display: grid; align-content: start; gap: .1rem; margin: 0; padding: .25rem .4rem .4rem; list-style: none; overflow: auto; }
  li label { display: flex; align-items: flex-start; gap: var(--space-2); margin: 0; padding: .25rem .3rem; border-radius: var(--radius-ctl); cursor: pointer; }
  li label:hover { background: var(--hover-fill); }
  li input { flex: none; width: 1.05rem; height: 1.05rem; margin: .15rem 0 0; accent-color: var(--ok-text); }
  .text { min-width: 0; font-size: var(--text-sm); overflow-wrap: anywhere; }
  .text :global(p) { margin: 0; display: inline; }
  .ticked .text { color: var(--text-muted); text-decoration: line-through; }
  .points { margin-left: .4rem; color: var(--text-muted); font-family: var(--font-mono); }
</style>
