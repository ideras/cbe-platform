# The student IDE

What a student sees in a workspace exam, how it is built, and how it is tested. The wire it speaks is `relay-protocol.md`; the decisions
are P4-1 to P4-13 in `DECISIONS.md`.

## What the student sees

```
┌ exam toolbar: title, time chip, ● Connected, Saved 12:03:44, Server copy 12:03:00, A− A+ ───────────┐
├ Instructions ┐ ┌ [Run] [Check] [› Terminal]                                          [Save]   ┤
│ TODO  ☐ ☐ ☑   │ ├ list.cpp ● │ main.cpp ────────────────────────────────────────── A− A+ ⋯ ✕ ┤
├ Files ────────┤ │  editor (CodeMirror 6, highlighting only): all the room there is              │
│ ▾ src         │ │                                                                               │
│    list.cpp   │ ├ Run ✓ │ Terminal 1 ───────────────────────────────────────────── + ✕ ⌄ ───┤  only while a
└───────────────┘ │  terminal (xterm.js), about a third of the room                              │  terminal is wanted
                  └───────────────────────────────────────────────────────────────────────────────┘
```

- **Instructions and TODO**: the exam's text (sanitized like every exam text) and its TODO list; a tick is the student's own record and is
  kept by the server.
- **Files**: a tree; new file, new folder, rename and delete (which asks first) act on the selected item; the tree follows what the
  terminal and the programs do to the files within about 0.3 s.
- **Editor**: one tab per open file, a dot on a file with unsaved changes, saved as the student types; a tab brought back to the front
  is where it was left (cursor and scroll). A tab closes with its ×, a middle click, Delete on the tab or **Ctrl+W** (Cmd+W on a Mac)
  anywhere but a terminal; the **⋯** menu has **Close all**, **Close others** and **Close saved** (WS-1, WS-2). A file is saved before
  its tab closes, and one that cannot be saved stays open.
- **Two panes** (WS-5): the editor can show two files side by side, each pane with its own tabs, **A−**, **A+**, **⫿** and **⋯**, and
  the terminals under both. Alt+click (or Ctrl+Enter) in the files opens a file on the other side; **⫿** or ⋯ › Move sends the tab in
  front there; a file is in one pane at a time and keeps its text, undo history, cursor and scroll (an SVG its view and zoom) when it
  moves. A file opened from the tree goes to the pane last used (a click or the keyboard in a pane makes it so; its tab in front is
  marked in blue, the other pane's in grey). A pane left with no tab goes. A splitter between the panes (each keeps 16rem) moves with
  the pointer or the keys, Enter or a double click for half and half. The tree marks open files L or R (a dot with one pane) and files
  with unsaved changes. Below 900 px the two panes are one above the other.
- A file that is not text, or over
  1 MiB, is named and not opened. It has all the room the window leaves, and its own **A−** and **A+** (the size of the code only,
  seven steps from 85 % to 200 %, also Ctrl and + − 0), on top of the toolbar's A− and A+ (which size the whole workspace, files,
  buttons and terminals too).
