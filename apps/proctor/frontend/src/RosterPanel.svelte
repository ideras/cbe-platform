<script>
  import Icon from '../../../../shared/theme/Icon.svelte';
  import StudentHighlight from './StudentHighlight.svelte';
  import { visibleStudents } from './student-roster.js';
  import { initials } from './roster.js';
  // Searching, sorting and selection are local presentation only. The roster is
  // still read-only and can only change through the confirmed replacement flow.
  let { roster, loading = false, problem = '', readonly = true, tools, onReload } = $props();
  let query = $state('');
  let sortKey = $state('account_number');
  let ascending = $state(true);
  let selected = $state(null);
  const students = $derived(roster?.students ?? []);
  const visible = $derived(visibleStudents(students, query, sortKey, ascending));
  const heading = $derived(roster ? `Roster (${roster.total} ${roster.total === 1 ? 'student' : 'students'})` : 'Roster');
  $effect(() => { if (selected && !students.some(student => student.account_number === selected)) selected = null; });
  function sort(key) { if (sortKey === key) ascending = !ascending; else { sortKey = key; ascending = true; } }
  function select(account) { selected = selected === account ? null : account; }
</script>
<section class="panel" aria-label="Roster" aria-busy={loading}>
  <header class="panel-head">
    <h2 aria-label={heading}>Roster</h2>
    {#if readonly}<span class="badge" title="The list cannot be edited student by student; it changes by replacing the whole roster file."><Icon name="lock" size={11} />Read-only</span>{/if}
    <div class="tools" role="group" aria-label="Roster actions">
      {@render tools?.()}
      <button type="button" disabled={loading} onclick={onReload}><Icon name="refresh" size={15} />Reload</button>
    </div>
  </header>
  <div class="panel-body">
    {#if problem}
      <p role="alert">The roster could not be loaded. {problem}</p>
    {:else if loading && !roster}
      <p class="muted">Loading roster…</p>
    {:else if roster}
      {#if roster.total > students.length}<p class="muted">Showing the first {students.length} of {roster.total}. Search covers these loaded students only.</p>{/if}
      <div class="roster-toolbar">
        <label class="search"><Icon name="search" size={16} /><input type="search" aria-label="Search students by name or account" placeholder="Search by name or account…" autocomplete="off" spellcheck="false" bind:value={query} /></label>
        <span class="row-count" aria-live="polite">{query.trim() ? `${visible.length} of ${students.length}` : students.length} {students.length === 1 ? 'student' : 'students'}</span>
      </div>
      <!-- svelte-ignore a11y_no_noninteractive_tabindex (A long roster must be keyboard-scrollable in WebKit.) -->
      <div class="table-scroll" role="region" aria-label="Students table — scroll for more students" tabindex="0">
        <table>
          <caption>Students of {roster.name}</caption>
          <colgroup><col class="index-column" /><col class="account-column" /><col /></colgroup>
          <thead><tr>
            <th class="idx" scope="col">#</th>
            {#each [{ key: 'account_number', title: 'Account number' }, { key: 'student_name', title: 'Full name' }] as column}
              <th scope="col" aria-sort={sortKey === column.key ? ascending ? 'ascending' : 'descending' : 'none'}>
                <button type="button" onclick={() => sort(column.key)}>{column.title}<Icon name="chevron" class={`sort-arrow ${sortKey === column.key ? 'sorted' : ''} ${sortKey === column.key && !ascending ? 'descending' : ''}`} size={13} /></button>
              </th>
            {/each}
          </tr></thead>
          <tbody>{#each visible as student, index (student.account_number)}
            <!-- svelte-ignore a11y_click_events_have_key_events a11y_no_noninteractive_element_interactions (The name button provides the same selection action for keyboard users.) -->
            <tr class:sel={selected === student.account_number} onclick={() => select(student.account_number)}>
              <td class="idx">{index + 1}</td>
              <td class="acct"><StudentHighlight text={student.account_number} {query} /></td>
              <td><button type="button" class="who" aria-label="Select student {student.student_name || student.account_number}" aria-pressed={selected === student.account_number} onclick={event => { event.stopPropagation(); select(student.account_number); }}>
                <span class="avatar" aria-hidden="true">{initials(student.student_name || '')}</span><span class="student-name"><StudentHighlight text={student.student_name || '—'} {query} /></span>
              </button></td>
            </tr>
          {:else}<tr><td class="empty" colspan="3">{students.length ? 'No students match your search.' : 'This class has no students.'}</td></tr>{/each}</tbody>
        </table>
      </div>
    {/if}
  </div>
</section>
<style>
  .panel { min-width: 0; overflow: hidden; background: var(--bg-raised); border: 1px solid var(--border-subtle); border-radius: 14px; box-shadow: var(--shadow-card); }
  .panel-head { display: flex; flex-wrap: wrap; align-items: center; gap: .6rem; padding: .8rem 1rem; background: var(--header-soft); border-bottom: 1px solid var(--cyan-border); }
  h2 { margin: 0; color: var(--action-hover); font-size: 1.05rem; }
  .badge { display: inline-flex; align-items: center; gap: .3rem; padding: .15rem .55rem; border: 1px solid var(--border-subtle); border-radius: 999px; color: var(--text-main); background: var(--bg-secondary); font-size: .75rem; font-weight: 700; letter-spacing: .04em; text-transform: uppercase; }
  .tools { display: flex; flex-wrap: wrap; align-items: center; gap: .5rem; margin-left: auto; }
  .tools :global(button) { display: inline-flex; align-items: center; justify-content: center; gap: .45rem; min-height: 38px; margin: 0; padding: .3rem 1rem; border: 1px solid var(--action-border); border-radius: 9px; background: var(--bg-raised); color: var(--action-hover); font-size: .92rem; }
  .tools :global(button:hover:not(:disabled)) { background: var(--selection-fill); border-color: var(--action-primary); }
  .tools :global(button:disabled) { background: var(--bg-secondary); color: var(--muted-dot); }
  .panel-body { display: flex; flex-direction: column; gap: .9rem; min-width: 0; padding: 1rem; }
  .panel-body > p { margin: 0; }
  .roster-toolbar { display: flex; flex-wrap: wrap; align-items: center; gap: .75rem; }
  .search { display: flex; align-items: center; flex: 1 1 200px; min-width: 0; max-width: 360px; gap: .5rem; min-height: 38px; padding: 0 .75rem; background: var(--bg-primary); border: 1px solid var(--control-border); border-radius: 9px; color: var(--text-muted); }
  .search:focus-within { border-color: var(--action-primary); box-shadow: 0 0 0 3px var(--focus-halo); color: var(--action-primary); }
  .search input { width: 100%; min-width: 0; min-height: 36px; padding: 0; border: 0; background: var(--transparent); box-shadow: none; font-size: .92rem; }
  .search input:focus-visible { outline: none; background: var(--transparent); box-shadow: none; }
  .row-count { margin-left: auto; color: var(--text-muted); font-size: .82rem; }
  .table-scroll { max-height: 420px; overflow: auto; border: 1px solid var(--border-subtle); border-radius: 12px; }
  table { width: 100%; min-width: 520px; border-collapse: collapse; table-layout: fixed; background: var(--bg-raised); }
  caption { position: absolute; width: 1px; height: 1px; overflow: hidden; clip-path: inset(50%); white-space: nowrap; }
  .index-column { width: 56px; } .account-column { width: 220px; }
  th { position: sticky; top: 0; z-index: 1; padding: .6rem .9rem; text-align: left; white-space: nowrap; background: var(--bg-secondary); color: var(--action-hover); font-size: .7rem; font-weight: 700; letter-spacing: .08em; text-transform: uppercase; border-bottom: 1px solid var(--border-subtle); }
  th button { display: inline-flex; align-items: center; gap: .25rem; min-height: 0; margin: 0; padding: 0; border: 0; border-radius: 0; background: var(--transparent); color: inherit; box-shadow: none; font: inherit; letter-spacing: inherit; text-transform: inherit; }
  th button:hover:not(:disabled) { background: var(--transparent); color: var(--action-primary); }
  th :global(.sort-arrow) { opacity: .35; transform: rotate(-90deg); }
  th :global(.sort-arrow.sorted) { opacity: 1; color: var(--action-primary); }
  th :global(.sort-arrow.descending) { transform: rotate(90deg); }
  td { padding: .55rem .9rem; vertical-align: middle; overflow-wrap: anywhere; border-bottom: 1px solid var(--border-subtle); font-size: .92rem; }
  tbody tr:last-child td { border-bottom: 0; }
  tbody tr { cursor: pointer; }
  tbody tr:nth-child(even) { background: var(--bg-primary); }
  tbody tr:hover { background: var(--hover-fill); }
  tbody tr.sel { background: var(--selection-fill); box-shadow: inset 3px 0 0 var(--action-primary); }
  .idx { color: var(--text-muted); font-family: var(--font-mono); font-size: .78rem; }
  tr.sel .idx { color: var(--text-main); }
  .acct { color: var(--action-hover); font-family: var(--font-mono); font-weight: 600; }
  .who { display: flex; align-items: center; gap: .6rem; width: 100%; min-height: 28px; margin: 0; padding: 0; text-align: left; background: var(--transparent); color: var(--text-main); border: 0; border-radius: 0; box-shadow: none; font-size: inherit; font-weight: 400; }
  .who:hover:not(:disabled) { background: var(--transparent); color: var(--text-main); }
  .avatar { display: grid; place-items: center; flex: none; width: 28px; height: 28px; border-radius: 50%; background: var(--bg-secondary); color: var(--text-main); font-size: .7rem; font-weight: 700; }
  tr.sel .avatar { background: var(--action-primary); color: var(--bg-primary); }
  .student-name { min-width: 0; overflow-wrap: anywhere; }
  .empty { padding: 1.5rem; text-align: center; color: var(--text-muted); }
  @media (max-width: 760px) { .search { max-width: none; } .row-count { width: 100%; margin-left: 0; } .tools { margin-left: 0; } }
</style>
