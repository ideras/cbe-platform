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

Made by the same function as a student's container (`hardened` in `internal/runner/policy.go`), so a grading run cannot be given what a student's cannot:
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
