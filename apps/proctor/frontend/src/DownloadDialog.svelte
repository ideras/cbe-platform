<script>
  import { onDestroy, onMount } from 'svelte';
  import * as api from '$proctor-api';
  import Modal from './Modal.svelte';
  // Downloading the submissions of one exam: one Markdown file per finished student, in a folder the proctor names, with an
  // _index.md checklist. Choosing the parent folder and starting the download are separate steps, so the proctor can edit
  // the folder name and see what will happen before anything is written. When the folder of an earlier download already
  // exists, a second step asks before replacing its files. The dialog stays open after the download, with Open folder and
  // Close, so the result is never lost by an automatic close.
  let { exam, onClose, onDownloaded } = $props();
  let preview = $state(null);
  let parentFolder = $state('');
  let folderName = $state('');
  // form, downloading, replace or done.
  let stage = $state('form');
  let replaceInfo = $state(null);
  let result = $state(null);
  let problem = $state('');
  let progress = $state({ running: false, done: 0, total: 0, current: '' });
  let timer = null;

  const busy = $derived(stage === 'downloading');
  const done = $derived(stage === 'done');
  const replacing = $derived(!!replaceInfo);
  // Nothing can be changed while the download runs, after it finished, or while the replace question is being answered.
  const locked = $derived(busy || done || replacing);
  const canStart = $derived(!!preview && parentFolder !== '' && folderName.trim() !== '' && !locked);
  const percent = $derived(progress.total > 0 ? Math.min(100, Math.round((progress.done * 100) / progress.total)) : busy ? 5 : 100);

  onMount(async () => {
    try {
      preview = await api.PrepareDownload(exam.exam_id);
      if (!folderName) folderName = preview.folder_name;
    } catch (err) { problem = String(err); }
  });
  onDestroy(stopPolling);

  const plural = (n, noun) => `${n} ${noun}${n === 1 ? '' : 's'}`;
  function stopPolling() {
    if (timer) { clearInterval(timer); timer = null; }
  }
  function startPolling() {
    stopPolling();
    const tick = async () => {
      try { progress = await api.DownloadProgress(); } catch { /* a failed poll leaves the last progress on screen */ }
    };
    tick();
    timer = setInterval(tick, 200);
  }
  async function chooseFolder() {
    if (locked) return;
    problem = '';
    try {
      const path = await api.ChooseDownloadFolder();
      if (path) parentFolder = path;
    } catch (err) { problem = String(err); }
  }
  async function start(replace) {
    if (busy || done) return;
    if (!parentFolder) { problem = 'Choose a folder for the download first.'; return; }
    if (!folderName.trim()) { problem = 'Enter a name for the output folder.'; return; }
    problem = '';
    replaceInfo = null;
    stage = 'downloading';
    progress = { running: true, done: 0, total: preview?.finished ?? 0, current: '' };
    startPolling();
    try {
      const value = await api.DownloadSubmissions(exam.exam_id, folderName.trim(), replace, true);
      if (value.needs_confirmation) { replaceInfo = value; stage = 'replace'; return; }
      result = value;
      stage = 'done';
      onDownloaded?.(value);
    } catch (err) {
      problem = String(err);
      stage = 'form';
    } finally {
      stopPolling();
      try { progress = await api.DownloadProgress(); } catch { /* keep the last progress */ }
    }
  }
  async function openFolder() {
    problem = '';
    try { await api.OpenDownloadFolder(); } catch (err) { problem = String(err); }
  }
