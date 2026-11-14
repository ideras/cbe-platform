# Phase 5 report: submission, logging and the proctor's views

**Date:** 2026-11-13. **Branch:** `feat/workspace-phase-5-submission-logging` (from `feat/workspace-phase-4-student-ide`, which is pushed). **Plan:** `.agents/WORKSPACE-PLAN.md` v1.1.
**Status:** built and tested; **all six acceptance items are met** (listed below). Decisions `P5-1` to `P5-11` are proposed and wait for you, and the student notice's wording is
yours to review. Phase 4 is still not ticked in Appendix D (its manual focus-loss script still waits for you; you allowed starting Phase 5 before it), so Phase 5 is
ticked there with that noted. **The branch is not pushed.**

## What changed

| Path | What |
|------|------|
| `internal/storage/migrations/021_workspace_submissions.sql` | Tables `workspace_submissions` and `workspace_terminals`; the snapshot table is rebuilt to allow the reason `final`. |
| `internal/workspace/glob.go`, `testdata/workspace/glob-cases.json` | The `submit`/`ignore` matcher, and the one table of cases that it and the importer's checker (`shared/examdoc`) both run (P5-1). |
| `internal/workspace/submission.go` | `Manager.Submit(session, kind)`: a snapshot, then the manifest of the files the exam submits. The session's end now makes a `final` one (P5-1, P5-2). |
| `internal/workspace/events.go`, `relay.go` | The workspace's events, written by the server from what it saw (P5-3); `paste.blocked` from the page, checked and rate-limited (P5-5). |
| `internal/workspace/recorder.go`, `cast.go` | The server's own connection to each container's agent that records every terminal as an asciinema v2 file, and the writer (P5-6). |
| `internal/workspace/admin.go`, `report.go`, `htmlmd.go` | What the proctor reads: roster, trees, files, zip, Markdown report, transcripts (P5-7, P5-9). |
| `internal/storage/workspace_submissions.go`, `internal/httpapi/{router,workspace}.go` | The storage and the nine admin routes (`docs/workspace/proctor-view.md`). |
| `apps/student/frontend/src/workspace/` | "Finish exam…" with its confirmation (P5-4); the paste guard tells the server which target and how large (P5-5). |
| `apps/proctor/internal/dashboard/workspace.go`, `service.go`, `api.go` | The proctor application's Go side: the roster alongside the sessions, the files, the transcript, and the two saves (P5-7, P5-9). |
| `apps/proctor/frontend/src/{workspace.js,WorkspaceReview,TranscriptPlayer}.svelte`, `Monitoring*.svelte`, `ReviewWindow.svelte`, `history.js`, `HistoryTable.svelte` | The Workspace column, the review panel, the replay, the history's words and filter (P5-8, P5-10). `@xterm/xterm` 6.0.0 is a new dependency of the proctor app. |
| `docs/tools/STUDENT-NOTICE.md` | The workspace notice (P5-11). **Yours to review.** |
| `docs/workspace/{DECISIONS.md,proctor-view.md}`, this report, `reports/phase-5-e2e.log`, `reports/phase-5-tests.log` | |

## Tests run and results

- **The server's whole flow (`internal/httpapi/workspace_phase5_test.go`):** a student's session through the real router and the manager, with a fake runner, in the order the plan lists: the
  history's sequence (opened, a file saved, created, renamed, deleted, a terminal opened and closed, a run with its exit code, a TODO ticked, a paste blocked, a Submit, the disconnection, the
  final submission), the final submission at the deadline for a student who never pressed Submit, `ignore` and `submit` applied (including a pattern that ignores what `submit` names), the latest submission
  counting, the early finish through `/submit` (the supervisor freezes the container within 2 s with a `final` snapshot), `paste.blocked` refused with unknown fields or a bad target and limited to a burst of 5,
  the roster's TODO progress, and the viewer, the zip and the report matching the manifest. Also: transcripts replay what the terminal printed, including what it printed **while the student was
  disconnected** and for a terminal that opened and ended between two looks of the recorder.
