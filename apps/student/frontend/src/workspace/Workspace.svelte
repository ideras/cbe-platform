<script>
  import { onMount, tick, untrack } from 'svelte';
  import Icon from '../../../../../shared/theme/Icon.svelte';
  import ConfirmDialog from '../ConfirmDialog.svelte';
  import EditorPane from './EditorPane.svelte';
  import FileTree from './FileTree.svelte';
  import NameDialog from './NameDialog.svelte';
  import TerminalPane from './TerminalPane.svelte';
  import TodoPanel from './TodoPanel.svelte';
  import { WorkspaceModel } from './model.js';
  import { PasteGuard, guardWindow } from './paste-guard.js';
  import { makeTransport, decodeOutput } from './transport.js';
  import { basename, dirname } from './paths.js';

  // The student's workspace: instructions and TODO list, the files, the editor, the terminals, and what the connection is doing. The
  // application's Go side holds the connection; this page shows what the model (model.js) makes of it and tells it what the student
  // does. `workspace` is the application's view of it, `api` the application's calls, `notify` posts a toast, and `onStatus` hears the
  // connection and the saves, which the application tells in its toolbar (the room below is the editor's).
  let { api, workspace, notify, onFinish, finishing = false, onStatus = () => {} } = $props();
  const model = new WorkspaceModel({
    transport: makeTransport(api),
    notify: note => notify(note),
    onChange: () => schedule(),
  });
  let snap = $state(model.snapshot());
  let queued = false;
  // Many changes in a burst (a build writing files) are drawn once.
  function schedule() {
    if (queued) return;
    queued = true;
    requestAnimationFrame(() => { queued = false; snap = model.snapshot(); });
  }
  let editor;
  let tree;
  let panes;
  // The workspace fills the window below whatever the page puts above it (the toolbar, the notices), so it measures where it starts.
  function measure() {
    if (!panes) return;
    panes.style.setProperty('--ide-top', `${panes.getBoundingClientRect().top + window.scrollY}px`);
  }
  let naming = $state(null); // { kind: 'file' | 'folder' | 'rename', folder, path }
  let deleting = $state(null); // the path to confirm
  let submitting = $state(false);
  let confirmingFinish = $state(false);
  let finishWorking = $state(false);
  let blockedAt = 0;

  // What the application says about the connection, on every snapshot of it.
  $effect(() => { const view = workspace; untrack(() => model.setView(view)); });
  // What the toolbar tells, handed over when one of its parts changes (the values are compared one by one, so a burst of file changes is not).
  const connection = $derived(snap.connection);
  const unsaved = $derived(snap.unsaved);
  const lastSave = $derived(snap.lastSave);
  const lastSnapshotAt = $derived(workspace.status?.last_snapshot_at ?? null);
  const restarts = $derived(workspace.status?.agent_restarts ?? 0);
  $effect(() => { const status = { connection, unsaved, lastSave, lastSnapshotAt, restarts }; untrack(() => onStatus(status)); });

  // The terminals take room only while there is one to show. `terminalWanted` is the student's wish (what Run, Check and the Terminal
  // button say, and Hide takes back); the panel is there when it is wanted and a terminal exists. Hiding keeps the terminals running.
  let terminalWanted = $state(false);
  const terminalVisible = $derived(terminalWanted && snap.terminals.length > 0);
  let focusWhenShown = false;
  $effect(() => { if (terminalVisible && focusWhenShown) { focusWhenShown = false; tick().then(() => focusPane('terminal')); } });
  // A panel that goes away with the focus in it leaves the focus nowhere: it goes to the editor.
  let wasVisible = false;
  $effect(() => {
    const visible = terminalVisible;
    if (wasVisible && !visible) queueMicrotask(() => { if (!document.activeElement || document.activeElement === document.body) editor?.focus(); });
    wasVisible = visible;
  });
  function hideTerminal() { terminalWanted = false; editor?.focus(); }
  async function toggleTerminal() {
    if (terminalVisible) { hideTerminal(); return; }
    terminalWanted = true; focusWhenShown = true;
    if (snap.terminals.length === 0) await model.openTerminal();
  }
  function run(which) { terminalWanted = true; model.run(which); }

  onMount(() => {
    model.setTodoState(workspace.done ?? {});
    const stopOutput = api.OnWorkspaceOutput(event => model.output(event.term, decodeOutput(event.data)));
    const stopMessage = api.OnWorkspaceMessage(text => { try { model.handleMessage(JSON.parse(text)); } catch { /* not a message of ours */ } });
    const guard = new PasteGuard();
    const stopGuard = guardWindow(window, guard, (kind, detail) => {
      model.reportPaste(detail);
      // One notice for a burst of attempts.
      if (Date.now() - blockedAt < 3000) return;
      blockedAt = Date.now();
      notify({ kind: 'warning', key: 'paste-blocked', text: kind === 'drop' ? 'Dropping files or text from outside the exam is not allowed.' : 'Pasting text from outside the exam is not allowed. You can copy and paste between your files and terminals.' });
    });
    window.addEventListener('keydown', keys, true);
    window.addEventListener('beforeunload', warn);
    window.addEventListener('resize', measure);
    measure();
    const measuring = setInterval(measure, 500);
    return () => { clearInterval(measuring); window.removeEventListener('resize', measure); onStatus(null); stopOutput?.(); stopMessage?.(); stopGuard(); window.removeEventListener('keydown', keys, true); window.removeEventListener('beforeunload', warn); model.close(); };
  });
  const warn = event => { if (model.snapshot().unsaved) { event.preventDefault(); event.returnValue = ''; } };

  const order = ['todo', 'tree', 'editor', 'terminal'];
  function focusPane(name) {
    const node = panes?.querySelector(`[data-pane="${name}"]`);
    if (name === 'editor') { editor?.focus(); return; }
    (node?.querySelector('[tabindex="0"], input:not(:disabled), button:not(:disabled)') ?? node)?.focus?.();
    if (name === 'terminal') node?.querySelector('textarea')?.focus();
  }
  function keys(event) {
    if ((event.ctrlKey || event.metaKey) && !event.shiftKey && !event.altKey && event.key.toLowerCase() === 's') {
      // Ctrl+S saves. In a terminal it is flow control, which a shell program may want: the terminal keeps it.
      if (event.target.closest?.('.xterm')) return;
      event.preventDefault();
      model.saveAll().then(ok => { if (!ok) notify({ kind: 'warning', key: 'save-all', text: 'Some changes could not be saved yet.' }); });
    } else if (event.key === 'F6') {
      event.preventDefault();
      // A hidden terminal panel is not a stop.
      const stops = order.filter(name => name !== 'terminal' || terminalVisible);
      const here = stops.findIndex(name => event.target.closest?.(`[data-pane="${name}"]`));
      focusPane(stops[(here + (event.shiftKey ? stops.length - 1 : 1)) % stops.length]);
    }
  }

  async function open(path) { await model.openFile(path); editor?.focus(); }
  async function create(name) {
    const { kind, folder, path } = naming;
    const problem = kind === 'rename' ? await model.rename(path, name) : kind === 'folder' ? await model.createFolder(folder, name) : await model.createFile(folder, name);
    if (!problem) naming = null;
    return problem;
  }
  async function confirmDelete() {
    const path = deleting;
    const problem = await model.remove(path);
    deleting = null;
    if (problem) notify({ kind: 'error', key: 'delete', text: `${basename(path)} could not be deleted: ${problem}` });
  }
  async function submit() {
    if (submitting) return;
    submitting = true;
    try {
      if (await model.submit()) notify({ kind: 'info', key: 'submitted', text: 'Your work was saved. You can keep working: what is in your workspace is submitted automatically when you finish the exam or your time ends.' });
    } finally { submitting = false; }
  }
  // Finishing the exam is final, so it is asked about first. Once it is confirmed the files are saved and a copy is submitted (if either
  // cannot be done the exam is not finished, and the student is told why), and then the application ends the session.
  async function finish() {
    if (finishWorking) return;
    finishWorking = true;
    try {
      if (!(await model.saveAll())) {
        confirmingFinish = false;
        notify({ kind: 'error', key: 'finish', text: 'Some changes could not be saved, so the exam was not finished. Resolve the file marked as changed or unsaved, then finish again.' });
        return;
      }
      // The copy is made by the server when the session ends anyway; this one is the student's own, made now, with the files they see.
      await model.submit();
      confirmingFinish = false;
      await onFinish();
    } finally { finishWorking = false; }
  }
  const clock = value => new Date(value).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' });
  const banner = $derived.by(() => {
    if (finishing) return { text: 'Finishing your exam… waiting for the server to confirm. Nothing can be changed now.', kind: 'pending' };
    switch (snap.connection) {
      case 'connecting': return { text: 'Connecting to your workspace…', kind: 'pending' };
      case 'reconnecting': return { text: 'Reconnecting… The editors and terminals are read-only until the connection is back. What you typed is kept and will be saved then.', kind: 'warning' };
      case 'locked': return { text: snap.note || 'Your exam is locked. Call your instructor.', kind: 'danger' };
      case 'replaced': return { text: snap.note, kind: 'warning', retry: true };
      case 'ended': case 'closed': return { text: snap.note || 'The workspace is closed.', kind: 'danger' };
      case 'idle': return { text: 'The workspace is not connected.', kind: 'warning' };
      default:
        // Connected, and the server says what the container is doing.
        if (workspace.status?.state === 'preparing' || workspace.status?.state === 'restarting') return { text: 'Your workspace is starting… This takes a few seconds.', kind: 'pending' };
        if (workspace.status?.state === 'failed') return { text: 'Your workspace could not be prepared. Call your instructor.', kind: 'danger' };
        return null;
    }
  });
  const disabled = $derived(snap.readOnly || finishing);
  const ticked = $derived(workspace.todos.filter(item => snap.todo[item.id]).length);
  const confirmName = $derived(deleting ? basename(deleting) : '');
  const deletingFolder = $derived(deleting ? snap.folders.get(dirname(deleting))?.entries.find(entry => entry.name === basename(deleting))?.kind === 'dir' : false);
