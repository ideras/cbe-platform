<script>
  import Panel from './Panel.svelte';
  import Icon from './Icon.svelte';
  // The roster of one class as a table of account number and full name. It is read-only in this version: `tools` is the place
  // where actions that change a roster (add, edit, remove, import, sync) will go, so they can be added without redesigning the panel.
  let { roster, loading = false, problem = '', readonly = true, tools, onReload } = $props();
  const students = $derived(roster?.students ?? []);
  function plural(n, noun) { return `${n} ${noun}${n === 1 ? '' : 's'}`; }
  // "Roster (28 students)": the count belongs to the heading, on the same line.
  const heading = $derived(roster ? `Roster (${plural(roster.total, 'student')})` : 'Roster');
</script>
<Panel label="Roster" title={heading}>
  {#snippet actions()}
    {#if readonly}<span class="badge" title="The list cannot be edited student by student; it changes by replacing the whole roster file.">Read-only</span>{/if}
    {@render tools?.()}
    <button type="button" disabled={loading} onclick={onReload}><Icon name="refresh" size={18} />Reload</button>
  {/snippet}
  {#if problem}
    <p role="alert">The roster could not be loaded. {problem}</p>
  {:else if loading && !roster}
    <p class="muted">Loading roster…</p>
  {:else if roster}
    {#if roster.total > students.length}<p class="muted">Showing the first {students.length} of {roster.total}.</p>{/if}
    <div class="table-scroll" tabindex="-1">
      <table>
        <caption>Students of {roster.name}, by account number</caption>
        <thead><tr><th scope="col">Account number</th><th scope="col">Full name</th></tr></thead>
        <tbody>{#each students as student (student.account_number)}
          <tr><td>{student.account_number}</td><td>{student.student_name || '—'}</td></tr>
        {/each}</tbody>
      </table>
    </div>
  {/if}
</Panel>
<style>
  button { display: inline-flex; align-items: center; gap: .4rem; margin: 0; }
  .badge { padding: .15rem .6rem; color: var(--color-text-muted); background: var(--surface); border: 1px solid var(--panel-border); border-radius: 999px; font-size: var(--text-sm); font-weight: 600; }
  .table-scroll { max-height: 30rem; overflow: auto; border: 1px solid var(--panel-border); border-radius: 10px; }
  table { width: 100%; border-collapse: collapse; background: var(--color-surface); }
  caption { position: absolute; width: 1px; height: 1px; overflow: hidden; clip-path: inset(50%); white-space: nowrap; }
  th { position: sticky; top: 0; padding: .55rem .8rem; text-align: left; white-space: nowrap; background: var(--color-surface); color: var(--ink-2); font-size: var(--text-sm); border-bottom: 1px solid var(--panel-border); }
  td { padding: .5rem .8rem; vertical-align: top; overflow-wrap: anywhere; border-bottom: 1px solid var(--line); }
  td:first-child { width: 14rem; font-variant-numeric: tabular-nums; }
  tbody tr:last-child td { border-bottom: 0; }
  tbody tr:hover { background: var(--hover); }
</style>
