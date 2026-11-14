import { dirname, basename, join, isInside, moved, sortEntries } from './paths.js';

// The state and the rules of a student's workspace: the files they have open, what is saved and what is not, the terminals, the TODO
// ticks. It knows nothing of the page; the components show its snapshot and call its methods. Everything it does to the workspace is
// a request to the server through the transport (docs/workspace/relay-protocol.md), which the server checks again.
//
// The rule that matters most: an edit is never dropped. An edit that cannot be written stays in memory and marks its file as unsaved
// until it is written, and a write is always made against the version the student last saw, so a change made in the terminal in the
// meantime is a question for the student ("Reload or keep yours?"), never a silent overwrite.

export const MAX_TERMINALS = 4;
export const AUTOSAVE_MS = 1000;
// Folder listings and re-reads after a change are gathered for this long, so a build that writes a hundred files is one listing.
export const REFRESH_MS = 150;
const HELD_OUTPUT_LIMIT = 256 << 10;

const defaultScheduler = { set: (fn, ms) => setTimeout(fn, ms), clear: id => clearTimeout(id) };

export class WorkspaceModel {
  // transport: { request(type, fields) -> Promise<reply>, input(term, data) -> Promise, reconnect() -> Promise }
  // notify({ kind, key, text }) tells the student something (a toast); onChange() says the snapshot changed.
  constructor({ transport, notify = () => {}, onChange = () => {}, scheduler = defaultScheduler, now = () => Date.now() }) {
    this.transport = transport;
    this.notify = notify;
    this.onChange = onChange;
    this.scheduler = scheduler;
    this.now = now;
    this.connection = 'idle';
    this.note = '';
    this.generation = 0;
    this.terminalAllowed = true;
    this.canRun = false;
    this.canCheck = false;
    this.folders = new Map(); // path -> { entries, loaded, error }
    this.expanded = new Set(['']);
    this.tabs = [];
    this.activePath = null;
    this.terminals = [];
    this.activeTerm = null;
    this.todo = {};
    this.lastSave = null;
    this.lastSubmit = null;
    this.size = { cols: 100, rows: 24 };
    this.buffers = new Map(); // tab id -> { text(), replace(text) } of the open editors
    this.tabSeq = 0;
    this.sinks = new Map(); // term -> { write(bytes), reset() } of the open terminal views
    this.held = new Map(); // term -> output that came before its view did
    this.saving = new Map(); // path -> promise of the write in flight
    this.timers = new Map(); // path -> autosave timer
    this.refreshTimer = null;
    this.pendingFolders = new Set();
    this.pendingFiles = new Set();
    this.syncing = null;
    this.closed = false;
    this.revision = 0;
  }

  // ----- what the page reads -----------------------------------------------------------------------------------------------------

  get readOnly() { return this.connection !== 'connected'; }

  snapshot() {
    return {
      revision: this.revision, connection: this.connection, note: this.note, readOnly: this.readOnly,
      terminalAllowed: this.terminalAllowed, canRun: this.canRun, canCheck: this.canCheck,
      folders: new Map([...this.folders].map(([path, folder]) => [path, { ...folder }])), expanded: new Set(this.expanded),
      tabs: this.tabs.map(tab => ({ ...tab })), activePath: this.activePath,
      terminals: this.terminals.map(term => ({ ...term })), activeTerm: this.activeTerm,
      todo: { ...this.todo }, lastSave: this.lastSave, lastSubmit: this.lastSubmit,
      unsaved: this.tabs.some(tab => tab.dirty),
    };
  }
  changed() { this.revision += 1; if (!this.closed) this.onChange(); }
  tab(path) { return this.tabs.find(tab => tab.path === path); }
  terminal(term) { return this.terminals.find(item => item.term === term); }

  // ----- the connection ----------------------------------------------------------------------------------------------------------

