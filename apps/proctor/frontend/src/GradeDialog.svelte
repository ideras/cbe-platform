<script>
  import Modal from './Modal.svelte';
  import Icon from '../../../../shared/theme/Icon.svelte';

  // Advisory only: explicit preview and submission, no destructive gate or grade publication.
  let { summary, noSubmission, exam, busy, disabled, error, onCancel, onConfirm } = $props();
  const plural = (n, noun) => `${n} ${noun}${n === 1 ? '' : 's'}`;
</script>

<Modal title="Grade the class" titleId="grade-title" tone="deep" icon="fileCheck" subtitle={`${exam} · hidden tests on each latest submission`} compact lightDismiss {busy} onClose={onCancel} width="36rem">
  <form id="grade-class-form" onsubmit={event => { event.preventDefault(); if (summary.gradable && !busy && !disabled) onConfirm(); }}>
    <dl class="preview-counts" aria-label="Estimated scope">
      <div><dt>{summary.gradable === 1 ? 'submission will' : 'submissions will'} be graded</dt><dd>{summary.gradable}</dd></div>
      <div><dt>existing grades · kept</dt><dd>{summary.done}</dd></div>
      <div><dt>without a submission · left out</dt><dd>{noSubmission}</dd></div>
    </dl>
    <ul class="impact">
      <li><Icon name="lock" size={17} /><span>Each run uses a fresh container with <strong>no network</strong>.</span></li>
      <li><Icon name="info" size={17} /><span>A new grade is <strong>advisory until you confirm it</strong> in Student Review.</span></li>
      <li class="keep"><Icon name="done" size={17} /><span>Earlier runs and any confirmed grades are <strong>kept</strong>.</span></li>
      {#if summary.waiting + summary.running}
        <li class="skip"><Icon name="clock" size={17} /><span><strong>{plural(summary.waiting + summary.running, 'student')}</strong> already waiting or being graded will be skipped.</span></li>
      {/if}
    </ul>
    <p class="note">This preview uses the latest roster. The server checks eligibility when you queue the class, regardless of your current filter.</p>
    {#if error}<p class="callout warning" role="alert">{error}</p>{/if}
  </form>
  {#snippet footer()}
    <div class="actions">
      <button type="button" onclick={onCancel} disabled={busy}>Cancel</button>
      <!-- svelte-ignore a11y_autofocus (Explicit initial focus for this native dialog.) -->
      <button class="primary" type="submit" form="grade-class-form" autofocus disabled={busy || disabled || !summary.gradable}>{busy ? 'Waiting for server…' : `Grade ${plural(summary.gradable, 'submission')}`}</button>
    </div>
  {/snippet}
</Modal>

<style>
  form { display: grid; gap: .8rem; padding: 1rem 1.1rem; }
  .preview-counts { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: .45rem; margin: 0; }
  .preview-counts div { display: flex; flex-direction: column-reverse; gap: .2rem; padding: .45rem .6rem; background: var(--bg-primary); border: 1px solid var(--border-subtle); border-top: 3px solid var(--muted-dot); border-radius: 9px; }
  .preview-counts div:first-child { border-top-color: var(--accent-cyan); }
  .preview-counts div:nth-child(2) { border-top-color: var(--warn); }
  dd { margin: 0; font: 700 1.25rem/1.2 var(--font-mono); }
  dt, .note { font-size: .78rem; color: var(--text-muted); }
  .impact { display: grid; gap: .4rem; margin: 0; padding: 0; list-style: none; }
  li { display: flex; align-items: flex-start; gap: .5rem; padding: .45rem .6rem; border-radius: 8px; background: var(--hover-fill); color: var(--action-hover); font-size: .86rem; }
  li :global(svg) { flex: none; }
  li.keep { background: var(--ok-fill); color: var(--ok-text); }
  li.skip { background: var(--warn-fill); color: var(--warn-text); }
  .note, .callout { margin: 0; }
  .actions { display: flex; justify-content: flex-end; gap: .5rem; }
  .actions button { min-height: 40px; margin: 0; }
  @media (max-width: 480px) { .preview-counts { grid-template-columns: 1fr; } .actions { flex-direction: column-reverse; } }
</style>
