# Phase 4 report: student IDE

**Date:** 2026-11-13. **Branch:** `feat/workspace-phase-4-student-ide` (from `feat/workspace-phase-3-runner-relay`, which is pushed). **Plan:** `.agents/WORKSPACE-PLAN.md` v1.1.
**Status:** built and tested; **five of the six acceptance items are met, and the sixth (the manual focus-loss script) needs you at the real application**, so
Phase 4 is not ticked in Appendix D and Phase 5 should wait for it (standing rule 2). Decisions `P4-1` to `P4-13` are proposed and wait for you.
**The branch is not pushed.**

## What changed

| Path | What |
|------|------|
| `apps/student/internal/client/workspace.go` | The Go side of a workspace exam: the connection (token, reconnection with backoff, requests by id, output batched in order, close on lock or end, no fighting between windows), `View.Workspace`, and the calls `WorkspaceRequest`, `WorkspaceInput`, `WorkspaceReconnect`. `service.go` tells a workspace from a question exam (`GET /workspace`, asked only when the exam has no exercises) |
| `apps/student/cmd/student/main.go` | The events `workspace:output` and `workspace:message` |
| `apps/student/frontend/src/workspace/` | The IDE: `model.js` (the rules), `Workspace.svelte`, `EditorPane`, `FileTree`, `TerminalPane`, `TerminalView`, `TodoPanel`, `StatusBar`, `NameDialog`, `paste-guard.js`, `paths.js`, `tree.js`, `transport.js` (about 2,200 lines with the Go file) |
| `apps/student/frontend/src/App.svelte`, `api.js`, `editor-theme.js`, `CodeEditor.svelte` | The IDE takes the place of the exercises in a workspace exam (and stays mounted, hidden, under the lock screen); the code editor's look moved to a module both editors use |
| `internal/workspace`, `internal/storage`, `internal/httpapi` | A locked session reaches nothing of its workspace (423, a live connection closed within a second, `locked` in `ws.status`); a student is never told why a workspace failed |
| `shared/theme/theme.css` | The sixteen `--terminal-*` tokens, derived from the palette |
| `apps/student/frontend/tests/` | The model's tests and the in-memory stand-in for the container (`fake-workspace-server.js`), the IDE's Playwright spec, the fake API's workspace part |
| `apps/student/internal/client/real_workspace_test.go`, `deploy/workspace/test-server.sh`, `internal/workspace/e2e` | Phase D of the end-to-end script: a student finishes the example package through the real client, the real server and a container |
| `docs/workspace/` | `student-ide.md`, `DECISIONS.md` (P4-1 to P4-13), this report and its logs; `docs/tools/TEST-FOCUS.md` has the IDE's steps |
| `package.json` | `@xterm/xterm` 6.0.0 and `@xterm/addon-fit` 0.11.0 (new, exact), `@axe-core/playwright` 4.14.0 (new, development) |

