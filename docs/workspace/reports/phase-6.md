# Phase 6 report: the grading runner

**Date:** 2026-11-14. **Branch:** `feat/workspace-phase-6-grading-runner` (from `feat/workspace-phase-5-submission-logging`, which is pushed). **Plan:** `.agents/WORKSPACE-PLAN.md` v1.1.
**Status:** built and tested; **all four acceptance items are met** (below), in real containers as well as with fakes. Decisions `P6-1` to `P6-13` are **confirmed by the owner (2026-11-14)**.
**The branch is not pushed.** How it works: `docs/workspace/grading.md`.

## What changed

| Path | What |
|------|------|
| `internal/runner/grading.go`, `api.go`, `client.go`, `config.go`, `policy.go`, `seed.go` | **`grading.run`** (`POST /v1/grading`, P6-1): a fresh container from the same hardening as a student's (`hardened`, shared with `BuildSpec`), the submission in `/work`, the bundle read-only at `/grading`, `/grading/grade` as the program, a timeout, bounded parallelism, output read from an attachment, everything removed after, and a sweep of what a stopped runner left (P6-2, P6-5, P6-7, P6-8, P6-10). The sanitizer has a lenient mode for what a student made. New settings `grade_root`, `grade_parallel`, `grade_max_seconds`, `grade_output_bytes`. |
| `internal/runner/runtime.go`, `dockerrt/docker.go` | The runtime gains `Wait` and `Attach` (a `Collector`), a read-only bind and an entrypoint/command/environment on the spec. |
| `internal/storage/migrations/022_workspace_grading.sql`, `internal/storage/workspace_grading.go` | `workspace_gradings` and `workspace_grading_results`; the roster query carries each student's grade. |
| `internal/workspace/grading.go`, `grading_admin.go`, `submission.go`, `snapshot.go`, `types.go`, `events.go` | The grader (queue, workers, `ParseGrade`, failure codes), the proctor's reads and actions, the submission as a tar, and the events (P6-3, P6-4, P6-6, P6-7). |
| `internal/httpapi/{workspace,router}.go`, `internal/server/run.go`, `internal/config/config.go` | Six admin routes, the grader in the server's life (`Recover`, `Run`), flags `-workspace-grade-parallel` and `-workspace-grade-timeout`. |
| `shared/examdoc/workspace*.go`, `internal/workspace/import.go` | The `grade_timeout` front-matter key (P6-3). |
| `testdata/workspace/lab-listas/grading/*`, `lab-listas-submissions/`, `lab-listas.json` | A real `grade` script with a hidden driver and cases; a good and a partial submission; the golden file and the compiler's count follow (P6-11). |
| `apps/proctor/internal/dashboard/grading.go` | The dashboard's Go side: `GradeExam`, `GradeStudent`, `WorkspaceGradings`, `WorkspaceGrading`, `ConfirmGrading`, with the roster's grade checked (P6-9). |
| `apps/proctor/frontend/src/{WorkspaceGrading,GradeDialog}.svelte`, `workspace.js`, `MonitoringRow`, `Monitoring`, `ReviewWindow`, `api.js` | The roster's grade, the Grading line and *Grade submissions…*, the Grading tab, the summary's Grade row (screens: `phase-6-grading-roster.png`, `phase-6-grading-tab.png`, made with the fake API). |
| `deploy/workspace/{install.sh,runner.json,test-grading.sh}`, `internal/workspace/e2e/grading_e2e_test.go` | The grading folder; **`test-grading.sh`**, the real test against a private runner (P6-12). |
| `docs/workspace/{grading.md,DECISIONS.md,deployment.md,proctor-view.md}`, `docs/workspace-format.md`, this report, `reports/phase-6-e2e.log`, `reports/phase-6-tests.log` | |

## Tests run and results