- **The glob table:** 1 table of cases, run by the server's matcher and by the importer's checker, which would disagree if either changed.
- **`castWriter` (`cast_test.go`):** the header, UTF-8 characters split across writes, the 4 MiB cut and its marker, and `ReplayCast`.
- **The proctor application's Go side (`workspace_test.go`):** the roster probe remembered for a question exam, trees and files through the application's calls, the transcript refused when it is not asciinema v2 or
  is too large, and the two saves (a cancelled dialog saves nothing; the file is written to a temporary name and renamed, mode 0600).
- **For real (`deploy/workspace/test-server.sh`, phase D, `reports/phase-5-e2e.log`):** the student application's own Go client, against the real server and a real container, finishes the example package and
  finishes the exam (`Submit({}, 0)`); then the rig reads it as the proctor does: two submissions, the last final; snapshots ending with `final`; the state frozen; the snapshot has the build products, the
  submission has not (`build/**`, `*.o`, and a root-level file the exam does not submit); the zip equals the submission; the report has the instructions, the ticks said to be self-reported, the files, and none
  of the grading bundle; the transcripts (the first Check failing, then `push_front: ok`; a Run printing `1 2 3`; the shell with the file the student made in it); the history's sequence and counts; the runs' exit
  codes (`check 2`, `check 0`, `run 0`); and the roster at 3 of 3 final. Phases A to C of Phase 3 pass as before. Thirteen runs of this script were needed while building it (see the last item).
- **The proctor's page (`tests/ui/workspace.spec.js`, 12 tests; `tests/workspace.test.js`, 9; `tests/history.test.js`, 2 more):** the column with the three states of a student and "self-reported", the
  TODO progress updating as the roster is read again, no column on a question exam, the review panel (ticks as the student's word, submissions), the files of a snapshot and of a submission (build products only
  in the snapshot), a file's text drawn as text with a script and an image tag in a **file name** and in a **file's contents** (nothing runs, no element appears), a binary and a large file described, the saves,
  a transcript replayed (played at 10×, a long pause shortened, sought, shown whole, paused, closed, the plain text without control sequences), a transcript that is not one refused, and the history's words
  and filter.
- **Student application:** `Workspace.svelte`'s finish flow (the dialog's safe button focused, Escape, saving first, nothing finished when a file cannot be saved), the paste event's fields, and axe on the
  finish dialog: no critical or serious finding.
- **Baseline (`reports/phase-5-tests.log`):** `scripts/ci/test.py --race` over the 7 modules passes; student unit **95**; proctor unit **85** (74 + 11); student Playwright **158** (156 + 2);
  proctor Playwright **306** (294 + 12). `gofmt` is clean; `go vet` is clean for `GOOS=windows` and `GOOS=darwin` on the packages the phase touched.
- **Do the tests catch what they claim?** The real run found two real faults: the relay's read from the agent did not stop when the student's connection ended (fixed with
  `context.AfterFunc`), and the final snapshot's reason was decided by the session's status, which the server's own worker may already have changed when time ran out (now decided by the clock, P5-2).
  The other failures of the rig's thirteen runs were my wrong expectations, not the code's: `make` exits 2 when a recipe fails (the rig expected 1), the report check looked for a build path that the report
  rightly quotes (the Makefile it includes names it), a file made in the terminal's root is not one of the example package's `submit` patterns, and a size argument to `zip.NewReader`. The proctor's page tests were written against what the page
  must do, and failed first for reasons of their own (a missing space in "1 submission · last", the review window's address, data that arrived after the window's first read); the page was fixed, not the tests.

## Acceptance (plan, Phase 5)

- [x] **The integration test produces the expected event sequence for a full session.** `workspace_phase5_test.go` (fake runner) and phase D (real).
- [x] **A student who never presses Submit still has a submission at the deadline.** Both tests; the reason is `deadline` (P5-2).
- [x] **Files matching `ignore` are not in the submission (test).** Both tests, and the glob table.
- [x] **A terminal transcript replays correctly.** The server's tests replay the file, the proctor's page plays it back and the plain text has no control sequences; the real run's transcripts contain what the student's programs printed.
- [x] **The proctor sees TODO progress update live, labeled "self-reported".** The roster is read again every few seconds; the page test changes it and sees the column follow; the label is in the heading, on every row and in the review panel (P5-8). Nothing checks the ticks.
- [x] **Downloads open and match the snapshot.** The zip is compared with the submission's manifest (every file, content and mode) and the report is built from the same manifest, in the server's test and in the real run. **Not checked: opening them in Word-like tools or on Windows**; the zip is plain deflate and the report plain Markdown.

