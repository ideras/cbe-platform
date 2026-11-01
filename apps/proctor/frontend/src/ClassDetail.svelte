<script>
  import { onMount } from 'svelte';
  import * as api from '$proctor-api';
  import ClassExams from './ClassExams.svelte';
  import RosterPanel from './RosterPanel.svelte';
  import RenameClassDialog from './RenameClassDialog.svelte';
  import ReplaceRosterDialog from './ReplaceRosterDialog.svelte';
  import Confirmation from './Confirmation.svelte';
  import Icon from '../../../../shared/theme/Icon.svelte';
  import { message } from './errors.js';
  // One class, in two tabs: its exams (the default) and its roster, which is loaded from the server. The exams are imported into
  // the class and stay there. The roster is read-only; it changes by replacing the whole file. The class can be renamed, and
  // deleted once it has no exams: the ones students have taken can never be deleted.
  let { view, entry, onBack, onChanged, onSelected, onDeleted } = $props();
  let tab = $state('exams');
  let roster = $state(null);
  let loading = $state(true);
  let problem = $state('');
  let status = $state('');
  let renaming = $state(false);
  let replacing = $state(false);
  let deleting = $state(false);
  let deleteBusy = $state(false);
  let deleteError = $state('');
  let epoch = 0;
  const hasExams = $derived(entry.exam_count > 0);
  // The tab counts what the table shows once the exams are known; until then, what the class list says.
  const examCount = $derived(view.catalog_loaded ? view.catalog.exams.filter(exam => exam.class_id === entry.class_id).length : entry.exam_count);
  const sessionCount = $derived(view.catalog_loaded ? view.catalog.exams.filter(exam => exam.class_id === entry.class_id).reduce((sum, exam) => sum + exam.total_sessions, 0) : null);
  const tabs = $derived([{ id: 'exams', text: 'Exams', count: examCount, icon: 'exams', label: `Exams (${examCount})` }, { id: 'students', text: 'Students', count: entry.student_count, icon: 'classes', label: `Students (${entry.student_count})` }]);
  function plural(n, noun) { return `${n} ${noun}${n === 1 ? '' : 's'}`; }

  async function load() {
    const mine = ++epoch;
    loading = true; problem = '';
    try { const value = await api.ClassRoster(entry.class_id); if (mine === epoch) roster = value; }
    catch (err) { if (mine === epoch) problem = message(err); }
    finally { if (mine === epoch) loading = false; }
  }
  onMount(() => { load(); return () => { epoch++; }; });

  // Arrow keys, Home and End move between the tabs, as in the tabs pattern; only the chosen tab is in the tab order.
  function moveTab(event) {
    const index = tabs.findIndex(item => item.id === tab);
    const next = { ArrowRight: (index + 1) % tabs.length, ArrowLeft: (index + tabs.length - 1) % tabs.length, Home: 0, End: tabs.length - 1 }[event.key];
    if (next === undefined) return;
    event.preventDefault();
    tab = tabs[next].id;
    document.getElementById(`tab-${tab}`)?.focus();
  }
  async function renamed(updated) { renaming = false; status = `The class is now called “${updated.name}”.`; await onChanged(); }
  async function replaced(change) {
    replacing = false;
    status = `Roster replaced: ${plural(change.students, 'student')} (${change.added} new, ${change.removed} removed${change.pruned > 0 ? `; ${plural(change.pruned, 'student')} no longer on the server` : ''}).`;
    await load(); await onChanged();
  }
  function askDelete() { deleteError = ''; deleting = true; }
  async function confirmDelete() {
    if (deleteBusy) return;
    deleteBusy = true; deleteError = '';
    try { const result = await api.DeleteClass(entry.class_id, true); deleting = false; await onDeleted(result); }
    catch (err) { deleteError = message(err); }
    finally { deleteBusy = false; }
  }
