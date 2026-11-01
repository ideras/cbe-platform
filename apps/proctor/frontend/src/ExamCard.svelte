<script>
  import { onDestroy } from 'svelte';
  import Icon from '../../../../shared/theme/Icon.svelte';
  import { formatTime } from '../../../../shared/exam/timing.js';
  import { localDateTime } from './dates.js';
  let { entry, monitored = false, locked = false, deemphasized = false, monitorable, calculator, logins, admission,
    onMonitor, onPreview, onPublish, onUnpublish, onDownload, onPurge, onDelete } = $props();
  let copying = $state(false);
  let copied = $state(false);
  let copyError = $state('');
  let timer, alive = true;
  const workingPercent = $derived(entry.total_sessions > 0 ? Math.min(100, Math.max(0, entry.active_sessions / entry.total_sessions * 100)) : 0);
  onDestroy(() => { alive = false; clearTimeout(timer); });
  async function copyID() {
    if (copying) return;
    copying = true; copied = false; copyError = ''; clearTimeout(timer);
    try {
      if (!navigator.clipboard?.writeText) throw new Error('Clipboard unavailable');
      await navigator.clipboard.writeText(entry.exam_id);
      if (alive) { copied = true; timer = setTimeout(() => copied = false, 1500); }
    } catch { if (alive) copyError = 'Could not copy the exam ID. Select and copy it manually.'; }
    finally { if (alive) copying = false; }
  }
