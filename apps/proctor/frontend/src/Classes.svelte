<script>
  import { untrack } from 'svelte';
  import Icon from './Icon.svelte';
  import ClassDetail from './ClassDetail.svelte';
  import AddClassDialog from './AddClassDialog.svelte';
  import { examLine } from './classes.js';
  // The classes stored on the server, as cards. Opening one shows its exams and, in another tab, its read-only roster.
  // `home` changes each time the Classes entry of the navigation is chosen again, which leads back to the list; `openRequest`
  // ({ classId, n }) opens a class on its exams, as the "Now admitting" bar does.
  let { view, disabled = false, onRefresh, onSelected, home = 0, openRequest = null } = $props();
  let search = $state('');
  let openId = $state('');
  let adding = $state(false);
  let status = $state('');
  $effect(() => { home; untrack(() => { openId = ''; }); });
  $effect(() => { if (openRequest) { const { classId } = openRequest; untrack(() => { openId = classId; }); } });
  const classes = $derived(view.classes ?? []);
  const fold = text => text.normalize('NFD').replace(/\p{M}/gu, '').toLowerCase();
  const shown = $derived(classes.filter(entry => fold(entry.name).includes(fold(search.trim()))));
  const open = $derived(classes.find(entry => entry.class_id === openId));
  const unreachable = $derived(!!view.error && !view.classes_loaded);
  function plural(n, noun) { return `${n} ${noun}${n === 1 ? '' : 's'}`; }
  // A class that was deleted from its own page: back to the list, with what happened.
  async function deleted(result) {
    openId = '';
    status = `Class “${result.name}” deleted${result.pruned > 0 ? `; ${plural(result.pruned, 'student')} who never logged in left the server too` : ''}.`;
    await onRefresh();
  }
  async function created(entry) {
    adding = false;
    status = `Class “${entry.name}” added with ${plural(entry.student_count, 'student')}.`;
    await onRefresh();
  }
</script>
{#if open}
  {#key `${open.class_id}:${openRequest?.n ?? 0}`}<ClassDetail {view} entry={open} onBack={() => openId = ''} onChanged={onRefresh} onSelected={onSelected} onDeleted={deleted} />{/key}
{:else}
  <div class="classes">
    <header class="page-head">
      <h1>Classes</h1>
      <div class="tools">
        <label class="search"><Icon name="search" size={22} /><input type="search" placeholder="Search classes…" aria-label="Search classes" bind:value={search} autocomplete="off" /></label>
        <button type="button" class="primary add" {disabled} onclick={() => { status = ''; adding = true; }}><Icon name="plus" size={22} />Add class</button>
      </div>
    </header>
    {#if status}<p role="status">{status}</p>{/if}
    {#if unreachable}
      <p role="alert">Cannot load the classes from the server. {view.error}</p>
    {:else if !view.classes_loaded}
      <p class="muted">Loading classes…</p>
    {:else if classes.length === 0}
      <div class="empty"><Icon name="classes" size={44} /><p><strong>No classes yet.</strong></p><p class="muted">Add a class with its roster file, then import its exams into it.</p></div>
    {:else if shown.length === 0}
      <p class="muted">No class matches “{search}”.</p>
    {:else}
      <ul class="cards" role="list">{#each shown as entry (entry.class_id)}
        <li><button type="button" class="card" onclick={() => openId = entry.class_id}>
          <span class="tile"><Icon name="classes" size={34} /></span>
          <span class="text"><strong>{entry.name}</strong><span>{plural(entry.student_count, 'student')}</span><span>{examLine(entry)}</span></span>
          <span class="go"><Icon name="chevron" size={24} /></span>
        </button></li>
      {/each}</ul>
    {/if}
  </div>
{/if}
{#if adding}<AddClassDialog onClose={() => adding = false} onCreated={created} />{/if}
<style>
  .classes { display: grid; grid-template-columns: minmax(0, 1fr); gap: var(--space-4); align-content: start; }
  .page-head { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: var(--space-3) var(--space-5); margin-bottom: var(--space-2); }
  h1 { margin: 0; font-size: clamp(1.9rem, 3.4vw, 2.7rem); font-weight: 800; letter-spacing: -.015em; }
  .tools { display: flex; flex: 1 1 28rem; min-width: min(28rem, 100%); flex-wrap: wrap; align-items: center; justify-content: flex-end; gap: var(--space-3); }
  .search { display: flex; flex: 1 1 14rem; align-items: center; gap: .6rem; max-width: 24rem; padding: 0 1rem; color: var(--color-text-muted); background: var(--color-surface); border: 1px solid var(--ink-2); border-radius: 12px; box-shadow: var(--shadow-search); }
  .search:focus-within { border-color: var(--ocean); box-shadow: 0 0 0 3px var(--focus-halo); }
  .search input { flex: 1; min-width: 0; min-height: 2.9rem; padding: 0; border: 0; background: var(--transparent); box-shadow: none; font-size: 1rem; }
  .search input:focus-visible { outline: none; }
  .add { display: inline-flex; align-items: center; gap: .55rem; min-height: 3.05rem; margin: 0; padding: .4rem 1.35rem; border-radius: 12px; font-size: 1.05rem; box-shadow: var(--shadow-action); }
  .cards { display: grid; grid-template-columns: repeat(auto-fill, minmax(min(22rem, 100%), 1fr)); gap: var(--space-4); margin: 0; padding: 0; list-style: none; }
  .card { display: flex; align-items: center; gap: 1.1rem; width: 100%; min-height: 6.6rem; margin: 0; padding: 1rem 1.2rem; text-align: left; color: var(--title-ink); background: var(--color-surface); border: 1px solid var(--line); border-radius: var(--radius-card); box-shadow: none; font-weight: 400; }
  .card:hover:not(:disabled) { background: var(--color-surface); border-color: var(--line-strong); box-shadow: none; }
  .tile { display: grid; place-items: center; flex: none; width: 4.3rem; height: 4.3rem; color: var(--ink-2); background: var(--transparent); border: 1px solid var(--line); border-radius: 12px; }
  .text { display: grid; gap: .15rem; flex: 1; min-width: 0; color: var(--color-text-muted); }
  .text strong { color: var(--title-ink); font-size: 1.15rem; font-weight: 700; overflow-wrap: anywhere; }
  .text span { overflow-wrap: anywhere; }
  .go { flex: none; color: var(--ink-2); }
  .empty { display: grid; justify-items: center; gap: 0; padding: var(--space-6) var(--space-4); color: var(--ink); background: var(--color-surface); border: 1px dashed var(--ink-2); border-radius: var(--radius-card); text-align: center; }
  .empty p { margin: var(--space-1) 0 0; color: var(--title-ink); }
  .empty .muted { color: var(--color-text-muted); }
</style>