  // The page tells the model what the application's view says, on every snapshot of it. When a connection is made, the workspace is
  // read again: what a student sees is always what the container holds, whoever changed it while they were away.
  setView(view) {
    this.terminalAllowed = view.terminal !== false;
    this.canRun = !!view.can_run;
    this.canCheck = !!view.can_check;
    const connection = view.connection ?? 'idle';
    const connectedNow = connection === 'connected';
    const became = connectedNow && view.generation > this.generation;
    this.connection = connection;
    this.note = view.note ?? '';
    if (connectedNow) this.generation = Math.max(this.generation, view.generation);
    if (became) this.syncing = this.synchronize().finally(() => { this.syncing = null; });
    this.changed();
  }
  setTodoState(done) { this.todo = { ...done }; this.changed(); }

  // Everything is read again, and what the student had not saved is written.
  async synchronize() {
    try {
      const loaded = [...this.folders.keys()].filter(path => this.folders.get(path).loaded);
      if (!loaded.includes('')) loaded.unshift('');
      await Promise.all(loaded.map(path => this.loadFolder(path)));
      await this.reconcileTerminals();
      await Promise.all(this.tabs.map(tab => this.refreshFile(tab.path)));
      await this.saveAll();
    } catch { /* a connection that drops again is synchronized again when it returns */ }
  }

  async call(type, fields = {}) {
    return this.transport.request(type, fields);
  }
  // A request whose answer says it failed is a value, not an exception; a request that got no answer at all is an exception.
  tell(kind, key, text) { this.notify({ kind, key, text }); }

  // ----- folders and the tree ----------------------------------------------------------------------------------------------------

  async loadFolder(path) {
    try {
      const reply = await this.call('fs.list', { path });
      const folder = this.folders.get(path) ?? { entries: [], loaded: false, error: '' };
      if (reply.ok) { folder.entries = sortEntries(reply.entries ?? []); folder.loaded = true; folder.error = ''; }
      else if (reply.code === 'NOT_FOUND') { this.folders.delete(path); this.expanded.delete(path); this.changed(); return; }
      else { folder.error = reply.message || 'The folder cannot be read.'; }
      this.folders.set(path, folder);
    } catch {
      const folder = this.folders.get(path) ?? { entries: [], loaded: false, error: '' };
      folder.error = 'The folder cannot be read now.';
      this.folders.set(path, folder);
    }
    this.changed();
  }

  async toggleFolder(path) {
    if (this.expanded.has(path)) { this.expanded.delete(path); this.changed(); return; }
    this.expanded.add(path);
    this.changed();
    if (!this.folders.get(path)?.loaded) await this.loadFolder(path);
  }

  async revealFolder(path) {
    const parts = path.split('/').filter(Boolean);
    let at = '';
    for (const part of parts) {
      at = join(at, part);
      this.expanded.add(at);
      if (!this.folders.get(at)?.loaded) await this.loadFolder(at);
    }
    this.changed();
  }

  // ----- the files ---------------------------------------------------------------------------------------------------------------

  registerBuffer(path, buffer) { const tab = this.tab(path); if (tab) this.buffers.set(tab.id, buffer); }
  unregisterBuffer(id, buffer) { if (this.buffers.get(id) === buffer) this.buffers.delete(id); }
  // The editor of a tab says its text changed; it knows the tab by its id, which a rename does not change.
  editedById(id) { const tab = this.tabs.find(item => item.id === id); if (tab) this.edited(tab.path); }

  async openFile(path) {
    const existing = this.tab(path);
    if (existing) { this.activate(path); return; }
    const tab = { id: (this.tabSeq += 1), path, state: 'loading', version: '', savedText: '', initial: '', dirty: false, conflict: null, missing: false, message: '' };
    this.tabs.push(tab);
    this.activate(path);
    await this.readInto(tab);
  }

  async readInto(tab) {
    try {
      const reply = await this.call('fs.read', { path: tab.path });
      if (!this.tab(tab.path)) return;
      if (reply.ok) {
        tab.state = 'ready'; tab.version = reply.version; tab.savedText = reply.content; tab.initial = reply.content; tab.missing = false; tab.message = '';
      } else if (reply.code === 'BINARY') {
        tab.state = 'binary'; tab.message = 'This file is not text, so it cannot be shown or edited here.';
      } else if (reply.code === 'TOO_LARGE') {
        tab.state = 'toolarge'; tab.message = 'This file is larger than 1 MiB, so it cannot be shown or edited here.';
      } else if (reply.code === 'NOT_FOUND') {
        tab.state = 'error'; tab.missing = true; tab.message = 'This file no longer exists.';
      } else {
        tab.state = 'error'; tab.message = reply.message || 'The file cannot be opened.';
      }
    } catch {
      if (!this.tab(tab.path)) return;
      tab.state = 'error'; tab.message = 'The file cannot be opened now. Try again when the connection is back.';
    }
    this.changed();
  }

