<script>
  // Presentation only: the class view retains the preview lifecycle, PIN and import callbacks.
  import PreviewDialog from './PreviewDialog.svelte';
  import Icon from '../../../../shared/theme/Icon.svelte';

  let {
    preview, className, contents, duration, grace, calculator, logins,
    pin = $bindable(''), showPin = $bindable(false), forwardOnly = $bindable(false),
    locked, working, onImport, onCancel, onChoose,
  } = $props();
  const id = $props.id();
  let previewOpen = $state(false);
</script>

<div class="import-stack">
  <section class="import-card" aria-labelledby="{id}-file-title">
    <header class="card-header">
      <span class="tile"><Icon name="exams" size={14} /></span>
      <h3 id="{id}-file-title">Exam file</h3>
      <span class="file-chip" title={preview.file}>{preview.file}</span>
      {#if preview.valid}
        <span class="validated"><Icon name="check" size={12} />Validated</span>
      {/if}
    </header>
    <div class="card-body">
      {#if preview.valid}
        <dl>
          <div class="row"><dt>Title</dt><dd>{preview.title}</dd></div>
          <div class="row"><dt>Contents</dt><dd>{contents}</dd></div>
          <div class="row"><dt>Time</dt><dd><span class="duration">{duration}</span><small>plus {grace} grace, counted from each student’s first login</small></dd></div>
          <div class="row"><dt>Sound</dt><dd>{preview.min_volume_percent === 0 ? 'the student’s volume is left alone' : `kept at ${preview.min_volume_percent}% or louder`}</dd></div>
          <div class="row"><dt>Calculator</dt><dd>{calculator ? calculator.replace(/^Calculator: /, '') : 'Not available'}</dd></div>
          <div class="row"><dt>Logins</dt><dd>Allowed {logins}</dd></div>
        </dl>
        <button type="button" class="student-view" aria-haspopup="dialog" disabled={locked} onclick={() => previewOpen = true}>
          <span class="preview-icon"><Icon name="preview" size={18} /></span>Preview the exam
        </button>
      {:else}
        <p role="alert">This file cannot be imported. Fix these problems in the file and choose it again:</p>
        <ul class="problems">{#each preview.problems as item}<li>{item}</li>{/each}</ul>
        <div class="actions"><button type="button" disabled={locked} onclick={onChoose}>Choose the file again…</button><button type="button" disabled={working} onclick={onCancel}>Close</button></div>
      {/if}
    </div>
  </section>

  {#if preview.valid}
    <section class="import-card" aria-labelledby="{id}-access-title">
      <header class="card-header">
        <span class="tile"><Icon name="lock" size={14} /></span>
        <h3 id="{id}-access-title">Exam access</h3>
      </header>
      <form class="card-body" onsubmit={event => { event.preventDefault(); if (pin.trim() && !locked) onImport(); }}>
        <label class="field" for="{id}-pin">Exam PIN</label>
        <p class="hint" id="{id}-pin-hint">Students log in with it. It is not stored in the file.</p>
        <div class="input-wrap">
          <input id="{id}-pin" type={showPin ? 'text' : 'password'} bind:value={pin} required autocomplete="off" spellcheck="false" maxlength="256" aria-describedby="{id}-pin-hint" />
          <button type="button" class="icon-btn" onclick={() => showPin = !showPin} aria-label={showPin ? 'Hide PIN' : 'Show PIN'} aria-pressed={showPin} title={showPin ? 'Hide PIN' : 'Show PIN'}>
            <Icon name={showPin ? 'eyeOff' : 'eye'} size={18} />
          </button>
        </div>
        <label class="check" for="{id}-forward">
          <input id="{id}-forward" type="checkbox" bind:checked={forwardOnly} aria-describedby="{id}-forward-hint" />
          <span><strong>Students cannot go back to an exercise once they move on</strong><small id="{id}-forward-hint">Applies to everyone and cannot be changed after the exam is published.</small></span>
        </label>
        <div class="note" role="note">
          <Icon name="info" size={16} />
          <span>Importing stores the exam in <strong>{className}</strong>. Students cannot log in until you publish it.</span>
        </div>
        <div class="actions">
          <button class="primary" disabled={locked || !pin.trim()} aria-describedby={!pin.trim() ? `${id}-import-hint` : undefined}>Import exam</button>
          <button type="button" disabled={working} onclick={onCancel}>Cancel</button>
          {#if !pin.trim()}<span class="disabled-hint" id="{id}-import-hint">Enter a PIN to continue</span>{/if}
        </div>
      </form>
    </section>
  {/if}
</div>
{#if previewOpen}<PreviewDialog {preview} onClose={() => previewOpen = false} />{/if}

<style>
  /* The mockups' exam file card: ice header with a tile icon, a filename chip and the Validated badge, then mono keys. */
  .import-stack { width: 100%; max-width: 560px; min-width: 0; display: grid; gap: 12px; color: var(--text-main); font-size: 14px; line-height: 1.4; }
  .import-card { min-width: 0; background: var(--bg-raised); border: 1px solid var(--border-subtle); border-radius: var(--radius); box-shadow: var(--shadow-raised); overflow: hidden; }
  .card-header { min-height: 40px; padding: 8px 16px; display: flex; align-items: center; gap: 8px; background: var(--header-soft); color: var(--text-main); border-bottom: 1px solid var(--accent-cyan-soft); }
  .tile { width: 24px; height: 24px; border-radius: 6px; background: var(--bg-raised); color: var(--action-primary); border: 1px solid var(--accent-cyan-soft); display: grid; place-items: center; flex: none; }
  h3 { margin: 0; font-size: 16px; font-weight: 700; white-space: nowrap; }
  .file-chip { font: 12.5px/1.3 var(--mono); background: var(--bg-raised); color: var(--action-hover); border: 1px solid var(--action-border); border-radius: 5px; padding: 2px 8px; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .validated { margin-left: auto; flex: none; background: var(--success-badge); color: var(--bg-primary); font: 700 11px/1.4 var(--mono); letter-spacing: .05em; text-transform: uppercase; padding: 2px 8px; border-radius: 999px; display: inline-flex; align-items: center; gap: 4px; }
  .card-body { padding: 12px 16px 16px; }
  dl { margin: 0; }
  .row { display: grid; grid-template-columns: 104px minmax(0, 1fr); gap: 12px; align-items: baseline; padding: 7px 0; border-bottom: 1px solid var(--border-subtle); }
  .row:first-child { padding-top: 0; }
  .row:last-child { border-bottom: 0; padding-bottom: 0; }
  dt { color: var(--text-muted); font: 600 11px/1.4 var(--mono); letter-spacing: .06em; text-transform: uppercase; }
  dd { margin: 0; font-weight: 600; }
  small { display: block; margin-top: 2px; color: var(--text-muted); font-size: 12.5px; line-height: 1.3; font-weight: 400; }
  /* A time value is a monospace chip on the warm fill. */
  .duration { display: inline-block; background: var(--warn-fill); color: var(--warn-text); border: 1px solid var(--warn-border); font: 700 13.5px/1.3 var(--mono); padding: 1px 8px; border-radius: 5px; }
  .preview-icon { display: inline-flex; flex: none; }
  .field { display: block; font-weight: 700; }
  .hint { color: var(--text-muted); font-size: 12.5px; margin: 0 0 6px; }
  .input-wrap { display: flex; gap: 6px; }
  .input-wrap input { flex: 1; min-width: 0; min-height: 0; height: var(--control-h); padding: 0 10px; font: 15px var(--mono); letter-spacing: .08em; color: var(--text-main); border: 1px solid var(--control-border); border-radius: var(--radius-ctl); background: var(--bg-primary); }
  .input-wrap input:focus-visible { outline: 2px solid var(--transparent); outline-offset: 0; border-color: var(--action-primary); background: var(--paper); box-shadow: 0 0 0 3px var(--focus-halo); }
  button { min-height: 0; height: var(--control-h); margin: 0; padding: 0 18px; font-size: 14px; font-weight: 700; background: var(--bg-secondary); color: var(--text-main); border: 1px solid var(--border-subtle); border-radius: var(--radius-ctl); }
  button:hover:not(:disabled) { background: var(--selection-fill); border-color: var(--action-primary); color: var(--action-hover); }
  .student-view { margin-top: 10px; display: inline-flex; align-items: center; gap: 6px; max-width: 100%; padding: 0 12px; font-weight: 600; font-size: 13px; line-height: 1.2; }
  .icon-btn { width: var(--control-h); flex: none; display: grid; place-items: center; padding: 0; }
  .check { display: flex; align-items: flex-start; gap: 8px; margin-top: 12px; cursor: pointer; }
  .check input { width: 16px; height: 16px; margin: 2px 0 0; accent-color: var(--action-primary); flex: none; cursor: pointer; }
  .check strong { display: block; font-size: 14px; font-weight: 600; }
  .check input:checked ~ span small { color: var(--alert-text); font-weight: 600; }
  .note { margin-top: 12px; padding: 7px 12px; background: var(--bg-secondary); color: var(--text-main); border-radius: 10px; font-size: 13px; line-height: 1.35; display: flex; gap: 8px; }
  .note :global(svg) { flex: none; margin-top: 0; color: var(--text-muted); }
  .actions { margin-top: 14px; display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
  .primary { background: var(--action-primary); color: var(--bg-primary); border-color: var(--action-primary); box-shadow: var(--shadow-action); }
  .primary:hover:not(:disabled) { background: var(--action-hover); border-color: var(--action-hover); color: var(--bg-primary); box-shadow: var(--shadow-action-hover); }
  button:disabled { background: var(--bg-secondary); border-color: var(--border-subtle); color: var(--muted-dot); box-shadow: none; opacity: 1; cursor: not-allowed; }
  .disabled-hint { color: var(--text-muted); font-size: 12.5px; }
  .problems { color: var(--alert-text); }
  button:focus-visible, .check input:focus-visible { outline: 2px solid var(--action-primary); outline-offset: 2px; }
  @media (max-width: 480px) {
    .row { grid-template-columns: minmax(0, 1fr); gap: 0; }
    .card-header { flex-wrap: wrap; display: grid; grid-template-columns: 24px auto minmax(0, 1fr); }
    .validated { grid-column: 2 / -1; justify-self: start; margin-left: 0; }
  }
</style>
