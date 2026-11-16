# Phase 8 report: hardening, load test and release

**Date:** 2026-11-15. **Branch:** `feat/workspace-phase-8-hardening-release` (from `feat/workspace-phase-7-image-customization`, which is pushed). **Plan:** `.agents/WORKSPACE-PLAN.md` v1.1.
**Status:** built and tested on this host. **All four acceptance items are met on the host**; what remains is the owner's (the router run, which needs a second device on their Wi-Fi). The owner's answers of 2026-11-15 are in: the production limits applied and the 30 minutes rerun, the installed runner upgraded, the Windows smoke test passed on their VM, decisions `P8-1` to `P8-8` confirmed, and two things the teacher guide lacked added (**the download of the grades**, and **a switch for the image warnings**). Decisions `P8-9` to `P8-14` are proposed (`DECISIONS.md`). **The branch is not pushed** (the owner said not yet, until the grade download was reviewed).

## What changed

| Path | What |
|------|------|
| `internal/httpapi/workspace_fuzz_test.go`, `apps/student/internal/client/workspace_fuzz_test.go`, `deploy/workspace/fuzz.sh` | **Four new fuzz targets** and the script that runs all eight for five minutes each (P8-1): the student's WebSocket through the real relay and the real agent, **a hostile agent** through the real relay, and the student application's two readers (what the server sends, what the page asks). |
| `internal/workspace/manager.go`, `relay.go`, `workspacetest/` | The relay's rate limit on control messages is an option (`Options.ControlRate`; the default is the old constant), and the fake runner can play a hostile agent (`SetConnectHook`). The httpapi fixtures take `testing.TB`. |
| `internal/runner/{policy,config,client,api,service}.go`, `deploy/workspace/runner.json` | **The memory budget** (P8-4): `host_memory_mb` (default 70% of the machine's memory); `container.create` answers 503 `ErrCapacity` when the limits of the containers that exist plus the new one would go over it. |
| `deploy/workspace/limits.sh`, `install.sh` | **The production limits of the containers' control group** (P8-3), replacing the spike's backstop: `CPUWeight=50`, `MemoryMax` from the budget, `TasksMax=12000`. **Written, tested by reading, not applied on this host** (below). `install.sh` runs it on a new one. |
| `internal/loadtest/`, `cmd/cbe-loadtest/`, `testdata/workspace/lab-carga/`, `internal/workspace/e2e/load_e2e_test.go`, `deploy/workspace/{test-load,private-runner}.sh` | **The load test** (P8-2): simulated students (login, folders, files, autosave, terminal echo, `make`, notes typed while it builds, storms, submit), the machine's readings every 10 s, the project (eight `-O2` units), the real-server phase and the script that runs it on a private runner. `cmd/cbe-loadtest` runs the same against any server, for the router. |
| `internal/runner/integration_test.go`, `deploy/workspace/test-security.sh` | **The security subtest in a real container**: links to `/etc`, through `/proc/self/root` and `/proc/1/root`, to `/`, upwards, to the agent's folder and a hard link; the archive of them; the container's mounts, privileges and the 20,000-file limit. |
| `internal/runner/surface_test.go`, `apps/student/internal/client/isolation_test.go`, `real_workspace_test.go` | The runner's routes are pinned to Appendix E; the student application links no container runtime and opens no socket of its own; the real client's run leaves nothing of the workspace on the student's disk. |
| `scripts/regression/` | **The question-exam regression** (P8-5): one scripted session, two server builds, the same record. |
| `apps/proctor/…/ReviewWindow.svelte`, `dashboard/workspace.go` | A workspace that **failed** says why in the review window (found while writing the guide: the reason was in the API and nowhere on the screen). |
| `internal/workspace/grades.go`, `internal/storage/workspace_grading.go`, `internal/httpapi/workspace.go`, `router.go`; `apps/proctor/…` (`Monitoring.svelte`, `ClassExams.svelte`, `ExamCard.svelte`, `dashboard/workspace.go`) | **The download of the grades** (P8-9): `GET /admin/workspace/exam/{id}/grades.csv`, a CSV with a line for every student of the class, the confirmed grade in `points` and an unconfirmed one in `advisory_points`, a column for each TODO; **Download grades…** in the monitoring window, and the download button of a workspace exam in the exam list (which opened the dialog of a question exam's answers). |
| `shared/examdoc`, `internal/workspace`, `internal/runner/image.go` | **`image_warnings: off`** (P8-10): the Containerfile checker's warnings are on by default and an exam can switch them off; the checker's problems never are. |
| `internal/storage/telemetry.go`, `proctor.go`, `workspace_submissions.go` | **The fix the load test found** (P8-13): the newest focus event of a session, and the other newest-event lookups, walked back through all of a session's events; they are seeks of the index now. |
| `internal/runner/service.go`, `dockerrt/docker.go` | **The runner makes the CPU percentage from two samples of its own** (P8-11): Podman's own gave 0.03% for a container using a whole core. |
| `cmd/server/pprof.go` (`-tags pprof`), `internal/loadtest` (the proctor's reads) | An opt-in profiler that a normal build does not contain, and the simulated class with the proctor's two reads going (P8-14). |
| `docs/workspace/TEACHER-GUIDE.md`, `load-test.md`, `security-checklist.md`, `windows-smoke-test.md`, `deployment.md`, `proctor-view.md` | The teacher guide; the load test's method; the security checklist with its tests; the Windows checklist for the owner; the new settings and scripts. |

## Tests run and results

| What | Result |
|---|---|
| **Fuzzing**, `deploy/workspace/fuzz.sh 5m`, eight targets, two at a time (`phase-8-fuzz.log`) | **No failure.** `FuzzDecodeRequest` 15.4 M inputs, `FuzzReadFrame` 11.2 M, `FuzzCheckPath` 12.5 M, `FuzzFilePaths` 1.9 M, **`FuzzStudentMessages` 353,000** (through the real relay, the real agent and the real database), **`FuzzHostileAgent` 135,000**, `FuzzWorkspaceServerMessage` 2.9 M, `FuzzWorkspacePageRequest` 2.9 M. Writing them found two faults of the *harness*, none of the code: the terminal recorder took the hostile agent's first connection and waited on it, and the first iteration took ten seconds for that reason. |
| **Question-exam regression**, `scripts/regression/qexam-diff.sh` | **Identical.** The server of `3861680` (where this branch left `main`) and this tree, one scripted session of 30 steps (1,449 lines of record: two logins, focus events, autosaves, a submission, a refused login, a forced submission, the proctor's routes), run twice on one build first to show the noise is none. |
| **Security**, `deploy/workspace/test-security.sh`, a real container, a private runner | **Passed.** 9 reads through links refused, the hard link not made, 7 links archived as links, the mount table (only `/work` on XFS and `/run/cbe` are the host's disk), no capability, no new privileges, seccomp, the 20,000-file limit (a student asked for 25,000 and got 19,979), and the runner's own CPU figure for a loop on the container's one core: 100%. |
| `images/test.sh cpp20` | **31 passed, 0 failed.** |
| `deploy/workspace/test-grading.sh`, `test-image.sh` (private runners, with this tree's runner) | **Passed**, nothing left behind. |
| **The load test**, 40 students for 30 minutes, four times (`reports/phase-8-load.md`): under the spike's backstop; under the production limits; after the first fix; **after all the fixes** | **40 of 40 students worked in every run, with no failure**; the last: autosave **p95 18 ms, p99 99 ms, max 276 ms** (below) |
| Two 15-minute runs with the server's CPU profiled (`reports/phase-8-profiles/`), before and after the fix | **40% of the server's CPU, late in a run, was one query** (below) |
| `scripts/ci/test.py --race` (every Go module, vet and tests), the proctor's Go tests with the race detector, its unit tests (91) and its Playwright suite (335), the regression diff | **All pass** after the last change (CI exit 0, 28 packages; Playwright 335 passed; the regression diff identical again). `phase-8-tests.log` has the earlier outputs; the last run is in `phase-8-tests-final.log`. New tests: the grade sheet (server, application, Playwright), `image_warnings` (compiler, runner, server), the newest-event queries (the same row as before, and the plan: a seek for each type), the runner's CPU figure (a unit test and a real container). |

### The load test (`reports/phase-8-load.md` says what each run was and found)

Forty students (`LT001` to `LT040`), a throwaway exam server of the tree's build, a private runner of the tree's build and rootless Podman on this machine (12 threads, 64 GiB, the owner's desktop on it), the project `lab-carga` (eight `-O2` units), logging in over 30 seconds; each opens files, **autosaves** (3 to 6 saves a second apart after each edit, and a note typed every 2 to 6 seconds whatever else is going on), types in a terminal and runs `make`; **everyone builds the whole project at once at 10 and at 20 minutes**; at the end everyone presses Save; **and the proctor's application's two reads (the sessions and the workspace roster) go every 3 seconds**, as a class has them open.

| Run | Conditions | Autosave p95 | p99 | max | The exam server's CPU, first to last 5 minutes |
|---|---|---:|---:|---:|---|
| 1 | the spike's backstop (CPU capped at 400% of 1,200) | 161 ms | 273 ms | **16.4 s** | 29% to 45% |
| 2 | the production limits (`limits.sh`) | 110 ms | 182 ms | 548 ms | **13% to 44%** |
| 3 | and the first fix (`LatestFocusEvent`, the roster's last activity), with the proctor's reads | 31 ms | 202 ms | 523 ms | 16% to 27% |
| **4** | **and the other newest-event lookups** | **18 ms** | **99 ms** | **276 ms** | **15% to 21%** |

**Run 4, the run of the phase** (`reports/phase-8-load-4-final/`, 2026-11-15 18:27 to 18:59, production limits, every fix):

| | Result |
|---|---|
| Logins, workspaces up | 40 of 40; **ready after login: p50 211 ms, p95 252 ms**, max 261 ms |
| **Autosave** (`fs.write`, from the request sent to its reply), **36,207** of them | **p50 2.7 ms, p95 18.2 ms, p99 98.7 ms, max 276 ms**; no failure; by minute, p95 between 5 and 54 ms except 111 ms in the minute of the second storm, **with no drift**; **the plan's 300 ms at p95 is met, by a factor of 16**, on the loopback |
| Open a file, a folder, a new file, a delete, a TODO tick | no failure; terminal echo p95 4.1 ms, p99 8.5 ms |
| Heartbeat (14,503 of them) | p95 14 ms, p99 30 ms |
| **The proctor's reads**, 599 of each | the sessions p50 101 ms, p95 207 ms; the workspace roster p50 67 ms, p95 215 ms; no failure |
| **Submit** (a snapshot of `/work`, all forty within seconds of each other) | 40 of 40; p50 0.6 s, p95 0.7 s |
| `make` of the whole project | forty at once: first build p50 78 s, p95 83 s; the two storms p50 78 s, p95 91 s; one unit after an edit p50 1.9 s, p95 3.1 s; **none failed** |
| The exam server | CPU 17.7% of one core on average (peak 24%), **32 MB** of memory |
| The runner | CPU 5.6% (peak 6.6%), 32 MB |
| The containers, all forty | memory **1.1 GB on average, 3.9 GB at most**; about 470 processes |
| The machine | 68% busy on average (100% in the storms), 48 GB of memory available all along |

**What it means.** With every container compiling, the exam held for half an hour with nothing failed, nothing lost and the editor's save at **2.7 ms at the median and 99 ms at the 99th percentile**; the server and the runner use a small part of one core each. The first run's `make` times, and its worst autosaves, were the cap's. **What is still not measured is the router** (the owner's, `load-test.md`), and **a slow creep of the server's CPU** that is left (15% to 21% over the half hour, about 0.2 points a minute, which at two hours would be about 40% of a core): not identified, and a profile of a run of that length (`CBE_LOAD_PROFILE=1`) is how to find it.

## Acceptance (plan, Phase 8)

- [x] **Load test meets the latency target, with the results recorded.** On the host (the loopback): **p95 18 ms, p99 99 ms** for 36,207 autosaves with 40 students, every core busy, and the proctor's application's reads going; results in `reports/phase-8-load-4-final/`, with the three runs before it. **The router** is the one thing not done: the plan's target is for the network the class uses, which for this deployment is the owner's Wi-Fi router (P0-25); `cbe-loadtest`, the package and the steps are ready (`load-test.md`, `build/phase8/`), and the run needs a second device on the Wi-Fi.
- [x] **Security checklist complete.** All ten checks have a test that runs and passed (`security-checklist.md`); three of them did not before this phase and now do (the runner's surface, the student application's isolation, the student's disk), and links, mounts, privileges and the file count are now checked in a real container. The half of check 9 about `userns-remap` does not apply to rootless Podman.
- [x] **Question-exam regression diff is empty** (again after the last change to the queries).
- [x] **Teacher guide reviewed by the owner.** They read it on 2026-11-15 and found two things missing, both added: the download of the grades, and a switch for the warnings (`image_warnings: off`).

Not in the plan's four, but in its tasks: **fuzzing** (task 2) done; **Windows build and smoke test** (task 6): the owner ran it on their Windows VM on 2026-11-15, **every step passed** (`windows-smoke-test.md`).

## What the work found

1. **A query of the lifecycle worker made the exam server slower the longer a class worked** (found by the load test's own numbers: the server's CPU rose from 13% to 44% of a core in half an hour at a constant load, and the autosave's p95 from 11 ms to 177 ms). A CPU profile of the server, early and late in a run (`reports/phase-8-profiles/`), put **40% of its time, late, in one query**: `LatestFocusEvent`, which the worker asks of every session every second. It was `event_type IN (…) ORDER BY id DESC LIMIT 1`, which SQLite plans as a walk back through the session's events until one matches, and a workspace session has thousands of file events and none of those: all of them were read, every second, for every student. Two others were the same (the proctor's `LatestReportedFocus` and the roster's last-activity subquery). All are a seek of the index for each type now (`newestEventID`); the result is the same row (a test compares them on thousands of random events, and the plan is checked). Autosave p95 went from 110 ms to 18 ms and p99 from 182 ms to 99 ms. A question exam, whose sessions have few events and focus events often, never saw it.
2. **The proctor's CPU figure was wrong on the real runtime** (found reading the load test's own numbers: forty containers each using a core added up to 30%). Rootless Podman gives no previous sample (`precpu_stats` is empty), so the runner's division gave **0.03% for a container using a whole core**. Fixed: the runner keeps the last sample of each container and gives the percentage between two samples at least a second apart (`CPUKnown` says there was one; the roster shows memory alone for the first reading). Checked in a real container (a loop on its one core: 100%) and by a unit test. The fake could not have shown it.
3. **A workspace that failed did not say why.** The reason was in the API (`failure`) and nowhere on the proctor's screen: the review window's Container row shows it now (as text, with a test that a `<b>` in it is not markup).
4. **The exam list's download button of a workspace exam opened the dialog of a question exam's answers**, which a workspace exam has none of: it is the grade sheet's download now (the owner found the grades could not be taken away).
5. **The memory budget did not exist** (Discovery proposed it in Phase 0 and no phase built it): forty students at 2 GiB would have been 80 GiB of limits on a machine of 64. Built (P8-4).
6. **The spike's backstop made the control path slow, and the production limits fix it**: under the cap (CPU held to 400% of 1,200 for the containers, the runner that relays every request to them and the runtime) the slowest autosave took 16.4 s and a `make` of the whole project 7 to 10 minutes; without it, 548 ms and 77 to 117 s. The server's own handling of a reply was never the delay (a run that logged any reply taking over 500 ms between the agent's answer and the student logged none).
7. **The generator had three faults, fixed:** a write in flight when a context is cancelled closes the WebSocket (three students' final saves failed, in the seven-minute run, that way); two goroutines of a student shared a random-number generator (a data race, found by the CI's `-race` after the runs); and the exit status of the load test's script was swallowed in my own invocation. None touches what the server was asked or how long it took.
8. **Rootless Podman keeps the agent's folder on whatever file system `/data/cbe-platform/run` is on** (btrfs here) and `/work` on the XFS volume: the test reads the mount table and only demands that they are disks, not tmpfs or overlay.
9. **A container that starts creating files stops at 19,979 of 25,000**: the limit is 20,000 inodes of the project quota, and the agent's own files in `/work` count (a couple).
10. **A slow creep of the server's CPU is left** (15% to 21% over the half hour in the last run): not identified (it may be the proctor's reads, which cost 70 to 100 ms each and are made every 3 seconds, or the growth of the history table). At two hours it would be about 40% of a core; the profile (`CBE_LOAD_PROFILE=1`, a two-hour run) is how to find it.

## Deviations from the plan, and conflicts found (standing rule 1)

- **The plan says "40 simulated students each ... running `make` in a loop"**: it is a loop of stretches of work (an edit, saves, a build, a pause), **with a typist alongside**, because a student who is waiting for a build would otherwise stop saving and the autosave would not be measured when the machine is busiest.
- **The router test and the "pull the network for 30 seconds" test**: the second passed on the owner's Windows VM (step 9 of its smoke test); the first needs a second device on the owner's Wi-Fi, and the tooling is built (`cmd/cbe-loadtest`).
- **Production code changed in this phase beyond the plan**, each with a test: the runner's memory budget and CPU figure, the failure reason on the proctor's screen, the relay's control rate as an option and a warning when a reply takes over half a second, the newest-event queries, and the two features the owner asked for (the grade sheet, `image_warnings`).
- **The download of grades was not in the plan**: the plan has the proctor "read per-TODO results and confirm grades" and nothing to take them away. P8-9.

## Not run, and what is unverified

- **The router**: the autosave p95 over the owner's Wi-Fi.
- **A class of two hours**: the longest run is 30 minutes. The server's CPU creeps (finding 10), and a student's `/work` and the history grow for longer than that.
- **macOS** (deferred by the owner), **rootful Docker** (not the chosen runtime), **a real reboot during an exam** (Phase 3 simulated it and a real one passed in Phase 0; not repeated with a class).
- **Heartbeats, screenshots and focus events of the monitoring** are only partly simulated by the load test (the heartbeats are; the screenshots and the focus events are not): a class of 40 on a question exam is the load they were built against, and they come on top of this.
- **A class whose build is much heavier than `lab-carga`**, and **a student whose program fills the 20,000 files while 39 compile**: the budget and the limits are what is there for them.
- **The student application on a slow machine** (the IDE's own latency in WebView2): the owner's Windows VM was not slow.

## Left behind

Nothing from the runs: the scripts remove their containers, folders and private runners and report what they find; the runtime holds the course image and the spike's three, and `snapprobe`. The image-build tests leave untagged layers (`<none>:<none>`); I removed 17 of them with `podman image prune` after looking that all were from that day and none had a child. **On the host, changed with the owner's go:** the production limits (`/etc/systemd/system/user-1001.slice.d/cbe-limits.conf`; the spike's drop-in is `/etc/cbe-platform/spike-backstop.conf.removed`) and the installed runner (the Phase 8 build; the Phase 7 one is `/usr/local/bin/cbe-runner.phase7`). In `build/phase8/` (new files, nothing of the owner's overwritten): the Windows student application, `cbe-loadtest` for Linux and Windows and the compiled `lab-carga` package.

## For the owner

1. **The router run** (`load-test.md`): when the router is on and a laptop is on its Wi-Fi, give it `cbe-loadtest` (or `.exe`) and the `lab-carga` folder from `build/phase8/`; on this machine, `deploy/workspace/test-server.sh --manual 0.0.0.0:8090`. Tell me when it is up and I will start the server side and give you the address and the one command to run on the laptop.
2. **Confirm or change P8-9 to P8-14** (`DECISIONS.md`): the grade sheet (two columns of points, so that an unconfirmed grade does not go into `points`), `image_warnings`, the CPU figure, the failure reason, the newest-event fix, the profiler.
3. **Look at the grade sheet**: monitoring window → **Download grades…**, or the download button of the exam in the list. `grading.md` ("The grade sheet") says each column; if you want another layout (one column of points, the TODO texts in the header, names last, Canvas's import format), say so.
4. **May I push the Phase 8 branch?** And do you want **one pull request** to `main` for the whole workspace work (Phases 0 to 8 are stacked branches, all pushed except this one, and no pull request has been opened)?
