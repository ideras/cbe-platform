<script>
  import { untrack } from 'svelte';
  import Icon from '../../../../shared/theme/Icon.svelte';
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
          <span class="text"><strong>{entry.name}</strong><span class="pill students">{plural(entry.student_count, 'student')}</span><span class="pill exams">{examLine(entry)}</span></span>
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
  h1 { margin: 0; font-size: clamp(1.9rem, 3.4vw, 2.3rem); font-weight: 800; letter-spacing: -.02em; }
  .tools { display: flex; flex: 1 1 28rem; min-width: min(28rem, 100%); flex-wrap: wrap; align-items: center; justify-content: flex-end; gap: var(--space-3); }
  .search { display: flex; flex: 1 1 14rem; align-items: center; gap: .6rem; max-width: 22.5rem; padding: 0 .9rem; color: var(--text-muted); background: var(--bg-raised); border: 1px solid var(--control-border); border-radius: var(--radius-control); transition: border-color .2s ease, box-shadow .2s ease, color .2s ease; }
  .search:focus-within { outline: 2px solid var(--transparent); border-color: var(--action-primary); box-shadow: 0 0 0 3px var(--focus-halo); color: var(--action-primary); }
  .search input { flex: 1; min-width: 0; min-height: 2.9rem; padding: 0; border: 0; background: var(--transparent); box-shadow: none; font-size: 1rem; }
  .search input:focus-visible { outline: none; background: var(--transparent); box-shadow: none; }
  .add { display: inline-flex; align-items: center; gap: .55rem; min-height: 2.9rem; margin: 0; padding: .4rem 1.3rem; font-size: 1rem; }
  .cards { display: grid; grid-template-columns: repeat(auto-fill, minmax(min(22rem, 100%), 1fr)); gap: var(--space-4); margin: 0; padding: 0; list-style: none; }
  /* A clickable card lifts on hover: cyan edge, a cyan-to-blue bar on the left, and the chevron turns blue and moves. */
  .card { position: relative; display: flex; align-items: center; gap: 1.1rem; width: 100%; min-height: 6.4rem; margin: 0; padding: 1.1rem 1.2rem; overflow: hidden; text-align: left; color: var(--text-main); background: var(--bg-raised); border: 1px solid var(--border-subtle); border-radius: var(--radius-card); box-shadow: var(--shadow-card); font-weight: 400; transition: transform .15s ease, box-shadow .15s ease, border-color .15s ease; }
  .card::before { content: ''; position: absolute; left: 0; top: 0; bottom: 0; width: 4px; background: var(--accent-bar); opacity: 0; transition: opacity .15s ease; }
  .card:hover:not(:disabled) { transform: translateY(-2px); background: var(--bg-raised); border-color: var(--accent-cyan-soft); color: var(--text-main); box-shadow: var(--shadow-card-hover); }
  .card:hover::before, .card:focus-visible::before { opacity: 1; }
  .card:active:not(:disabled) { transform: none; box-shadow: var(--shadow-card); }
  .tile { display: grid; place-items: center; flex: none; width: 4.3rem; height: 4.3rem; color: var(--action-primary); background: var(--tile-fill); border: 1px solid var(--cyan-border); border-radius: 12px; }
  .text { display: flex; flex-wrap: wrap; align-items: center; gap: .4rem .45rem; flex: 1; min-width: 0; }
  .text strong { flex: 1 1 100%; color: var(--text-main); font-size: 1.1rem; font-weight: 700; overflow-wrap: anywhere; }
  /* Count pills: students neutral, exams in the warm accent. Muted text would be 4.4:1 on --bg-secondary, so both use full ink. */
  .pill { display: inline-block; max-width: 100%; padding: .1rem .6rem; border-radius: 999px; font-size: .84rem; line-height: 1.45; overflow-wrap: anywhere; }
  .pill.students { background: var(--bg-secondary); color: var(--text-main); }
  .pill.exams { background: var(--warn-fill); color: var(--warn-text); }
  .go { flex: none; color: var(--text-muted); transition: color .15s ease, transform .15s ease; }
  .card:hover .go, .card:focus-visible .go { color: var(--action-primary); transform: translateX(3px); }
  .empty { display: grid; justify-items: center; gap: 0; padding: var(--space-6) var(--space-4); color: var(--action-primary); background: var(--bg-raised); border: 1px dashed var(--control-border); border-radius: var(--radius-card); text-align: center; }
  .empty p { margin: var(--space-1) 0 0; color: var(--text-main); }
  .empty .muted { color: var(--text-muted); }
</style>