49 files, about 5,000 lines added, 87 removed (of which about 40 are the code editor's theme, moved, not changed).

## Tests run and results

- **The IDE against the in-memory container (Playwright, `tests/ui/workspace.spec.js`, 25 tests), in Chromium and in WebKit, all green:** editing and
  autosave after a second, Ctrl+S, saving when a tab is left, Run and Check with their output tabs and exit marks, a terminal with keystrokes (and Ctrl+C and
  Ctrl+D reaching it), the tree following the terminal within a second, four terminals at most, new file and folder, rename, and delete with its
  confirmation, a file changed in the terminal (reloaded when untouched, a question when edited: Keep yours and Reload), TODO ticks, Submit, the connection
  dropping (read-only, edits kept, written on return, also for a 30-second drop on a fake clock), another window taking the workspace, pastes from outside
  stopped and pastes from inside let through (editor and terminal, and a dropped file), keyboard use of the tree and F6, an exam without a terminal, lock and
  unlock keeping the typed text, nothing written to storage, the accessibility scan (axe, critical and serious) on the workspace and its dialogs, window sizes
  from 1000x700 to 1920x1080 with no page scroll, the A+ and A− buttons reaching the editor and the terminals, a workspace that is starting or failed, and
  that using the IDE calls no native method but the workspace ones and the poll.
- **The model and the helpers (`npm test`, `node --test`):** 34 tests of the rules (saving, conflicts, a lost answer, a drop and return, renames, deletes,
  terminals, Run saving first, a workspace that is still starting), the path rules, the tree's rows, the paste guard, and the check that the workspace code uses no
  storage API. The student's unit tests went from 45 to 95.
- **The Go client (`go test -race`, 15 tests of the connection and the service):** the token on the dial, requests and ids, what the page may not send, output
  batched and in order before the message that follows, input in frames and a large paste split, a dropped connection and the requests waiting on it, a
  request nobody answers, 423, 401, the end of the exam, another window, the exam's lock closing the connection, a question exam never asking about a
  workspace, a workspace that cannot be asked about being retried. Three runs in a row for flakiness.
- **The server (`go test -race`):** the lock (423 on the view and the socket, the live connection closed, the container running meanwhile, the unlock),
  and a failed workspace telling the student nothing of why, with the proctor's view having it.
- **For real (`deploy/workspace/test-server.sh`, phase D, `reports/phase-4-e2e.log`):** the student application's own client, as a compiled test, logs in to the real
  server, sees the project, opens a terminal and makes a file (**told to the page after 51 ms**), runs the checks (they fail), implements the two functions
  against the version it read (a stale write is refused), runs the checks again (they pass), runs the program, adds a file of its own, ticks the three TODO
  items and submits; the rig then reads the submission back as the proctor does: the implementation, the new file, the build files, the ticks, and nothing of the
  grading bundle. Phases A to C, from Phase 3, pass as before.
- **Baseline (`reports/phase-4-tests.log`):** `scripts/ci/test.py --race` over the 7 modules passes; student unit **95**; proctor unit 74; student Playwright **156**
  (the 131 of before and the 25 new); proctor Playwright 294. The proctor and the shared theme tests pass with the new tokens.
- **Builds:** `vite build`; the student application built for production into the scratchpad (not launched), `-version` runs; `GOOS=windows go vet` of the client is clean.
- **Do the tests catch what they claim?** The first run of the Playwright spec found 13 failures, none of them in the tests: the status bar was not a `contentinfo`,
  a class overrode the `hidden` attribute (a locked exam would have shown the IDE under the lock screen), the tab strips held buttons a tablist may not (a
  critical axe finding), tab text below 4.5:1, the page scrolled, the tree lost its selection after a rename, and the theme contract refused raw colors. They are
  all fixed; the two things found only by thinking about use (the first request arriving while the container is still starting, and the reason for a failure
  reaching the student) have tests now.

## Acceptance (plan, Phase 4)

- [x] **A student can finish the example package end to end: edit, build, run `make check`, add a file, Submit.** Phase D (real client, server and container), and the same steps through the page in the Playwright spec.
- [x] **Files created or deleted in the terminal appear in the tree within 1 second.** The page test bounds it at 1 s; the real client measured **51 ms** from the keystroke to the page being told.
- [x] **Pulling the network for 30 seconds loses no edits.** The page test with a fake clock (30 s of "Reconnecting…", the text kept, written on return), the model's tests (several drops, a change in the terminal meanwhile asks instead of overwriting), and the Go link's tests (the requests waiting on a dropped connection fail as lost, not as done). **Not done: a real Wi-Fi drop**; that is Phase 8's rehearsal, and the manual script below asks for it once more.
- [ ] **The focus-loss manual script (`docs/tools/TEST-FOCUS.md`) passes with the IDE in use, including terminal and editor focus changes.** **Not run: it needs you at the real application on a real window.** The IDE's steps are written into the script. What is automated: using the IDE calls no native method but the workspace ones and the poll (`tests/ui/workspace.spec.js`), and focus detection is native, which no page can cause.
- [x] **An accessibility scan shows no critical issues. The terminal has a label, and the file tree is keyboard-navigable.** axe on the workspace and its two dialogs: no critical or serious finding, except in the exam toolbar, which is the question exams' own and was left out (P4-13). The terminal's field is labelled "Terminal 1 input"; the tree is an ARIA tree with arrows, Home, End, Enter, F2 and Delete (tested).
- [x] **Nothing is written to the student's disk.** A page test that watches the storage calls and the browser's databases and cookies while a student works, and `tests/workspace-no-storage.test.js`, which reads the page code (and the Go client's workspace file) for any of those APIs.

## What the work found, and what Phase 5 must know

1. **A locked exam has to take the workspace away too**, and the IDE must not lose unsaved text while the lock screen is up. Both are built (P4-2): the server refuses and closes, and the page keeps the IDE mounted but hidden.
2. **The reason for a failure reached the student** through `ws.status`, and can name the runner's socket. Fixed (P4-3); the proctor still has it.
3. **The first requests can arrive while the container is still starting.** The relay waits 8 s; the page now asks again for up to 30 s and says "starting".
4. **A snapshot is unfiltered (P3-14) and Submit is not final (P4-11).** Phase 5 gives Submit its record and its filter, and has to decide, with you, whether a student can end the exam early.
5. **Blocked pastes are told to the student and are not logged.** Phase 5's event log should take the page's `blocked` callback (`workspace/Workspace.svelte`) and send it.
6. **The terminal's keys.** A running program takes the keyboard, Ctrl+S is the terminal's and not "save" there, and F6 is how a keyboard user leaves a terminal. A student has to be told the last one; `student-ide.md` has the table, and `STUDENT-NOTICE.md` (which you review) does not mention it yet.
7. **Two small pre-existing accessibility findings in the exam toolbar** (the A− and A+ buttons' label does not hold their text) were left alone, per rule 7.
8. **The page's script is now about 916 kB** (it was not measured before the IDE, and xterm.js is a large part of it); Wails serves it from memory, so it costs a little start-up time and nothing on the wire.

## Deviations from the plan, and conflicts found (standing rule 1)

1. **Server changes in a student phase (P4-2, P4-3):** the lock and the failure text. The plan says every existing control still applies, and both are the server's to enforce.
2. **The code editor's theme moved into a module (P4-10)**, so the two editors share it; the code answers are unchanged (their tests pass).
3. **Terminal tokens were added to the shared theme (P4-10)**, derived from the palette: the theme's contract forbids a color in a component.
4. **Submit is not final (P4-11)** and has no confirmation, since a second submit is harmless; there is no way to end early yet.

## Not run, and what is unverified

- **The real application window.** The page is tested in Chromium and in WebKit (Playwright's build, the closest to the Linux window's engine) against stand-ins, and the Go client for real against a container, but **nobody has driven the real Wails window**: the focus-loss script, a real Wi-Fi drop, and a look at the IDE on the students' machines are yours (Windows and macOS included: only the Linux engine was approximated, and `GOOS=darwin` was not vetted for the student module because the Wails side needs cgo).
- **Large projects and slow machines.** The tree lists a folder at a time (5,000 names at most), the editor holds one file at most 1 MiB, the terminal keeps 5,000 lines; the load test is Phase 8.
- **The CI jobs.** The new Playwright specs need Chromium (already there), and the WebKit run was done by hand with a throwaway configuration, not added to CI.

## Left behind

On this machine: the Playwright WebKit build in `~/.cache/ms-playwright` (103 MB); the student application built into the scratchpad only (`build/` was not touched);
nothing from the end-to-end runs (no containers, folders or processes, checked). The runner, the quota helper and the image are as Phase 3 left them (the
Phase 0 resource backstop on `cbe-runner`'s slice **must still go before production**).

## For the owner

1. **Confirm or change P4-1 to P4-13**, first **P4-11 (the question: should a student be able to end a workspace exam early?)**, P4-2 (a lock hides the IDE but keeps the text), P4-7 (what the paste guard does and does not do) and P4-13.
2. **Run the focus-loss script with the IDE** (`docs/tools/TEST-FOCUS.md`, "The workspace IDE") on the platforms you use, and tell me the result. I will tick Phase 4 when it passes.
3. **May I push the branch?** And **go for Phase 5** (submission, logging and the proctor's views) once the script passes, or now if you would rather run it while I start.
