<script>
  // The exam preview dialog: validated import content or a stored exam as the students see it. Stored exams
  // also have their calculator available as a non-modal dialog from the tools button — the same
  // embeddable component the student application hosts, evaluating through the proctor's service with the same
  // engine. Nothing in a preview is recorded: no log, no record; the preview's state lives only until another
  // exam is previewed or the dialog closes.
  import { onDestroy } from 'svelte';
  import * as api from '$proctor-api';
  import Modal from './Modal.svelte';
  import ExamPreview from './ExamPreview.svelte';
  import Calculator from '../../../../shared/theme/calculator/Calculator.svelte';
  import Icon from './Icon.svelte';

  let { examID, preview = null, onClose } = $props();

  let loadedView = $state(null);
  const view = $derived(preview ?? loadedView);
  let problem = $state('');
  let allowed = $state([]);
  let history = $state([]);
  let calcOpen = $state(false);

  // Import previews use data already checked by ChooseExam, not the stored-exam calculator session.
  // Closing one must neither discard the pending import nor interfere with a stored preview's state.
  onDestroy(() => { if (!preview) api.DiscardPreview().catch(() => {}); });

  $effect(() => {
    if (preview) return;
    let current = true;
    (async () => {
      try {
        const loaded = await api.PreviewExam(examID);
        if (!current) return;
        loadedView = loaded;
        if (loaded.calculator) allowed = await api.CalcAllowedFunctions();
      } catch (err) { if (current) problem = String(err); }
    })();
    return () => { current = false; };
  });

  function record(entry) {
    history = [...history, entry].slice(-20);
  }
</script>

<Modal title={view ? `Preview — ${view.title}` : 'Loading preview…'} width="52rem" onClose={onClose}>
  {#if problem}<p role="alert">{problem}</p>{/if}
  {#if view}
    <div class="toolbar">
      {#if view.calculator && !preview}
        <button type="button" class="tools" aria-label="Open the calculator" aria-expanded={calcOpen}
          title="The calculator this exam gives its students" onclick={() => { calcOpen = !calcOpen; }}>
          <Icon name="calculator" size="1.25rem" /> Calculator
        </button>
      {/if}
    </div>
    {#if calcOpen && view.calculator && !preview}
      <Calculator open floating angleSetting={view.calculator.angle} modes={view.calculator.modes} {allowed}
        disabledNames={view.calculator.disabled_names ?? []} {history}
        note="Preview — this is the students’ calculator. Nothing here is recorded." {api} onEvaluated={record}
        onClose={() => { calcOpen = false; }} />
    {/if}
    {#if preview && view.calculator}
      <p class="muted">You can try this exam’s calculator after importing it.</p>
    {:else if !view.calculator}
      <p class="muted">This exam has no calculator.</p>
    {/if}
    <div class="content"><details open><summary>The exam as students see it</summary><ExamPreview content={view.content} /></details></div>
  {:else if !problem}
    <p class="muted">Loading…</p>
  {/if}
</Modal>

<style>
  .toolbar { display: flex; justify-content: flex-start; margin: 0 0 var(--space-3); }
  .toolbar .tools { display: inline-flex; align-items: center; gap: var(--space-2); margin: 0; font-weight: 600; }
  .toolbar .tools :global(svg) { width: 1.25rem; height: 1.25rem; object-fit: contain; }
  .toolbar .tools[aria-expanded='true'] { background: var(--sea); border-color: var(--sea-ink); color: var(--sea-ink); }
  .content { min-width: 0; }
  .muted { color: var(--color-text-muted); font-size: var(--text-sm); }
  details summary { cursor: pointer; font-weight: 600; color: var(--brand-strong); margin: var(--space-2) 0; }
</style>
