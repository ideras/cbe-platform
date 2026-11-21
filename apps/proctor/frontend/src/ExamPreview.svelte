<script>
  import Icon from '../../../../shared/theme/Icon.svelte';
  import PreviewText from './PreviewText.svelte';

  let { instructions, exercises, fieldNames, answers, onChange } = $props();
  const numbers = $derived(new Map(exercises.flatMap(exercise => exercise.fields).map((slot, i) => [slot.id, i + 1])));
  const filled = exercise => exercise.fields.some(slot => answers.get(slot.id)?.trim());
  const title = (exercise, index) => exercise.title || `Exercise ${index + 1}`;
  const points = value => value == null ? '' : `${value} ${value === 1 ? 'pt' : 'pts'}`;

  function jump(index) {
    const section = document.getElementById(`preview-exercise-${index}`);
    section?.focus({ preventScroll: true });
    section?.scrollIntoView({ block: 'start' });
  }
</script>

<div class="exam-preview">
  <p class="mode-note" class:fieldNames aria-live="polite">
    <Icon name={fieldNames ? 'fileCheck' : 'preview'} size={16} />
    {fieldNames ? 'Showing the answer field names as they appear in submissions.' : 'The exam as students see it. You can type in the answer boxes to try them.'}
  </p>
  {#if exercises.length}
    <nav class="jump" aria-label="Exercises">
      {#each exercises as exercise, index (exercise.id)}
        <button type="button" class:filled={filled(exercise)} onclick={() => jump(index)}>
          {title(exercise, index)}
          {#if filled(exercise)}<Icon name="check" size={14} /><span class="sr-only"> (has trial answers)</span>{/if}
        </button>
      {/each}
    </nav>
  {/if}
  {#if instructions}
    <section class="card instructions" aria-labelledby="preview-instructions-title">
      <div class="card-head"><Icon name="info" size={18} /><h3 id="preview-instructions-title">General instructions</h3></div>
      <div class="card-body"><PreviewText html={instructions} {numbers} {answers} {onChange} /></div>
    </section>
  {/if}
  {#each exercises as exercise, index (exercise.id)}
    <section class="card" id="preview-exercise-{index}" aria-labelledby="preview-exercise-title-{index}" tabindex="-1">
      <div class="card-head">
        <span class="number" aria-hidden="true">{index + 1}</span>
        <h3 id="preview-exercise-title-{index}">{title(exercise, index)}</h3>
        <div class="meta"><span class="chip" title="Exercise ID in the exam file">id {exercise.id}</span>{#if exercise.points != null}<span class="chip">{points(exercise.points)}</span>{/if}</div>
      </div>
      <div class="card-body">
        {#if exercise.body}<div class="exercise-context"><PreviewText html={exercise.body} slots={exercise.slots ?? []} {numbers} {fieldNames} {answers} {onChange} /></div>{/if}
        {#each exercise.items ?? [] as item (item.id)}
          <div class="item">
            <h4>{item.title || item.id} {#if fieldNames}<small>{item.id}</small>{/if}{#if item.points != null}<small>{points(item.points)}</small>{/if}</h4>
            <PreviewText html={item.body} slots={item.slots ?? []} {numbers} {fieldNames} {answers} {onChange} />
          </div>
        {/each}
      </div>
    </section>
  {:else}
    <p class="empty">This preview has no exercises.</p>
  {/each}
</div>

<style>
  .exam-preview { display: grid; min-width: 0; gap: .85rem; overflow-wrap: anywhere; }
  .mode-note { display: flex; align-items: center; gap: .5rem; margin: 0; padding: .45rem .7rem; border: 1px dashed var(--action-border); border-radius: 9px; background: var(--hover-fill); color: var(--action-hover); font-size: .82rem; }
  .mode-note :global(svg), .jump :global(svg), .card-head :global(svg) { flex: none; }
  .mode-note.fieldNames { border-color: var(--warn-border); background: var(--warn-fill); color: var(--warn-text); }
  .jump { display: flex; flex-wrap: wrap; gap: .35rem; }
  .jump button { display: inline-flex; align-items: center; gap: .3rem; max-width: 100%; min-height: 0; margin: 0; padding: .25rem .65rem; border-color: var(--border-subtle); border-radius: 999px; background: var(--bg-primary); color: var(--text-muted); font-size: .8rem; font-weight: 600; text-align: left; }
  .jump button.filled { background: var(--ok-fill); border-color: var(--ok-border); color: var(--ok-text); }
  .jump button:hover { border-color: var(--action-primary); color: var(--action-hover); }
  .sr-only { position: absolute; width: 1px; height: 1px; overflow: hidden; clip-path: inset(50%); white-space: nowrap; }
  .empty { color: var(--text-muted); font-size: var(--text-sm); }
  .card { min-width: 0; border: 1px solid var(--border-subtle); border-radius: 12px; background: var(--bg-raised); box-shadow: var(--shadow-raised); scroll-margin: .5rem; }
  .card-head { display: flex; align-items: center; flex-wrap: wrap; gap: .55rem; padding: .6rem .9rem; border-bottom: 1px solid var(--cyan-border); border-radius: 11px 11px 0 0; background: linear-gradient(90deg, var(--selection-fill), var(--hover-fill)); color: var(--action-hover); }
  .card-head h3 { flex: 1 1 9rem; min-width: 0; margin: 0; font-size: .98rem; color: inherit; }
  .number { display: grid; place-items: center; flex: none; width: 26px; height: 26px; border-radius: 50%; background: var(--action-primary); color: var(--on-dark); font: 700 .8rem var(--font-mono); }
  .meta { display: flex; flex-wrap: wrap; gap: .35rem; }
  .chip { padding: .1rem .45rem; border: 1px solid var(--border-subtle); border-radius: 999px; background: var(--bg-raised); color: var(--text-muted); font: 700 .72rem var(--font-mono); }
  .card-body { padding: .85rem .9rem .95rem; font-size: .95rem; }
  .exercise-context + .item, .item + .item { padding-top: .6rem; margin-top: .6rem; border-top: 1px solid var(--border-subtle); }
  .item h4 { display: flex; flex-wrap: wrap; align-items: baseline; gap: .5rem; margin: 0 0 .4rem; font-size: var(--text-sm); }
  small { color: var(--text-muted); font-weight: 500; }
</style>