- **SVG pictures** (SVG-1..SVG-5 in [DECISIONS.md](DECISIONS.md)): an `.svg` file whose text starts as an SVG (what `dot -Tsvg automaton.gv -o
  automaton.svg` writes) opens as the **picture** it draws, with **Picture** and **Source** in the tab row to switch to its XML in the editor
  and back. The tab is the same either way (saved, reloaded and in conflict as any file), and each view keeps its place behind the other
  and while another tab is in front (the picture its zoom and scroll; a tab closed and opened again starts at Fit).
  The picture starts at **Fit** (as large as the room allows, never larger than its own size), and **−**, **+** and **100 %** zoom it in
  steps of its own size (50 % to 400 %; also + − 0 on the picture, and Ctrl and the wheel); a picture larger than the room scrolls, with
  the arrow keys too. It has a white ground, as the exam's pictures do. A file made again in the terminal is drawn again by itself; one
  caught half written says "This file cannot be drawn as a picture yet", with a button to its source, until the next change. Edits made in
  Source show in the picture before they are saved. An SVG the student is typing stays in Source until they ask for the picture. The
  picture is an `<img>` from a `data:` address, so nothing in the file runs or is fetched (a test with a script, handlers, a frame and
  addresses elsewhere holds this). **PNG and PDF are not shown** (they are not text, and the workspace's file channel carries text only).
- **Terminals**: up to four, plus a tab for the last Run and the last Check. Run and Check save every file first. **They take no room
  until one is wanted** (P5-12): Run, Check, or the **Terminal** button (which opens a terminal if there is none) show the panel, about a
  third of the room under the editor; **⌄** in the panel's tab row, or the Terminal button again, hides it, and the terminals go on
  running with what they printed. Closing the last one takes the panel away. F6 skips a hidden panel. The panel has its own **A−** and **A+** in
  its tab row (the same seven steps as the editor's, for the terminal text only; the size is kept when a terminal is closed and another opened,
  P8-15), and a **splitter** between the editor and the panel (drag it, or the arrow keys when it has the focus; Home and End are the largest and
  the smallest terminal, Enter or a double click the usual size; P8-16). Typing `exit` (or Ctrl+D) in a terminal closes its tab; a program that Run
  or Check started, or a shell that was stopped, stays and says how it ended (P8-17).
- **Status**: the connection, the last save and the last copy the server took, told in the exam toolbar next to the time chip
  (P5-13), so that it takes no row of the window. A change in the connection also shows as a banner above the panes. The time left is the
  toolbar's chip alone.

## Keys

| Key | Does |
|---|---|
| Ctrl+S | Save every file (not in a terminal) |
| Ctrl+W (Cmd+W on a Mac) | Close the file in front, saving it first (not in a terminal, where bash deletes a word and nano searches with it) |
| F6, Shift+F6 | Next, previous pane (instructions, files, each editor pane, terminal; a hidden terminal panel is skipped) |
| Ctrl+1, Ctrl+2 (Cmd on a Mac) | The keyboard to the left, the right editor pane (not in a terminal) |
| Ctrl+Page Down, Ctrl+Page Up | The next, the previous tab of the pane last used (not in a terminal) |
| Ctrl and +, Ctrl and −, Ctrl and 0 | Larger, smaller, normal size of the code text (in the editor) |
| Arrows, Home, End, Enter | Move in the file tree, open a file, open or close a folder |
| Ctrl+Enter, Alt+click | Open the file on the other side of the editor (in the file tree) |
| F2, Delete | Rename, delete the selected file or folder |
| Arrows, Home, End | Move between tabs; Delete closes the file |
| Arrows, Home, End, Enter, Escape | In the ⋯ menu: move, do the action, close the menu |
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
- `EditorPane.svelte` is one pane: its tabs and one CodeMirror editor, which tells the model when the text changes; the model decides
  when to write, and keeps which files are in which pane (`panes`, `activePane`). What a tab is (its CodeMirror state with the undo
  history and the cursor, its scroll, an SVG's view and zoom) is in `editor-store.js`, shared by the panes: a pane that shows a tab
  the other pane still shows takes its state from that pane's editor, never an older copy. `TerminalView.svelte` is one xterm.js terminal; what the program prints reaches it through the model,
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
| `apps/student/frontend/tests/workspace-*.test.js`, `paste-guard.test.js` | The model (42), paths, the tree's rows, the paste guard, the no-storage check. Run with the others by `npm test`. |
| `apps/student/frontend/tests/workspace-picture.test.js` | Which files are pictures, whether a text starts as an SVG, the `data:` address (a `#`, a `%`, accents, 1 MiB), Fit and the zoom steps, and that the picture is an `<img>` and never the SVG put into the page. |
| `tests/ui/workspace-picture.spec.js` (Playwright, 16) | SVG files: the picture and its source, each keeping its place behind the other and while another tab is in front, a file made again in the terminal, a half-written one (a notice, no error), **that nothing in an SVG runs or is fetched**, Fit, the zoom and the scroll (buttons, keys, Ctrl and the wheel), a picture in the other pane beside the code (drawn again there; its zoom kept when it moves), the window's size, the white ground, other files as before, unsaved edits in the picture, an SVG being typed, the connection lost, that ticks, Run and other files do not draw the picture again or move it, the keyboard, axe, no storage. Run in Chromium and in WebKit. |
| `tests/ui/workspace.spec.js` (Playwright, 56) | The whole page against the in-memory stand-in (`tests/fake-workspace-server.js` and the workspace part of `tests/fake-api.js`): editing and autosave, closing tabs (Ctrl+W, and not in a terminal; a middle click; the ⋯ menu with the mouse and the keyboard), two panes (opening on the other side, the pane last used, the tree's marks, a moved tab's unsaved text, undo, cursor and scroll, a file changed in the terminal in the right pane, Ctrl+1, Ctrl+2, Ctrl+Page Down and Up, F6, the splitter), a tab's cursor and scroll after a trip to another tab, Ctrl+S, Run and Check, terminals, the tree's operations, conflicts, TODO, Save, the drop and the return of the connection (also with a 30-second drop on a fake clock), pastes, keyboard, no terminal, lock and unlock, no storage, accessibility (axe), sizes, text size (the toolbar's and the editor's), the terminal panel's showing and hiding, F6, that an autosave (or a tick, a Run, a file changed elsewhere) does not draw the editor again, move its scroll or move what is around it, and that the arrow keys and a click land on the right line after any change of the text size. Run in Chromium (`npm run test:ui`) and in WebKit. |
| `apps/student/cmd/student/menu_test.go` | The macOS menu bar has no key that closes, reloads or zooms the window, and keeps the Edit keys (WS-3). |
| `apps/student/internal/client/workspace_test.go` | The connection: token, requests and ids, output batching and order, input frames, a dropped connection and the requests waiting on it, 423, 401, the end of the exam, another window, the exam's lock, and the service's own way of telling a workspace from a question exam. |
| `apps/student/internal/client/real_workspace_test.go` | A student finishing the example package through the real client, run against the real server and a container by phase D of `deploy/workspace/test-server.sh`. |

## What it does not do (v1)

No search across files, no more than two editor panes and no file in both, no dragging of tabs, no drag-and-drop in the tree, no language server or autocompletion (highlighting only, as in the
exam's code answers), no binary or very large files (so no PNG or PDF picture: an SVG is text, and is shown), no graphical programs, no
installing packages.
