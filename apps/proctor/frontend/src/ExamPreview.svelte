<script>
  // The exam as students will see it, read-only, so the proctor can check the compiled file before importing it. Each answer
  // field shows its ID, the key its answer is stored and graded under.
  import { sanitizeHTML } from '../../../../shared/exam/sanitize.js';

  let { content } = $props();

  function points(value) { return value == null ? '' : ` · ${value} ${value === 1 ? 'pt' : 'pts'}`; }
  // Placeholders are filled with plain DOM text, never markup, after the HTML has been sanitized.
  function render(html, slots = []) {
    const template = document.createElement('template');
    template.innerHTML = sanitizeHTML(html);
    for (const element of template.content.querySelectorAll('[data-slot]')) {
      const slot = slots.find(candidate => candidate.id === element.dataset.slot);
      if (!slot) continue;
      if (slot.kind === 'blank') {
        const box = document.createElement('span');
        box.className = 'preview-blank';
        box.textContent = slot.id;
        element.replaceChildren(box);
        continue;
      }
      const field = document.createElement('div');
      field.className = 'preview-answer';
      const label = document.createElement('span');
      label.textContent = `${slot.id} · ${slot.kind === 'code' ? `code${slot.language ? ` (${slot.language})` : ''}` : `text, ${slot.lines || 6} lines`}`;
      const starter = document.createElement('pre');
      starter.textContent = slot.starter || ' ';
      field.replaceChildren(label, starter);
      element.replaceChildren(field);
    }
    return template.innerHTML;
  }
</script>

<div class="exam-preview">
  {#if content.instructions}<section class="block"><h4>General instructions</h4>{@html render(content.instructions)}</section>{/if}
  {#each content.exercises as exercise (exercise.id)}
    <section class="block">
      <h4>{exercise.title}<small>{exercise.id}{points(exercise.points)}</small></h4>
      {#if exercise.body}{@html render(exercise.body, exercise.slots)}{/if}
      {#each exercise.items ?? [] as item (item.id)}
        <h5>{item.title}<small>{item.id}{points(item.points)}</small></h5>
        {@html render(item.body, item.slots)}
      {/each}
    </section>
  {/each}
</div>

<style>
  .exam-preview { display: grid; gap: var(--space-3); max-height: 34rem; overflow: auto; padding: var(--space-2); background: var(--color-surface); border: 1px solid var(--panel-border); border-radius: var(--radius-md); overflow-wrap: anywhere; }
  .block + .block { padding-top: var(--space-3); border-top: 1px solid var(--panel-border); }
  h4, h5 { display: flex; flex-wrap: wrap; align-items: baseline; gap: var(--space-2); margin: var(--space-2) 0; }
  h4 { font-size: var(--text-base); }
  h5 { font-size: var(--text-sm); }
  small { color: var(--color-text-muted); font-weight: 400; }
  .exam-preview :global(pre) { margin: var(--space-2) 0; padding: var(--space-2); overflow: auto; background: var(--transparent); border: 1px solid var(--line); border-radius: var(--radius-sm); font-size: var(--text-sm); }
  .exam-preview :global(table) { border-collapse: collapse; margin: var(--space-2) 0; }
  .exam-preview :global(th), .exam-preview :global(td) { padding: .25rem .55rem; border: 1px solid var(--panel-border); }
  .exam-preview :global(img) { display: block; max-width: 100%; height: auto; margin: .75rem auto; background: var(--color-figure-bg); -webkit-user-drag: none; user-select: none; }
  .exam-preview :global(.preview-blank) { display: inline-block; min-width: 6ch; padding: 0 .35rem; background: var(--transparent); border: 1px dashed var(--ink-2); border-radius: var(--radius-sm); color: var(--ink-2); font-family: var(--font-mono, monospace); font-size: var(--text-sm); }
  .exam-preview :global(.preview-answer) { margin: var(--space-2) 0; background: var(--transparent); border: 1px dashed var(--ink-2); border-radius: var(--radius-sm); }
  .exam-preview :global(.preview-answer span) { display: block; padding: .15rem .5rem; color: var(--brand-strong); font-size: var(--text-sm); font-weight: 600; }
  .exam-preview :global(.preview-answer pre) { margin: 0; background: var(--transparent); }
</style>
