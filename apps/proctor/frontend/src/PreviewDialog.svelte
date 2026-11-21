<script>
  import { onDestroy } from 'svelte';
  import { SvelteMap } from 'svelte/reactivity';
  import * as api from '$proctor-api';
  import { formatTime } from '../../../../shared/exam/timing.js';
  import Modal from './Modal.svelte';
  import ExamPreview from './ExamPreview.svelte';
  import Calculator from '../../../../shared/theme/calculator/Calculator.svelte';
  import Icon from '../../../../shared/theme/Icon.svelte';

  let { entry = null, preview = null, onClose } = $props();

  let loadedView = $state(null);
  const view = $derived(preview ?? loadedView);
  const timing = $derived(preview ?? entry);
  const exercises = $derived((view?.content?.exercises ?? []).map(exercise => ({
    ...exercise,
    fields: [...(exercise.slots ?? []), ...(exercise.items ?? []).flatMap(item => item.slots ?? [])],
  })));
  const fieldCount = $derived(exercises.reduce((total, exercise) => total + exercise.fields.length, 0));
  const grace = $derived((timing?.grace_seconds ?? 0) + (timing?.extra_grace_seconds ?? 0));
  const answers = new SvelteMap();
  let problem = $state('');
  let calcProblem = $state('');
  let allowed = $state(null);
  let history = $state([]);
  let calcOpen = $state(false);
  let fieldNames = $state(false);

  // Import previews use data already checked by ChooseExam. They don't open or discard a stored-exam
  // calculator session. The trial answers are local to this component and never sent to either API.
  onDestroy(() => { if (!preview) api.DiscardPreview().catch(() => {}); });

  $effect(() => {
    if (preview) return;
    const examID = entry.exam_id;
    let current = true;
    (async () => {
      try {
        const loaded = await api.PreviewExam(examID);
        if (!current) return;
        loadedView = loaded;
        if (loaded.calculator) {
          try {
            const functions = await api.CalcAllowedFunctions();
            if (current) allowed = functions ?? [];
          } catch (err) { if (current) calcProblem = String(err); }
        }
      } catch (err) { if (current) problem = String(err); }
    })();
    return () => { current = false; };
  });

  function changeAnswer(slot, value) {
    if (value === (slot.starter ?? '')) answers.delete(slot.id);
    else answers.set(slot.id, value);
  }

  function record(entry) {
    history = [...history, entry].slice(-20);
  }
</script>