  activate(path) {
    if (this.activePath && this.activePath !== path) this.save(this.activePath).catch(() => {});
    this.activePath = path;
    this.changed();
  }

  // Closing a tab saves it first; a tab that cannot be saved stays open, with its edits.
  async closeTab(path) {
    const tab = this.tab(path);
    if (!tab) return true;
    if (tab.dirty) {
      const saved = await this.save(path);
      if (!saved && this.tab(path)?.dirty) {
        this.tell('error', 'close-unsaved', `${basename(path)} has changes that could not be saved yet, so it stays open.`);
        return false;
      }
    }
    this.dropTab(path);
    return true;
  }

  dropTab(path) {
    const index = this.tabs.findIndex(tab => tab.path === path);
    if (index < 0) return;
    this.tabs.splice(index, 1);
    this.cancelAutosave(path);
    if (this.activePath === path) this.activePath = this.tabs[Math.min(index, this.tabs.length - 1)]?.path ?? null;
    this.changed();
  }

  // The editor says the text changed.
  edited(path) {
    const tab = this.tab(path);
    if (!tab || tab.state !== 'ready') return;
    if (!tab.dirty) { tab.dirty = true; this.changed(); }
    this.scheduleAutosave(path);
  }

  scheduleAutosave(path) {
    this.cancelAutosave(path);
    this.timers.set(path, this.scheduler.set(() => { this.timers.delete(path); this.save(path).catch(() => {}); }, AUTOSAVE_MS));
  }
  cancelAutosave(path) {
    const timer = this.timers.get(path);
    if (timer !== undefined) { this.scheduler.clear(timer); this.timers.delete(path); }
  }

  // Writes one file, if it has unsaved changes. Resolves true when the file is saved when it returns, false when it is not (a conflict
  // that awaits the student, a disk that is full, no connection): the edits are still in memory then.
  async save(path) {
    const tab = this.tab(path);
    if (!tab || tab.state !== 'ready') return true;
    this.cancelAutosave(path);
    const running = this.saving.get(path);
    if (running) { await running; return this.save(path); }
    const buffer = this.buffers.get(tab.id);
    if (!tab.dirty || !buffer) return !tab.dirty;
    if (tab.conflict) return false;
    if (this.readOnly) return false;
    const job = this.write(tab, buffer).finally(() => this.saving.delete(path));
    this.saving.set(path, job);
    return job;
  }

  async write(tab, buffer) {
    const text = buffer.text();
    let reply;
    try {
      reply = await (tab.missing ? this.call('fs.create', { path: tab.path, content: text }) : this.call('fs.write', { path: tab.path, content: text, base_version: tab.version }));
    } catch {
      // The answer was lost: the write may or may not have been made. The edits stay; the next write settles it (below).
      this.changed();
      return false;
    }
    if (reply.ok) {
      tab.version = reply.version; tab.savedText = text; tab.missing = false;
      this.lastSave = this.now();
      if (buffer.text() === text) tab.dirty = false;
      else this.scheduleAutosave(tab.path);
      this.changed();
      return !tab.dirty;
    }
    switch (reply.code) {
      case 'CONFLICT':
        await this.meet(tab, buffer);
        return !tab.dirty;
      case 'NOT_FOUND':
        // The file, or its folder, is gone: the student chooses to put it back (the next save makes it again).
        tab.missing = true; tab.message = 'This file no longer exists. Save to put it back.';
        this.tell('warning', `gone-${tab.path}`, `${tab.path} was deleted or moved. Your text is kept; save again to put the file back.`);
        break;
      case 'EXISTS':
        // A file was made with the name it was to get (it was missing, and someone made it): treat it as a change made elsewhere.
        tab.missing = false;
        await this.meet(tab, buffer);
        return !tab.dirty;
      case 'QUOTA':
        this.tell('error', 'disk-full', 'Your disk space is full, so your changes cannot be saved. Delete files you do not need (in the terminal or the file tree), then save again.');
        break;
      case 'TOO_LARGE':
        this.tell('error', `large-${tab.path}`, `${tab.path} is larger than 1 MiB, which is more than a file can be saved with here. Shorten it.`);
        break;
      default:
        this.tell('error', `save-${tab.path}`, `${tab.path} could not be saved: ${reply.message || reply.code || 'the server refused it'}. Your text is kept.`);
    }
    this.changed();
    return false;
  }

