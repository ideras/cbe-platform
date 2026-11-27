<script>
  import Icon from '../../../../shared/theme/Icon.svelte';

  let { run, todos, query } = $props();
  let filter = $state('all');
  const search = $derived(query.trim().toLowerCase());
  const limits = $derived(new Map(todos.map(todo => [todo.id, todo.points])));
  const labels = { full: 'Full marks', partial: 'Partial', zero: 'Zero', passed: 'Passed', failed: 'Failed' };
  const number = value => Number(value.toFixed(2));
  const percent = (points, max) => Math.min(100, Math.max(0, points / max * 100));

  function parseItem(result) {
    const { todo_id: id, points, passed, known } = result;
    const log = result.log ?? '';
    const parts = log.split(' · ');
    const score = /^(\d+(?:\.\d+)?)\s*\/\s*(\d+(?:\.\d+)?)\s*pts?\.?$/i.exec(parts[0].trim());
    const checks = [];
    for (const part of score ? parts.slice(1) : parts) {
      const match = /^(.+?)\s+([✓✗])(?:\s+\(([\s\S]*)\))?$/u.exec(part.trim());
      if (!match) continue;
      const [, name, mark, reason = ''] = match;
      const comparison = /^respondió «([\s\S]*)», se esperaba «([\s\S]*)»$/u.exec(reason);
      checks.push({ name, passed: mark === '✓', reason, comparison });
    }
    const printedMax = score ? Number(score[2]) : null;
    const max = limits.get(id) ?? (Number.isFinite(printedMax) ? printedMax : null);
    let state = passed ? 'passed' : 'failed';
    if (max > 0) state = points >= max ? 'full' : points > 0 ? 'partial' : 'zero';
    const exercise = /^e(\d+)/i.exec(id);
    const group = !known ? 'Other output' : exercise ? `Exercise ${exercise[1]}` : 'TODO items';
    return { id, points, max, passed, known, state, group, log, checks, passedChecks: checks.filter(check => check.passed).length };
  }

  const items = $derived((run.results ?? []).map(parseItem));
  const graded = $derived(items.filter(item => item.known));
  const counts = $derived({
    full: graded.filter(item => item.state === 'full').length,
    partial: graded.filter(item => item.state === 'partial').length,
    zero: graded.filter(item => item.state === 'zero').length,
    checks: items.reduce((sum, item) => sum + item.checks.length, 0),
    passed: items.reduce((sum, item) => sum + item.passedChecks, 0),
  });
  const filters = $derived([
    { id: 'all', label: 'All', count: items.length },
    { id: 'lost', label: 'Lost points', count: counts.partial + counts.zero },
    { id: 'full', label: 'Full marks', count: counts.full },
    { id: 'zero', label: 'Zero', count: counts.zero },
  ]);
  const groups = $derived.by(() => {
    const byName = new Map();
    for (const item of items) {
      if (!byName.has(item.group)) byName.set(item.group, []);
      byName.get(item.group).push(item);
    }
    return Array.from(byName, ([name, members]) => ({
      name,
      points: members.reduce((sum, item) => sum + item.points, 0),
      max: members.every(item => item.known && item.max != null) ? members.reduce((sum, item) => sum + item.max, 0) : null,
      members: members.filter(item => {
        const lost = item.state === 'partial' || item.state === 'zero';
        const matchesFilter = filter === 'all' || (item.known && (filter === 'lost' ? lost : item.state === filter));
        return matchesFilter && (item.id.toLowerCase().includes(search) || item.log.toLowerCase().includes(search));
      }),
    })).filter(group => group.members.length);
  });

  let expanded = $derived(new Set(items.filter(item => search || !['full', 'passed'].includes(item.state)).map(item => item.id)));
  function toggle(id) {
    const next = new Set(expanded);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    expanded = next;
  }
</script>

