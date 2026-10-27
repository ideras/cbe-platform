<script>
  // One exercise: its shared context, then each of its items with their answer fields.
  import RichText from './RichText.svelte';
  import Card from '../../../../shared/theme/Card.svelte';
  import { points } from './exercises.js';

  let { exercise, answers, locked, onChange } = $props();
</script>

<Card label="Exercise {exercise.title}" title={exercise.title} level={3}>
  {#snippet actions()}{#if exercise.points != null}<span class="points">{points(exercise.points)}</span>{/if}{/snippet}
  {#if exercise.body}<RichText html={exercise.body} slots={exercise.slots} {answers} {locked} {onChange} />{/if}
  {#each exercise.items ?? [] as item (item.id)}
    <section class="item" aria-label={item.title}>
      <h4>{item.title}{#if item.points != null}<span class="points">{points(item.points)}</span>{/if}</h4>
      <RichText html={item.body} slots={item.slots} {answers} {locked} {onChange} />
    </section>
  {/each}
</Card>

<style>
  .points { margin-left: var(--space-2); padding: .05rem .5rem; border-radius: 999px; background: var(--transparent); border: 1px solid var(--line-strong); color: var(--ink); font-size: var(--text-sm); font-weight: 600; white-space: nowrap; }
  .item { margin-top: var(--space-4); padding-top: var(--space-3); border-top: 1px solid var(--color-border-soft); }
  .item h4 { display: flex; flex-wrap: wrap; align-items: baseline; gap: var(--space-2); margin: 0 0 var(--space-2); color: var(--color-heading); font-size: var(--text-base); }
  .item h4 .points { margin-left: 0; background: var(--transparent); color: var(--color-accent); }
</style>
