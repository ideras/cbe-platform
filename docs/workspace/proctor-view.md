# What the proctor sees of a workspace exam

Phase 5. The proctor application reads a workspace exam through the admin token, the same way it reads a question exam. Nothing here lets the proctor
change a student's files; it is all reading, and two downloads.

## Importing a workspace exam, and its image (Phase 7)

In the class's exam list, choose **Import exam…** and select the package's `workspace.md`. The picker detects that filename as **Workspace Exam**; other Markdown filenames default to **Traditional Exam**. Confirm or change the kind, then select **Validate as Workspace Exam** or **Validate as Traditional Exam**. This validates the selected file as that kind; it does not convert it.
For a workspace exam, the app compiles the folder that holds the manifest (with the same compiler as `build/examc`) and shows what it made of it, storing nothing: the title, the time, the **TODO items** (said to be self-reported), the project's starter files, the hidden tests (kept on the
server, never sent to students), the container's limits, and whether the exam has **its own image**, with the Containerfile checker's warnings. A package with problems shows them all and
cannot be imported. The PIN is chosen here, as for a question exam. The server checks everything again; what it refuses is shown in its words, and the package is used up whatever it says, so an unclear
failure never imports an exam twice. An exam is imported, never published.

In the exam list a workspace exam has a **Workspace** tag, the number of **TODO items** where a question exam has exercises, and, when its package has `image/`, a chip for its image:
*Image not built*, *Build waiting*, *Building the image…*, *Image ready*, *Build failed*, *Last build failed* (an earlier build of the same files is still the exam's image) or *Image missing* (the runner lost
it). **Preview** is off for a workspace exam (it has no question fields), and **Publish** is off, with the reason, until the image is ready. The **Image** button opens the dialog of
[images.md](images.md): the state, **Build the image** (or **Build again**, **Build again from scratch**), the course image and its digest the build was made on, the image's name, how long it took, the
Containerfile's warnings, and the log of the latest build or of any earlier one (a list of the exam's builds). While a build goes the dialog reads it every second, **asking only for the log from where the last look ended**, and shows it as it grows with the build's step
(*Step 4 of 9*, the command, a bar), when it last printed anything, and a *Follow the end of the log* box. A build that is waiting says it is waiting its turn.

## The monitoring table

On a workspace exam the table has one more column, **Workspace**, headed "TODO is self-reported". Per student:

| Line | What it says | Where it comes from |
|------|--------------|---------------------|
| the badge | Starting, Running, Paused, Frozen, Failed or Removed: the container's state | `workspace_sessions.state` |
| Student | connected or not connected | whether the student's application holds the workspace's connection now |
| Last activity | "12 s ago", or "no activity yet" | the time of the last event the workspace wrote (a save, a terminal, a tick) |
| Container | "12.5% CPU · 180 MB", or "not measured" | the runtime's statistics, sampled by the server every 10 s. **The CPU is made by the runner from two of its own samples** (Phase 8: rootless Podman gives no previous sample, and its own percentage of a container using a whole core was 0.03%), so the first reading of a container shows its memory alone |
| TODO | "2 of 3 done (self-reported)" | the ticks the student made; **nothing checks them** |
| submissions | "2 submissions · last 4:20 PM · final made" | `workspace_submissions` |
| restarts | "Workspace restarted 2 times" | how often the agent's container restarted |
| Grade | "Grade: 28 of 40 points (advisory until you confirm it)", "(confirmed)", "Waiting to be graded", "Being graded…", "Grading failed: it did not end in time"; nothing before any grading | the confirmed run of the student, or else the latest (`workspace_gradings`, Phase 6) |

A student who has no workspace yet reads "No workspace yet". On an exam that is not a workspace exam the column is not there: the application asks
`GET /admin/workspace/exam/{id}/roster`, the server answers 404 for a question exam, and the application remembers that and does not ask again.

**Download grades…**, beside it, saves the class's grades as a spreadsheet file the proctor chooses (enabled once somebody has a grade), and tells how many of the grades in it are confirmed. The exam list's download button of a workspace exam does the same (a question exam's opens the dialog of its students' answers, as before).

Above the table, a **Grading** line counts the class ("2 graded (1 confirmed) · 1 waiting or being graded · 1 failed · 3 submitted, not graded") and has
**Grade submissions…**: it asks first (how many submissions will be graded, that students already being graded or with nothing submitted are left out, that
grades are advisory), then queues one run for each, and says how many were queued and who was left out. The button is not there for a question exam.

## The review window

Each student's review window (Review) is the session's summary and, for a workspace's session, **four tabs** (P5-15, P6-9): **History**, **Files**,
**Terminals** (with the number of terminals) and **Grading** (with the number of runs). A session that is not a workspace's has no tabs, only the history. The tabs go by arrow keys, Home and End;
every tab stays in the page, so a file that was chosen or a replay that was started is still there when its tab is chosen again.