  // A write was refused because the file is not the version the student last saw. Read what is there now: if it is the same text the
  // student has (a write whose answer was lost, or the same change made in the terminal), there is nothing to ask; if not, ask.
  async meet(tab, buffer) {
    try {
      const reply = await this.call('fs.read', { path: tab.path });
      if (reply.ok) {
        if (reply.content === buffer.text()) {
          tab.version = reply.version; tab.savedText = reply.content; tab.dirty = false; tab.conflict = null; this.lastSave = this.now();
        } else {
          tab.conflict = { version: reply.version, content: reply.content };
          this.tell('warning', `conflict-${tab.path}`, `${tab.path} changed in the terminal. Choose Reload or Keep yours in the editor.`);
        }
      } else {
        tab.conflict = { version: '', content: null };
      }
    } catch { /* the question is asked again when the connection is back */ }
    this.changed();
  }

  // The student's answer to "This file changed in the terminal. Reload or keep yours?"
  async resolveConflict(path, choice) {
    const tab = this.tab(path);
    const conflict = tab?.conflict;
    if (!tab || !conflict) return;
    if (choice === 'reload' && conflict.content !== null) {
      this.buffers.get(tab.id)?.replace(conflict.content);
      tab.version = conflict.version; tab.savedText = conflict.content; tab.dirty = false; tab.conflict = null;
      this.cancelAutosave(path);
      this.changed();
      return;
    }
    if (choice === 'keep') {
      // The student's text is written over the file as it is now: against its current version, which is what they have just seen.
      tab.version = conflict.version; tab.conflict = null; tab.missing = conflict.version === '';
      this.changed();
      await this.save(path);
    }
  }

  async saveAll() {
    const results = await Promise.all(this.tabs.map(tab => this.save(tab.path)));
    return results.every(Boolean);
  }

  // A file that may have been changed from outside (the terminal, a build): read it, and take the change or ask.
  async refreshFile(path) {
    const tab = this.tab(path);
    if (!tab || tab.state === 'loading') return;
    try {
      const reply = await this.call('fs.read', { path });
      if (!this.tab(path)) return;
      if (!reply.ok) {
        if (reply.code === 'NOT_FOUND') { tab.missing = true; tab.message = 'This file no longer exists.'; this.changed(); }
        else if (reply.code === 'BINARY' || reply.code === 'TOO_LARGE') { tab.state = reply.code === 'BINARY' ? 'binary' : 'toolarge'; this.changed(); }
        return;
      }
      if (tab.state !== 'ready') { await this.readInto(tab); return; }
      if (reply.version === tab.version && !tab.missing) return;
      if (this.saving.has(path)) return; // our own write is on its way; its answer settles the version
      const buffer = this.buffers.get(tab.id);
      if (!tab.dirty || !buffer || buffer.text() === reply.content) {
        buffer?.replace(reply.content);
        tab.version = reply.version; tab.savedText = reply.content; tab.dirty = false; tab.conflict = null; tab.missing = false;
        this.changed();
      } else if (!tab.conflict || tab.conflict.version !== reply.version) {
        tab.conflict = { version: reply.version, content: reply.content };
        this.tell('warning', `conflict-${path}`, `${path} changed in the terminal. Choose Reload or Keep yours in the editor.`);
        this.changed();
      }
    } catch { /* read again after the next connection */ }
  }

  // ----- making, renaming and deleting -------------------------------------------------------------------------------------------

  async createFile(folder, name) { return this.make('fs.create', folder, name, { content: '' }, true); }
  async createFolder(folder, name) { return this.make('fs.mkdir', folder, name, {}, false); }

