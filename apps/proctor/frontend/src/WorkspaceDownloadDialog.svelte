<script>
  import { onDestroy, tick } from 'svelte';
  import * as api from '$proctor-api';
  import Icon from '../../../../shared/theme/Icon.svelte';
  import Modal from './Modal.svelte';

  let { batch, onClose } = $props();
  const files = $derived(batch.kind === 'files');
  const title = $derived(files ? 'Download all student files' : 'Download all evaluation reports');
  let busy = $state(false);
  let format = $state('markdown');
  let overwrite = $state(false);
  let problem = $state('');
  let result = $state(null);
  let progress = $state(null);
  let doneButton = $state(null);
  let chooseButton = $state(null);
  let timer;
  let polling = false;

  onDestroy(() => clearInterval(timer));

  async function readProgress() {
    if (!busy || polling) return;
    polling = true;
    try {
      const value = await api.WorkspaceDownloadProgress();
      if (busy) progress = value;
    } catch { /* The result of the download itself is authoritative. */ }
    finally { polling = false; }
  }

  async function download() {
    if (busy || result) return;
    busy = true;
    problem = '';
    progress = null;
    timer = setInterval(readProgress, 250);
    try {
      const kind = !files && format === 'json' ? 'reports-json' : batch.kind;
      const value = await api.DownloadWorkspaceBatch(batch.examId, batch.server, kind, !files && overwrite);
      if (value.cancelled) { onClose(); return; }
      result = value;
    } catch (err) {
      problem = String(err);
    } finally {
      busy = false;
      clearInterval(timer);
    }
    await tick();
    (doneButton ?? chooseButton)?.focus();
  }
</script>

<Modal {title} subtitle={batch.title} titleId="workspace-download-title" tone="deep" icon="download" compact lightDismiss {busy} {onClose}>
  <div class="content">
    {#if busy}
      <div class="callout" role="status">
        {#if progress?.total}
          {progress.done} of {progress.total} students checked
        {:else}
          Waiting for the folder selection or roster…
        {/if}
      </div>
      <progress aria-label="Students checked" value={progress?.total ? progress.done : undefined} max={progress?.total || 1}></progress>
    {:else if result}
      <div class="callout" class:warning={result.failed > 0} role="status">
        <strong>{result.written} written · {result.skipped} skipped · {result.failed} failed</strong>
        <div class="path" title={result.folder}>{result.folder}</div>
      </div>
      {#if result.issues?.length}
        <ul class="issues" aria-label="Skipped and failed students">
          {#each result.issues as { student, status, reason }}
            <li><strong>{student.student_name} ({student.account_number})</strong><span>{status === 'failed' ? 'Failed' : 'Skipped'}: {reason}</span></li>
          {/each}
        </ul>
      {/if}
    {:else}
      <div class="callout"><strong>Entire exam</strong> · Includes students outside the current filter.</div>
      <p>{files
        ? 'Files from the latest recorded submission go into a folder for each student, with their directory structure intact.'
        : 'Saves one report per student, based on the TODO items in Student Review → Grading. It includes results, points and the feedback shown when a TODO is expanded. The latest submission must have a completed grading run.'}</p>
      <p>Students without an eligible {files ? 'submission' : 'report'} are skipped.{files ? ' Existing folders are never replaced.' : ''}</p>
      {#if !files}
        <label class="format">Format
          <select bind:value={format}>
            <option value="markdown">Markdown</option>
            <option value="json">JSON</option>
          </select>
        </label>
        <p>Review the grader’s feedback before sharing the reports with students.</p>
        <label class="overwrite" class:checked={overwrite}>
          <input type="checkbox" bind:checked={overwrite} aria-labelledby="workspace-overwrite-label" aria-describedby="workspace-overwrite-note" />
          <span><strong id="workspace-overwrite-label">Overwrite existing files</strong><small id="workspace-overwrite-note">{overwrite ? 'Matching reports will be replaced, including any edits. Other files are untouched.' : 'Matching reports are skipped unless you select this option.'}</small></span>
        </label>
      {/if}
      {#if problem}<div class="callout warning" role="alert">{problem}</div>{/if}
    {/if}
  </div>
  {#snippet footer()}
    <div class="actions">
      {#if result}
        <button type="button" class="primary" bind:this={doneButton} onclick={onClose}>Done</button>
      {:else}
        <button type="button" class="ghost" disabled={busy} onclick={onClose}>Cancel</button>
        <!-- svelte-ignore a11y_autofocus (Focus the primary action in this level-zero modal.) -->
        <button type="button" class="primary" bind:this={chooseButton} autofocus disabled={busy} onclick={download}><Icon name="folder" size={16} />Choose folder…</button>
      {/if}
    </div>
  {/snippet}
</Modal>

<style>
  .content { display: grid; gap: .75rem; padding: 1.1rem; }
  p { margin: 0; }
  progress { width: 100%; accent-color: var(--action-primary); }
  .format { display: grid; gap: .25rem; font-weight: 600; }
  .overwrite { display: flex; align-items: flex-start; gap: .55rem; border: 1px solid var(--border-subtle); border-left: 4px solid var(--warn); border-radius: 8px; background: var(--bg-primary); padding: .55rem .65rem; cursor: pointer; }
  .overwrite.checked { border-left-color: var(--alert); background: var(--alert-fill); color: var(--alert-text); }
  .overwrite input { flex: none; margin: .2rem 0 0; accent-color: var(--action-primary); }
  .overwrite small { display: block; }
  .path { margin-top: .25rem; font-family: var(--font-mono); font-size: .85rem; overflow-wrap: anywhere; }
  .issues { display: grid; gap: .4rem; padding: 0; margin: 0; list-style: none; }
  .issues li { display: grid; gap: .15rem; padding: .45rem .65rem; border-radius: 8px; background: var(--bg-primary); overflow-wrap: anywhere; }
  .issues span { color: var(--text-muted); }
  .actions { display: flex; justify-content: flex-end; gap: .5rem; }
  .actions button { justify-content: center; }
  @media (max-width: 480px) { .actions { flex-direction: column-reverse; } }
</style>
