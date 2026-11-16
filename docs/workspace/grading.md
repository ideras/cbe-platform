# Grading workspace exams

Phase 6. The exam's hidden tests (`grading/grade`, `docs/workspace-format.md`) are run on a student's submission in a fresh container, and the
proctor reads what each TODO scored and confirms a grade. The student never sees any of it.

## The flow

```
proctor app ──"grade the class"──▶ exam server ──queue──▶ grader (a few workers) ──grading.run──▶ cbe-runner ──▶ a fresh container
   ▲                                    │ migration 022: runs and their results                                     │ no network, /work = the submission,
   └── the roster's grade, the Grading tab, "confirm" ◀──────────────────────────┘                                  │ /grading = the bundle, read-only
```

1. The proctor asks for **one student** (the review window's Grading tab: their latest submission, or one they pick) or **the class** (the monitoring
   window's *Grade submissions…*: the latest submission of every student who has one, except those already waiting or being graded). Each is a **run**: a
   row in `workspace_gradings`, `queued`.
2. A worker takes it (`-workspace-grade-parallel`, 2 by default), marks it `running`, and sends the runner the submission (a tar of the files the exam
   submits, written from the blob store as it is read) and the exam's grading bundle.
3. **The runner** (`internal/runner/grading.go`) checks both archives, writes the bundle in a folder of its own, makes the student's quota folder for
   `/work`, creates a container, copies the submission in, **attaches to its output**, starts it, waits at most the exam's `grade_timeout`, and returns how
   it ended and what it printed. Whatever happens, it then removes the container, the folder and the bundle. At most `grade_parallel` run at once; others wait.
4. The exam server reads the result (`ParseGrade`, below), records it, writes `grading.finished` in the student's history, and the roster shows the grade.
5. A **grade is advisory** until the proctor **confirms** a run. Confirming one run of a student takes the confirmation from another run of theirs; a run
   that did not finish cannot be confirmed. A student can be graded again at any time; every run is kept.

## The grading container

Made from the exam's image (its own image when the package has `image/`, `images.md`: the hidden tests may need the library the exam added), by the same function as a student's container (`hardened` in `internal/runner/policy.go`), so a grading run cannot be given what a student's cannot:
the student user (1000), **no network**, a read-only root file system, `/tmp` without exec, no capabilities, no new privileges, 256 processes, no swap, and
the exam's own limits of memory and processor. What differs:

- exactly two mounts: **`/work`**, a fresh quota folder holding the submission (what a container cannot be given, such as a link that leaves the project, is left
  out and named in the result), and **`/grading`**, the exam's bundle, **read-only**, from a folder only the runner can enter;
- the program is `/grading/grade`, in place of the agent, with `HOME=/work` and `CBE_GRADING=1`;
- it has no `cbe.managed` label (the runner does not take it for a student's session), and a `cbe.role=grading` one.

A student's container has no `/grading` and the bundle is never in a snapshot, a submission or any answer to a student or a proctor (tests of each, and in a
real container: `deploy/workspace/test-grading.sh`).

## What `grade` must do

Said to teachers in `docs/workspace-format.md` ("The `grade` program"). In short: **standard output is one JSON object and nothing else**, `grade` ends with
exit code 0, and each TODO's value has `passed` (required), `points` and `log`. The server fails a run, with the reason and what it printed, when:

| Failure | When |
|---|---|
| `malformed` | the output is empty, is not one JSON object, has anything before or after it, or a result has no `passed`, or points that are not a number of zero or more |
| `timeout` | `grade` did not end within the exam's `grade_timeout` (default 5 minutes) and was stopped |
| `oom` | the runtime says it was killed for memory |
| `crash` | it ended with an exit code other than 0, or was killed (exit code 137, which most often is the memory limit: this runtime does not always say so) |
| `runner` | the runner could not run it (it did not answer, refused the request, the image is gone) |
| `interrupted` | the exam server stopped while the run was waiting or going; it is marked at the next start, and the proctor runs it again |

A failed run **never holds up another student's**: the runs are independent, and every one ends in `done` or `failed`.

## What is kept

`workspace_gradings` (the run's state, failure, times, exit code, what it printed up to 256 KiB of each stream, points and the points the TODOs are worth, and
whether it is confirmed) and `workspace_grading_results` (for each key the output had: whether it is a TODO of the exam, passed, points, and the log up to 8 KiB).
The history of the student has `grading.finished`, `grading.confirmed` and `grading.unconfirmed`.

## Limits and known limits

| | |
|---|---|
| A submission | 64 MiB and at most the runner's file count (`inodes`); more is refused with a clear message |
| Runs at once | the lower of `-workspace-grade-parallel` and `grade_parallel` (2 each) |
| A run | the exam's `grade_timeout` (10 s to 30 min; the runner's `grade_max_seconds` is 30 min) |
| What a run prints | the first 256 KiB of each stream, the rest is read and dropped (a program that prints a great deal is not held up) |

- **The student's code runs in the container `grade` runs in**, so it can read `/grading` (the hidden cases). Grading measures; it is not a lock. A teacher who
  needs one runs the student's code under another user or in a second container: that is the grade program's business, and not built.
- A server stop interrupts the runs that were going (marked failed, run again); a runner stop does the same, and the runner removes what a stopped run left
  when it starts.
- Phase 6 does not grade at the end of the exam by itself: the proctor asks.

## The grade sheet (Phase 8)

What a teacher takes away of the grades: **Download grades…** in the monitoring window (and the download button of a workspace exam in the exam list) saves `GET /admin/workspace/exam/{exam}/grades.csv`
where the proctor chooses. The server makes it from the database; the application only keeps it (written beside its name first, so a failed download leaves no half file).

- **A line for every student of the exam's class**, sorted by name, also one who never logged in (`not logged in`) and one with nothing submitted (`no submission`).
- **The columns:** `account_number, name, email, status, points, advisory_points, max_points`, one column for each TODO (`t1 (10)`: its ID and what it is worth), `todos_ticked_by_student, submitted,
  final_submission, last_submission_at, graded_at`. Times are UTC, RFC 3339.
- **A grade you have not confirmed does not go in `points`.** The sheet keeps the two apart (P8-9): `points` is the confirmed run's, `advisory_points` the latest run's points of a student whose grade is
  not confirmed, and `status` says which. The TODO columns have the points of that same run. A run that failed or is going has no points.
- **UTF-8 with a byte order mark and CR LF line ends**, which Excel, LibreOffice and Google Sheets read without being told. A cell that begins with `=`, `+`, `-` or `@` (a name or an e-mail address from the
  roster) is written with a `'` in front, so that a spreadsheet does not take it for a formula.
- The student's own files and reports are not in it: they are in the review window (a zip and a Markdown report for each student).

Tests: `TestTheGradeSheetHasEveryStudentAndKeepsAdvisoryGradesApart` (the server, through its route and the real database), `TestTheGradeSheetIsSavedWhereTheProctorChoosesAndNowhereElse` (the application),
and the Playwright tests of the button.