<Modal title={view?.title || entry?.title || 'Exam preview'} titleId="exam-preview-title" eyebrow="Preview · as students see it"
  tone="deep" icon="preview" width="47.5rem" resizable {onClose}>
  {#snippet headerDetails()}
    {#if view}
      <div class="facts" role="group" aria-label="Exam details">
        <span><b>{exercises.length}</b> {exercises.length === 1 ? 'exercise' : 'exercises'}</span>
        <span><b>{fieldCount}</b> {fieldCount === 1 ? 'answer field' : 'answer fields'}</span>
        {#if timing?.duration_seconds != null}
          <span><b>{formatTime(timing.duration_seconds)}</b>{#if grace > 0}{' + '}{grace < 60 ? `${grace} s` : formatTime(grace)} grace{/if}</span>
        {/if}
        {#if view.calculator}<span>Calculator allowed</span>{/if}
      </div>
    {/if}
  {/snippet}

  {#snippet footer()}
    <div class="preview-footer">
      {#if view}<p><Icon name="info" size={15} /> Answers typed here are only for trying it out; nothing is saved.</p>{/if}
      <div class="footer-actions">
        {#if view}<button type="button" disabled={!answers.size} onclick={() => answers.clear()}>Clear answers</button>{/if}
        <button type="button" class="primary" onclick={onClose}>Close preview</button>
      </div>
    </div>
  {/snippet}

  {#if view}
    <div class="preview-frame">
      <div class="toolbar">
        <div class="modes" role="group" aria-label="What to show">
          <button type="button" aria-pressed={!fieldNames} onclick={() => fieldNames = false}><Icon name="preview" size={15} /> Student view</button>
          <button type="button" aria-pressed={fieldNames} onclick={() => fieldNames = true}><Icon name="fileCheck" size={15} /> Field names</button>
        </div>
        {#if view.calculator && !preview}
          <button type="button" class="tools" aria-label="Open the calculator" aria-expanded={calcOpen}
            title="The calculator this exam gives its students" disabled={allowed === null} onclick={() => calcOpen = !calcOpen}>
            <Icon name="calculator" size={16} /> Calculator
          </button>
        {:else if preview && view.calculator}
          <p class="toolbar-note">You can try this exam’s calculator after importing it.</p>
        {:else}
          <p class="toolbar-note">This exam has no calculator.</p>
        {/if}
      </div>
      <div class="exam-pane">
        {#if calcProblem}<p role="alert">The calculator could not be loaded: {calcProblem}</p>{/if}
        <ExamPreview instructions={view.content?.instructions} {exercises} {fieldNames} {answers} onChange={changeAnswer} />
      </div>
    </div>
    {#if view.calculator && !preview}
      <Calculator open={calcOpen} floating angleSetting={view.calculator.angle} modes={view.calculator.modes} allowed={allowed ?? []}
        disabledNames={view.calculator.disabled_names ?? []} {history}
        note="Preview — this is the students’ calculator. Nothing here is recorded." {api} onEvaluated={record}
        onClose={() => calcOpen = false} />
    {/if}
  {:else}
    <div class="loading">
      {#if problem}<p role="alert">{problem}</p>{:else}<p aria-live="polite">Loading preview…</p>{/if}
    </div>
  {/if}
</Modal>

<style>
  .facts { display: flex; flex-wrap: wrap; gap: .35rem; margin-top: .35rem; }
  .facts span { padding: .05rem .5rem; border: 1px solid var(--cyan-chip-border); border-radius: 999px; background: var(--side-active); color: var(--side-text); font-size: .74rem; }
  .facts b { color: var(--on-dark); font-family: var(--font-mono); }
  .preview-frame { display: grid; grid-template-rows: auto minmax(0, 1fr); height: 100%; min-width: 0; }
  .toolbar { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: .5rem; padding: .6rem 1.1rem; border-bottom: 1px solid var(--border-subtle); background: var(--bg-primary); }
  .modes { display: flex; flex-wrap: wrap; gap: 3px; padding: 3px; border-radius: 10px; background: var(--bg-secondary); }
  .toolbar button, .footer-actions button { display: inline-flex; align-items: center; gap: .4rem; min-height: 34px; margin: 0; padding: .32rem .75rem; border-radius: 8px; font-size: .84rem; font-weight: 600; }
  .modes button { min-height: 28px; border-color: var(--transparent); background: var(--transparent); color: var(--text-muted); }
  .modes button:hover { color: var(--action-hover); }
  .modes button[aria-pressed='true'] { background: var(--bg-raised); color: var(--action-hover); box-shadow: var(--shadow-raised); }
  .tools { border-color: var(--action-border); color: var(--action-hover); }
  .tools[aria-expanded='true'] { background: var(--selection-fill); border-color: var(--action-primary); }
  .toolbar-note, .loading { margin: 0; color: var(--text-muted); font-size: var(--text-sm); }
  .loading { padding: 1rem 1.1rem; }
  .exam-pane { min-width: 0; overflow: auto; padding: 1rem 1.1rem 1.25rem; }
  .preview-footer { display: flex; align-items: center; flex-wrap: wrap; gap: .5rem 1rem; }
  .preview-footer p { display: flex; align-items: center; gap: .4rem; flex: 1 1 16rem; margin: 0; color: var(--text-muted); font-size: .8rem; }
  .preview-footer p :global(svg) { flex: none; }
  .footer-actions { display: flex; justify-content: flex-end; flex-wrap: wrap; gap: .5rem; margin-left: auto; }
</style>
