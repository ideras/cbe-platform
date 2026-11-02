<script>
  import { onDestroy, onMount } from 'svelte';
  import * as api from '$proctor-api';
  import Modal from './Modal.svelte';
  import Icon from '../../../../shared/theme/Icon.svelte';
  // Downloading the submissions of one exam: one Markdown file per finished student, in a folder the proctor names, with an
  // _index.md checklist. The files are either the complete exam with each student's answers inserted (the default) or, as
  // answers only, the saved answers under their field labels without the questions. The dialog reads top to bottom as three
  // steps (format, who is included, destination) so the proctor can edit the folder name and see what will happen before
  // anything is written. When the folder of an earlier download already exists, a second step asks before replacing its
  // files, and repeats exactly the format and folder name of the first. The dialog stays open after the download, with
  // Open folder and Done, so the result is never lost by an automatic close.
  let { exam, onClose, onDownloaded } = $props();
  const formats = [
    { value: 'complete', label: 'Complete exam', phrase: 'complete-exam', icon: 'exams', help: 'The exam text with each student’s answers inserted.', tag: 'Self-contained for grading', tone: 'rec' },
    { value: 'answers-only', label: 'Answers only', phrase: 'answers-only', icon: 'fileCheck', help: 'Saved answers and field labels, without the exam questions.', tag: 'Needs the original exam to grade', tone: 'warn' },
  ];
  const formatOf = value => formats.find(item => item.value === value) ?? formats[0];
  // The characters the dashboard refuses in a folder name. It is the part of its rules that can be told while typing; the
  // dashboard still checks the whole name and says why when it refuses one.
  const FOLDER_NAME_CHARS = /[\\/:*?"<>|]/;
  let preview = $state(null);
  let parentFolder = $state('');
  let folderName = $state('');
  let format = $state('complete');
  // The folder name stays what the proctor typed once they typed anything; until then it follows the format's suggestion.
  let folderEdited = $state(false);
  // The options of the download that started, kept for the replace question: its answer repeats them, whatever the page shows.
  let request = $state(null);
  // form, downloading, replace or done.
  let stage = $state('form');
  let replaceInfo = $state(null);
  let result = $state(null);
  let problem = $state('');
  let progress = $state({ running: false, done: 0, total: 0, current: '' });
  // '', copied or failed: what the Copy path button of the result says for a moment.
  let copyState = $state('');
  let timer = null;
  let copyTimer = null;

  const busy = $derived(stage === 'downloading');
  const done = $derived(stage === 'done');
  const replacing = $derived(!!replaceInfo);
  // Nothing can be changed while the download runs, after it finished, or while the replace question is being answered.
  const locked = $derived(busy || done || replacing);
  const defaultName = $derived(preview ? suggestion(format) : '');
  const nameProblem = $derived(!preview ? ''
    : folderName.trim() === '' ? 'Enter a name for the output folder.'
    : FOLDER_NAME_CHARS.test(folderName) ? 'The output folder name cannot contain / \\ : * ? " < > or |.' : '');
  const canReset = $derived(folderEdited && folderName !== defaultName);
  const canStart = $derived(!!preview && parentFolder !== '' && !nameProblem && !locked);
  const percent = $derived(progress.total > 0 ? Math.min(100, Math.round((progress.done * 100) / progress.total)) : busy ? 5 : 100);
  // Students who are not finished are skipped; the tiles say how many, and one of them is only there when somebody is finishing.
  const unfinished = $derived(preview ? preview.working + preview.finishing : 0);
  const count = (list, status) => list.filter(item => item.status === status).length;
  const notIncluded = $derived(done ? [
    [count(result.skipped, 'working'), 'still working'],
    [count(result.skipped, 'finishing'), 'still finishing'],
    [preview?.never_logged_in ?? 0, 'never logged in (listed in the index)'],
  ].filter(([n]) => n > 0).map(([n, what]) => `${n} ${what}`) : []);
  const partial = $derived(done && result.failed.length > 0);

  function suggestion(value) { return value === 'answers-only' ? preview.answers_only_folder_name : preview.folder_name; }

  onMount(async () => {
    try {
      preview = await api.PrepareDownload(exam.exam_id);
      if (!folderEdited) folderName = suggestion(format);
    } catch (err) { problem = String(err); }
  });
  onDestroy(() => { stopPolling(); clearTimeout(copyTimer); });

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
  // Choosing a format changes what the dialog says and, until the proctor typed a folder name, the name it offers. It sends nothing.
  function chooseFormat(value) {
    if (locked) return;
    format = value;
    if (preview && !folderEdited) folderName = suggestion(value);
  }
  function useDefaultName() {
    if (locked) return;
    folderEdited = false;
    folderName = defaultName;
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
    const options = replace && request ? request : { format, folderName: folderName.trim() };
    if (!parentFolder) { problem = 'Choose a folder for the download first.'; return; }
    if (!options.folderName) { problem = 'Enter a name for the output folder.'; return; }
    request = options;
    problem = '';
    replaceInfo = null;
    stage = 'downloading';
    progress = { running: true, done: 0, total: preview?.finished ?? 0, current: '' };
    startPolling();
    try {
      const value = await api.DownloadSubmissionsWithMode(exam.exam_id, options.folderName, options.format, replace, true);
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
  async function copyPath() {
    clearTimeout(copyTimer);
    try {
      if (!navigator.clipboard?.writeText) throw new Error('Clipboard unavailable');
      await navigator.clipboard.writeText(result.folder);
      copyState = 'copied';
    } catch { copyState = 'failed'; }
    copyTimer = setTimeout(() => copyState = '', 1500);
  }
</script>
<Modal title="Download submissions" subtitle={exam.title} titleId="download-title" tone="deep" icon="download" width="620px" {busy} {onClose}>
  <div class="content">
    {#if done}
      <div class="hero" class:partial role="status">
        <span class="badge"><Icon name={partial ? 'warning' : 'check'} size={22} /></span>
        <div>
          <h3>{partial ? `${result.files.length} of ${result.files.length + result.failed.length} submissions downloaded` : `${plural(result.files.length, 'submission')} downloaded`}</h3>
          <p>{formatOf(result.format).label} · plus <span class="mono">_index.md</span></p>
        </div>
      </div>

      <dl class="summary">
        <dt>Folder</dt>
        <dd>
          <span class="mono path-text">{result.folder}</span>
          <button type="button" class="copy" onclick={copyPath}>
            <Icon name={copyState === 'copied' ? 'check' : 'copy'} size={13} />
            <span aria-live="polite">{copyState === 'copied' ? 'Copied!' : copyState === 'failed' ? 'Could not copy' : 'Copy path'}</span>
          </button>
        </dd>
        <dt>Files</dt>
        <dd><span class="mono">{result.files.length}</span> student {result.files.length === 1 ? 'file' : 'files'} + <span class="mono">_index.md</span></dd>
        {#if notIncluded.length}<dt>Not included</dt><dd>{notIncluded.join(', ')}</dd>{/if}
      </dl>

      {#if result.failed.length}
        <p class="tip" role="alert"><Icon name="warning" size={15} /><span>PARTIAL: {result.failed.length} could not be written. Retry for {result.failed.map(item => item.student_name || item.account_number).filter(Boolean).join(', ')}.</span></p>
      {/if}
      {#each result.warnings as warning}<p class="tip"><Icon name="warning" size={15} /><span>{warning}</span></p>{/each}
      {#if result.format === 'answers-only'}
        <p class="tip"><Icon name="info" size={15} /><span>Answers-only files don’t include the questions. Keep the original exam handy for grading.</span></p>
      {/if}
    {:else}
      <fieldset disabled={locked} aria-labelledby="download-format-label">
        <div class="section-label" id="download-format-label"><span class="step" aria-hidden="true">1</span>Format · one Markdown file per student</div>
        <div class="formats">
          {#each formats as option (option.value)}
            <label class="fmt" class:selected={format === option.value}>
              <input type="radio" name="download-format" value={option.value} checked={format === option.value} onchange={() => chooseFormat(option.value)}
                aria-labelledby={`format-name-${option.value}`} aria-describedby={`format-help-${option.value} format-tag-${option.value}`} />
              <span class="top">
                <span class="ico"><Icon name={option.icon} size={16} /></span>
                <span class="name" id={`format-name-${option.value}`}>{option.label}</span>
                <span class="radio" aria-hidden="true"></span>
              </span>
              <span class="desc" id={`format-help-${option.value}`}>{option.help}</span>
              <span class={`tag ${option.tone}`} id={`format-tag-${option.value}`}>{option.tag}</span>
            </label>
          {/each}
        </div>
      </fieldset>

      <div role="group" aria-labelledby="download-who-label">
        <div class="section-label" id="download-who-label"><span class="step" aria-hidden="true">2</span>Who is included</div>
        {#if preview}
          <div class="who" style:--tiles={preview.finishing > 0 ? 4 : 3}>
            <div class="w in"><span class="num">{preview.finished}</span><span class="lbl">finished</span><span class="s">one file each</span></div>
            <div class="w wip"><span class="num">{preview.working}</span><span class="lbl">still working</span><span class="s">skipped</span></div>
            {#if preview.finishing > 0}<div class="w wip"><span class="num">{preview.finishing}</span><span class="lbl">still finishing</span><span class="s">skipped; being finalized</span></div>{/if}
            <div class="w out"><span class="num">{preview.never_logged_in}</span><span class="lbl">never logged in</span><span class="s">listed in <code>_index.md</code> only</span></div>
          </div>
          {#if unfinished > 0}<p class="hint">Download again when {unfinished === 1 ? 'that student finishes' : 'they finish'}.</p>{/if}
        {:else if !problem}
          <p class="hint">Reading the list of students…</p>
        {/if}
      </div>

      <fieldset disabled={locked} aria-labelledby="download-destination-label">
        <div class="section-label" id="download-destination-label"><span class="step" aria-hidden="true">3</span>Destination</div>
        <div class="destination">
          <div class="field">
            <span class="field-label">Parent folder <span class="optional">· the output folder is created here</span></span>
            <div class="folder-row">
              <div class="path" title={parentFolder || undefined}>
                <Icon name="folder" size={16} />
                {#if parentFolder}<span class="txt set">&lrm;{parentFolder}&lrm;</span>{:else}<span class="txt">No folder chosen</span>{/if}
              </div>
              <button type="button" class="outline" onclick={chooseFolder}>Choose folder…</button>
            </div>
          </div>

          <div class="field">
            <label class="field-label" for="download-folder-name">Output folder name</label>
            <input id="download-folder-name" class="text-input" class:err={!!nameProblem} bind:value={folderName} oninput={() => folderEdited = true}
              maxlength="255" spellcheck="false" aria-invalid={!!nameProblem} aria-describedby={nameProblem ? 'download-folder-problem' : undefined} />
            {#if nameProblem}<p class="field-error" id="download-folder-problem">{nameProblem}</p>{/if}
            {#if canReset}<button type="button" class="reset" onclick={useDefaultName}>Use default name</button>{/if}
          </div>

          {#if parentFolder}
            <div class="final">
              <Icon name="chevron" size={15} />
              <div>
                <div class="k">Files go into{#if replacing}<span class="tagnew exists">existing folder · files replaced</span>{/if}</div>
                <div class="p">{parentFolder}/<b>{folderName.trim() || '…'}</b></div>
              </div>
            </div>
          {/if}
        </div>
      </fieldset>

      {#if busy}
        <div class="running">
          <div class="row">
            <span>{progress.total > 0 ? 'Writing files…' : 'Preparing the files…'}{progress.current ? ` ${progress.current}` : ''}</span>
            <b>{progress.total > 0 ? `${progress.done} / ${progress.total}` : ''}</b>
          </div>
          <div class="progress" role="progressbar" aria-label="Downloading submissions" aria-valuemin="0" aria-valuemax={Math.max(progress.total, 1)} aria-valuenow={progress.total > 0 ? progress.done : 0}>
            <div class="bar" style:width={`${percent}%`}></div>
          </div>
        </div>
      {/if}

      {#if replacing}
        <div class="tip replace">
          <Icon name="warning" size={15} />
          <div>
            <p><strong>Replace the {plural(replaceInfo.replace_count, 'file')} from the earlier download?</strong></p>
            {#if replaceInfo.existing_format && replaceInfo.existing_format !== replaceInfo.format}
              <p>The earlier download in this folder is {formatOf(replaceInfo.existing_format).phrase}, and this one is {formatOf(replaceInfo.format).phrase}. Its files change format.</p>
            {/if}
            <p>Notes you wrote in them are lost. Files you added yourself are left alone.</p>
          </div>
        </div>
      {/if}
    {/if}

    {#if problem}<p class="tip problem" role="alert"><Icon name="error" size={15} /><span>{problem}</span></p>{/if}
  </div>

  <div class="foot">
    {#if replacing}
      <button type="button" class="cancel" onclick={onClose}>Cancel</button>
      <button type="button" class="danger" onclick={() => start(true)}>Replace files</button>
    {:else if done}
      {#if result.files.length}<button type="button" class="outline" onclick={openFolder}><Icon name="folder" size={16} />Open folder</button>{/if}
      <button type="button" class="primary" onclick={onClose}>Done</button>
    {:else}
      {#if preview}
        <span class="foot-note">{#if preview.finished > 0}<b>{preview.finished}</b> {preview.finished === 1 ? 'file' : 'files'} will be written{:else}Only <b>_index.md</b> will be written{/if}</span>
      {/if}
      <button type="button" class="cancel" disabled={busy} onclick={onClose}>Cancel</button>
      <button type="button" class="primary" disabled={!canStart} onclick={() => start(false)}>
        <Icon name="download" size={16} />
        <span>{busy ? 'Downloading…' : !preview ? 'Download' : preview.finished > 0 ? `Download ${plural(preview.finished, 'file')}` : 'Download index only'}</span>
      </button>
    {/if}
  </div>
</Modal>
<style>
  p { margin: 0; overflow-wrap: anywhere; }
  h3 { margin: 0; }
  fieldset { min-width: 0; margin: 0; padding: 0; border: 0; }
  .mono { font-family: var(--font-mono); }
  .content { display: flex; flex-direction: column; gap: 1.1rem; padding: 1.1rem; }

  .section-label { display: flex; align-items: center; gap: .5rem; margin-bottom: .5rem; color: var(--action-primary); font-size: .72rem; font-weight: 700; letter-spacing: .08em; text-transform: uppercase; }
  .step { display: grid; place-items: center; width: 20px; height: 20px; border-radius: 50%; background: var(--selection-fill); color: var(--action-hover); font-size: .7rem; letter-spacing: 0; }
  .hint { color: var(--text-muted); font-size: .82rem; }
  .who + .hint { margin-top: .5rem; }

  /* Format cards. The radio covers its card, so a click anywhere on it chooses the format and the keyboard still has a real control. */
  .formats { display: grid; grid-template-columns: 1fr 1fr; gap: .6rem; }
  .fmt { position: relative; display: flex; flex-direction: column; gap: .35rem; padding: .8rem .85rem; border: 1.5px solid var(--border-subtle); border-radius: 12px; background: var(--bg-primary); cursor: pointer; transition: border-color .15s, background-color .15s, box-shadow .15s; }
  fieldset:not(:disabled) .fmt:hover { border-color: var(--accent-cyan-soft); background: var(--hover-fill); }
  fieldset:disabled .fmt { cursor: default; }
  .fmt input { position: absolute; inset: 0; width: 100%; height: 100%; margin: 0; opacity: 0; cursor: inherit; }
  .top { display: flex; align-items: center; gap: .55rem; font-weight: 700; }
  .ico { display: grid; place-items: center; width: 30px; height: 30px; border-radius: 8px; background: var(--bg-secondary); color: var(--text-muted); transition: background-color .15s, color .15s; }
  .radio { display: grid; place-items: center; width: 18px; height: 18px; margin-left: auto; border: 2px solid var(--control-border); border-radius: 50%; transition: border-color .15s; }
  .radio::after { content: ''; width: 8px; height: 8px; border-radius: 50%; background: var(--transparent); }
  .desc { color: var(--text-muted); font-size: .82rem; }
  .tag { width: fit-content; padding: .08rem .5rem; border-radius: 999px; font-size: .72rem; font-weight: 600; }
  .tag.rec { background: var(--ok-fill); color: var(--ok-text); }
  .tag.warn { background: var(--warn-fill); color: var(--warn-text); }
  .fmt.selected { border-color: var(--action-primary); background: var(--selection-fill); box-shadow: var(--shadow-card-hover); }
  .fmt.selected .ico { background: var(--action-primary); color: var(--bg-primary); }
  .fmt.selected .radio { border-color: var(--action-primary); }
  .fmt.selected .radio::after { background: var(--action-primary); }
  /* The muted description is under 4.5:1 on the selected card's fill. */
  .fmt.selected .desc { color: var(--text-main); }
  .fmt:has(input:focus-visible) { box-shadow: 0 0 0 3px var(--focus-halo-strong); }

  /* Who is included. */
  .who { display: grid; grid-template-columns: repeat(var(--tiles, 3), minmax(0, 1fr)); gap: .6rem; }
  .w { display: flex; flex-direction: column; gap: .1rem; padding: .6rem .75rem; border: 1px solid var(--border-subtle); border-top: 3px solid var(--muted-dot); border-radius: 10px; background: var(--bg-primary); }
  .num { font-family: var(--font-mono); font-size: 1.45rem; font-weight: 700; line-height: 1.1; }
  .lbl { font-size: .85rem; font-weight: 600; }
  .s { color: var(--text-muted); font-size: .76rem; }
  .s code { padding: 0 .25rem; border-radius: 3px; background: var(--bg-secondary); color: var(--text-main); font-size: .74rem; }
  .w.in { border-top-color: var(--ok); }
  .w.in .num { color: var(--ok-text); }
  .w.out .num, .w.wip .num { color: var(--text-muted); }
  .w.wip { border-top-color: var(--warn); }

  /* Destination. */
  .destination { display: flex; flex-direction: column; gap: .75rem; }
  .field { display: flex; flex-direction: column; gap: .35rem; }
  .field-label { font-size: .85rem; font-weight: 600; }
  .optional { color: var(--text-muted); font-weight: 400; }
  .folder-row { display: flex; gap: .5rem; }
  .path { display: flex; flex: 1; align-items: center; gap: .5rem; min-width: 0; height: 40px; padding: 0 .7rem; border: 1px solid var(--border-subtle); border-radius: 9px; background: var(--bg-primary); }
  .path :global(svg) { color: var(--warn); }
  .txt { overflow: hidden; color: var(--text-muted); font-family: var(--font-mono); font-size: .82rem; text-overflow: ellipsis; white-space: nowrap; }
  /* The end of a long path says more than its start, so the path is cut at the front. */
  .txt.set { color: var(--text-main); direction: rtl; text-align: left; }
  .text-input { height: 40px; padding: 0 .7rem; border-radius: 9px; font-family: var(--font-mono); font-size: .9rem; font-weight: 600; }
  .text-input.err, .text-input.err:focus-visible { border-color: var(--alert); }
  .field-error { color: var(--alert-text); font-size: .8rem; }
  .reset { width: fit-content; min-height: 0; padding: 0; border: 0; background: var(--transparent); color: var(--action-primary); font-size: .78rem; font-weight: 400; }
  .reset:hover:not(:disabled) { background: var(--transparent); color: var(--action-primary); text-decoration: underline; }
  .reset:disabled { background: var(--transparent); color: var(--muted-dot); }
  .final { display: flex; align-items: flex-start; gap: .55rem; padding: .6rem .75rem; border: 1px dashed var(--border-subtle); border-radius: 10px; background: var(--bg-primary); }
  .final :global(svg) { margin-top: .15rem; color: var(--action-primary); }
  .k { color: var(--text-muted); font-size: .72rem; font-weight: 700; letter-spacing: .06em; text-transform: uppercase; }
  .p { color: var(--text-muted); font-family: var(--font-mono); font-size: .8rem; word-break: break-all; }
  .p b { padding: 0 .2rem; border-radius: 3px; background: var(--selection-fill); color: var(--action-hover); font-weight: 700; }
  .tagnew { margin-left: .3rem; padding: .05rem .45rem; border-radius: 999px; background: var(--hover-fill); color: var(--action-hover); font-size: .72rem; font-weight: 600; letter-spacing: 0; text-transform: none; }
  .tagnew.exists { background: var(--warn-fill); color: var(--warn-text); }

  /* Progress. */
  .running { display: flex; flex-direction: column; gap: .4rem; }
  .row { display: flex; justify-content: space-between; gap: .75rem; color: var(--text-muted); font-size: .82rem; }
  .row b { color: var(--action-hover); font-family: var(--font-mono); white-space: nowrap; }
  .progress { height: 8px; overflow: hidden; border-radius: 999px; background: var(--bg-secondary); }
  .progress .bar { height: 100%; border-radius: inherit; background: var(--bar-fill); transition: width .12s; }

  /* The result. */
  .hero { display: flex; align-items: center; gap: .85rem; margin: 0; padding: .9rem 1rem; border: 1px solid var(--ok-border); border-left: 5px solid var(--ok); border-radius: 12px; background: var(--ok-fill); color: var(--ok-text); }
  .hero.partial { border-color: var(--warn-border); border-left-color: var(--warn); background: var(--warn-fill); color: var(--warn-text); }
  .badge { display: grid; place-items: center; flex: none; width: 40px; height: 40px; border-radius: 50%; background: var(--ok-text); color: var(--ok-fill); }
  .hero.partial .badge { background: var(--warn-text); color: var(--warn-fill); }
  .hero h3 { color: inherit; font-size: 1.05rem; overflow-wrap: anywhere; }
  .hero p { font-size: .85rem; }
  .summary { display: grid; grid-template-columns: auto minmax(0, 1fr); gap: .45rem 1rem; margin: 0; font-size: .88rem; }
  .summary dt { color: var(--text-muted); }
  .summary dd { margin: 0; font-weight: 600; overflow-wrap: anywhere; }
  .summary dd .mono { font-weight: 500; }
  .path-text { display: block; font-size: .8rem; word-break: break-all; }
  .copy { display: inline-flex; align-items: center; gap: .3rem; min-height: 0; margin-top: .25rem; padding: 0; border: 0; background: var(--transparent); color: var(--action-primary); font-size: .78rem; }
  .copy:hover:not(:disabled) { background: var(--transparent); color: var(--action-hover); }
  .reset:active:not(:disabled), .copy:active:not(:disabled) { box-shadow: none; }
  .tip { display: flex; align-items: flex-start; gap: .5rem; margin: 0; padding: .5rem .7rem; border: 1px solid var(--warn-border); border-left: 4px solid var(--warn); border-radius: 9px; background: var(--warn-fill); color: var(--warn-text); font-size: .82rem; font-weight: 400; }
  .tip :global(svg) { margin-top: .15rem; }
  .tip.replace p + p { margin-top: .25rem; }
  .tip.replace, .tip.problem { border-color: var(--alert-border); border-left-color: var(--alert); background: var(--alert-fill); color: var(--alert-text); }
  .tip strong { color: inherit; }

  /* Footer: it stays at the bottom of the dialog while the content scrolls. */
  .foot { position: sticky; bottom: 0; display: flex; align-items: center; justify-content: flex-end; gap: .6rem; padding: .85rem 1.1rem; border-top: 1px solid var(--border-subtle); background: var(--bg-primary); }
  .foot-note { margin-right: auto; color: var(--text-muted); font-size: .8rem; }
  .foot-note b { color: var(--text-main); }
  .foot button, .folder-row button { display: inline-flex; align-items: center; justify-content: center; gap: .45rem; min-height: 40px; margin: 0; padding: 0 1.1rem; border-radius: 9px; font-weight: 700; white-space: nowrap; }
  .folder-row button { font-weight: 600; }
  .outline { border-color: var(--action-border); background: var(--bg-raised); color: var(--action-hover); font-weight: 600; }
  .cancel:not(:disabled) { background: var(--transparent); color: var(--text-muted); }
  .cancel:hover:not(:disabled) { background: var(--bg-secondary); color: var(--text-main); }

  @media (max-width: 560px) {
    .formats, .who { grid-template-columns: minmax(0, 1fr); }
    .folder-row { flex-direction: column; }
    .path { flex: none; }
    .foot-note { display: none; }
    .foot { flex-direction: column-reverse; }
    .foot button { width: 100%; }
  }
</style>