{#snippet highlighted(text)}
  {@const index = search ? text.toLowerCase().indexOf(search) : -1}
  {#if index < 0}{text}{:else}{text.slice(0, index)}<mark>{text.slice(index, index + search.length)}</mark>{text.slice(index + search.length)}{/if}
{/snippet}

{#snippet chip(check)}
  <span class="check" class:ok={check.passed} class:bad={!check.passed}>{check.passed ? '✓' : '✗'} {@render highlighted(check.name)}</span>
{/snippet}

<div class="summary" role="group" aria-label="Run summary">
  <div class="total">
    <div>
      <strong>{run.points == null ? '—' : number(run.points)}</strong>
      {#if run.max_points != null}<span>/ {number(run.max_points)}</span>{/if}
      <small>points</small>
    </div>
    {#if run.max_points > 0 && run.points != null}<div class="track"><i style:width="{percent(run.points, run.max_points)}%"></i></div>{/if}
  </div>
  {#each ['full', 'partial', 'zero'] as state}
    <div class="tile {state}"><strong class:muted={!counts[state]}>{counts[state]}</strong><small>{labels[state].toLowerCase()}</small></div>
  {/each}
  <div class="tile checks"><strong>{counts.checks ? `${counts.passed}/${counts.checks}` : '—'}</strong><small>checks passed</small></div>
</div>

<div class="tools">
  <div class="filters" role="group" aria-label="Filter results">
    {#each filters as choice (choice.id)}
      <button class="filter" aria-pressed={filter === choice.id} onclick={() => filter = choice.id}>{choice.label} <span>{choice.count}</span></button>
    {/each}
  </div>
  <div class="actions">
    <button onclick={() => expanded = new Set(items.map(item => item.id))}>Expand all</button>
    <button onclick={() => expanded = new Set()}>Collapse all</button>
  </div>
</div>

{#each groups as group (group.name)}
  <section aria-label={group.name}>
    <div class="group-head"><h3>{group.name}</h3><span class="rule"></span>
      {#if group.max != null}<b class:complete={group.max > 0 && group.points >= group.max} class:none={group.points === 0}>{number(group.points)} / {number(group.max)}</b>{/if}
    </div>
    {#each group.members as item (item.id)}
      <div class="item {item.state}">
        <button class="item-head" aria-expanded={expanded.has(item.id)} onclick={() => toggle(item.id)}>
          <span class="caret"><Icon name="chevron" size={14}/></span>
          <span class="key">{@render highlighted(item.id)}</span>
          <span class="preview">
            {#if item.checks.length}
              <span class="chips">{#each item.checks as check}{@render chip(check)}{/each}</span>
              <span class="compact">{item.passedChecks}/{item.checks.length} checks</span>
            {:else}<span class="excerpt">{@render highlighted(item.log || labels[item.state])}</span>{/if}
          </span>
          <span class="score"><b>{number(item.points)} {#if item.max != null}<small>/ {number(item.max)}</small>{:else}<small>pts</small>{/if}</b>
            {#if item.max > 0}<span class="track"><i style:width="{percent(item.points, item.max)}%"></i></span>{/if}
          </span>
          <span class="pill">{labels[item.state]}</span>
        </button>
        {#if expanded.has(item.id)}
          {@const bare = item.checks.filter(c => !c.passed && !c.reason)}
          <div class="detail">
            {#if !item.known}<span>This key is outside the exam’s TODOs and contributes no points to the grade.</span>{/if}
            {#if (item.state === 'full' && !item.passed) || (['zero', 'partial'].includes(item.state) && item.passed)}
              <span>Grader reported {item.passed ? 'passed' : 'failed'}.</span>
            {/if}
            {#if item.passedChecks}
              <div class="check-row"><b class="pass-label">Passed</b>{#each item.checks.filter(c => c.passed) as check}{@render chip(check)}{/each}</div>
            {/if}
            {#if bare.length}
              <div class="check-row"><b class="fail-label">Failed</b>{#each bare as check}{@render chip(check)}{/each}<span>· no detail given</span></div>
            {/if}
            {#each item.checks.filter(c => !c.passed && c.reason) as check}
              <div class="failure"><span class="cross" aria-label="Failed">✕</span><div>
                <b>{@render highlighted(check.name)}</b>
                {#if check.comparison}
                  <dl><dt>Answered</dt><dd class="bad">{@render highlighted(check.comparison[1])}</dd>
                    <dt>Expected</dt><dd class="ok">{@render highlighted(check.comparison[2])}</dd></dl>
                {:else}<div>{@render highlighted(check.reason)}</div>{/if}
              </div></div>
            {/each}
            <div class="log">{@render highlighted(item.log || 'No log printed.')}</div>
          </div>
        {/if}
      </div>
    {/each}
  </section>
{:else}
  <p class="empty">No results match{query.trim() ? ` “${query.trim()}”` : ' this filter'}.</p>
{/each}

<style>
  .summary { display: grid; grid-template-columns: minmax(160px, 1.3fr) repeat(4, minmax(0, 1fr)); gap: .5rem; }
  .total, .tile { display: grid; align-content: center; min-width: 0; padding: .5rem .75rem; border-radius: 10px; }
  .total { gap: .4rem; background: var(--header-soft); border: 1px solid var(--cyan-chip-border); }
  .total > div:first-child { display: flex; align-items: baseline; gap: .35rem; flex-wrap: wrap; }
  .total strong { color: var(--action-hover); font: 700 1.55rem/1 var(--mono); }
  .total span { font-family: var(--mono); color: var(--text-muted); }
  .total small { margin-left: auto; }
  .summary small { font-size: .76rem; color: var(--text-muted); }
  .track { display: block; height: 5px; border-radius: 1rem; background: var(--bg-secondary); overflow: hidden; }
  .track i { display: block; height: 100%; background: var(--accent); }
  .total .track { height: 7px; background: var(--bg-raised); border: 1px solid var(--action-border); }
  .total .track i { background: var(--bar-fill); }
  .full, .passed { --accent: var(--ok); --ink: var(--ok-text); --fill: var(--ok-fill); }
  .partial { --accent: var(--warn); --ink: var(--warn-text); --fill: var(--warn-fill); }
  .zero, .failed { --accent: var(--alert); --ink: var(--alert-text); --fill: var(--alert-fill); }
  .checks { --accent: var(--accent-cyan); --ink: var(--action-primary); }
  .tile { border: 1px solid var(--border-subtle); border-top: 3px solid var(--accent); background: var(--bg-primary); }
  .tile strong { font: 700 1.2rem/1.2 var(--mono); color: var(--ink); }
  .tile strong.muted { color: var(--text-muted); }
  .tools, .filters, .actions, .check-row { display: flex; flex-wrap: wrap; gap: .4rem; align-items: center; }
  .tools { margin: .8rem 0; justify-content: space-between; }
  .filter, .actions button { min-height: 28px; font-size: .8rem; font-weight: 650; box-shadow: none; }
  .filter { padding: .2rem .65rem; border-radius: 1rem; background: var(--bg-primary); color: var(--text-muted); }
  .filter span { margin-left: .2rem; font: .75rem var(--mono); }
  .filter[aria-pressed='true'] { color: var(--action-hover); background: var(--selection-fill); border-color: var(--action-primary); }
  .actions button { padding: .1rem; background: none; border: 0; color: var(--action-primary); }
  .actions button:hover { text-decoration: underline; }
  section { display: grid; gap: .4rem; margin-top: .8rem; }
  .group-head { display: flex; align-items: center; gap: .6rem; }
  .group-head h3 { margin: 0; text-transform: uppercase; letter-spacing: .08em; color: var(--action-primary); font-size: .75rem; }
  .rule { flex: 1; border-top: 1px solid var(--border-subtle); }
  .group-head b { color: var(--warn-text); font: 700 .8rem var(--mono); }
  .group-head .complete, .pass-label { color: var(--ok-text); }
  .group-head .none, .fail-label { color: var(--alert-text); }
  .item { min-width: 0; border: 1px solid var(--border-subtle); border-left: 4px solid var(--accent); border-radius: 10px; overflow: hidden; }
  .item-head { display: grid; grid-template-columns: 14px minmax(3.2rem, max-content) minmax(0, 1fr) minmax(5rem, 14%) 6rem; gap: .6rem; align-items: center; width: 100%; padding: .5rem .75rem; border: 0; border-radius: 0; background: var(--bg-raised); text-align: left; font-weight: 400; box-shadow: none; }
  .item-head:hover { background: var(--hover-fill); }
  .item-head:focus-visible { outline-offset: -2px; }
  .caret { display: flex; color: var(--text-muted); }
  .item-head[aria-expanded='true'] .caret { transform: rotate(90deg); color: var(--action-primary); }
  .key { min-width: 0; max-width: 10rem; padding: .1rem .35rem; overflow-wrap: anywhere; text-align: center; color: var(--action-hover); background: var(--hover-fill); border: 1px solid var(--action-border); border-radius: 6px; font: 700 .86rem var(--mono); }
  .preview { min-width: 0; color: var(--text-muted); font-size: .8rem; }
  .chips { display: flex; flex-wrap: wrap; gap: .25rem; }
  .compact { display: none; }
  .excerpt { display: block; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .check, dd { max-width: 100%; padding: .1rem .35rem; border-radius: 5px; font: .76rem/1.35 var(--mono); overflow-wrap: anywhere; }
  .ok { color: var(--ok-text); background: var(--ok-fill); }
  .bad { color: var(--alert-text); background: var(--alert-fill); }
  .score { display: grid; gap: .25rem; color: var(--ink); text-align: right; font: .92rem var(--mono); }
  .score small { color: var(--text-muted); font-weight: 500; }
  .pill { border-radius: 1rem; padding: .18rem .35rem; text-align: center; font-size: .75rem; font-weight: 700; color: var(--ink); background: var(--fill); }
  .detail { display: grid; gap: .45rem; padding: .6rem .85rem .75rem 2.35rem; border-top: 1px solid var(--border-subtle); background: var(--bg-primary); color: var(--text-muted); font-size: .8rem; overflow-wrap: anywhere; }
  .failure { display: grid; grid-template-columns: 20px minmax(0, 1fr); gap: .5rem; padding: .5rem .6rem; border: 1px solid var(--alert-border); border-radius: 8px; background: var(--bg-raised); }
  .failure b { font-family: var(--mono); color: var(--text-main); }
  .cross { display: grid; place-items: center; height: 20px; border-radius: 50%; background: var(--alert-strong); color: var(--on-dark); }
  dl { display: grid; grid-template-columns: auto minmax(0, 1fr); gap: .2rem .65rem; margin: .3rem 0 0; align-items: baseline; }
  dt { text-transform: uppercase; letter-spacing: .05em; font-size: .7rem; font-weight: 700; }
  dd { justify-self: start; margin: 0; white-space: pre-wrap; }
  .log { white-space: pre-wrap; font: .76rem/1.5 var(--mono); }
  mark { background: var(--warn-fill); color: inherit; border-radius: 3px; }
  .empty { text-align: center; padding: 2rem .5rem; color: var(--text-muted); }
  @media (max-width: 720px) {
    .summary { grid-template-columns: repeat(2, minmax(0, 1fr)); }
    .total { grid-column: 1 / -1; }
    .item-head { grid-template-columns: 12px minmax(3rem, max-content) minmax(0, 1fr) 4.8rem; gap: .45rem; }
    .key { max-width: 6rem; }
    .pill, .chips { display: none; }
    .compact { display: inline; }
    .detail { padding-left: .85rem; }
  }
</style>