</script>
<div class="detail">
  <header class="page-head">
    <div class="title-block">
      <button type="button" class="back" onclick={onBack}><Icon name="back" size={15} />Classes</button>
      <h1>{entry.name}</h1>
      <div class="meta" aria-label="Class totals">
        <span class="chip"><b>{examCount}</b> {examCount === 1 ? 'exam' : 'exams'}</span>
        <span class="chip"><b>{entry.student_count}</b> {entry.student_count === 1 ? 'student' : 'students'}</span>
        {#if sessionCount != null}<span class="chip"><b>{sessionCount}</b> sessions total</span>{/if}
      </div>
    </div>
    <div class="head-actions">
      <div class="action-row">
        <button type="button" class="outline" onclick={() => { status = ''; renaming = true; }}><Icon name="pencil" size={15} />Rename…</button>
        <button type="button" class="delete" disabled={hasExams} aria-describedby={hasExams ? 'delete-hint' : undefined} onclick={askDelete}><Icon name="deleteExam" size={15} />Delete class…</button>
      </div>
      {#if hasExams}<p class="hint" id="delete-hint"><Icon name="lock" size={13} /><span>Delete its exams first. Exams students have taken cannot be deleted.</span></p>{/if}
    </div>
  </header>
  {#if status}<p role="status">{status}</p>{/if}
  <div class="tabs" role="tablist" aria-label="{entry.name} sections">
    {#each tabs as item (item.id)}
      <button type="button" role="tab" id="tab-{item.id}" aria-selected={tab === item.id} aria-controls="panel-{item.id}" tabindex={tab === item.id ? 0 : -1} aria-label={item.label} onkeydown={moveTab} onclick={() => tab = item.id}><Icon name={item.icon} size={17} />{item.text}{' '}<span class="n">{item.count}</span></button>
    {/each}
  </div>
  <div role="tabpanel" id="panel-exams" aria-labelledby="tab-exams" hidden={tab !== 'exams'}>
    <ClassExams {view} {entry} {onSelected} onRefresh={onChanged} />
  </div>
  <div role="tabpanel" id="panel-students" aria-labelledby="tab-students" hidden={tab !== 'students'}>
    <RosterPanel {roster} {loading} {problem} onReload={load}>
      {#snippet tools()}<button type="button" onclick={() => { status = ''; replacing = true; }}><Icon name="fileCheck" size={15} />Replace roster…</button>{/snippet}
    </RosterPanel>
  </div>
</div>
{#if renaming}<RenameClassDialog {entry} onClose={() => renaming = false} onRenamed={renamed} />{/if}
{#if replacing}<ReplaceRosterDialog {entry} onClose={() => replacing = false} onReplaced={replaced} />{/if}
{#if deleting}<Confirmation kind="deleteClass" details={{ entry }} busy={deleteBusy} error={deleteError} onCancel={() => deleting = false} onConfirm={confirmDelete} />{/if}
<style>
  .detail { display: grid; grid-template-columns: minmax(0, 1fr); gap: 1rem; width: 100%; max-width: 1040px; min-width: 0; margin: 0 auto; }
  .page-head { display: flex; flex-wrap: wrap; align-items: flex-start; gap: 1rem; margin-bottom: .2rem; }
  .title-block { min-width: 0; }
  h1 { margin: 0; font-size: 2rem; font-weight: 800; letter-spacing: -.02em; line-height: 1.15; overflow-wrap: anywhere; }
  .back { display: inline-flex; align-items: center; gap: .35rem; min-height: 0; margin: 0 0 .2rem; padding: .2rem .4rem .2rem .1rem; background: var(--transparent); border-color: var(--transparent); color: var(--action-primary); font-size: .85rem; }
  .back:hover:not(:disabled) { background: var(--hover-fill); border-color: var(--transparent); color: var(--action-hover); }
  .back :global(svg) { transition: transform .15s ease; } .back:hover :global(svg) { transform: translateX(-2px); }
  .meta { display: flex; flex-wrap: wrap; gap: .4rem; margin-top: .45rem; }
  .chip { display: inline-flex; align-items: center; gap: .35rem; padding: .12rem .6rem; border-radius: 999px; background: var(--bg-secondary); color: var(--text-main); font-size: .8rem; }
  .chip b { font-family: var(--font-mono); }
  .head-actions { display: flex; flex-direction: column; align-items: flex-end; gap: .4rem; margin-left: auto; }
  .action-row { display: flex; flex-wrap: wrap; justify-content: flex-end; gap: .5rem; }
  .head-actions button { display: inline-flex; align-items: center; gap: .45rem; min-height: 38px; margin: 0; padding: .3rem 1rem; border-radius: 9px; font-size: .92rem; }
  .outline { background: var(--bg-raised); border-color: var(--action-border); color: var(--action-hover); }
  .delete:not(:disabled) { background: var(--bg-raised); border-color: var(--alert-border); color: var(--alert-text); }
  .delete:hover:not(:disabled) { background: var(--alert-fill); border-color: var(--alert); color: var(--alert-text); }
  .hint { display: flex; align-items: flex-start; gap: .35rem; max-width: 340px; color: var(--text-muted); font-size: .78rem; text-align: right; }
  .hint :global(svg) { margin-top: .15rem; }
  p { margin: 0; }
  .tabs { display: inline-flex; justify-self: start; max-width: 100%; flex-wrap: wrap; gap: 4px; padding: 4px; border: 1px solid var(--border-subtle); border-radius: 12px; background: var(--bg-secondary); }
  .tabs button { position: relative; display: inline-flex; align-items: center; gap: .5rem; margin: 0; padding: .55rem 1rem; border: 0; border-radius: 9px; background: var(--transparent); color: var(--text-main); font-weight: 600; box-shadow: none; }
  .tabs button :global(svg) { color: var(--text-muted); }
  .tabs button:hover:not(:disabled) { color: var(--action-hover); }
  .tabs button[aria-selected="true"], .tabs button[aria-selected="true"]:hover:not(:disabled) { background: var(--bg-raised); color: var(--action-hover); box-shadow: var(--shadow-card); }
  .tabs button[aria-selected="true"] :global(svg) { color: var(--action-primary); }
  .tabs button[aria-selected="true"]::after { content: ''; position: absolute; left: 22%; right: 22%; bottom: 3px; height: 3px; border-radius: 3px; background: var(--bar-fill); }
  .n { padding: .02rem .45rem; border: 1px solid var(--border-subtle); border-radius: 999px; background: var(--bg-primary); color: var(--text-muted); font-family: var(--font-mono); font-size: .75rem; font-weight: 700; }
  .tabs button[aria-selected="true"] .n { background: var(--selection-fill); color: var(--action-hover); border-color: var(--action-border); }
  [role="tabpanel"][hidden] { display: none; }
  @media (max-width: 760px) { h1 { font-size: 1.6rem; } .head-actions { margin-left: 0; align-items: flex-start; } .action-row { justify-content: flex-start; } .hint { text-align: left; } }
</style>