</script>
<Modal title={`Download submissions — ${exam.title}`} titleId="download-title" tone={done ? 'info' : replacing ? 'warning' : 'info'} busy={busy} onClose={onClose} width="42rem">
  <p>One Markdown file per student, with their answers in the exam.</p>
  {#if preview}
    <table class="counts"><tbody>
      <tr><td class="number">{preview.finished}</td><td>finished</td><td class="what">will be downloaded</td></tr>
      {#if preview.working > 0}<tr><td class="number">{preview.working}</td><td>still working</td><td class="what">skipped; download again when {preview.working === 1 ? 'that student finishes' : 'they finish'}</td></tr>{/if}
      {#if preview.finishing > 0}<tr><td class="number">{preview.finishing}</td><td>still finishing</td><td class="what">skipped; the server is finalizing their answers</td></tr>{/if}
      {#if preview.never_logged_in > 0}<tr><td class="number">{preview.never_logged_in}</td><td>never logged in</td><td class="what">listed in _index.md only</td></tr>{/if}
    </tbody></table>
  {:else if !problem}
    <p class="muted">Reading the list of students…</p>
  {/if}

  <fieldset disabled={locked}>
    <label>Folder <span class="muted">— where the output folder is created</span>
      <span class="folder-row">
        <input class="grow" readonly value={parentFolder} placeholder="No folder chosen" aria-label="Selected folder" />
        <button type="button" onclick={chooseFolder}>Choose folder…</button>
      </span>
    </label>
    <label>Output folder name
      <input bind:value={folderName} maxlength="255" spellcheck="false" aria-label="Output folder name" />
    </label>
  </fieldset>
  {#if parentFolder && folderName.trim()}<p class="folder">Files go into <code>{parentFolder}/{folderName.trim()}</code>.</p>{/if}

  {#if busy}
    <div class="progress" role="progressbar" aria-label="Downloading submissions" aria-valuemin="0" aria-valuemax={Math.max(progress.total, 1)} aria-valuenow={progress.total > 0 ? progress.done : 0}>
      <div class="bar" style:width={`${percent}%`}></div>
    </div>
    <p class="muted progress-text">{progress.total > 0 ? `${progress.done} of ${progress.total} files` : 'Preparing the files…'}{progress.current ? ` — ${progress.current}` : ''}</p>
  {/if}

  {#if done}
    <div class="result">
      <p role="status">Downloaded {plural(result.files.length, 'submission')} to <code>{result.folder}</code>.</p>
      {#if result.skipped.length}
        <p class="muted">{result.skipped.filter(item => item.status === 'working').length
          ? `${plural(result.skipped.filter(item => item.status === 'working').length, 'student')} still working and ${result.skipped.filter(item => item.status === 'working').length === 1 ? 'was' : 'were'} skipped. `
          : ''}{result.skipped.filter(item => item.status === 'finishing').length
          ? `${plural(result.skipped.filter(item => item.status === 'finishing').length, 'student')} still finishing and ${result.skipped.filter(item => item.status === 'finishing').length === 1 ? 'was' : 'were'} skipped.`
          : ''}</p>
      {/if}
      {#if result.failed.length}
        <p class="callout warning" role="alert">PARTIAL: {result.failed.length} could not be written. Retry for {result.failed.map(item => item.student_name || item.account_number).filter(Boolean).join(', ')}.</p>
      {/if}
      {#each result.warnings as warning}<p class="callout warning">{warning}</p>{/each}
    </div>
  {/if}

  {#if replacing}
    <div class="replace">
      <p><strong>Replace the {plural(replaceInfo.replace_count, 'file')} from the earlier download?</strong></p>
      <p>Notes you wrote in them are lost. Files you added yourself are left alone.</p>
    </div>
  {/if}

  {#if problem}<p role="alert">{problem}</p>{/if}

  <div class="actions">
    {#if replacing}
      <button type="button" onclick={onClose}>Cancel</button>
      <button type="button" class="danger" onclick={() => start(true)}>Replace files</button>
    {:else if done}
      {#if result.files.length}<button type="button" onclick={openFolder}>Open folder</button>{/if}
      <button type="button" class="primary" onclick={onClose}>Close</button>
    {:else}
      <button type="button" disabled={busy} onclick={onClose}>Cancel</button>
      <button type="button" class="primary" disabled={!canStart} onclick={() => start(false)}>{busy ? 'Downloading…' : 'Start download'}</button>
    {/if}
  </div>
</Modal>
<style>
  p { margin: var(--space-2) 0; overflow-wrap: anywhere; word-break: break-word; }
  code { overflow-wrap: anywhere; word-break: break-word; }
  .counts { margin: var(--space-3) 0; border-collapse: collapse; max-width: 100%; }
  .counts td { padding: .2rem .6rem .2rem 0; vertical-align: baseline; overflow-wrap: anywhere; }
  .counts .number { min-width: 3.5rem; color: var(--title-ink); font-size: 1.25rem; font-weight: 750; text-align: right; }
  .counts .what { color: var(--color-text-muted); font-size: var(--text-sm); }
  fieldset { display: grid; gap: var(--space-2); margin: var(--space-3) 0 0; padding: 0; border: 0; }
  label { display: grid; gap: var(--space-1); font-weight: 600; }
  .folder-row { display: flex; gap: var(--space-2); }
  .folder-row .grow { flex: 1; min-width: 0; }
  .folder-row button { margin: 0; white-space: nowrap; }
  .folder { color: var(--color-text-muted); font-size: var(--text-sm); overflow-wrap: anywhere; }
  .progress { height: .7rem; margin: var(--space-3) 0 var(--space-1); background: var(--line); border-radius: 999px; overflow: hidden; }
  .progress .bar { height: 100%; background: var(--sea-ink); border-radius: inherit; transition: width .2s ease; }
  .progress-text { margin-top: 0; }
  .result, .replace { margin-top: var(--space-3); padding-top: var(--space-3); border-top: 1px solid var(--panel-border); }
  .result p:first-child { margin-top: 0; }
  .replace p:first-child { margin-top: 0; }
  .actions { display: flex; flex-wrap: wrap; justify-content: flex-end; gap: var(--space-2); margin-top: var(--space-4); }
  .actions button { margin: 0; }
</style>
