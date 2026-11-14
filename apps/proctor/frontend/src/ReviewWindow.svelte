<script>
  import { remainingTime } from '../../../../shared/exam/timing.js';
  import { onMount } from 'svelte';
  import * as api from '$proctor-api';
  import Panel from './Panel.svelte';
  import Tabs from './Tabs.svelte';
  import HistoryTable from './HistoryTable.svelte';
  import WorkspaceFiles from './WorkspaceFiles.svelte';
  import WorkspaceTerminals from './WorkspaceTerminals.svelte';
  import { localDateTime } from './dates.js';
  import { todoNote } from './workspace.js';
  // One student's session in a window of its own: the summary and the read-only history, page by page. While the session
  // is still running the shown page refreshes by itself. On a workspace exam the student's files and terminals are tabs of the same
  // window, and what the server knows of the workspace (its state, the ticks, the submissions, the downloads) is in the summary.
  let { sessionId } = $props();
  let page = $state(null);
  let busy = $state(false);
  let error = $state('');
  let cursors = $state(['']);
  let index = $state(0);
  let follow = $state(true);
  let epoch = 0;
  let alive = true;
  let workspace = $state(null); // null for a session that is not a workspace's
  let problem = $state('');
  let tab = $state('history');
  let saving = $state('');
  let saved = $state('');

  const session = $derived(page?.session);
  const running = $derived(session && (session.status === 'active' || session.status === 'grace'));
  const ticked = $derived(Object.entries(workspace?.done ?? {}).filter(([, done]) => done).map(([id]) => id).sort());
  const latestSubmission = $derived(workspace?.submissions.at(-1) ?? null);
  const tabs = $derived(workspace ? [{ id: 'history', text: 'History', icon: 'clock' }, { id: 'files', text: 'Files', icon: 'folder' }, { id: 'terminals', text: 'Terminals', icon: 'terminal', count: workspace.terminals.length }] : []);

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
  async function loadWorkspace() {
    try {
      const value = await api.WorkspaceSession(sessionId);
      if (!alive) return;
      workspace = value; problem = '';
    } catch (err) {
      // A session that is not a workspace's has no workspace to show, which is not a problem to report.
      if (alive && workspace) problem = `The workspace could not be read: ${String(err)}`;
    }
  }
  async function save(what) {
    saving = what; saved = ''; problem = '';
    try {
      const path = what === 'zip' ? await api.SaveWorkspaceZip(sessionId, latestSubmission.id) : await api.SaveWorkspaceReport(sessionId);
      if (path) saved = `Saved to ${path}`;
    } catch (err) { problem = String(err); }
    finally { saving = ''; }
  }
  onMount(() => {
    load('', 0);
    loadWorkspace();
    const timer = setInterval(() => { if (follow && running && !busy) load(cursors[index], index, true); }, 5000);
    const watching = setInterval(loadWorkspace, 5000);
    return () => { alive = false; clearInterval(timer); clearInterval(watching); };
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
        {#if workspace}
          <dt>Container</dt><dd><strong>{workspace.state}</strong>{#if workspace.agent_restarts}{' · restarted '}{workspace.agent_restarts} time{workspace.agent_restarts === 1 ? '' : 's'}{/if}</dd>
          <dt>TODO ({todoNote})</dt><dd>{#if ticked.length}{ticked.join(', ')} ticked by the student{:else}nothing ticked{/if}. Nothing checked these ticks.</dd>
          <dt>Submissions</dt><dd>{#if latestSubmission}{workspace.submissions.length} · latest {latestSubmission.final ? 'is the final one' : 'was made by the student pressing Save'}, {localDateTime(latestSubmission.submitted_at)}{:else}none yet{/if}</dd>
        {/if}
      </dl>
      <p class="muted">Session {session.session_id}. Read-only server history as of {localDateTime(page.server_time)}; version sequence is commit order. Signals are observations, not proof of misconduct.</p>
    {/if}
    {#if workspace}
      <div class="actions">
        <button type="button" disabled={!latestSubmission || !!saving} onclick={() => save('zip')}>Download submitted files (zip)…</button>
        <button type="button" disabled={!latestSubmission || !!saving} onclick={() => save('report')}>Download report (Markdown)…</button>
        {#if saved}<span class="saved" role="status">{saved}</span>{/if}
      </div>
      {#if problem}<p role="alert" class="problem">{problem}</p>{/if}
    {/if}
    {#if error}<p role="alert">{error}</p>{/if}
    {#snippet history()}
      <div class="actions">
        <button disabled={busy} onclick={() => load(cursors[index], index)}>Refresh</button>
        <button disabled={busy || index === 0} onclick={() => load(cursors[index - 1], index - 1)}>Previous page</button>
        <button disabled={busy || !page?.next_cursor} onclick={() => load(page.next_cursor, index + 1)}>Next page</button>
        <span class="muted">Page {index + 1}{#if page && !page.next_cursor} · end of history{/if}</span>
      </div>
      {#if busy && !page}<p>Loading history…</p>{/if}
      {#if page}<HistoryTable items={page.items} finalVersionId={session.final_version?.final_version_id} />{/if}
    {/snippet}
    <!-- On a workspace exam the history, the student's files and their terminals are tabs; any other session has only the history. -->
    {#if workspace}
      <Tabs id="review" label="Parts of the session review" {tabs} bind:selected={tab}>
        {#snippet panel(id)}
          {#if id === 'history'}{@render history()}
          {:else if id === 'files'}<WorkspaceFiles {workspace} />
          {:else}<WorkspaceTerminals {workspace} />{/if}
        {/snippet}
      </Tabs>
    {:else}{@render history()}{/if}
  </Panel>
</div>

<style>
  .window { display: grid; gap: var(--space-4); padding: var(--space-4); }
  .summary { display: grid; grid-template-columns: max-content minmax(0, 1fr); gap: .2rem var(--space-3); margin: 0 0 var(--space-2); }
  .summary dt { color: var(--text-muted); font-size: var(--text-sm); }
  .summary dd { margin: 0; }
  .warning { color: var(--warn-text); font-weight: 600; }
  .saved { color: var(--ok-text); font-size: var(--text-sm); overflow-wrap: anywhere; }
  .problem { color: var(--alert-text); }
  .actions { display: flex; flex-wrap: wrap; align-items: center; gap: var(--space-2); margin: var(--space-3) 0; }
  .actions button { margin: 0; }
  .follow { display: inline-flex; align-items: center; gap: .35rem; font-size: var(--text-sm); }
</style>
