// An in-memory stand-in for a student's container and the server in front of it, speaking the requests and events of
// docs/workspace/relay-protocol.md closely enough that the IDE's model and the UI tests run against it: files and folders with the
// agent's versions and its CONFLICT rule, terminals with a tiny shell, Run and Check, and fs.event for every change, including the
// ones a "terminal" makes. Used by the node tests and by the UI tests' fake API (tests/fake-api.js).

const encoder = new TextEncoder();

// Not SHA-256: any stable string will do for a version.
export function versionOf(text) {
  let hash = 5381;
  for (let i = 0; i < text.length; i += 1) hash = ((hash << 5) + hash + text.charCodeAt(i)) >>> 0;
  return `sha256:${hash.toString(16).padStart(8, '0')}${text.length.toString(16).padStart(8, '0')}`;
}

const parent = path => { const i = path.lastIndexOf('/'); return i < 0 ? '' : path.slice(0, i); };
const nameOf = path => path.slice(path.lastIndexOf('/') + 1);

export class FakeWorkspaceServer {
  constructor({ files = {}, terminal = true, canRun = true, canCheck = true, maxTerminals = 4 } = {}) {
    this.files = new Map();
    this.dirs = new Set(['']);
    for (const [path, content] of Object.entries(files)) this.put(path, content);
    this.terminal = terminal; this.canRun = canRun; this.canCheck = canCheck; this.maxTerminals = maxTerminals;
    this.terminals = new Map();
    this.nextTerm = 1;
    this.todo = {};
    this.submits = [];
    this.requests = [];
    this.typed = []; // every keystroke that reached a terminal
    this.online = true;
    this.onMessage = () => {};
    this.onOutput = () => {};
    this.diskFull = false;
    this.loseNextReply = false;
    this.failNext = null; // { type, code, message }
    this.delay = 0;
  }

  put(path, content) {
    for (let at = parent(path); at !== ''; at = parent(at)) this.dirs.add(at);
    this.files.set(path, content);
  }
  emit(kind, path, isDir = false, from) { queueMicrotask(() => this.onMessage({ type: 'fs.event', kind, path, is_dir: isDir, ...(from ? { from } : {}) })); }

  // A change made from outside the IDE, as a program or the terminal would make it.
  externalWrite(path, content) { const existed = this.files.has(path); this.put(path, content); this.emit(existed ? 'modify' : 'create', path); }
  externalDelete(path) { this.files.delete(path); this.emit('delete', path); }

  list(path) {
    const entries = [];
    const seen = new Set();
    for (const dir of this.dirs) if (dir !== '' && parent(dir) === path && !seen.has(dir)) { seen.add(dir); entries.push({ name: nameOf(dir), kind: 'dir', size: 0, mtime: 0 }); }
    for (const [file, content] of this.files) if (parent(file) === path) entries.push({ name: nameOf(file), kind: 'file', size: encoder.encode(content).length, mtime: 0 });
    return entries.sort((a, b) => (a.name < b.name ? -1 : 1));
  }

  async request(type, fields = {}) {
    if (!this.online) throw new Error('The workspace is not connected.');
    this.requests.push({ type, ...fields });
    if (this.delay) await new Promise(resolve => setTimeout(resolve, this.delay));
    if (!this.online) throw new Error('The workspace connection was lost; the request may or may not have been done.');
    const failure = this.failNext && this.failNext.type === type ? this.failNext : null;
    if (failure) { this.failNext = null; return { ok: false, type, code: failure.code, message: failure.message ?? failure.code }; }
    const reply = this.handle(type, fields);
    if (this.loseNextReply) { this.loseNextReply = false; throw new Error('The workspace connection was lost; the request may or may not have been done.'); }
    return { type, ...reply };
  }

  fail(code, message) { return { ok: false, code, message: message ?? code }; }

