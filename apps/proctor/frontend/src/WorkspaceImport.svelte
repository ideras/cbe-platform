<script>
  // Presentation only: the class view keeps the preview, the PIN and the import and cancel callbacks (as ExamImport does for a question exam).
  import Icon from '../../../../shared/theme/Icon.svelte';
  import { formatTime } from '../../../../shared/exam/timing.js';
  import { pointsText } from './workspace.js';

  let { preview, className, pin = $bindable(''), showPin = $bindable(false), locked, working, onImport, onCancel, onChoose } = $props();
  const id = $props.id();
  const plural = (n, noun) => `${n} ${noun}${n === 1 ? '' : 's'}`;
  const buttons = $derived([preview.run && `Run: ${preview.run}`, preview.check && `Check: ${preview.check}`].filter(Boolean));
</script>

<div class="import-stack" data-testid="workspace-import">
  <section class="import-card" aria-labelledby="{id}-title">
    <header class="card-header">
      <span class="tile"><Icon name="terminal" size={14} /></span>
      <h3 id="{id}-title">Workspace package</h3>
      <span class="file-chip" title={preview.folder}>{preview.folder}</span>
      {#if preview.valid}<span class="validated"><Icon name="check" size={12} />Compiled</span>{/if}
    </header>
    <div class="card-body">
      {#if preview.valid}
        <dl>
          <div class="row"><dt>Title</dt><dd>{preview.title}</dd></div>
          <div class="row"><dt>Time</dt><dd><span class="duration">{formatTime(preview.duration_seconds)}</span><small>counted from each student’s first login</small></dd></div>
          <div class="row"><dt>TODO items</dt><dd>
            <ol class="todos">{#each preview.todos as todo (todo.id)}<li><code>{todo.id}</code> {todo.text}{#if todo.points != null} <small>{pointsText(todo.points)}</small>{/if}</li>{/each}</ol>
            <small>Students tick them by hand: progress is self-reported.</small>
          </dd></div>
          <div class="row"><dt>Project</dt><dd>{plural(preview.starter_files, 'starter file')}, copied to each student’s <code>/work</code>. The terminal is {preview.terminal ? 'on' : 'off'}.{#if buttons.length} <small>{buttons.join(' · ')}</small>{/if}</dd></div>
          <div class="row"><dt>Hidden tests</dt><dd>{#if preview.has_grading}{plural(preview.grading_files, 'file')}, kept on the server and never sent to students.{:else}None: this exam cannot be graded by the server.{/if}</dd></div>
          <div class="row"><dt>Container</dt><dd>{preview.image}: {preview.cpu} cpu, {preview.memory_mb} MB of memory, {preview.disk_mb} MB of disk for each student.</dd></div>
          <div class="row"><dt>Image</dt><dd>
            {#if preview.own_image}
              <strong>Its own image</strong> built from {preview.image} ({plural(preview.image_files, 'file')} in <code>image/</code>). Build it from the exam’s Image button after importing; the exam cannot be published until it is built.
            {:else}The course image {preview.image}, as it is.{/if}
          </dd></div>
        </dl>
        {#if preview.image_warnings?.length}
          <div class="warn" role="status"><strong>The Containerfile has warnings:</strong>
            <ul>{#each preview.image_warnings as item}<li>{item}</li>{/each}</ul>
            <small>They do not stop the import. A package installed without a version may not give the same image when it is built again.</small>
          </div>
        {/if}
      {:else}
        <p role="alert">This package cannot be imported. Fix these problems in it and choose it again:</p>
        <ul class="problems">{#each preview.problems as item}<li>{item}</li>{/each}</ul>
        <div class="actions"><button type="button" disabled={locked} onclick={onChoose}>Choose the package again…</button><button type="button" disabled={working} onclick={onCancel}>Close</button></div>
      {/if}
    </div>
  </section>

  {#if preview.valid}
    <section class="import-card" aria-labelledby="{id}-access-title">
      <header class="card-header"><span class="tile"><Icon name="lock" size={14} /></span><h3 id="{id}-access-title">Exam access</h3></header>
      <form class="card-body" onsubmit={event => { event.preventDefault(); if (pin.trim() && !locked) onImport(); }}>
        <label class="field" for="{id}-pin">Exam PIN</label>
        <p class="hint" id="{id}-pin-hint">Students log in with it. It is not stored in the package.</p>
        <div class="input-wrap">
          <input id="{id}-pin" type={showPin ? 'text' : 'password'} bind:value={pin} required autocomplete="off" spellcheck="false" maxlength="256" aria-describedby="{id}-pin-hint" />
          <button type="button" class="icon-btn" onclick={() => showPin = !showPin} aria-label={showPin ? 'Hide PIN' : 'Show PIN'} aria-pressed={showPin} title={showPin ? 'Hide PIN' : 'Show PIN'}>
            <Icon name={showPin ? 'eyeOff' : 'eye'} size={18} />
          </button>
        </div>
        <div class="note" role="note"><Icon name="info" size={16} /><span>Importing stores the exam in <strong>{className}</strong>. Students cannot log in until you publish it.</span></div>
        <div class="actions">
          <button class="primary" disabled={locked || !pin.trim()} aria-describedby={!pin.trim() ? `${id}-import-hint` : undefined}>Import workspace exam</button>
          <button type="button" disabled={working} onclick={onCancel}>Cancel</button>
          {#if !pin.trim()}<span class="disabled-hint" id="{id}-import-hint">Enter a PIN to continue</span>{/if}
        </div>
      </form>
    </section>
  {/if}
</div>

<style>
  .import-stack { display: grid; gap: var(--space-3); min-width: 0; }
  .import-card { min-width: 0; overflow: hidden; background: var(--bg-raised); border: 1px solid var(--border-subtle); border-radius: 12px; }
  .card-header { display: flex; flex-wrap: wrap; align-items: center; gap: .5rem; padding: .6rem 1rem; background: var(--header-soft); border-bottom: 1px solid var(--cyan-border); }
  .card-header h3 { margin: 0; font-size: .95rem; color: var(--action-hover); }
  .tile { display: inline-flex; align-items: center; justify-content: center; width: 1.6rem; height: 1.6rem; border-radius: 7px; background: var(--selection-fill); color: var(--action-hover); }
  .file-chip { max-width: 18rem; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; padding: .05rem .5rem; border: 1px solid var(--border-subtle); border-radius: 999px; font-family: var(--font-mono); font-size: .78rem; }
  .validated { display: inline-flex; align-items: center; gap: .25rem; margin-left: auto; padding: .1rem .55rem; border-radius: 999px; background: var(--ok-fill); color: var(--ok-text); font-size: .78rem; font-weight: 700; }
  .card-body { display: grid; gap: .7rem; padding: .9rem 1rem; min-width: 0; }
  dl { display: grid; margin: 0; gap: .15rem; }
  .row { display: grid; grid-template-columns: 8rem minmax(0, 1fr); gap: .75rem; padding: .35rem 0; border-bottom: 1px solid var(--border-subtle); }
  .row:last-child { border-bottom: 0; }
  dt { color: var(--text-muted); font-size: .68rem; font-weight: 700; letter-spacing: .08em; text-transform: uppercase; padding-top: .2rem; }
  dd { margin: 0; overflow-wrap: anywhere; }
  dd small { display: block; color: var(--text-muted); font-weight: 400; }
  .duration { font-family: var(--font-mono); font-weight: 700; }
  .todos { margin: 0 0 .2rem; padding-left: 1.1rem; }
  code { font-family: var(--font-mono); font-size: .85rem; }
  .warn { padding: .55rem .75rem; border: 1px solid var(--border-subtle); border-radius: 9px; background: var(--warn-fill); color: var(--warn-text); }
  .warn ul, .problems { margin: .3rem 0 .3rem 1.1rem; padding: 0; }
  .field { font-weight: 700; }
  .hint { margin: 0; color: var(--text-muted); font-size: .85rem; }
  .input-wrap { display: flex; gap: .4rem; align-items: center; }
  .input-wrap input { flex: 1 1 auto; min-width: 0; }
  .icon-btn { display: inline-flex; align-items: center; justify-content: center; width: 2.3rem; height: 2.3rem; margin: 0; padding: 0; }
  .note { display: flex; align-items: flex-start; gap: .5rem; padding: .5rem .7rem; border: 1px dashed var(--border-subtle); border-radius: 9px; background: var(--bg-primary); color: var(--text-muted); font-size: .85rem; }
  .actions { display: flex; flex-wrap: wrap; align-items: center; gap: .5rem; }
  .actions button { margin: 0; }
  .disabled-hint { color: var(--text-muted); font-size: .85rem; }
  @media (max-width: 640px) { .row { grid-template-columns: minmax(0, 1fr); gap: .1rem; } }
</style>