  async make(type, folder, name, extra, open) {
    const path = join(folder, name);
    try {
      const reply = await this.call(type, { path, ...extra });
      if (!reply.ok) return this.refused(reply, path);
      await this.revealFolder(folder);
      await this.loadFolder(folder);
      if (open) await this.openFile(path);
      return '';
    } catch { return 'The workspace is not connected. Try again when it is.'; }
  }

  // The words for what the server said, shown in the dialog that asked.
  refused(reply, path) {
    switch (reply.code) {
      case 'EXISTS': return `${basename(path)} already exists here.`;
      case 'NOT_FOUND': return 'The folder is not there any more.';
      case 'QUOTA': return 'Your disk space is full.';
      case 'BAD_PATH': return 'That name cannot be used.';
      case 'DENIED': return 'You are not allowed to do that here.';
      default: return reply.message || 'The workspace refused it.';
    }
  }

  async rename(path, newName) {
    const to = join(dirname(path), newName);
    if (to === path) return '';
    await Promise.all(this.tabs.filter(tab => isInside(tab.path, path) && tab.dirty).map(tab => this.save(tab.path)));
    try {
      const reply = await this.call('fs.rename', { path, to });
      if (!reply.ok) return this.refused(reply, to);
    } catch { return 'The workspace is not connected. Try again when it is.'; }
    // Open files under it follow it.
    for (const tab of this.tabs) {
      const next = moved(tab.path, path, to);
      if (next !== tab.path) {
        this.cancelAutosave(tab.path);
        tab.path = next;
      }
    }
    this.activePath = this.activePath && moved(this.activePath, path, to);
    const expanded = [...this.expanded].map(item => moved(item, path, to));
    this.expanded = new Set(expanded);
    for (const key of [...this.folders.keys()]) if (isInside(key, path) && key !== '') this.folders.delete(key);
    await this.loadFolder(dirname(path));
    if (this.expanded.has(to)) await this.loadFolder(to);
    this.changed();
    return '';
  }

  async remove(path) {
    try {
      const reply = await this.call('fs.delete', { path });
      if (!reply.ok && reply.code !== 'NOT_FOUND') return this.refused(reply, path);
    } catch { return 'The workspace is not connected. Try again when it is.'; }
    for (const tab of [...this.tabs]) if (isInside(tab.path, path)) { tab.dirty = false; this.dropTab(tab.path); }
    for (const key of [...this.folders.keys()]) if (key !== '' && isInside(key, path)) { this.folders.delete(key); this.expanded.delete(key); }
    await this.loadFolder(dirname(path));
    this.changed();
    return '';
  }

  // ----- what the server tells us ------------------------------------------------------------------------------------------------

  handleMessage(message) {
    switch (message.type) {
      case 'fs.event': this.fileEvent(message); break;
      case 'pty.exit': {
        const item = this.terminal(message.term);
        if (item) { item.exited = true; item.exitCode = message.exit_code ?? null; item.signal = message.signal ?? ''; item.oom = !!message.oom_killed; this.changed(); }
        break;
      }
      case 'ws.agent_restarted':
        // The container started again: its files are as they were, its terminals are gone.
        for (const item of this.terminals) { this.held.delete(item.term); this.sinks.get(item.term)?.write?.(new TextEncoder().encode('\r\n[The workspace restarted; this terminal ended.]\r\n')); }
        this.terminals = []; this.activeTerm = null;
        this.tell('warning', 'agent-restarted', 'Your workspace restarted. Your files are safe; your terminals were closed.');
        this.changed();
        break;
      case 'agent.error':
        if (message.code === 'RATE') this.tell('warning', 'input-dropped', 'The program is not reading its input fast enough; some of what you typed was dropped.');
        break;
      case 'ws.error':
        this.tell('error', 'ws-error', message.message || 'The server did not understand a message.');
        break;
      default:
    }
  }