  handle(type, f) {
    switch (type) {
      case 'fs.list':
        if (f.path !== '' && !this.dirs.has(f.path)) return this.fail('NOT_FOUND');
        return { ok: true, entries: this.list(f.path) };
      case 'fs.read': {
        if (!this.files.has(f.path)) return this.fail('NOT_FOUND');
        const content = this.files.get(f.path);
        if (content.includes('\0')) return this.fail('BINARY');
        if (encoder.encode(content).length > (1 << 20)) return this.fail('TOO_LARGE');
        return { ok: true, content, version: versionOf(content), size: encoder.encode(content).length };
      }
      case 'fs.write': {
        if (!this.files.has(f.path)) return this.fail('NOT_FOUND');
        if (versionOf(this.files.get(f.path)) !== f.base_version) return { ok: false, code: 'CONFLICT', message: 'the file changed', version: versionOf(this.files.get(f.path)) };
        if (this.diskFull) return this.fail('QUOTA');
        this.files.set(f.path, f.content);
        this.emit('modify', f.path);
        return { ok: true, version: versionOf(f.content) };
      }
      case 'fs.create': {
        if (this.files.has(f.path) || this.dirs.has(f.path)) return this.fail('EXISTS');
        if (parent(f.path) !== '' && !this.dirs.has(parent(f.path))) return this.fail('NOT_FOUND');
        if (this.diskFull) return this.fail('QUOTA');
        this.files.set(f.path, f.content ?? '');
        this.emit('create', f.path);
        return { ok: true, version: versionOf(f.content ?? '') };
      }
      case 'fs.mkdir':
        if (this.files.has(f.path) || this.dirs.has(f.path)) return this.fail('EXISTS');
        if (parent(f.path) !== '' && !this.dirs.has(parent(f.path))) return this.fail('NOT_FOUND');
        this.dirs.add(f.path); this.emit('create', f.path, true);
        return { ok: true };
      case 'fs.rename': {
        if (this.files.has(f.to) || this.dirs.has(f.to)) return this.fail('EXISTS');
        if (this.files.has(f.path)) { this.files.set(f.to, this.files.get(f.path)); this.files.delete(f.path); this.emit('rename', f.to, false, f.path); return { ok: true }; }
        if (this.dirs.has(f.path)) {
          for (const dir of [...this.dirs]) if (dir === f.path || dir.startsWith(`${f.path}/`)) { this.dirs.delete(dir); this.dirs.add(f.to + dir.slice(f.path.length)); }
          for (const [file, content] of [...this.files]) if (file.startsWith(`${f.path}/`)) { this.files.delete(file); this.files.set(f.to + file.slice(f.path.length), content); }
          this.emit('rename', f.to, true, f.path);
          return { ok: true };
        }
        return this.fail('NOT_FOUND');
      }
      case 'fs.delete': {
        if (this.files.delete(f.path)) { this.emit('delete', f.path); return { ok: true }; }
        if (this.dirs.has(f.path)) {
          for (const dir of [...this.dirs]) if (dir === f.path || dir.startsWith(`${f.path}/`)) this.dirs.delete(dir);
          for (const file of [...this.files.keys()]) if (file.startsWith(`${f.path}/`)) this.files.delete(file);
          this.emit('delete', f.path, true);
          return { ok: true };
        }
        return this.fail('NOT_FOUND');
      }
      case 'pty.open': {
        if (!this.terminal) return this.fail('DENIED', 'this exam has no terminal; use the Run and Check buttons');
        return this.openTerminal(f, '');
      }
      case 'run.start': {
        const command = f.which === 'run' ? (this.canRun ? 'run' : '') : f.which === 'check' ? (this.canCheck ? 'check' : '') : null;
        if (command === null) return this.fail('BAD_REQUEST');
        if (command === '') return this.fail('NOT_FOUND', `this exam has no ${f.which} command`);
        return this.openTerminal(f, command);
      }
      case 'pty.list':
        return { ok: true, terminals: [...this.terminals.values()].map(t => ({ term: t.term, cols: t.cols, rows: t.rows, cmd: t.cmd, exited: t.exited, exit_code: t.exitCode })) };
      case 'pty.attach': {
        const t = this.terminals.get(f.term);
        if (!t) return this.fail('NOT_FOUND');
        queueMicrotask(() => { if (t.history) this.onOutput(t.term, encoder.encode(t.history)); });
        return { ok: true, term: t.term, cols: t.cols, rows: t.rows, exited: t.exited, exit_code: t.exitCode };
      }
      case 'pty.resize': {
        const t = this.terminals.get(f.term);
        if (!t) return this.fail('NOT_FOUND');
        t.cols = f.cols; t.rows = f.rows;
        return { ok: true };
      }
      case 'pty.close':
        if (!this.terminals.delete(f.term)) return this.fail('NOT_FOUND');
        return { ok: true };
      case 'todo.set':
        if (!['t1', 't2', 't3'].includes(f.item)) return this.fail('NOT_FOUND');
        this.todo[f.item] = !!f.done;
        return { ok: true };
      case 'submit':
        this.submits.push(new Map(this.files));
        return { ok: true, snapshot: this.submits.length };
      default:
        return this.fail('BAD_REQUEST', `unknown message type ${type}`);
    }
  }

