<script>
  import { onMount } from 'svelte';
  import * as api from '$proctor-api';
  import ClassExams from './ClassExams.svelte';
  import RosterPanel from './RosterPanel.svelte';
  import RenameClassDialog from './RenameClassDialog.svelte';
  import ReplaceRosterDialog from './ReplaceRosterDialog.svelte';
  import Confirmation from './Confirmation.svelte';
  import Icon from './Icon.svelte';
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
  const tabs = $derived([{ id: 'exams', label: `Exams (${examCount})` }, { id: 'students', label: `Students (${entry.student_count})` }]);
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
    <button type="button" class="back" onclick={onBack}><Icon name="back" size={20} />Classes</button>
    <h1>{entry.name}</h1>
    <div class="head-actions">
      <button type="button" class="outline" onclick={() => { status = ''; renaming = true; }}>Rename…</button>
      <button type="button" class="delete" disabled={hasExams} aria-describedby={hasExams ? 'delete-hint' : undefined} onclick={askDelete}>Delete class…</button>
    </div>
  </header>
  {#if hasExams}<p class="muted" id="delete-hint">Delete its exams first. Exams students have taken cannot be deleted.</p>{/if}
  {#if status}<p role="status">{status}</p>{/if}
  <div class="tabs" role="tablist" aria-label="{entry.name} sections">
    {#each tabs as item (item.id)}
      <button type="button" role="tab" id="tab-{item.id}" aria-selected={tab === item.id} aria-controls="panel-{item.id}" tabindex={tab === item.id ? 0 : -1} onkeydown={moveTab} onclick={() => tab = item.id}>{item.label}</button>
    {/each}
  </div>
  <div role="tabpanel" id="panel-exams" aria-labelledby="tab-exams" hidden={tab !== 'exams'}>
    <ClassExams {view} {entry} {onSelected} onRefresh={onChanged} />
  </div>
  <div role="tabpanel" id="panel-students" aria-labelledby="tab-students" hidden={tab !== 'students'}>
    <RosterPanel {roster} {loading} {problem} onReload={load}>
      {#snippet tools()}<button type="button" onclick={() => { status = ''; replacing = true; }}>Replace roster…</button>{/snippet}
    </RosterPanel>
  </div>
</div>
{#if renaming}<RenameClassDialog {entry} onClose={() => renaming = false} onRenamed={renamed} />{/if}
{#if replacing}<ReplaceRosterDialog {entry} onClose={() => replacing = false} onReplaced={replaced} />{/if}
{#if deleting}<Confirmation kind="deleteClass" details={{ entry }} busy={deleteBusy} error={deleteError} onCancel={() => deleting = false} onConfirm={confirmDelete} />{/if}
<style>
  .detail { display: grid; grid-template-columns: minmax(0, 1fr); gap: var(--space-4); }
  .page-head { display: flex; flex-wrap: wrap; align-items: center; gap: var(--space-3) var(--space-4); }
  h1 { margin: 0; font-size: clamp(1.7rem, 3vw, 2.4rem); font-weight: 800; letter-spacing: -.01em; overflow-wrap: anywhere; }
  .back { display: inline-flex; align-items: center; gap: .4rem; margin: 0; background: var(--transparent); border-color: var(--transparent); color: var(--ink-2); }
  .back:hover:not(:disabled) { background: var(--hover); border-color: var(--transparent); }
  .head-actions { display: flex; flex-wrap: wrap; gap: var(--space-2); margin-left: auto; }
  .head-actions button { margin: 0; }
  .head-actions .outline { border-color: var(--line-strong); color: var(--ink); }
  .head-actions .outline:hover:not(:disabled) { background: var(--hover); border-color: var(--line-strong); }
  .head-actions .outline:disabled { background: var(--transparent); border-color: var(--line-strong); }
  .delete { background: var(--transparent); border-color: var(--transparent); color: var(--color-danger); }
  .delete:hover:not(:disabled) { background: var(--color-danger-bg); border-color: var(--transparent); color: var(--color-danger-strong); }
  p { margin: 0; }
  .tabs { display: flex; flex-wrap: wrap; gap: var(--space-1); border-bottom: 1px solid var(--panel-border); }
  .tabs button { margin: 0 0 -1px; padding: .6rem 1.3rem; border: 0; border-bottom: 3px solid var(--transparent); border-radius: var(--radius-control) var(--radius-control) 0 0; background: var(--transparent); color: var(--color-text-muted); font-size: 1.05rem; font-weight: 600; }
  .tabs button:hover:not(:disabled) { background: var(--brand-soft); color: var(--brand-strong); }
  .tabs button[aria-selected="true"] { color: var(--ink); border-bottom-color: var(--ocean); }
  [role="tabpanel"][hidden] { display: none; }
</style>
