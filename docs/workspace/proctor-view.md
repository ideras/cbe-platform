# What the proctor sees of a workspace exam

Phase 5. The proctor application reads a workspace exam through the admin token, the same way it reads a question exam. Nothing here lets the proctor
change a student's files; it is all reading, and two downloads.

## The monitoring table

On a workspace exam the table has one more column, **Workspace**, headed "TODO is self-reported". Per student:

| Line | What it says | Where it comes from |
|------|--------------|---------------------|
| the badge | Starting, Running, Paused, Frozen, Failed or Removed: the container's state | `workspace_sessions.state` |
| Student | connected or not connected | whether the student's application holds the workspace's connection now |
| Last activity | "12 s ago", or "no activity yet" | the time of the last event the workspace wrote (a save, a terminal, a tick) |
| Container | "12.5% CPU · 180 MB", or "not measured" | the runtime's statistics, sampled by the server every 10 s |
| TODO | "2 of 3 done (self-reported)" | the ticks the student made; **nothing checks them** |
| submissions | "2 submissions · last 4:20 PM · final made" | `workspace_submissions` |
| restarts | "Workspace restarted 2 times" | how often the agent's container restarted |

A student who has no workspace yet reads "No workspace yet". On an exam that is not a workspace exam the column is not there: the application asks
`GET /admin/workspace/exam/{id}/roster`, the server answers 404 for a question exam, and the application remembers that and does not ask again.

## The review window

Each student's review window (Review) is the session's summary and, for a workspace's session, **three tabs** (P5-15): **History**, **Files** and
**Terminals** (with the number of terminals). A session that is not a workspace's has no tabs, only the history. The tabs go by arrow keys, Home and End;
every tab stays in the page, so a file that was chosen or a replay that was started is still there when its tab is chosen again.

- **The summary** (above the tabs) adds to the session's own rows the container's state and restarts, the ticks (as the student's word), and the
  submissions.
- **Downloads** (under the summary): *Download submitted files (zip)…* and *Download report (Markdown)…*, both of the latest submission, saved where the proctor
  chooses. Both are disabled until something has been submitted.
- **History:** the page-by-page history (below).
- **Files:** a source selector (the newest snapshot first, then each submission), a tree, and a read-only viewer. A snapshot is everything in the workspace,
  build products too; a submission is only what the exam submits (`submit` and `ignore`). Files are shown as text with line numbers; a binary file or one over
  1 MiB is described and not shown (it is in the zip). Links are shown as links and not followed.
- **Terminals:** a grid with a row for each terminal the student had (what it was, when it started, how it ended, how much it printed). **Choosing a row**
  (click, or Enter or Space; the arrow keys, Home and End move between rows) shows its replay under the grid and scrolls the window to it, so that it is
  never out of sight; *Close replay* clears the choice. The replay is the terminal as it printed, with the pauses shortened to 2 seconds; it can be paused,
  sped up, sought and shown whole, and there is a plain-text version under it for a screen reader or a copy. The chosen row stays chosen when the workspace
  is read again (every five seconds).

The history table has the workspace's events in words ("File saved: src/main.c · 120 bytes (+16)", "Blocked a paste into the editor (30 bytes)", "Final
submission · snapshot 5 · 6 files") and a **Workspace** filter that is there only when the session has some.

## The routes

All need the admin token. `{id}` is a session, snapshot, submission or terminal id. (`snapshot/{id}/tar`, from Phase 3, is the whole snapshot as a tar.)

| Route | Answers |
|-------|---------|
| `GET /admin/workspace/exam/{exam}/roster` | the table's column; 404 for a question exam |
| `GET /admin/workspace/session/{session}` | the review panel's summary: state, restarts, `done`, `snapshots`, `submissions`, `terminals` |
| `GET /admin/workspace/snapshot/{id}/tree`, `.../file?path=` | the files of a snapshot |
| `GET /admin/workspace/submission/{id}/tree`, `.../file?path=`, `.../zip` | the files of a submission, and its zip (regular files only) |
| `GET /admin/workspace/session/{session}/report` | the Markdown report of the latest submission |
| `GET /admin/workspace/terminal/{id}/cast` | the asciinema v2 transcript |

## What is shown and what is only text

Everything a student's workspace holds is untrusted text to the proctor application: file names, file contents, transcripts, TODO identifiers and event
details are drawn as text (the transcript by xterm.js), never as markup, and a test puts a script and an image tag in a file name and a file's content to prove
it. A transcript that does not start as an asciinema v2 file is refused with a message.
