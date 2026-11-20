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
  import { EditorStore } from './editor-store.js';
  import { PasteGuard, guardWindow } from './paste-guard.js';
  import { makeTransport, decodeOutput } from './transport.js';
  import { basename, dirname } from './paths.js';
  import { MIN_PANE, MAX_PANE, DEFAULT_PANE, PANE_STEP } from './sizes.js';
  import { MIN_SHARE, MAX_SHARE, DEFAULT_SHARE, SHARE_STEP, MIN_SIDE, MAX_SIDE, SIDE_STEP, MIN_INSTRUCTIONS, MAX_INSTRUCTIONS, DEFAULT_INSTRUCTIONS,
    INSTRUCTIONS_STEP } from './sizes.js';

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
  // What the editor's panes share about the open files (editor-store.js), and each pane's component, by the pane's id.
  const store = new EditorStore(model);
  const editors = {};
  // The keyboard goes to the editor of a pane (the one last used, by default). A pane just made is drawn at the next snapshot: it is waited for.
  async function focusEditor(index = model.activePane) {
    const pane = () => editors[model.panes[index]?.id];
    if (!pane()) { await new Promise(resolve => requestAnimationFrame(resolve)); await tick(); }
    pane()?.focus();
  }
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
    if (wasVisible && !visible) queueMicrotask(() => { if (!document.activeElement || document.activeElement === document.body) focusEditor(); });
    wasVisible = visible;
  });
  // The splitter between the editor and the terminals: dragged with the pointer, or moved with the arrow keys when it has the focus. The share it
  // sets is of the room from the top of the editor to the bottom of the terminals, kept in the model so that hiding the terminals and showing them
  // again leaves it where it was.
  let editorBox; // the editors' row: one pane, or two side by side
  let terminalBox;
  let dragging = $state(false);
  function startDrag(event) {
    if (event.button !== 0) return;
    event.preventDefault();
    event.currentTarget.setPointerCapture(event.pointerId);
    dragging = true;
  }
  function drag(event) {
    if (!dragging || !editorBox || !terminalBox) return;
    const top = editorBox.getBoundingClientRect().top;
    const bottom = terminalBox.getBoundingClientRect().bottom;
    if (bottom > top) model.setTerminalShare((bottom - event.clientY) / (bottom - top));
  }
  function endDrag() { dragging = false; }
  function splitterKeys(event) {
    const moves = { ArrowUp: SHARE_STEP, ArrowDown: -SHARE_STEP, PageUp: 3 * SHARE_STEP, PageDown: -3 * SHARE_STEP };
    if (event.key in moves) model.setTerminalShare(model.terminalShare + moves[event.key]);
    else if (event.key === 'Home') model.setTerminalShare(MAX_SHARE);
    else if (event.key === 'End') model.setTerminalShare(MIN_SHARE);
    else if (event.key === 'Enter') model.setTerminalShare(DEFAULT_SHARE);
    else return;
    event.preventDefault();
  }

  // The column of the instructions and the files has two splitters, as the terminals have one: the one at its right edge gives it more or less
  // of the width (an exam's instructions with a wide table need it), and the one between the instructions and the files shares its height.
  // Their keys are the terminals' splitter's: the arrows a step, Page Up and Page Down three, Home and End the ends, Enter the usual size.
  let panesBox;
  let sideBox;
  let panesWidth = $state(0);
  let sideWidth = $state(0);
  // Until the student moves it, the column is at its usual width, whose share is measured.
  const sideNow = $derived(snap.sideShare ?? (panesWidth > 0 ? sideWidth / panesWidth : MIN_SIDE));
  let sideDragging = $state(false);
  let instructionsDragging = $state(false);
  function startSplit(event) {
    if (event.button !== 0) return false;
    event.preventDefault();
    event.currentTarget.setPointerCapture(event.pointerId);
    return true;
  }
  function sideDrag(event) {
    if (!sideDragging || !panesBox) return;
    const box = panesBox.getBoundingClientRect();
    if (box.width > 0) model.setSideShare((event.clientX - box.left) / box.width);
  }
  function instructionsDrag(event) {
    if (!instructionsDragging || !sideBox) return;
    const box = sideBox.getBoundingClientRect();
    if (box.height > 0) model.setInstructionsShare((event.clientY - box.top) / box.height);
  }
  // A key moves from what the model has now: the snapshot the page draws is a frame behind, and keys repeat faster than that.
  function sideKeys(event) {
    const moves = { ArrowRight: SIDE_STEP, ArrowLeft: -SIDE_STEP, PageUp: 3 * SIDE_STEP, PageDown: -3 * SIDE_STEP };
    const now = model.sideShare ?? (panesWidth > 0 ? sideWidth / panesWidth : MIN_SIDE);
    if (event.key in moves) model.setSideShare(now + moves[event.key]);
    else if (event.key === 'Home') model.setSideShare(MAX_SIDE);
    else if (event.key === 'End') model.setSideShare(MIN_SIDE);
    else if (event.key === 'Enter') model.setSideShare(null);
    else return;
    event.preventDefault();
  }
  function instructionsKeys(event) {
    // Up moves the splitter up, which gives the instructions less room.
    const moves = { ArrowDown: INSTRUCTIONS_STEP, ArrowUp: -INSTRUCTIONS_STEP, PageDown: 3 * INSTRUCTIONS_STEP, PageUp: -3 * INSTRUCTIONS_STEP };
    if (event.key in moves) model.setInstructionsShare(model.instructionsShare + moves[event.key]);
    else if (event.key === 'Home') model.setInstructionsShare(MAX_INSTRUCTIONS);
    else if (event.key === 'End') model.setInstructionsShare(MIN_INSTRUCTIONS);
    else if (event.key === 'Enter') model.setInstructionsShare(DEFAULT_INSTRUCTIONS);
    else return;
    event.preventDefault();
  }
  // The column's width and the share of its height go to the CSS as fractions (the editor keeps at least 24rem, the column 15rem).
  const sideColumns = $derived(snap.sideShare === null ? null
    : `minmax(15rem, ${Math.round(snap.sideShare * 1000)}fr) minmax(24rem, ${Math.round((1 - snap.sideShare) * 1000)}fr)`);

  // The splitter between the two panes of the editor, as the others: the arrows a step, Page Up and Page Down three, Home and End the ends,
  // Enter (or a double click) half and half. Each pane keeps at least 16rem.
  let paneDragging = $state(false);
  function paneDrag(event) {
    if (!paneDragging || !editorBox) return;
    const box = editorBox.getBoundingClientRect();
    if (box.width > 0) model.setPaneShare((event.clientX - box.left) / box.width);
  }
  function paneKeys(event) {
    const moves = { ArrowRight: PANE_STEP, ArrowLeft: -PANE_STEP, PageUp: 3 * PANE_STEP, PageDown: -3 * PANE_STEP };
    if (event.key in moves) model.setPaneShare(model.paneShare + moves[event.key]);
    else if (event.key === 'Home') model.setPaneShare(MAX_PANE);
    else if (event.key === 'End') model.setPaneShare(MIN_PANE);
    else if (event.key === 'Enter') model.setPaneShare(DEFAULT_PANE);
    else return;
    event.preventDefault();
  }
  const twoPanes = $derived(snap.panes.length > 1);
  const paneColumns = $derived(twoPanes ? `minmax(16rem, ${Math.round(snap.paneShare * 1000)}fr) minmax(16rem, ${Math.round((1 - snap.paneShare) * 1000)}fr)` : 'minmax(0, 1fr)');
  const tabsByPath = $derived(new Map(snap.tabs.map(tab => [tab.path, tab])));
  // The tree's marks: where each open file is, and which have changes not saved yet.
  const openIn = $derived(new Map(snap.panes.flatMap((pane, index) => pane.items.map(path => [path, twoPanes ? (index === 0 ? 'left' : 'right') : 'open']))));
  const unsavedPaths = $derived(new Set(snap.tabs.filter(tab => tab.dirty).map(tab => tab.path)));
  // What the store keeps are the tabs that exist, and a new editor state is read-only while the workspace is.
  $effect(() => { const ids = new Set(snap.tabs.map(tab => tab.id)); untrack(() => store.prune(ids)); });
  $effect(() => { store.readOnly = disabled; });

  function hideTerminal() { terminalWanted = false; focusEditor(); }
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

  // The panes F6 goes round: the editor is one stop for each of its panes.
  const order = ['todo', 'tree', 'editor-0', 'editor-1', 'terminal'];
  function focusPane(name) {
    const node = panes?.querySelector(`[data-pane="${name}"]`);
    if (name.startsWith('editor-')) { focusEditor(Number(name.slice('editor-'.length))); return; }
    (node?.querySelector('[tabindex="0"], input:not(:disabled), button:not(:disabled)') ?? node)?.focus?.();
    if (name === 'terminal') node?.querySelector('textarea')?.focus();
  }
  // Ctrl+W (Cmd+W on a Mac) closes the file in front, wherever the keyboard is, as in a code editor; a key held down closes one file, not
  // every file. Not in a terminal, where Ctrl+W is the program's (bash deletes a word with it, nano searches): the first time it is pressed
  // there the student is told how to close a file. Not while a dialog is open, or while the workspace is hidden behind the lock screen.
  let toldTerminalClose = false;
  function closeKey(event) {
    if (event.target.closest?.('dialog') || !panes || panes.closest('[hidden]')) return;
    if (event.target.closest?.('.xterm')) {
      if (toldTerminalClose) return;
      toldTerminalClose = true;
      notify({ kind: 'info', key: 'close-in-terminal', text: event.metaKey
        ? 'Cmd+W does not close files from a terminal. To close the file in front, leave the terminal first (F6).'
        : 'In a terminal, Ctrl+W belongs to the program (bash deletes a word, nano searches). To close the file in front, leave the terminal first (F6).' });
      return;
    }
    event.preventDefault();
    if (event.repeat || !model.activePath) return;
    const inEditor = event.target.closest?.('[data-pane^="editor-"]');
    model.closeTab(model.activePath).then(closed => { if (closed && inEditor && model.activePath) focusEditor(); });
  }
  // The keys of the panes, as in a code editor: Ctrl+1 and Ctrl+2 (Cmd on a Mac) put the keyboard in the left or the right pane, and
  // Ctrl+Page Up and Ctrl+Page Down bring the previous or the next tab of the pane last used to the front. Not in a terminal, whose
  // programs may want them, and not behind a dialog.
  function paneKey(event) {
    if (event.target.closest?.('.xterm') || event.target.closest?.('dialog') || !panes || panes.closest('[hidden]')) return;
    event.preventDefault();
    if (event.key === 'PageUp' || event.key === 'PageDown') {
      model.cycleTab(event.key === 'PageDown' ? 1 : -1);
      if (event.target.closest?.('[data-pane^="editor-"]')) focusEditor();
      return;
    }
    const index = Number(event.key) - 1;
    if (index < model.panes.length) { model.usePane(index); focusEditor(index); }
    else notify({ kind: 'info', key: 'no-right-pane', text: 'There is one pane. To open a file on the other side, Alt+click it in the files (or Ctrl+Enter), or move a tab with ⫿.' });
  }
  function keys(event) {
    const plain = (event.ctrlKey || event.metaKey) && !event.shiftKey && !event.altKey;
    if (plain && event.key.toLowerCase() === 's') {
      // Ctrl+S saves. In a terminal it is flow control, which a shell program may want: the terminal keeps it.
      if (event.target.closest?.('.xterm')) return;
      event.preventDefault();
      model.saveAll().then(ok => { if (!ok) notify({ kind: 'warning', key: 'save-all', text: 'Some changes could not be saved yet.' }); });
    } else if (plain && event.key.toLowerCase() === 'w') {
      closeKey(event);
    } else if (plain && ['1', '2', 'PageUp', 'PageDown'].includes(event.key)) {
      paneKey(event);
    } else if (event.key === 'F6') {
      event.preventDefault();
      // A hidden terminal panel is not a stop.
      const stops = order.filter(name => (name !== 'terminal' || terminalVisible) && (name !== 'editor-1' || model.panes.length > 1));
      const here = stops.findIndex(name => event.target.closest?.(`[data-pane="${name}"]`));
      focusPane(stops[(here + (event.shiftKey ? stops.length - 1 : 1)) % stops.length]);
    }
  }

  async function open(path, side = false) { await model.openFile(path, { side }); focusEditor(); }
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
  <div class="panes" bind:this={panesBox} bind:clientWidth={panesWidth} style:--side-columns={sideColumns}>
    <aside class="side" id="side-panel" bind:this={sideBox} bind:clientWidth={sideWidth}
      style:--instructions-share="{Math.round(snap.instructionsShare * 1000)}fr" style:--files-share="{Math.round((1 - snap.instructionsShare) * 1000)}fr">
      <div data-pane="todo" class="todo-box" id="instructions-panel"><TodoPanel instructions={workspace.instructions} todos={workspace.todos} done={snap.todo} disabled={disabled}
        onToggle={(id, done) => model.setTodo(id, done)} wide={snap.sideWide} onWiden={() => model.toggleSideWide()} /></div>
      <div data-pane="tree" class="tree-box" bind:this={tree}>
        <!-- The splitter between the instructions and the files sits in the gap above the files. -->
        <div class="side-splitter across" class:dragging={instructionsDragging} role="separator" aria-orientation="horizontal" aria-label="Resize the instructions"
          aria-controls="instructions-panel" tabindex="0" aria-valuemin={Math.round(MIN_INSTRUCTIONS * 100)} aria-valuemax={Math.round(MAX_INSTRUCTIONS * 100)}
          aria-valuenow={Math.round(snap.instructionsShare * 100)} aria-valuetext="The instructions take {Math.round(snap.instructionsShare * 100)} percent of the height"
          title="Drag to give the instructions or the files more room, or use the up and down arrows. Double click: usual size"
          onpointerdown={event => { if (startSplit(event)) instructionsDragging = true; }} onpointermove={instructionsDrag}
          onpointerup={() => { instructionsDragging = false; }} onpointercancel={() => { instructionsDragging = false; }} onlostpointercapture={() => { instructionsDragging = false; }}
          onkeydown={instructionsKeys} ondblclick={() => model.setInstructionsShare(DEFAULT_INSTRUCTIONS)}></div>
        <FileTree folders={snap.folders} expanded={snap.expanded} activePath={snap.activePath} readOnly={disabled} {openIn} unsaved={unsavedPaths}
          onToggle={path => model.toggleFolder(path)} onOpen={open}
          onNew={(kind, folder) => { naming = { kind, folder }; }} onRename={path => { naming = { kind: 'rename', path, folder: dirname(path) }; }} onDelete={path => { deleting = path; }} />
      </div>
      <!-- The splitter between the column and the editor sits in the gap at the column's right. -->
      <div class="side-splitter upright" class:dragging={sideDragging} role="separator" aria-orientation="vertical" aria-label="Resize the instructions and the files"
        aria-controls="side-panel" tabindex="0" aria-valuemin={Math.round(MIN_SIDE * 100)} aria-valuemax={Math.round(MAX_SIDE * 100)} aria-valuenow={Math.round(sideNow * 100)}
        aria-valuetext="The instructions and the files take {Math.round(sideNow * 100)} percent of the width"
        title="Drag to give the instructions or the editor more room, or use the left and right arrows. Double click: usual width"
        onpointerdown={event => { if (startSplit(event)) sideDragging = true; }} onpointermove={sideDrag}
        onpointerup={() => { sideDragging = false; }} onpointercancel={() => { sideDragging = false; }} onlostpointercapture={() => { sideDragging = false; }}
        onkeydown={sideKeys} ondblclick={() => model.setSideShare(null)}></div>
    </aside>
    <div class="main" class:with-terminal={terminalVisible} style:--editor-share="{Math.round((1 - snap.terminalShare) * 1000)}fr" style:--terminal-share="{Math.round(snap.terminalShare * 1000)}fr">
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
      <!-- The editor: one pane, or two side by side with a splitter in the gap between them. A pane is kept by its id, so the one left when the
           other goes keeps its editor and its text size. A click or the keyboard in a pane makes it the one last used. -->
      <div class="editors" class:two={twoPanes} bind:this={editorBox} style:--pane-columns={paneColumns}>
        {#each snap.panes as pane, index (pane.id)}
          <div class="editor-box" data-pane="editor-{index}" onfocusin={() => model.usePane(index)} onpointerdown={() => model.usePane(index)}>
            {#if index === 1}
              <div class="pane-splitter" class:dragging={paneDragging} role="separator" aria-orientation="vertical" aria-label="Resize the two panes of the editor" tabindex="0"
                aria-valuemin={Math.round(MIN_PANE * 100)} aria-valuemax={Math.round(MAX_PANE * 100)} aria-valuenow={Math.round(snap.paneShare * 100)}
                aria-valuetext="The left pane takes {Math.round(snap.paneShare * 100)} percent of the width"
                title="Drag to give one pane or the other more room, or use the left and right arrows. Double click: half and half"
                onpointerdown={event => { event.stopPropagation(); if (startSplit(event)) paneDragging = true; }} onpointermove={paneDrag}
                onpointerup={() => { paneDragging = false; }} onpointercancel={() => { paneDragging = false; }} onlostpointercapture={() => { paneDragging = false; }}
                onkeydown={paneKeys} ondblclick={() => model.setPaneShare(DEFAULT_PANE)}></div>
            {/if}
            <EditorPane bind:this={editors[pane.id]} {model} {store} paneId={pane.id} tabs={pane.items.map(path => tabsByPath.get(path)).filter(Boolean)} allTabs={snap.tabs}
              activePath={pane.active} side={twoPanes ? (index === 0 ? 'left' : 'right') : ''} used={!twoPanes || index === snap.activePane}
              canMove={twoPanes || pane.items.length > 1} readOnly={disabled} onClose={path => model.closeTab(path)} onCloseMany={paths => model.closeTabs(paths)}
              onMove={path => { model.moveTab(path); focusEditor(); }} />
          </div>
        {/each}
      </div>
      {#if terminalVisible}
        <!-- A separator that can be focused is a control (the ARIA pattern of a window splitter): the arrows move it, Home and End go to the largest and the smallest
             terminal, and Enter or a double click go back to the usual size. -->
        <div class="splitter" class:dragging role="separator" aria-orientation="horizontal" aria-label="Resize the terminals" aria-controls="terminal-panel" tabindex="0"
          aria-valuemin={Math.round(MIN_SHARE * 100)} aria-valuemax={Math.round(MAX_SHARE * 100)} aria-valuenow={Math.round(snap.terminalShare * 100)}
          aria-valuetext="The terminals take {Math.round(snap.terminalShare * 100)} percent of the height" title="Drag to resize the terminals, or use the up and down arrows. Double click: usual size"
          onpointerdown={startDrag} onpointermove={drag} onpointerup={endDrag} onpointercancel={endDrag} onlostpointercapture={endDrag} onkeydown={splitterKeys}
          ondblclick={() => model.setTerminalShare(DEFAULT_SHARE)}></div>
      {/if}
      <div class="terminal-box" id="terminal-panel" data-pane="terminal" hidden={!terminalVisible} bind:this={terminalBox}>
        <TerminalPane {model} terminals={snap.terminals} activeTerm={snap.activeTerm} terminalAllowed={snap.terminalAllowed} readOnly={disabled} zoomIndex={snap.terminalZoom} onHide={hideTerminal} />
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
  /* The column of the instructions and the files is 15 to 21rem until the student gives it a share of the width (--side-columns). */
  .panes { flex: 1 1 0; min-height: 0; display: grid; grid-template-columns: var(--side-columns, minmax(15rem, 21rem) minmax(0, 1fr)); gap: var(--space-2); }
  .side { position: relative; display: grid; grid-template-rows: minmax(0, var(--instructions-share, 400fr)) minmax(0, var(--files-share, 600fr)); gap: var(--space-2); min-height: 0; min-width: 0; }
  .todo-box { min-height: 0; overflow: auto; padding-right: .15rem; }
  .tree-box { position: relative; min-height: 0; display: grid; }
  /* The column's splitters are strips of the gaps (out of the grid's flow): at the column's right edge, and above the files. */
  .side-splitter { position: absolute; z-index: 1; touch-action: none; border-radius: 4px; display: grid; place-items: center; }
  .side-splitter::before { content: ''; border-radius: 2px; background: var(--text-muted); opacity: .5; }
  .side-splitter.upright { top: 0; bottom: 0; right: calc(var(--space-2) * -1); width: var(--space-2); cursor: col-resize; }
  .side-splitter.upright::before { width: 3px; height: 3rem; }
  .side-splitter.across { left: 0; right: 0; top: calc(var(--space-2) * -1); height: var(--space-2); cursor: row-resize; }
  .side-splitter.across::before { width: 3rem; height: 3px; }
  .side-splitter:hover::before, .side-splitter.dragging::before, .side-splitter:focus-visible::before { background: var(--action-primary); opacity: 1; }
  .side-splitter:focus-visible { outline: 2px solid var(--action-primary); outline-offset: -1px; }
  /* The editor has all the room until a terminal is wanted, and then about two thirds of it. */
  .main { display: grid; grid-template-rows: auto minmax(0, 1fr); gap: var(--space-2); min-height: 0; min-width: 0; }
  .main.with-terminal { grid-template-rows: auto minmax(0, var(--editor-share, 650fr)) 0 minmax(6rem, var(--terminal-share, 350fr)); }
  /* The splitter is the strip of the gap between the editor and the terminals: it has no row of its own (a row of 0, whose margin lifts it into the gap
     above), and the terminals are lifted by one gap so that the space between the two is what it was before the splitter. */
  .splitter { z-index: 1; height: var(--space-2); margin-top: calc(var(--space-2) * -1); cursor: row-resize; touch-action: none; border-radius: 4px; display: grid; place-items: center; }
  .splitter::before { content: ''; width: 3rem; height: 3px; border-radius: 2px; background: var(--text-muted); opacity: .5; }
  .splitter:hover::before, .splitter.dragging::before { background: var(--action-primary); opacity: 1; }
  .splitter:focus-visible { outline: 2px solid var(--action-primary); outline-offset: -1px; }
  .splitter:focus-visible::before { background: var(--action-primary); opacity: 1; }
  .with-terminal .terminal-box { margin-top: calc(var(--space-2) * -1); }
  .editor-box, .terminal-box { min-height: 0; display: grid; }
  /* The editors' row: one pane, or two side by side (--pane-columns), and the splitter between them is the strip of the gap, as the others. */
  .editors { min-height: 0; min-width: 0; display: grid; grid-template-columns: var(--pane-columns, minmax(0, 1fr)); gap: var(--space-2); }
  .editor-box { position: relative; min-width: 0; }
  .pane-splitter { position: absolute; z-index: 1; top: 0; bottom: 0; left: calc(var(--space-2) * -1); width: var(--space-2); cursor: col-resize; touch-action: none; border-radius: 4px; display: grid; place-items: center; }
  .pane-splitter::before { content: ''; width: 3px; height: 3rem; border-radius: 2px; background: var(--text-muted); opacity: .5; }
  .pane-splitter:hover::before, .pane-splitter.dragging::before, .pane-splitter:focus-visible::before { background: var(--action-primary); opacity: 1; }
  .pane-splitter:focus-visible { outline: 2px solid var(--action-primary); outline-offset: -1px; }
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
    /* The column is the whole width, above the editor: nothing to resize. Two panes are one above the other. */
    .side-splitter, .pane-splitter { display: none; }
    .editors.two { grid-template-columns: minmax(0, 1fr); grid-auto-rows: minmax(14rem, 1fr); }
  }
</style>