</script>
<article class="exam" class:published={entry.published} class:watching={monitored} data-exam-id={entry.exam_id} aria-label={entry.title}>
  <div class="exam-top">
    <div class="exam-heading">
      <h3>{entry.title}</h3>
      <div class="exam-tags">
        {#if entry.published}<span class="tag open">Published</span>{/if}
        {#if monitored}<span class="tag monitoring">Monitoring</span>{/if}
        {#if entry.navigation === 'forward_only'}<span class="tag forward" title="Students cannot go back to an exercise once they move on">Forward only</span>{/if}
        {#if calculator}<span class="tag calc" title={calculator}><Icon name="calculator" size={12} />{calculator}</span>{/if}
      </div>
      <button type="button" class="exam-id" aria-label="Copy exam ID for {entry.title}" title="Copy exam ID" disabled={copying} onclick={copyID}><code>{entry.exam_id}</code><Icon name={copied ? 'check' : 'copy'} size={12} /></button>
      {#if copied}<span class="copy-feedback" role="status">Exam ID copied.</span>{/if}
      {#if copyError}<p role="alert">{copyError}</p>{/if}
      {#if entry.ends_at}<p class="end-time">Ends {localDateTime(entry.ends_at)}</p>{/if}
    </div>
    <div class="row-actions" role="group" aria-label="Actions for {entry.title}">
      <button class:primary={monitored && !deemphasized} disabled={locked || !monitorable} aria-label="Monitor {entry.title}" title={monitorable ? `Open the monitoring window for ${entry.title}` : 'Nobody has taken this exam yet; publish it first'} onclick={() => onMonitor(entry)}><Icon name="monitor" class="monitor" size="1.35rem" /></button>
      <button disabled={locked} aria-label="Preview {entry.title}" title={`Preview ${entry.title} as the students see it, calculator included`} onclick={() => onPreview(entry)}><Icon name="preview" size="1.35rem" /></button>
      {#if !entry.published}<button disabled={locked} aria-label="Publish {entry.title}…" title="Publish {entry.title}…" onclick={() => onPublish(entry)}><Icon name="publish" size="1.35rem" /></button>{/if}
      {#if entry.published}<button disabled={locked} aria-label="Close admission to {entry.title}…" title="Close admission to {entry.title}…" onclick={() => onUnpublish(entry)}><Icon name="closeAdmission" class="close-admission" size="1.35rem" /></button>{/if}
      {#if entry.total_sessions > 0}<button disabled={locked} aria-label="Download submissions of {entry.title}…" title="Download submissions of {entry.title}…" onclick={() => onDownload(entry)}><Icon name="download" class="download" size="1.35rem" /></button>{/if}
      {#if entry.total_sessions > 0}<button class="purge" disabled={locked || entry.active_sessions > 0} aria-label="Purge screenshots of {entry.title}…" title={entry.active_sessions > 0 ? 'Students are still working in this exam; purge once grades are final' : `Delete the screenshots of ${entry.title} after grading`} onclick={() => onPurge(entry)}><Icon name="purge" size="1.35rem" /></button>{/if}
      {#if entry.total_sessions === 0 && !entry.published}<button class="purge" disabled={locked} aria-label="Delete exam {entry.title}…" title="Delete exam {entry.title}…" onclick={() => onDelete(entry)}><Icon name="deleteExam" class="delete-exam" size="1.35rem" /></button>{/if}
    </div>
  </div>
  <dl class="exam-stats">
    <div class="es"><dt>Exercises</dt><dd class="number">{entry.exercise_count}</dd></div>
    <div class="es"><dt>Time</dt><dd><span class="time-chip">{formatTime(entry.duration_seconds)}</span>{' '}<span class="grace">+ {formatTime(entry.grace_seconds + (entry.extra_grace_seconds ?? 0))} grace</span></dd></div>
    <div class="es"><dt>Logins allowed</dt><dd>{logins === 'any time' ? 'Any time' : logins}{#if admission !== 'open'}<small class="late">Logins are {admission} now</small>{/if}</dd></div>
    <div class="es"><dt>Sessions</dt><dd><span class="number">{entry.active_sessions}</span>{' working / '}<span class="number">{entry.total_sessions}</span>{' total'}</dd>
      <div class="bar" role="meter" aria-label="Working sessions for {entry.title}" aria-valuemin="0" aria-valuemax={Math.max(1, entry.total_sessions)} aria-valuenow={entry.active_sessions} aria-valuetext="{entry.active_sessions} working of {entry.total_sessions} total"><span style:width={`${workingPercent}%`}></span></div>
    </div>
  </dl>
</article>
<style>
  .exam { min-width: 0; overflow: hidden; background: var(--bg-raised); border: 1px solid var(--border-subtle); border-radius: 12px; transition: border-color .15s ease, box-shadow .15s ease; }
  .exam:hover { border-color: var(--accent-cyan-soft); box-shadow: var(--shadow-card-hover); }
  .exam.published { box-shadow: inset 4px 0 0 var(--ok); }
  .exam.watching { box-shadow: inset 4px 0 0 var(--accent-cyan); }
  .exam-top { display: flex; align-items: flex-start; flex-wrap: wrap; gap: 1rem; padding: .9rem 1rem .75rem; }
  .exam-heading { flex: 1 1 20rem; min-width: 0; }
  h3 { margin: 0; font-size: 1.05rem; font-weight: 700; overflow-wrap: anywhere; }
  .exam-tags { display: flex; flex-wrap: wrap; gap: .35rem; margin-top: .4rem; }
  .tag { display: inline-flex; align-items: center; gap: .35rem; max-width: 100%; padding: .12rem .55rem; border: 1px solid var(--transparent); border-radius: 999px; font-size: .76rem; font-weight: 600; overflow-wrap: anywhere; }
  .open { background: var(--ok-fill); color: var(--ok-text); border-color: var(--ok-border); }
  .open::before { content: ''; width: 7px; height: 7px; border-radius: 50%; background: var(--ok); }
  .monitoring { background: var(--selection-fill); color: var(--action-hover); border-color: var(--cyan-border); }
  .monitoring::before { content: ''; width: 7px; height: 7px; border-radius: 50%; background: var(--accent-cyan); }
  .forward { background: var(--bg-secondary); color: var(--text-main); }
  .calc { background: var(--bg-secondary); color: var(--text-main); border-color: var(--border-subtle); }
  .exam-id { display: inline-flex; align-items: center; max-width: 100%; gap: .35rem; min-height: 0; margin: .45rem 0 0; padding: 0; background: var(--transparent); color: var(--text-muted); border: 0; border-radius: 0; box-shadow: none; font-size: .76rem; font-weight: 400; }
  .exam-id code { min-width: 0; overflow-wrap: anywhere; user-select: text; }
  .exam-id:hover:not(:disabled) { color: var(--action-primary); background: var(--transparent); }
  .copy-feedback { display: inline; margin: 0 0 0 .5rem; padding: 0; border: 0; background: var(--transparent); color: var(--ok-text); font-size: .76rem; }
  .end-time { margin: .35rem 0 0; color: var(--text-muted); font-size: .78rem; }
  .row-actions { display: flex; flex-wrap: wrap; gap: .3rem; margin-left: auto; }
  .row-actions button { display: inline-flex; align-items: center; justify-content: center; width: 36px; height: 36px; min-height: 0; margin: 0; padding: 0; background: var(--bg-primary); border: 1px solid var(--border-subtle); border-radius: 9px; color: var(--text-muted); box-shadow: none; }
  .row-actions button:hover:not(:disabled) { background: var(--selection-fill); border-color: var(--action-primary); color: var(--action-hover); }
  .row-actions button.primary { background: var(--action-primary); border-color: var(--action-primary); color: var(--bg-primary); }
  .row-actions button.primary:hover:not(:disabled) { background: var(--action-hover); border-color: var(--action-hover); }
  .row-actions button:disabled { background: var(--bg-secondary); color: var(--muted-dot); border-color: var(--border-subtle); }
  .row-actions :global(svg) { flex: none; width: 1.05rem; height: 1.05rem; }
  .row-actions :global(svg.monitor), .row-actions :global(svg.close-admission), .row-actions :global(svg.delete-exam), .row-actions :global(svg.download) { width: 1.25rem; height: 1.25rem; }
  .row-actions .purge { color: var(--alert-text); }
  .row-actions .purge:hover:not(:disabled) { background: var(--alert-fill); border-color: var(--alert); color: var(--alert-text); }
  .exam-stats { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); margin: 0; border-top: 1px solid var(--border-subtle); background: var(--bg-primary); }
  .es { min-width: 0; padding: .65rem 1rem; border-right: 1px solid var(--border-subtle); }
  .es:last-child { border-right: 0; }
  dt { color: var(--text-muted); font-size: .68rem; font-weight: 700; letter-spacing: .08em; text-transform: uppercase; }
  dd { margin: .15rem 0 0; font-weight: 700; overflow-wrap: anywhere; }
  .number { font-family: var(--font-mono); }
  .time-chip { display: inline-block; padding: .05rem .4rem; border-radius: 5px; background: var(--warn-fill); color: var(--warn-text); font-family: var(--font-mono); }
  .grace { display: block; color: var(--text-muted); font-size: .78rem; font-weight: 400; }
  .late { display: inline-block; margin-top: .2rem; padding: .05rem .4rem; border-radius: 5px; background: var(--warn-fill); color: var(--warn-text); font-size: .78rem; }
  .bar { height: 6px; margin-top: .35rem; overflow: hidden; border-radius: 999px; background: var(--bg-secondary); }
  .bar span { display: block; height: 100%; border-radius: inherit; background: var(--bar-fill); }
  @media (max-width: 760px) { .row-actions { margin-left: 0; } .exam-stats { grid-template-columns: repeat(2, minmax(0, 1fr)); } .es:nth-child(2) { border-right: 0; } .es:nth-child(-n+2) { border-bottom: 1px solid var(--border-subtle); } }
</style>
