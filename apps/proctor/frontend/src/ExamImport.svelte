<script>
  // Presentation only: the class view retains the preview lifecycle, PIN and import callbacks.
  import PreviewDialog from './PreviewDialog.svelte';
  import Icon from './Icon.svelte';

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
        <span class="validated"><svg width="10" height="10" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3.5" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M20 6 9 17l-5-5" /></svg>Validated</span>
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
            <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
              {#if showPin}
                <path d="M17.9 17.9A10.1 10.1 0 0 1 12 19c-6.4 0-10-7-10-7a18 18 0 0 1 4.1-4.9M9.9 5.2A9.7 9.7 0 0 1 12 5c6.4 0 10 7 10 7a18 18 0 0 1-2.2 3.2M1 1l22 22M9.9 9.9a3 3 0 0 0 4.2 4.2" />
              {:else}
                <path d="M2 12s3.6-7 10-7 10 7 10 7-3.6 7-10 7S2 12 2 12z" /><circle cx="12" cy="12" r="3" />
              {/if}
            </svg>
          </button>
        </div>
        <label class="check" for="{id}-forward">
          <input id="{id}-forward" type="checkbox" bind:checked={forwardOnly} aria-describedby="{id}-forward-hint" />
          <span><strong>Students cannot go back to an exercise once they move on</strong><small id="{id}-forward-hint">Applies to everyone and cannot be changed after the exam is published.</small></span>
        </label>
        <div class="note" role="note">
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><circle cx="12" cy="12" r="10" /><path d="M12 16v-4M12 8h.01" /></svg>
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
  .import-stack { width: 100%; max-width: 560px; min-width: 0; display: grid; gap: 12px; color: var(--ink); font-size: 14px; line-height: 1.4; }
  .import-card { min-width: 0; background: var(--card); border: 1px solid var(--border); border-radius: var(--radius); box-shadow: var(--shadow-search); overflow: hidden; }
  .card-header { min-height: 40px; padding: 8px 16px; display: flex; align-items: center; gap: 8px; background: var(--sea); color: var(--ink); border-bottom: 1px solid var(--header-divider); }
  .tile { width: 24px; height: 24px; border-radius: 6px; background: var(--card); color: var(--sea-ink); display: grid; place-items: center; flex: none; }
  h3 { margin: 0; font-size: 16px; font-weight: 700; white-space: nowrap; }
  .file-chip { font: 12.5px/1.3 var(--mono); background: var(--card); color: var(--ink); border-radius: 6px; padding: 2px 8px; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .validated { margin-left: auto; flex: none; background: var(--sea-ink); color: var(--card); font: 700 11px/1.4 var(--mono); letter-spacing: .04em; text-transform: uppercase; padding: 2px 8px; border-radius: 999px; display: inline-flex; align-items: center; gap: 4px; }
  .card-body { padding: 12px 16px 16px; }
  dl { margin: 0; }
  .row { display: grid; grid-template-columns: 104px minmax(0, 1fr); gap: 12px; align-items: baseline; padding: 7px 0; border-bottom: 1px solid var(--ground); }
  .row:first-child { padding-top: 0; }
  .row:last-child { border-bottom: 0; padding-bottom: 0; }
  dt { color: var(--muted); font: 600 11px/1.4 var(--mono); letter-spacing: .06em; text-transform: uppercase; }
  dd { margin: 0; font-weight: 600; }
  small { display: block; color: var(--muted); font-size: 12.5px; line-height: 1.3; font-weight: 400; }
  .duration { display: inline-block; background: var(--sand); color: var(--ink); font: 700 13.5px/1.3 var(--mono); padding: 1px 8px; border-radius: 6px; }
  .student-view { margin-top: 10px; display: inline-flex; align-items: center; gap: 6px; max-width: 100%; padding: 0 12px; background: var(--ground); color: var(--ink); border: 0; font-weight: 600; font-size: 13px; line-height: 1.2; }
  .student-view:hover:not(:disabled) { background: var(--ground-hover); }
  .preview-icon { display: inline-flex; flex: none; }
  .field { display: block; font-weight: 700; }
  .hint { color: var(--muted); font-size: 12.5px; margin: 0 0 6px; }
  .input-wrap { display: flex; gap: 6px; }
  .input-wrap input { flex: 1; min-width: 0; min-height: 0; height: var(--control-h); padding: 0 10px; font: 15px var(--mono); letter-spacing: .08em; color: var(--ink); border: 1px solid var(--border); border-radius: var(--radius-ctl); background: var(--card); }
  .input-wrap input:focus-visible { border-color: var(--ocean); box-shadow: 0 0 0 2px var(--focus-halo-strong); }
  button { min-height: 0; height: var(--control-h); margin: 0; padding: 0 18px; font-size: 14px; font-weight: 700; background: var(--card); color: var(--ink); border: 1px solid var(--border); border-radius: var(--radius-ctl); }
  button:hover:not(:disabled) { background: var(--hover); border-color: var(--border); }
  .icon-btn { width: var(--control-h); flex: none; display: grid; place-items: center; padding: 0; }
  .check { display: flex; align-items: flex-start; gap: 8px; margin-top: 12px; cursor: pointer; }
  .check input { width: 16px; height: 16px; margin: 2px 0 0; accent-color: var(--ink); flex: none; cursor: pointer; }
  .check strong { display: block; font-size: 14px; font-weight: 600; }
  .check input:checked ~ span small { color: var(--coral); font-weight: 600; }
  .note { margin-top: 12px; padding: 7px 12px; background: var(--ground); color: var(--ink); border-radius: 10px; font-size: 13px; line-height: 1.35; display: flex; gap: 8px; }
  .note svg { flex: none; margin-top: 1px; color: var(--muted); }
  .actions { margin-top: 14px; display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
  .primary { background: var(--ocean); color: var(--card); border-color: var(--ocean); }
  .primary:hover:not(:disabled) { background: var(--ocean-strong); border-color: var(--ocean-strong); }
  button:disabled { background: var(--ground); border-color: var(--ground); color: var(--disabled-ink); opacity: 1; cursor: not-allowed; }
  .disabled-hint { color: var(--muted); font-size: 12.5px; }
  .problems { color: var(--coral); }
  button:focus-visible, input:focus-visible { outline: 2px solid var(--ocean); outline-offset: 2px; }
  @media (max-width: 480px) {
    .row { grid-template-columns: minmax(0, 1fr); gap: 0; }
    .card-header { flex-wrap: wrap; display: grid; grid-template-columns: 24px auto minmax(0, 1fr); }
    .validated { grid-column: 2 / -1; justify-self: start; margin-left: 0; }
  }
</style>