  fileEvent(event) {
    const path = event.path ?? '';
    if (event.kind === 'overflow') {
      for (const [folder, state] of this.folders) if (state.loaded) this.pendingFolders.add(folder);
      for (const tab of this.tabs) this.pendingFiles.add(tab.path);
    } else {
      this.pendingFolders.add(dirname(path));
      if (event.from) this.pendingFolders.add(dirname(event.from));
      if (event.kind === 'modify' || event.kind === 'create') if (this.tab(path)) this.pendingFiles.add(path);
      if (event.kind === 'delete' || event.kind === 'rename') {
        for (const tab of this.tabs) {
          const gone = event.kind === 'delete' ? path : event.from;
          if (gone && isInside(tab.path, gone)) { this.pendingFiles.add(tab.path); }
        }
      }
    }
    if (this.refreshTimer === null) this.refreshTimer = this.scheduler.set(() => this.refreshNow(), REFRESH_MS);
  }

  async refreshNow() {
    this.refreshTimer = null;
    const folders = [...this.pendingFolders].filter(path => this.folders.get(path)?.loaded);
    const files = [...this.pendingFiles];
    this.pendingFolders.clear();
    this.pendingFiles.clear();
    await Promise.all([...folders.map(path => this.loadFolder(path)), ...files.map(path => this.refreshFile(path))]);
  }

  // ----- terminals ---------------------------------------------------------------------------------------------------------------

  setSize(cols, rows) { if (cols > 0 && rows > 0) this.size = { cols, rows }; }

  registerSink(term, sink) {
    this.sinks.set(term, sink);
    const held = this.held.get(term);
    if (held) { this.held.delete(term); for (const chunk of held.chunks) sink.write(chunk); }
  }
  unregisterSink(term, sink) { if (this.sinks.get(term) === sink) this.sinks.delete(term); }

  output(term, data) {
    const sink = this.sinks.get(term);
    if (sink) { sink.write(data); return; }
    // The view of a terminal that was just opened is not there yet; what the program printed meanwhile waits for it.
    const held = this.held.get(term) ?? { chunks: [], size: 0 };
    held.chunks.push(data); held.size += data.length;
    while (held.size > HELD_OUTPUT_LIMIT && held.chunks.length > 1) held.size -= held.chunks.shift().length;
    this.held.set(term, held);
  }

  nextTitle() {
    const used = new Set(this.terminals.filter(item => item.kind === 'shell').map(item => item.title));
    for (let n = 1; n <= MAX_TERMINALS; n += 1) if (!used.has(`Terminal ${n}`)) return `Terminal ${n}`;
    return `Terminal ${this.terminals.length + 1}`;
  }

  addTerminal(info) {
    if (this.terminal(info.term)) return this.terminal(info.term);
    const item = { term: info.term, title: info.title, kind: info.kind, exited: !!info.exited, exitCode: info.exitCode ?? null, signal: '', oom: false };
    this.terminals.push(item);
    return item;
  }

  async openTerminal() {
    if (!this.terminalAllowed) return;
    if (this.terminals.filter(item => !item.exited).length >= MAX_TERMINALS) { this.tell('warning', 'terminal-limit', `You can have ${MAX_TERMINALS} terminals open. Close one first.`); return; }
    try {
      const reply = await this.call('pty.open', { cols: this.size.cols, rows: this.size.rows });
      if (!reply.ok) { this.tell('error', 'terminal-open', reply.code === 'LIMIT' ? `You can have ${MAX_TERMINALS} terminals open. Close one first.` : (reply.message || 'The terminal could not be opened.')); return; }
      this.addTerminal({ term: reply.term, title: this.nextTitle(), kind: 'shell' });
      this.activeTerm = reply.term;
      this.changed();
    } catch { this.tell('error', 'terminal-open', 'The terminal could not be opened: the workspace is not connected.'); }
  }

  async closeTerminal(term) {
    const item = this.terminal(term);
    if (!item) return;
    this.terminals = this.terminals.filter(other => other.term !== term);
    this.held.delete(term);
    if (this.activeTerm === term) this.activeTerm = this.terminals.at(-1)?.term ?? null;
    this.changed();
    try { await this.call('pty.close', { term }); } catch { /* the terminal is closed when the connection returns, with the others it does not know */ }
  }

  selectTerminal(term) { this.activeTerm = term; this.changed(); }