## What the work found, and what Phase 6 must know

1. **A submission is a manifest of a snapshot** (P5-1), so Phase 6 copies a submission into a fresh container by taking the manifest's files from the blob store, not by reading a student's folder.
2. **A session's end is decided by two parties**: the supervisor and the server's own worker, which marks the session submitted at the deadline. Anything that wants to know "did time run out" has to read the clock.
3. **`run.started` is written when a run ends** (it carries the exit code the plan asks for), so a long run shows in the history at its end. The name is the plan's.
4. **The recorder is a second client of the agent**, and the agent keeps only the last 64 KiB of each terminal. A program that prints more than that before the recorder looks (the recorder looks every second) loses its beginning. The transcript says when its own connection dropped (P5-6).
5. **What a student types in a terminal is in its transcript**, because the terminal shows it; a password is not, because it is not shown. The notice says so (P5-11).
6. **The roster's CPU and memory are the last sample** (every 10 s) and say "not measured" for a container that is not running.
7. **`P3-14` is now closed:** a snapshot is unfiltered (it keeps what the container held, build products too), a submission is filtered.

## Deviations from the plan, and conflicts found (standing rule 1)

1. **Events have no `ts` (P5-3):** the event record already has its time, and a time the student's computer chose is not evidence.
2. **One "Workspace" column in the monitoring table, not four** (the plan: container state, last activity, CPU, TODO progress). Four columns would not fit the table at the width it has (it already has five); the one column has all four, and the submissions. The table gets wider on a workspace exam and scrolls inside its own frame, as it did.
3. **The viewer shows a snapshot or a submission, chosen by the proctor** (the plan: the current snapshot). The newest snapshot is the default.
4. **Transcripts are stored next to the snapshots under `<workspace-dir>/transcripts`** (the plan: "stored with the snapshots"), as files with a row each in the database, so the proctor's routes can find them and a purge of the exam can remove them.
5. **A student can end the exam early (P5-4),** which the plan's Phase 5 does not mention: your answer to P4-11.
6. **No bulk "download everything for the class"** (it was a stretch idea of mine). The plan asks for downloads per student, and they exist.

## Not run, and what is unverified

- **The proctor application itself was not launched** (it would connect to your live server at :8080, see the notes of earlier phases). The page was tested in Chromium (and WebKit, see below) against a stand-in for the application's calls, and the Go calls were tested in the application's package; **nobody has driven the real window** for the roster column, the review panel, the replay or the two save dialogs, on Linux, Windows or macOS. Windows and macOS were checked to compile and vet (`GOOS=windows`, `GOOS=darwin`), no more.
- **The student notice has not been read by a student.** The wording is mine.
- **The manual focus-loss script of Phase 4** (it now also covers using the IDE, not the new finish dialog, which is the same window).
- **A very long session.** The 4 MiB transcript limit and the 64 KiB agent buffer are tested at their edges; a whole exam's worth of terminals (many students, hours) is Phase 8's load test.
- **Real Wi-Fi.** As before, Phase 8.

## Left behind

On this machine: nothing new from the end-to-end runs (checked: no containers, folders or processes). The generated bindings of the proctor application (`apps/proctor/frontend/bindings/`) are regenerated and git-ignored. The Phase 0 resource backstop on `cbe-runner`'s slice **must still go before production**.

## For the owner

1. **Confirm or change P5-1 to P5-11, and P4-2 to P4-10 and P4-12**, which are still proposed: first **P5-4** (the finish dialog's wording is mine; the screenshots are in `docs/workspace/reports/phase-5-screens/`), **P5-6** (what a transcript holds, and that it holds what a student types where the terminal shows it) and **P5-11**.
2. **Read `docs/tools/STUDENT-NOTICE.md`'s new section.** It says what is really recorded; if you want less recorded, the code must change, not only the words.
3. **Run the focus-loss script** (`docs/tools/TEST-FOCUS.md`, "The workspace IDE") on Linux Mint and on the Windows VM. Steps are in my message.
4. **May I push the branch?** And **go for Phase 6** (the grading runner).
