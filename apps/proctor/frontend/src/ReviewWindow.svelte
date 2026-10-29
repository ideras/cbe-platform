<script>
  import { remainingTime } from '../../../../shared/exam/timing.js';
  import { onMount } from 'svelte';
  import * as api from '$proctor-api';
  import Panel from './Panel.svelte';
  import HistoryTable from './HistoryTable.svelte';
  import { localDateTime } from './dates.js';
  // One student's session in a window of its own: the summary and the read-only history, page by page. While the session
  // is still running the shown page refreshes by itself.
  let { sessionId } = $props();
  let page = $state(null);
  let busy = $state(false);
  let error = $state('');
  let cursors = $state(['']);
  let index = $state(0);
  let follow = $state(true);
  let epoch = 0;
  let alive = true;

  const session = $derived(page?.session);
  const running = $derived(session && (session.status === 'active' || session.status === 'grace'));

  async function load(cursor, at, quiet = false) {
    const mine = ++epoch;
    if (!quiet) busy = true;
    try {
      const value = await api.Timeline(sessionId, cursor);
      if (!alive || mine !== epoch) return;
      page = value; index = at; cursors = [...cursors.slice(0, at), cursor]; error = '';
    } catch (err) { if (alive && mine === epoch) error = `History unavailable: ${String(err)}`; }
    finally { if (alive && mine === epoch) busy = false; }
  }
  onMount(() => {
    load('', 0);
    const timer = setInterval(() => { if (follow && running && !busy) load(cursors[index], index, true); }, 5000);
    return () => { alive = false; clearInterval(timer); };
  });
</script>

<div class="window">
  <Panel label="Session review" title={session ? `${session.student_name} (${session.account_number})` : 'Session review'}>
    {#snippet actions()}<label class="follow"><input type="checkbox" bind:checked={follow} />Refresh while the session runs</label>{/snippet}
    {#if session}
      <dl class="summary">
        <dt>Status</dt><dd><strong>{session.status}</strong>{#if running} · {session.status === 'grace' ? 'Grace remaining' : 'Remaining'}: {remainingTime(session, page.server_time)}{/if}</dd>
        <dt>Started</dt><dd>{localDateTime(session.started_at)}</dd>
        <dt>Deadline</dt><dd>{localDateTime(session.deadline_at)} · grace until {localDateTime(session.grace_until)}</dd>
        <dt>Final answer</dt><dd>
          {#if session.finalization_pending}<span class="warning">Finalization pending — no committed final answer confirmed.</span>
          {:else if session.final_version}Version #{session.final_version.final_version_id} · sequence {session.final_version.version_seq} · {session.final_version.reason} · {localDateTime(session.final_version.submitted_at)} {session.final_version.operator_reason ?? ''}
          {:else}None yet{/if}</dd>
        <dt>Computer</dt><dd>{session.presence.hostname || 'not reported'} · {session.presence.remote_addr || 'IP not reported'}</dd>
      </dl>
      <p class="muted">Session {session.session_id}. Read-only server history as of {localDateTime(page.server_time)}; version sequence is commit order. Signals are observations, not proof of misconduct.</p>
    {/if}
    {#if error}<p role="alert">{error}</p>{/if}
    <div class="actions">
      <button disabled={busy} onclick={() => load(cursors[index], index)}>Refresh</button>
      <button disabled={busy || index === 0} onclick={() => load(cursors[index - 1], index - 1)}>Previous page</button>
      <button disabled={busy || !page?.next_cursor} onclick={() => load(page.next_cursor, index + 1)}>Next page</button>
      <span class="muted">Page {index + 1}{#if page && !page.next_cursor} · end of history{/if}</span>
    </div>
    {#if busy && !page}<p>Loading history…</p>{/if}
    {#if page}<HistoryTable items={page.items} finalVersionId={session.final_version?.final_version_id} />{/if}
  </Panel>
</div>

<style>
  .window { padding: var(--space-4); }
  .summary { display: grid; grid-template-columns: max-content minmax(0, 1fr); gap: .2rem var(--space-3); margin: 0 0 var(--space-2); }
  .summary dt { color: var(--color-text-muted); font-size: var(--text-sm); }
  .summary dd { margin: 0; }
  .warning { color: var(--color-warning); font-weight: 600; }
  .actions { display: flex; flex-wrap: wrap; align-items: center; gap: var(--space-2); margin: var(--space-3) 0; }
  .actions button { margin: 0; }
  .follow { display: inline-flex; align-items: center; gap: .35rem; font-size: var(--text-sm); }
</style>