- **The summary** (above the tabs) adds to the session's own rows the container's state and restarts (and, for a workspace that **failed**, **why**, as text: the exam's image not built, the host's memory budget used, the runtime away; Phase 8), the ticks (as the student's word), and the
  submissions.
- **Downloads** (under the summary): *Download submitted files (zip)…* and *Download report (Markdown)…*, both of the latest submission, saved where the proctor
  chooses. Both are disabled until something has been submitted.
- **History:** the page-by-page history (below).
- **Files:** a source selector (the newest snapshot first, then each submission), a tree, and a read-only viewer. A snapshot is everything in the workspace,
  build products too; a submission is only what the exam submits (`submit` and `ignore`). Files are shown as text with line numbers; a binary file or one over
  1 MiB is described and not shown (it is in the zip). Links are shown as links and not followed.
- **Grading** (Phase 6, `grading.md`): *Grade this student* (the latest submission, or the one picked), and the student's runs as a grid, newest first (run,
  when it was asked, which submission, its result, whether it is confirmed). The latest run is open to begin with; choosing another row opens it under the grid:
  for each TODO of the exam, *Passed* or *Not passed*, the points of what it is worth, and the log of the grade program; a key of the output that is not a TODO
  is shown and said not to count; a TODO the output does not mention says so. *What the run printed* has standard output and standard error as text. A failed run
  says why and shows the same. **Confirm this grade** makes the run the student's grade (and takes the confirmation from another run of theirs); **Take the
  confirmation back** makes it advisory again. A run that is going shows its end without a click. The summary above the tabs has a *Grade* row: the confirmed run's
  grade, or the latest's.
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
| `GET /admin/workspace/exam/{exam}/grades.csv` | **the grade sheet** (Phase 8): a CSV, UTF-8 with a byte order mark and CR LF line ends, a line for every student of the class (`grading.md`, "The grade sheet"); 404 for a question exam |
| `POST /admin/class/{class}/workspace-exam` (multipart: `document`, `starter`, and `grading`, `image` when the package has them) | import a package (the proctor application sends it); 400 lists every problem, and the PIN is in the document |
| `GET /admin/exams` | the exam list; a workspace exam carries `kind: "workspace"`, `todos` and `image` (its state, from the database alone: it does not ask the runner), and a question exam none of the three |
| `POST /admin/workspace/exam/{exam}/image/build` (`{"rebuild": true}` optional) | queue a build of the exam's own image: `202` with the image's state; 409 when the exam has no image folder or one is already being built |
| `GET /admin/workspace/exam/{exam}/image[?log=1[&log_from=N]]` | the state of the image (`none`, `not_built`, `queued`, `building`, `ready`, `failed`, `missing`), whether the exam can be published as far as its image goes (`ready`), whether the runner has it (`present`), the latest build (with its log when asked) and the exam's builds without their logs. For a build that is waiting or going, `live` is true and the log is read **from the offset `log_from`** (the `log_next` of the last look): `log_from` says where `log` starts (0 when the offset is not one the server has), `log_next` where to ask next, and `step`, `steps`, `step_text` and `last_output_at` say where the build is. A build that has ended has all of its log and `live` false. A `log_from` that is not a whole number is a 400 |
| `GET /admin/workspace/exam/{exam}/image/build/{build}` | one build with its log |
| `POST /admin/exam/{exam}/publish` | **409** when the exam's image is not ready, with the reason in the answer (Phase 7) |
| `GET /admin/workspace/session/{session}` | the review panel's summary: state, restarts, `done`, `snapshots`, `submissions`, `terminals` |
| `GET /admin/workspace/snapshot/{id}/tree`, `.../file?path=` | the files of a snapshot |
| `GET /admin/workspace/submission/{id}/tree`, `.../file?path=`, `.../zip` | the files of a submission, and its zip (regular files only) |
| `GET /admin/workspace/session/{session}/report` | the Markdown report of the latest submission |
| `GET /admin/workspace/terminal/{id}/cast` | the asciinema v2 transcript |
| `POST /admin/workspace/exam/{exam}/grade` `{"sessions": [...]}` | queue the latest submission of the exam's students, or of those named (none named is all): `202 {queued: [run…], skipped: [{session_id, account_number, reason}]}`; 409 when the exam has no grading folder |
| `POST /admin/workspace/session/{session}/grade` `{"submission": id}` | queue one student's latest submission (or the one named): `202 {grading: run}`; 409 with nothing submitted, or a run already waiting or going |
| `GET /admin/workspace/session/{session}/gradings` | the student's runs, newest first, with their results (not what they printed), and the exam's TODOs |
| `GET /admin/workspace/grading/{id}` | one run with what it printed |
| `POST /admin/workspace/grading/{id}/confirm`, `.../unconfirm` | confirm a finished run as the student's grade, or take it back; 409 for a run that is not done |

## What is shown and what is only text

Everything a student's workspace holds is untrusted text to the proctor application: file names, file contents, transcripts, TODO identifiers and event
details are drawn as text (the transcript by xterm.js), never as markup, and a test puts a script and an image tag in a file name and a file's content to prove
it. A transcript that does not start as an asciinema v2 file is refused with a message.