  resizeTerminal(term, cols, rows) {
    this.setSize(cols, rows);
    this.call('pty.resize', { term, cols, rows }).catch(() => {});
  }

  type(term, data) {
    const item = this.terminal(term);
    if (!item || item.exited || !this.terminalAllowed || this.readOnly) return;
    this.transport.input(term, data).catch(() => {});
  }

  // Run and Check: the files are saved first, so the program is the one on the screen.
  async run(which) {
    if (this.readOnly) return;
    if (!(await this.saveAll())) { this.tell('warning', 'run-unsaved', 'Some changes could not be saved, so the program would not be the one you see. Resolve the file marked as changed or unsaved first.'); return; }
    try {
      const reply = await this.call('run.start', { which, cols: this.size.cols, rows: this.size.rows });
      if (!reply.ok) { this.tell('error', 'run', reply.code === 'LIMIT' ? `You can have ${MAX_TERMINALS} terminals open. Close one first.` : (reply.message || `${which === 'run' ? 'Run' : 'Check'} could not start.`)); return; }
      const title = which === 'run' ? 'Run' : 'Check';
      // A new Run replaces the last Run, so the tabs do not pile up.
      for (const old of this.terminals.filter(item => item.kind === which && item.exited)) await this.closeTerminal(old.term);
      this.addTerminal({ term: reply.term, title, kind: which });
      this.activeTerm = reply.term;
      this.changed();
    } catch { this.tell('error', 'run', 'The program could not be started: the workspace is not connected.'); }
  }

  // After a connection: the terminals the agent holds are the truth. A terminal this side forgot is shown again, one that is gone is
  // dropped, and every one that remains is attached again, which sends the last of its output.
  async reconcileTerminals() {
    const reply = await this.call('pty.list');
    if (!reply.ok) return;
    const present = new Set((reply.terminals ?? []).map(item => item.term));
    this.terminals = this.terminals.filter(item => present.has(item.term));
    for (const info of reply.terminals ?? []) {
      this.addTerminal({ term: info.term, title: info.cmd ? 'Program' : this.nextTitle(), kind: info.cmd ? 'run' : 'shell', exited: info.exited, exitCode: info.exit_code });
    }
    if (!this.terminal(this.activeTerm)) this.activeTerm = this.terminals.at(-1)?.term ?? null;
    this.changed();
    for (const item of this.terminals) {
      this.sinks.get(item.term)?.reset?.();
      this.held.delete(item.term);
      const attached = await this.call('pty.attach', { term: item.term }).catch(() => null);
      if (attached?.ok) { item.exited = !!attached.exited; item.exitCode = attached.exit_code ?? item.exitCode; }
    }
    this.changed();
  }

  // ----- TODO and Submit ---------------------------------------------------------------------------------------------------------

  async setTodo(id, done) {
    const before = !!this.todo[id];
    this.todo = { ...this.todo, [id]: done };
    this.changed();
    try {
      const reply = await this.call('todo.set', { item: id, done });
      if (reply.ok) return;
    } catch { /* below */ }
    this.todo = { ...this.todo, [id]: before };
    this.tell('error', 'todo', 'The tick could not be saved. Try again when the connection is back.');
    this.changed();
  }

  async submit() {
    if (this.readOnly) { this.tell('error', 'submit', 'The workspace is not connected, so nothing can be submitted now.'); return false; }
    if (!(await this.saveAll())) { this.tell('warning', 'submit-unsaved', 'Some changes could not be saved, so they would not be in the submission. Resolve the file marked as changed or unsaved first.'); return false; }
    try {
      const reply = await this.call('submit');
      if (reply.ok) { this.lastSubmit = { at: this.now(), snapshot: reply.snapshot }; this.changed(); return true; }
      this.tell('error', 'submit', reply.message || 'The submission could not be saved. Try again.');
    } catch { this.tell('error', 'submit', 'The submission could not be saved: the connection was lost. Try again.'); }
    return false;
  }

  close() {
    this.closed = true;
    for (const timer of this.timers.values()) this.scheduler.clear(timer);
    this.timers.clear();
    if (this.refreshTimer !== null) this.scheduler.clear(this.refreshTimer);
  }
}