</script>
<div class="ide" bind:this={panes}>
  {#if banner}
    <p class="banner {banner.kind}" role="status">
      <Icon name={banner.kind === 'pending' ? 'refresh' : 'warning'} size={18} /><span>{banner.text}</span>
      {#if banner.retry}<button type="button" onclick={() => model.transport.reconnect().catch(() => {})}>Use this window</button>{/if}
    </p>
  {/if}
  <div class="panes">
    <aside class="side">
      <div data-pane="todo" class="todo-box"><TodoPanel instructions={workspace.instructions} todos={workspace.todos} done={snap.todo} disabled={disabled} onToggle={(id, done) => model.setTodo(id, done)} /></div>
      <div data-pane="tree" class="tree-box" bind:this={tree}>
        <FileTree folders={snap.folders} expanded={snap.expanded} activePath={snap.activePath} readOnly={disabled}
          onToggle={path => model.toggleFolder(path)} onOpen={open}
          onNew={(kind, folder) => { naming = { kind, folder }; }} onRename={path => { naming = { kind: 'rename', path, folder: dirname(path) }; }} onDelete={path => { deleting = path; }} />
      </div>
    </aside>
    <div class="main" class:with-terminal={terminalVisible}>
      <div class="actions" role="toolbar" aria-label="Run, check and save">
        {#if snap.canRun}<button type="button" onclick={() => run('run')} disabled={disabled}><Icon name="arrowRight" size={16} />Run</button>{/if}
        {#if snap.canCheck}<button type="button" onclick={() => run('check')} disabled={disabled}><Icon name="check" size={16} />Check</button>{/if}
        <!-- The terminals take room only when wanted: this shows the panel (opening a terminal if there is none) and hides it again. -->
        {#if snap.terminalAllowed || snap.terminals.length > 0}
          <button type="button" aria-pressed={terminalVisible} aria-controls="terminal-panel" onclick={toggleTerminal} disabled={snap.terminals.length === 0 && disabled}
            title={terminalVisible ? 'Hide the terminals (they keep running)' : snap.terminals.length ? 'Show the terminals' : 'Open a terminal'}>
            <Icon name="chevron" size={14} /><span>Terminal{snap.terminals.length && !terminalVisible ? ` (${snap.terminals.length})` : ''}</span>
          </button>
        {/if}
        <span class="spacer"></span>
        {#if snap.lastSubmit}<span class="submitted" role="status">Last Save {clock(snap.lastSubmit.at)}</span>{/if}
        <button type="button" class="primary" onclick={submit} disabled={disabled || submitting}><Icon name="fileCheck" size={16} />Save</button>
        <button type="button" onclick={() => { confirmingFinish = true; }} disabled={disabled}><Icon name="done" size={16} />Finish exam…</button>
      </div>
      <div class="editor-box" data-pane="editor">
        <EditorPane bind:this={editor} {model} tabs={snap.tabs} activePath={snap.activePath} readOnly={disabled} onClose={path => model.closeTab(path)} />
      </div>
      <div class="terminal-box" id="terminal-panel" data-pane="terminal" hidden={!terminalVisible}>
        <TerminalPane {model} terminals={snap.terminals} activeTerm={snap.activeTerm} terminalAllowed={snap.terminalAllowed} readOnly={disabled} onHide={hideTerminal} />
      </div>
    </div>
  </div>
</div>
{#if naming}
  <NameDialog title={naming.kind === 'rename' ? `Rename ${basename(naming.path)}` : naming.kind === 'folder' ? 'New folder' : 'New file'} label={naming.kind === 'rename' ? 'New name' : 'Name'}
    initial={naming.kind === 'rename' ? basename(naming.path) : ''} where={naming.kind === 'rename' ? '' : naming.folder || 'the project folder'} confirmLabel={naming.kind === 'rename' ? 'Rename' : 'Create'}
    onSubmit={create} onCancel={() => { naming = null; }} />
{/if}
{#if confirmingFinish}
  <ConfirmDialog tone="alarm" eyebrow="Final step" icon="fileCheck" danger title="Finish your exam now?" subtitle={workspace.title} confirmLabel="Finish my exam" cancelLabel="Keep working"
    disabled={disabled || finishWorking} onConfirm={finish} onCancel={() => { if (!finishWorking) confirmingFinish = false; }}>
    <div class="fact ok">
      <span class="mk"><Icon name="check" size={13} /></span>
      <div>
        <p class="h">Your files are saved, and a copy is submitted, when you finish.</p>
        <p>{snap.unsaved ? 'You have changes that are not saved yet: they are saved first.' : 'Everything you have typed is saved.'} {snap.lastSubmit ? `Your last Save was at ${clock(snap.lastSubmit.at)}.` : 'You have not pressed Save yet.'}</p>
      </div>
    </div>
    <div class="fact {ticked < workspace.todos.length ? 'warn' : 'ok'}">
      <span class="mk"><Icon name={ticked < workspace.todos.length ? 'warning' : 'check'} size={13} /></span>
      <div>
        <p class="h">You have ticked {ticked} of {workspace.todos.length} items of the TODO list.</p>
        <p>Your instructor sees your ticks as what you say you did; they are not checked.</p>
      </div>
    </div>
    <p class="note alert"><Icon name="warning" size={16} /><span><strong>This is final:</strong> you will not be able to change your files, run anything or open the workspace again.</span></p>
  </ConfirmDialog>
{/if}
{#if deleting}
  <ConfirmDialog icon="deleteExam" danger title="Delete {confirmName}?" subtitle={deleting} confirmLabel="Delete" cancelLabel="Keep it" disabled={disabled} onConfirm={confirmDelete} onCancel={() => { deleting = null; }}>
    <p class="note alert"><Icon name="warning" size={16} /><span>{deletingFolder ? 'The folder and everything in it will be deleted.' : 'The file will be deleted.'} This cannot be undone.</span></p>
  </ConfirmDialog>
{/if}
<style>
  /* The workspace fills the window below the toolbar. Its rows are the banner (when there is one) and the panes, which take the rest. */
  .ide { display: flex; flex-direction: column; gap: var(--space-2); height: calc(100vh - var(--ide-top, 8rem) - 1.5rem); min-height: 32rem; }
  .banner { display: flex; align-items: center; gap: var(--space-2); margin: 0; padding: .5rem .75rem; border-radius: var(--radius-ctl); border: 1px solid var(--border-subtle); background: var(--warn-fill); color: var(--warn-text); font-size: var(--text-sm); }
  .banner.pending { background: var(--info-fill, var(--bg-raised)); color: var(--text-main); }
  .banner.danger { background: var(--alert-fill); color: var(--alert-text); }
  .banner button { margin: 0 0 0 auto; }
  .panes { flex: 1 1 0; min-height: 0; display: grid; grid-template-columns: minmax(15rem, 21rem) minmax(0, 1fr); gap: var(--space-2); }
  .side { display: grid; grid-template-rows: minmax(0, 2fr) minmax(0, 3fr); gap: var(--space-2); min-height: 0; min-width: 0; }
  .todo-box { min-height: 0; overflow: auto; padding-right: .15rem; }
  .tree-box { min-height: 0; display: grid; }
  /* The editor has all the room until a terminal is wanted, and then about two thirds of it. */
  .main { display: grid; grid-template-rows: auto minmax(0, 1fr); gap: var(--space-2); min-height: 0; min-width: 0; }
  .main.with-terminal { grid-template-rows: auto minmax(0, 13fr) minmax(8rem, 7fr); }
  .editor-box, .terminal-box { min-height: 0; display: grid; }
  .terminal-box[hidden] { display: none; }
  .actions { display: flex; flex-wrap: wrap; align-items: center; gap: var(--space-2); }
  .actions button { display: inline-flex; align-items: center; gap: .35rem; margin: 0; }
  .actions button[aria-pressed="true"] { background: var(--selection-fill); }
  .actions button[aria-pressed="true"] :global(svg) { transform: rotate(90deg); }
  .spacer { flex: 1; }
  .submitted { color: var(--ok-text); font-size: var(--text-sm); }
  .note { display: flex; gap: var(--space-2); }
  @media (max-width: 900px) {
    .ide { height: auto; }
    .panes { grid-template-columns: minmax(0, 1fr); grid-template-rows: auto minmax(36rem, 1fr); }
    .side { grid-template-rows: auto minmax(12rem, 16rem); }
    .todo-box { max-height: 16rem; }
  }
</style>
