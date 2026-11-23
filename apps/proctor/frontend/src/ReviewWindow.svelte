<script>
  import { remainingTime } from '../../../../shared/exam/timing.js';
  import Icon from '../../../../shared/theme/Icon.svelte';
  import { onMount, tick } from 'svelte';
  import * as api from '$proctor-api';
  import Tabs from './Tabs.svelte';
  import HistoryTable from './HistoryTable.svelte';
  import WorkspaceFiles from './WorkspaceFiles.svelte';
  import WorkspaceTerminals from './WorkspaceTerminals.svelte';
  import WorkspaceGrading from './WorkspaceGrading.svelte';
  import { localDateTime, localTime, parseTime } from './dates.js';
  import { gradeLine, workspaceBadge } from './workspace.js';
  import './review.css';

  let { sessionId } = $props();
  let page = $state(null);
  let busy = $state(false);
  let error = $state('');
  let cursors = $state(['']);
  let index = $state(0);
  let lastPage = $state(null);
  let follow = $state(true);
  let details = $state(false);
  let exams = $state([]);
  let workspace = $state(null);
  let gradings = $state(null);
  let problem = $state('');
  let workspaceProblem = $state('');
  let tab = $state('history');
  let saving = $state('');
  let message = $state('');
  let toastTimer;
  let historyEpoch = 0, gradingEpoch = 0;
  let readingHistory = false, readingWorkspace = false, readingGrading = false;
  let alive = true;

  const session = $derived(page?.session);
  const examTitle = $derived(exams.find(exam => exam.exam_id === session?.exam_id)?.title);
  const running = $derived(session?.status === 'active' || session?.status === 'grace');
  const final = $derived(session?.finalization_pending ? null : session?.final_version);
  const ticked = $derived(Object.keys(workspace?.done ?? {}).filter(id => workspace.done[id]));
  const latestSubmission = $derived(workspace?.submissions.at(-1));
  const latestSnapshot = $derived(workspace?.snapshots.at(-1));
  const activeRun = $derived(gradings?.gradings.find(run => run.state === 'queued' || run.state === 'running'));
  const standing = $derived(activeRun ?? gradings?.gradings.find(run => run.confirmed) ?? gradings?.gradings[0]);
  const line = $derived(gradeLine(standing));
  const gradeTone = $derived(activeRun ? 'blue' : standing?.confirmed ? 'ok' : standing?.state === 'failed' ? 'alert' : 'warn');
  const score = $derived(standing?.state === 'done' && standing.points != null && standing.max_points != null ? `${standing.points} / ${standing.max_points}` : line?.text);
  const status = $derived(session ? ({ active: 'Active', grace: 'In grace', submitted: 'Submitted', expired: 'Expired', locked: 'Locked', force_submitted: 'Force-submitted' }[session.status] ?? session.status) : '');
  const statusTone = $derived(running ? 'blue' : session?.status === 'locked' || session?.status === 'expired' ? 'alert' : 'ok');
  const tabs = $derived([
    { id: 'history', text: 'History', icon: 'clock', count: page?.items.length },
    { id: 'files', text: 'Files', icon: 'folder', count: latestSnapshot?.files ?? latestSubmission?.files ?? 0 },
    { id: 'terminals', text: 'Terminals', icon: 'terminal', count: workspace?.terminals.length },
    { id: 'grading', text: 'Grading', icon: 'fileCheck', count: activeRun ? activeRun.state : standing?.confirmed ? '✓' : standing?.state === 'done' ? 'advisory' : standing?.state === 'failed' ? 'failed' : undefined, tone: gradeTone },
  ]);
  const start = $derived(parseTime(session?.started_at)?.getTime());
  const deadline = $derived(parseTime(session?.deadline_at)?.getTime());
  const grace = $derived(parseTime(session?.grace_until)?.getTime());
  const usedUntil = $derived(parseTime(final?.submitted_at ?? (running ? page?.server_time : null))?.getTime());
  const span = $derived(Math.max(grace ?? 0, deadline ?? 0, usedUntil ?? 0) - (start ?? 0));
  const percent = (from, to, total) => Number.isFinite(to - from) && total > 0 ? 100 * Math.min(1, Math.max(0, (to - from) / total)) : 0;
  const examPct = $derived(percent(start, deadline, span));
  const gracePct = $derived(percent(deadline, grace, span));
  const elapsed = $derived(Math.max(0, Math.floor((usedUntil - start) / 1000)));
  const duration = seconds => `${Math.floor(seconds / 3600)}:${String(Math.floor(seconds / 60) % 60).padStart(2, '0')}:${String(seconds % 60).padStart(2, '0')}`;
  const initials = $derived(session?.student_name?.trim().split(/\s+/).filter(Boolean).filter((_, i, parts) => i === 0 || i === parts.length - 1).map(name => [...name][0]).join('').toUpperCase() || '—');

  async function load(cursor, at, quiet = false) {
    const mine = ++historyEpoch;
    readingHistory = true;
    if (!quiet) busy = true;
    try {
      const value = await api.Timeline(sessionId, cursor);
      if (!alive || mine !== historyEpoch) return false;
      const next = value.next_cursor;
      if (next && cursors[at + 1] && next !== cursors[at + 1]) {
        cursors = [...cursors.slice(0, at + 1), next]; lastPage = null;
      } else if (next && !cursors[at + 1]) {
        cursors = [...cursors.slice(0, at), cursor, next]; lastPage = null;
      }
      if (!next) { cursors = [...cursors.slice(0, at), cursor]; lastPage = at + 1; }
      page = value; index = at; error = '';
      return true;
    } catch (err) { if (alive && mine === historyEpoch) error = `History unavailable: ${String(err)}`; return false; }
    finally { if (alive && mine === historyEpoch) { busy = false; readingHistory = false; } }
  }
  async function loadWorkspace() {
    if (readingWorkspace) return;
    readingWorkspace = true;
    try {
      const value = await api.WorkspaceSession(sessionId);
      if (!alive) return;
      workspace = value; workspaceProblem = '';
    } catch (err) {
      // A question exam has no workspace.
      if (alive && workspace) workspaceProblem = `The workspace could not be read: ${String(err)}`;
    } finally { readingWorkspace = false; }
  }
  async function loadGradings(force = true) {
    if (!workspace || (!force && readingGrading)) return;
    const mine = ++gradingEpoch;
    readingGrading = true;
    try {
      const value = await api.WorkspaceGradings(sessionId);
      if (alive && mine === gradingEpoch) gradings = value;
    } catch { /* An older server may not provide grading. */ }
    finally { if (mine === gradingEpoch) readingGrading = false; }
  }
  function toast(text) {
    if (!alive) return;
    clearTimeout(toastTimer); message = text;
    toastTimer = setTimeout(() => { message = ''; }, 2300);
  }
  async function copy(text, label) {
    let copied = false;
    try { await navigator.clipboard.writeText(text); copied = true; }
    catch {
      // Older desktop webviews may only offer selection-based copying. Keep the field inside any open native dialog.
      const trigger = document.activeElement;
      const field = document.createElement('textarea');
      field.value = text; field.readOnly = true; field.className = 'review-clipboard';
      (trigger?.closest('dialog[open]') ?? document.body).append(field);
      try { field.select(); copied = document.execCommand('copy'); }
      catch { /* report failure rather than claim it was copied */ }
      finally { field.remove(); trigger?.focus?.({ preventScroll: true }); }
    }
    toast(copied ? `${label} copied` : `Could not copy ${label.toLowerCase()}`);
    return copied;
  }
  async function refresh() {
    const okay = await load(cursors[index], index);
    await loadWorkspace();
    await loadGradings();
    if (okay) toast('History refreshed');
  }
  async function save(what) {
    if (!latestSubmission || saving) return;
    saving = what; problem = ''; message = '';
    try {
      const path = what === 'zip' ? await api.SaveWorkspaceZip(sessionId, latestSubmission.id) : await api.SaveWorkspaceReport(sessionId);
      if (path) toast(`Saved to ${path}`);
    } catch (err) { problem = String(err); }
    finally { saving = ''; }
  }
  async function openGrading() {
    tab = 'grading';
    await tick();
    document.getElementById('review-tab-grading')?.focus();
  }
  onMount(() => {
    load('', 0);
    loadWorkspace().then(() => { if (alive) loadGradings(); });
    api.Snapshot().then(view => { if (alive) exams = view.catalog?.exams ?? []; }).catch(() => {});
    const timer = setInterval(() => {
      if (!follow) return;
      if (running || activeRun) {
        if (!readingHistory) load(cursors[index], index, true);
        loadWorkspace();
      }
      // Discover runs started from the class roster even when the student's session has ended.
      if (!activeRun) loadGradings(false);
    }, 5000);
    const grading = setInterval(() => { if (follow && activeRun) loadGradings(false); }, 1500);
    return () => { alive = false; clearInterval(timer); clearInterval(grading); clearTimeout(toastTimer); };
  });