- **`scripts/ci/test.py --race` (vet and tests of the 7 modules): green** (`phase-6-tests.log`). New in it:
  - **the runner** (`grading_test.go`, 9): the container's hardening against a student's, exactly two mounts with `/grading` read-only, a student's spec with no `/grading`, the submission and the bundle where they should be and nothing left after, a timeout, a crash, a memory kill, output cut at the limit, requests refused before anything is made, what a container cannot be given left out and named, **forty runs with three at a time and no more**, a restarted runner removing what a stopped one left, and the same over the real API with the multipart request;
  - **the server** (`workspace_phase6_test.go`, 8, through the real routes and database): a student graded from their submission with the bundle in no answer and the stored submission unchanged, advisory and confirmed grades (one confirmation at a time, only a finished run), the five ways a run goes wrong and the next student still graded, the class with who was left out said, an exam with no grading folder, a student not queued twice, interrupted runs, reads of what is not there; `grading_test.go` (`ParseGrade` against 20 inputs; **forty submissions with a bound of four, four of the runs crashing**; a run interrupted by a stop); `grade_example_test.go` (the example's `grade` on the host: good 40, partial 16, starter 0, a submission that does not compile, one that crashes);
  - **the compiler** (`grade_timeout`, its bounds) and **the importer**; **the dashboard** (`grading_test.go`, 4).
- **The proctor's page:** unit 88 (3 new), **Playwright 320** (8 new: the roster's grade in each state, grading the class with its dialog and what was left out, nothing to grade, the Grading tab, confirming, grading a student with the end of the run shown by itself, a failed run, a student never graded), the workspace spec in WebKit 26/26. The student's app is untouched in this phase.
- **The real test, `deploy/workspace/test-grading.sh`, exit 0** (`phase-6-e2e.log`, about a minute): through the real server, the real runner's code and real containers of the course image:
  - the example's own `grade` scores the three students **40, 16 and 0 of 40** (about a second a run); `Download submitted files` is byte for byte the same after;
  - **the hidden files are in no student's container** (`ls /grading` fails, a search of `/work /home /tmp /run /usr/local` finds none of the marker) **and in no snapshot**;
  - **what a grading container is:** the student user, home `/work`, only its own submission visible (two runs at once each saw only theirs), `/grading` not writable or removable, the root not writable, `/tmp` not executable, **no network** (a connection to 1.1.1.1:53 fails; the only interface is `lo`), no capabilities (`CapEff` 0), `NoNewPrivs`;
  - a run that does not end is stopped at its timeout with what it printed; a program that grows past the limit is killed (exit 137); a child of `grade` that does too fails the run; a flood of 20 MB comes back cut at 256 KiB, flagged, and the run ends; a fork bomb does not bring the run or the host down; an exit code and its error text come back; a bundle with no `grade` and an image that is not approved are refused;
  - **forty runs of one second each took 30 s, never more than two at once** (the runner's setting); the host's free memory fell by 37 to 326 MiB at the lowest (49 GiB available) and its load did not move (2.0 before, 2.0 after);
  - **afterwards: no grading container, no bundle folder, no `/work` folder and no run note left** (the script checks and says so).

## Acceptance (plan, Phase 6)

- [x] **The example package's `grade` script scores a known-good and a known-bad submission correctly:** 40, 16 (partial) and 0 (the starter), on the host (a test) and in real containers (the real test).
- [x] **A submission that tries to read `/grading` before grading starts cannot:** a student's container has no `/grading` and none of the files (the real test reads from inside a student's terminal; the spec of a student's container has two mounts, neither of them the bundle; no snapshot, submission, proctor answer or student view has it, each tested).
- [x] **A grading run cannot change the stored submission:** the run works on a copy; the proctor's zip of the submission is the same bytes before and after, in the fake-runner test and in the real one, and the student's own files are untouched.
- [x] **Grading 40 submissions completes without exhausting the host, and runs with bounded parallelism:** measured above, in real containers, and in the unit tests (forty at once with the bound held).

## What the work found

1. **A container's logs, read when it has just ended, can be incomplete, and following them never ends.** The first real run came back with empty output for 4 of 40 small runs, and a 20 MB flood as 679 KB; `podman logs --follow` on an ended container did not return in three minutes. The runner now attaches to the output before it starts the container and reads the stream to its end (P6-8). This is also how Phase 7's build log should be read.
2. **This runtime does not always set the "killed for memory" flag**, even when the program the container runs is the one killed. A memory kill shows as exit code 137; the server says that is most likely the memory limit (P6-4).
3. **`StartExam` lost its "named students only" filter after the first student** (a test found it: the list of names emptied as students were met, and then everyone was queued). Fixed, with a test that names two of forty.
4. **A query cannot be made while another one's rows are open** (the database has one connection): the roster's grades are read before its rows are.
5. **Nothing is left behind** after any of it: the sweep, the removal after every run (also on timeout and on an error), and the checks in the real test.

## Deviations from the plan, and conflicts found (standing rule 1)

1. **`grade_timeout` is a new front-matter key** (the plan says the timeout is "configurable per exam" and its key table has none): P6-3.
2. **`grading.run` takes a multipart request and answers whatever the run did** (the plan's Appendix E: "returns the output"): P6-1.
3. **`grade` must exit 0**; the plan says only that it writes JSON to standard output: P6-4. A script that exits non-zero after printing a good JSON fails its runs, with the output shown.
4. **A student's submission is not refused for what a container cannot be given** (a link out of the project): it is left out and named (P6-5).
5. **The proctor's *Grade* column is a line in the Workspace column**, not a column of its own (the table has no room for another), as in Phase 5.
6. **Not done, the plan does not ask for it:** grading at the end of the exam by itself, a grade export, a way to change points by hand before confirming.

## Not run, and what is unverified

- **The installed runner and the exam server on `:8080` were not changed.** Your manual test server was running with its students' containers, and the other real tests restart the runtime's user manager, which would have taken them down. So the real test ran against a **private** runner built from this code (P6-12). To put grading on this host: re-run `deploy/workspace/install.sh` with the new `cbe-runner`, restart its unit (its containers keep running), and give the server the new binary (`docs/workspace/deployment.md`, "Upgrading"). The old runner answers `POST /v1/grading` with 501.
- **The proctor application was not launched for real** (it would connect to your live server): the page is tested against a fake of its calls, the Go calls against a fake server, and its dashboard package is vetted for Windows and macOS (`GOOS`), not run there.
- **A grade program that reads `/grading` from the student's own code** is possible (P6-13), by design of the plan.
- **A real class** (more than the forty simulated one-second runs, with compiles in each) and **a long session** are Phase 8's load test. The example's runs take about a second each; a course whose tests compile a large project will take longer, and the timeout and the parallelism are the settings for that.
- **A rootful Docker runtime** (the second one) is not run, as in Phase 3.

## Left behind

On this machine: nothing from the runs (checked by the script). The private runner's files are removed when the script ends. The Phase 0 resource backstop on `cbe-runner`'s slice **must still go before production**. The proctor application's bindings (`apps/proctor/frontend/bindings/`) are regenerated and git-ignored.

## Closed (2026-11-14)

The owner confirmed P6-1 to P6-13 as proposed, asked for the installed runner to be upgraded, the branch pushed and Phase 7 to start. The upgrade is in `docs/workspace/reports/phase-7.md`'s opening (it was done at the start of that turn).
