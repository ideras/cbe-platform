# The student IDE

What a student sees in a workspace exam, how it is built, and how it is tested. The wire it speaks is `relay-protocol.md`; the decisions
are P4-1 to P4-13 in `DECISIONS.md`.

## What the student sees

```
┌ exam toolbar: title, time chip, A− A+ ──────────────────────────────────────────────────────────────┐
├ Instructions ┐ ┌ [Run] [Check]                                                       [Submit] ┤
│ TODO  ☐ ☐ ☑   │ ├ list.cpp ● │ main.cpp ─────────────────────────────────────────────────────────┤
├ Files ────────┤ │  editor (CodeMirror 6, highlighting only)                                      │
│ ▾ src         │ ├ Terminal 1 │ Run ✓ │ Check ✗ ──────────────────────────────────────── + ✕ ───┤
│    list.cpp   │ │  terminal (xterm.js)                                                           │
└───────────────┘ └────────────────────────────────────────────────────────────────────────────────┘
 ● Connected · Saved 12:03:44 · Server copy 12:03:00 · Time left 0:58:12
```

- **Instructions and TODO**: the exam's text (sanitized like every exam text) and its TODO list; a tick is the student's own record and is
  kept by the server.
- **Files**: a tree; new file, new folder, rename and delete (which asks first) act on the selected item; the tree follows what the
  terminal and the programs do to the files within about 0.3 s.
- **Editor**: one tab per open file, a dot on a file with unsaved changes, saved as the student types. A file that is not text, or over
  1 MiB, is named and not opened.
- **Terminals**: up to four, plus a tab for the last Run and the last Check. Run and Check save every file first.
- **Status bar**: the connection, the last save, the last copy the server took, and the time left.

## Keys

| Key | Does |
|---|---|
| Ctrl+S | Save every file (not in a terminal) |
| F6, Shift+F6 | Next, previous pane (instructions, files, editor, terminal) |
| Arrows, Home, End, Enter | Move in the file tree, open a file, open or close a folder |
| F2, Delete | Rename, delete the selected file or folder |
| Arrows, Home, End | Move between tabs; Delete or Ctrl+W closes the file |
| Ctrl+C, Ctrl+D, Tab, … | Go to the program when a terminal has the focus |

## How it is built

```
Svelte page                                     Go (apps/student/internal/client)             exam server
 workspace/Workspace.svelte  ──WorkspaceRequest──▶ workspaceLink ──WebSocket /workspace/ws──▶ relay ─▶ agent
   model.js (rules, state)   ◀──workspace:output──  (token, reconnect, ids, batching)          (docs/workspace/relay-protocol.md)
   EditorPane / FileTree / TerminalPane / ...        ◀──workspace:message──
```

- `src/workspace/model.js` is plain JavaScript with no page in it, so the rules are tested alone (`tests/workspace-model.test.js`). It
  takes a transport and a clock, and keeps: the folders it has listed, the open files with their version and whether they are saved, the
  terminals, the TODO ticks, and the connection's state. **An edit is never dropped**: it stays in memory and marks its file as unsaved
  until it is written, and a write always names the version the student last saw.
- `EditorPane.svelte` keeps one CodeMirror state per tab (undo history, cursor, scroll) and tells the model when the text changes; the
  model decides when to write. `TerminalView.svelte` is one xterm.js terminal; what the program prints reaches it through the model,
  which keeps output that comes before the view exists.
- `paste-guard.js` is the paste control (P4-7). `transport.js` is the page's side of the three calls and the output encoding.
- `Workspace.svelte` stays mounted while the exam is locked (hidden behind the lock screen), so unsaved text survives the lock.
- The Go side (`workspace.go`) is a transport, not a second model: it dials, reconnects with backoff (`RetryMin` to `RetryMax`), matches
  answers to requests by id, fails the requests that were waiting when a connection drops (their result is unknown, and the page
  reads the file again to find out), and refuses to keep two windows fighting for one workspace.

## Nothing is written to the student's computer

The page uses no `localStorage`, `sessionStorage`, IndexedDB, cookies, Cache API or file picker, and the Go side writes no file for the
workspace. Two tests hold this: a UI test that watches the page's storage calls while a student works, and `tests/workspace-no-storage.test.js`,
which reads the code for any of those APIs.

## Tests

| Where | What |
|---|---|
| `apps/student/frontend/tests/workspace-*.test.js`, `paste-guard.test.js` | The model (34), paths, the tree's rows, the paste guard, the no-storage check. Run with the others by `npm test`. |
| `tests/ui/workspace.spec.js` (Playwright, 25) | The whole page against the in-memory stand-in (`tests/fake-workspace-server.js` and the workspace part of `tests/fake-api.js`): editing and autosave, Ctrl+S, Run and Check, terminals, the tree's operations, conflicts, TODO, Submit, the drop and the return of the connection (also with a 30-second drop on a fake clock), pastes, keyboard, no terminal, lock and unlock, no storage, accessibility (axe), sizes, text size. Run in Chromium (`npm run test:ui`) and in WebKit. |
| `apps/student/internal/client/workspace_test.go` | The connection: token, requests and ids, output batching and order, input frames, a dropped connection and the requests waiting on it, 423, 401, the end of the exam, another window, the exam's lock, and the service's own way of telling a workspace from a question exam. |
| `apps/student/internal/client/real_workspace_test.go` | A student finishing the example package through the real client, run against the real server and a container by phase D of `deploy/workspace/test-server.sh`. |

## What it does not do (v1)

No search across files, no split editor, no drag-and-drop in the tree, no language server or autocompletion (highlighting only, as in the
exam's code answers), no binary or very large files, no graphical programs, no installing packages.