</script>

<main class="review" aria-label="Student review">
  <section class="review-window" aria-label="Session review">
    <header class="hero">
      <span class="avatar" aria-hidden="true">{initials}</span>
      <div class="identity">
        <div class="eyebrow">Student review{examTitle ? ` · ${examTitle}` : ''}</div>
        <h1>{session?.student_name || 'Session review'} {#if session}<span class="acct">{session.account_number}</span>{/if}</h1>
      </div>
      <div class="hero-actions">
        {#if session}<span class="review-pill {statusTone}">{#if statusTone === 'ok'}<Icon name="check" size={12} />{/if}{status}</span>{/if}
        {#if standing}<span class="review-pill {gradeTone}">{score}{standing.state === 'done' ? ` · ${standing.confirmed ? 'confirmed' : 'advisory'}` : ''}</span>{/if}
        <label class="switch" title="Reload every few seconds while the session or a grading run is active"><input type="checkbox" role="switch" bind:checked={follow} /><span class="track"></span>Auto-refresh</label>
        <button class="details-button" disabled={!session} aria-expanded={details} aria-controls="review-session-details" onclick={() => { details = !details; }}>Session details <span class="review-caret"><Icon name="chevron" size={13} /></span></button>
      </div>
    </header>
    {#if session}
      <section class="review-tiles summary" aria-label="Summary">
        <div class="review-tile {statusTone}"><span class="key">Status</span><strong>{status}</strong><span class="sub">{running ? `${session.status === 'grace' ? 'Grace remaining' : 'Remaining'}: ${remainingTime(session, page.server_time)}` : workspace ? `Container ${workspaceBadge(workspace).text.toLowerCase()}` : 'Session ended'}</span></div>
        <div class="review-tile blue"><span class="key">Time used</span><strong class="mono">{Number.isFinite(elapsed) ? duration(elapsed) : '—'}</strong>
          <div class="usage-bar" title="Blue: exam time used · Yellow: grace used"><span style:width={`${percent(start, Math.min(deadline, usedUntil), span)}%`}></span><span style:width={`${percent(deadline, Math.min(grace, usedUntil), span)}%`}></span></div>
        </div>
        <div class="review-tile alert"><span class="key">Final answer</span><strong class="mono">{session.finalization_pending ? 'Pending' : localTime(final?.submitted_at, 'None yet')}</strong><span class="sub">{final ? `${final.reason.replaceAll('_', ' ')} · v#${final.final_version_id}` : session.finalization_pending ? 'No committed final answer confirmed' : ''}</span></div>
        {#if workspace}
          <button class="review-tile {gradeTone}" onclick={openGrading}><span class="key">Grade</span><strong class="mono">{score ?? 'Not graded'}</strong><span class="sub">{activeRun ? `Run #${activeRun.id} is ${activeRun.state}` : standing?.confirmed ? `Confirmed · run #${standing.id}` : line?.note ? 'Advisory until you confirm' : ''}</span></button>
          <button class="review-tile" onclick={openGrading}><span class="key">TODO ticks</span><strong class="mono">{ticked.length}{gradings ? ` / ${gradings.todos.length}` : ''}</strong><span class="sub">Self-reported, unchecked</span></button>
        {/if}
      </section>
      <section class="details" id="review-session-details" hidden={!details} aria-label="Session details">
        {#if start != null}
          <div class="timeline" aria-hidden="true">
            <span class="exam-segment" style:width={`${examPct}%`}></span><span class="grace-segment" style:left={`${examPct}%`} style:width={`${gracePct}%`}></span>
            <i class="start-dot"></i>{#if deadline != null}<i class="deadline-dot" style:left={`${examPct}%`}></i>{/if}{#if final}<i class="final-dot" style:left={`${percent(start, usedUntil, span)}%`}></i>{/if}
          </div>
          <div class="times" aria-label="Session timeline"><span><b>{localTime(session.started_at)}</b> Started</span><span><b>{localTime(session.deadline_at)}</b> Deadline</span><span><b>{localTime(session.grace_until)}</b> Grace ends</span>{#if final}<span><b>{localTime(final.submitted_at)}</b> Final answer</span>{/if}</div>
        {/if}
        <dl class="facts">
          <div><dt>Final answer</dt><dd>{#if session.finalization_pending}Finalization pending — no committed final answer confirmed.{:else if final}<span class="mono">Version #{final.final_version_id} · sequence {final.version_seq}</span> · <span class="review-pill alert">{final.reason}</span> {final.operator_reason ?? ''}{:else}None yet{/if}</dd></div>
          {#if workspace}<div><dt>Submissions</dt><dd>{latestSubmission ? `${workspace.submissions.length} · latest ${latestSubmission.final ? 'is the final one' : 'was saved by the student'} · ${localDateTime(latestSubmission.submitted_at)}` : 'None yet'}</dd></div>{/if}
          <div><dt>Computer</dt><dd class="mono">{session.presence.hostname || 'not reported'} · {session.presence.remote_addr || 'IP not reported'}</dd></div>
          {#if workspace}
            <div><dt>Container</dt><dd><span class="review-pill {workspace.state === 'failed' ? 'alert' : ''}">{workspace.state}</span> {workspace.agent_restarts ? `· restarted ${workspace.agent_restarts} time${workspace.agent_restarts === 1 ? '' : 's'}` : ''} {workspace.state === 'failed' ? workspace.failure ?? '' : ''}</dd></div>
            <div><dt>TODO ticks</dt><dd>{ticked.length ? ticked.join(', ') : 'None'} · self-reported</dd></div>
          {/if}
          <div><dt>Dates</dt><dd>{localDateTime(session.started_at)} → {localDateTime(final?.submitted_at ?? session.grace_until)}</dd></div>
          <div><dt>Session</dt><dd class="mono">{session.session_id} <button class="review-copy" aria-label="Copy session ID" onclick={() => copy(session.session_id, 'Session ID')}>Copy</button></dd></div>
        </dl>
        <div class="review-hint"><Icon name="info" size={14} /><span>Read-only server history as of <span class="mono">{localDateTime(page.server_time)}</span>. Versions are numbered in commit order. Signals are observations, not proof of misconduct.</span></div>
      </section>
    {/if}
    {#if problem || workspaceProblem}<p class="window-problem" role="alert">{problem || workspaceProblem}</p>{/if}
    {#snippet history()}
      <HistoryTable items={page?.items ?? []} finalVersionId={final?.final_version_id} showWorkspace={!!workspace}>
        {#snippet actions()}
          <div class="pager">
            <button class="review-button square" aria-label="Previous page" disabled={busy || index === 0} onclick={() => load(cursors[index - 1], index - 1)}><span class="prev"><Icon name="chevron" size={14} /></span></button>
            <span>Page <b>{index + 1}</b>{lastPage !== null ? ` of ${lastPage}` : ''}</span>
            <button class="review-button square" aria-label="Next page" disabled={busy || !page?.next_cursor} onclick={() => load(page.next_cursor, index + 1)}><span class="next"><Icon name="chevron" size={14} /></span></button>
          </div>
          <button class="review-button square" aria-label="Refresh" title="Refresh" disabled={busy} onclick={refresh}><Icon name="refresh" size={14} /></button>
        {/snippet}
        {#snippet status()}
          {#if error}<p role="alert" class="window-problem">{error}</p>{/if}
          {#if busy && !page}<p role="status" class="review-hint">Loading history…</p>{/if}
        {/snippet}
      </HistoryTable>
    {/snippet}
    {#if workspace}
      <Tabs id="review" label="Review sections" {tabs} bind:selected={tab}>
        {#snippet panel(id)}
          {#if id === 'history'}{@render history()}
          {:else if id === 'files'}<WorkspaceFiles {workspace} {copy} {saving} onDownload={() => save('zip')} />
          {:else if id === 'grading'}<WorkspaceGrading {sessionId} {workspace} {gradings} {copy} {toast} {saving} onDownload={() => save('report')} onChanged={loadGradings} />
          {:else}<WorkspaceTerminals {workspace} {copy} />{/if}
        {/snippet}
      </Tabs>
    {:else}<div class="review-panel">{@render history()}</div>{/if}
  </section>
  <div class="review-toast" class:show={message} role="status">{message}</div>
</main>

<style>
  .review-window { max-width: 1180px; margin: auto; background: var(--bg-raised); border: 1px solid var(--border-subtle); border-radius: 14px; overflow: hidden; box-shadow: var(--shadow-raised); }
  .hero { display: flex; align-items: center; flex-wrap: wrap; gap: .75rem; padding: .75rem 1rem; background: var(--header-strong); color: var(--on-dark); }
  .avatar { width: 38px; height: 38px; flex: none; border-radius: 10px; display: grid; place-items: center; font-weight: 750; background: var(--side-active); border: 1px solid var(--cyan-chip-border); color: var(--accent-cyan-soft); }
  .identity { min-width: 0; overflow-wrap: anywhere; }
  .eyebrow { font-size: .66rem; font-weight: 700; letter-spacing: .12em; text-transform: uppercase; color: var(--accent-cyan-soft); }
  h1 { margin: 0; font-size: 1.1rem; color: inherit; line-height: 1.3; }
  .acct { font: 600 .82rem var(--font-mono); color: var(--side-text); margin-inline-start: .35rem; }
  .hero-actions { margin-left: auto; display: flex; align-items: center; gap: .5rem; flex-wrap: wrap; }
  .details-button { display: inline-flex; align-items: center; gap: .35rem; min-height: 30px; margin: 0; padding: 0 .7rem; border-radius: 8px; background: var(--chip-on-dark); border-color: var(--cyan-chip-border); color: var(--side-text); font-size: .8rem; box-shadow: none; }
  .details-button:hover:not(:disabled) { background: var(--side-active); color: var(--on-dark); }
  .switch { position: relative; display: inline-flex; align-items: center; gap: .4rem; font-size: .78rem; color: var(--side-text); cursor: pointer; margin: 0; }
  .switch input { position: absolute; z-index: 1; inset: 0; width: 100%; height: 100%; margin: 0; opacity: 0; cursor: pointer; }
  .track { width: 30px; height: 17px; border-radius: 999px; background: var(--chip-on-dark); border: 1px solid var(--cyan-chip-border); position: relative; }
  .track::after { content: ''; position: absolute; top: 1px; left: 1px; width: 13px; height: 13px; border-radius: 50%; background: var(--on-dark); }
  .switch input:checked + .track { background: var(--accent-cyan); }
  .switch input:checked + .track::after { transform: translateX(13px); }
  .switch input:focus-visible + .track { outline: 2px solid var(--accent-cyan-soft); outline-offset: 2px; }
  .summary { border-bottom: 1px solid var(--border-subtle); }
  .usage-bar { background: var(--bg-secondary); border-radius: 999px; height: 4px; margin-top: .25rem; overflow: hidden; display: flex; }
  .usage-bar span:first-child { background: var(--action-primary); } .usage-bar span:last-child { background: var(--warn); }
  .details { padding: .75rem 1rem; background: var(--bg-primary); border-bottom: 1px solid var(--border-subtle); }
  .timeline { position: relative; height: 10px; border-radius: 999px; margin: .45rem .4rem .55rem; background: var(--bg-secondary); }
  .timeline span { position: absolute; inset-block: 0; border-radius: inherit; }
  .exam-segment { background: var(--selection-fill); } .grace-segment { background: var(--warn-fill); }
  .timeline i { position: absolute; top: 50%; width: 11px; height: 11px; border-radius: 50%; transform: translate(-50%, -50%); box-shadow: 0 0 0 3px var(--bg-primary); }
  .start-dot { background: var(--action-primary); } .deadline-dot { background: var(--warn); } .final-dot { background: var(--alert); }
  .times { display: flex; justify-content: space-between; flex-wrap: wrap; gap: .35rem .8rem; font-size: .74rem; color: var(--text-muted); }
  .times b { font-family: var(--font-mono); color: var(--text-main); }
  .facts { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: .35rem 1.5rem; margin: .75rem 0; }
  .facts div { display: grid; grid-template-columns: 100px minmax(0, 1fr); gap: .4rem; align-items: baseline; }
  .facts dt { text-transform: uppercase; font-size: .72rem; font-weight: 700; letter-spacing: .04em; color: var(--text-muted); }
  .facts dd { margin: 0; overflow-wrap: anywhere; }
  .pager { display: inline-flex; align-items: center; gap: .45rem; color: var(--text-muted); white-space: nowrap; }
  .pager b { color: var(--text-main); font-family: var(--font-mono); }
  .prev, .next { display: flex; } .prev { transform: rotate(180deg); }
  .window-problem { padding-inline: 1rem; color: var(--alert-text); overflow-wrap: anywhere; }
  .review-toast { position: fixed; z-index: 2; inset: auto 1rem 1rem; width: fit-content; max-width: calc(100% - 2rem); margin: auto; padding: .55rem .9rem; border-radius: 10px; background: var(--term-head); color: var(--on-dark); box-shadow: var(--shadow-raised); overflow-wrap: anywhere; opacity: 0; transform: translateY(.5rem); pointer-events: none; transition: opacity .18s, transform .18s; }
  .review-toast.show { opacity: 1; transform: none; }
  @media (max-width: 680px) { .hero-actions { margin-left: 0; } .facts { grid-template-columns: minmax(0, 1fr); } }
  @media (prefers-reduced-motion: reduce) { .review-toast { transition: none; } }
</style>