  openTerminal(f, cmd) {
    if ([...this.terminals.values()].filter(t => !t.exited).length >= this.maxTerminals) return this.fail('LIMIT', `at most ${this.maxTerminals} terminals can be open; close one first`);
    const t = { term: this.nextTerm, cols: f.cols, rows: f.rows, cmd, exited: false, exitCode: null, line: '', history: '' };
    this.nextTerm += 1;
    this.terminals.set(t.term, t);
    setTimeout(() => this.start(t), 0);
    return { ok: true, term: t.term, cols: t.cols, rows: t.rows };
  }

  say(t, text) { t.history += text; this.onOutput(t.term, encoder.encode(text)); }
  finish(t, code) { t.exited = true; t.exitCode = code; queueMicrotask(() => this.onMessage({ type: 'pty.exit', term: t.term, exit_code: code, signal: '', oom_killed: false })); }

  start(t) {
    if (t.cmd === 'run') {
      this.say(t, 'g++ -std=c++20 -o build/list_demo src/main.cpp src/list.cpp\r\n./build/list_demo\r\nlist: 3 2 1\r\n');
      this.files.has('build/list_demo') || this.put('build/list_demo', 'ELF');
      this.emit('create', 'build/list_demo');
      this.finish(t, 0);
    } else if (t.cmd === 'check') {
      const done = (this.files.get('src/list.cpp') ?? '').includes('DONE');
      this.say(t, done ? 'sample_test: 3 of 3 passed\r\n' : 'sample_test: FAILED push_front\r\n');
      this.finish(t, done ? 0 : 1);
    } else {
      this.say(t, 'student@workspace:/work$ ');
    }
  }

  // Keystrokes: echoed like a tty, and a line is run when it is ended.
  input(term, data) {
    if (!this.online) throw new Error('The workspace is not connected.');
    this.typed.push(data);
    const t = this.terminals.get(term);
    if (!t || t.exited || t.cmd) return;
    for (const char of data) {
      if (char === '\r' || char === '\n') {
        this.say(t, '\r\n');
        this.shell(t, t.line.trim());
        t.line = '';
        this.say(t, 'student@workspace:/work$ ');
      } else if (char === '\x7f') { if (t.line) { t.line = t.line.slice(0, -1); this.say(t, '\b \b'); } }
      else if (char >= ' ') { t.line += char; this.say(t, char); }
    }
  }

  shell(t, line) {
    const [command, ...rest] = line.split(/\s+/);
    const arg = rest.join(' ');
    switch (command) {
      case 'echo': this.say(t, `${arg}\r\n`); break;
      case 'touch': { const existed = this.files.has(arg); this.put(arg, this.files.get(arg) ?? ''); this.emit(existed ? 'modify' : 'create', arg); break; }
      case 'rm': this.files.delete(arg); this.emit('delete', arg); break;
      case 'mkdir': this.dirs.add(arg); this.emit('create', arg, true); break;
      case 'cat': this.say(t, `${(this.files.get(arg) ?? `cat: ${arg}: No such file or directory`).replace(/\n/g, '\r\n')}\r\n`); break;
      case 'ls': this.say(t, `${this.list('').map(e => e.name).join('  ')}\r\n`); break;
      case 'exit': this.finish(t, 0); break;
      case '': break;
      default: this.say(t, `${command}: command not found\r\n`);
    }
  }
}
