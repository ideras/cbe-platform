# Phase 8 report: hardening, load test and release

**Date:** 2026-11-15. **Branch:** `feat/workspace-phase-8-hardening-release` (from `feat/workspace-phase-7-image-customization`, which is pushed). **Plan:** `.agents/WORKSPACE-PLAN.md` v1.1.
**Status:** built and tested on this host. **Three of the four acceptance items are met; two things are open and are the owner's:** the **limits of the containers' control group** were written and not applied (the auto-mode check refused `sudo limits.sh`, a system-wide change, and it is not made without the owner's say-so), so **the load test ran under the spike's backstop** (CPU capped at 400% of 1,200); and the **router** and **Windows** runs need the owner's router and VM. The teacher guide waits for the owner's review. Decisions `P8-1` to `P8-8` are **proposed** (`DECISIONS.md`). **The branch is not pushed.**

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
| `docs/workspace/TEACHER-GUIDE.md`, `load-test.md`, `security-checklist.md`, `windows-smoke-test.md`, `deployment.md`, `proctor-view.md` | The teacher guide; the load test's method; the security checklist with its tests; the Windows checklist for the owner; the new settings and scripts. |

## Tests run and results

| What | Result |
|---|---|
| **Fuzzing**, `deploy/workspace/fuzz.sh 5m`, eight targets, two at a time (`phase-8-fuzz.log`) | **No failure.** `FuzzDecodeRequest` 15.4 M inputs, `FuzzReadFrame` 11.2 M, `FuzzCheckPath` 12.5 M, `FuzzFilePaths` 1.9 M, **`FuzzStudentMessages` 353,000** (through the real relay, the real agent and the real database), **`FuzzHostileAgent` 135,000**, `FuzzWorkspaceServerMessage` 2.9 M, `FuzzWorkspacePageRequest` 2.9 M. Writing them found two faults of the *harness*, none of the code: the terminal recorder took the hostile agent's first connection and waited on it, and the first iteration took ten seconds for that reason. |
| **Question-exam regression**, `scripts/regression/qexam-diff.sh` | **Identical.** The server of `3861680` (where this branch left `main`) and this tree, one scripted session of 30 steps (1,449 lines of record: two logins, focus events, autosaves, a submission, a refused login, a forced submission, the proctor's routes), run twice on one build first to show the noise is none. |
| **Security**, `deploy/workspace/test-security.sh`, a real container, a private runner | **Passed.** 9 reads through links refused, the hard link not made, 7 links archived as links, the mount table (only `/work` on XFS and `/run/cbe` are the host's disk), no capability, no new privileges, seccomp, the 20,000-file limit (a student asked for 25,000 and got 19,979), and the runner's own CPU figure for a loop on the container's one core: 100%. |
| `images/test.sh cpp20` | **31 passed, 0 failed.** |
| `deploy/workspace/test-grading.sh`, `test-image.sh` (private runners, with this tree's runner) | **Passed**, nothing left behind. |
| **The load test**, 40 students for 30 minutes, `deploy/workspace/test-load.sh` | **40 of 40 students worked for 30 minutes** (below). |
| The same, seven minutes, with the server logging any reply that took over 500 ms | **Not one** (below). |
| `scripts/ci/test.py --race` (every Go module), proctor and student suites | `scripts/ci/test.py --race` (every Go module, vet and tests): **exit 0**, 28 packages `ok` (its first run failed on **a data race in my load generator**, two goroutines of a student sharing a random-number generator; fixed). Proctor: Go tests with the race detector ok, **unit 91, Playwright 332 passed** (331 and the new test of a failed workspace's reason). Student: Go tests with the race detector ok; the student's page was not changed in this phase and its UI suites were not run. `phase-8-tests.log` has the outputs. |

### The load test (`reports/phase-8-load/`, 2026-11-15 11:01 to 11:33, under the spike's backstop)

Forty students (accounts `LT001` to `LT040`) on a throwaway exam server, the runner of this tree and rootless Podman on this machine (12 threads, 64 GiB), the project `lab-carga` (eight `-O2` units), logging in over 30 seconds; each opens files, **autosaves** (3 to 6 saves a second apart after each edit, and a note typed every 2 to 6 seconds whatever else is going on), types in a terminal and runs `make`; at 10 and 20 minutes **everyone builds the whole project at once**; at the end everyone presses Save.

| | Result |
|---|---|
| Logins, workspaces up | 40 of 40; **workspace ready after login: p50 340 ms, p95 898 ms, max 949 ms**; login p95 6.7 ms |
| **Autosave** (`fs.write`, from the request sent to its reply), **17,938** of them | **p50 51 ms, p95 161 ms, p99 273 ms**, no failure; by minute, p95 between 112 and 184 ms with no drift; **the plan's 300 ms at p95 is met on the loopback**. The slowest were **16.4 s** (minute 17) and a few more over a second (minute maxima 7.6 s, 9.3 s, 10 s, 11.6 s), see "What the work found" |
| Open a file, a folder, a new file, a delete, a TODO tick | no failure; p95 75, 15, 93, 85 and 27 ms |
| Terminal echo | p50 34 ms, p95 160 ms, p99 279 ms |
| Heartbeat (14,527 of them) | p95 99 ms |
| **Submit** (a snapshot of `/work`, all forty within seconds of each other) | 40 of 40; p50 3.6 s, p95 4.1 s |
| `make` of the whole project | **Not a capacity figure** (below): under the cap, forty at once took 7 to 10 minutes or did not end in the ten the test waits (24 of 40 first builds and 34 of 48 storm builds); one unit after an edit: p50 78 s. |
| The exam server | CPU 36% of one core on average (peak 49%), **32 MB** of memory |
| The runner | CPU 14% (peak 18%), 33 MB |
| The containers, all forty | memory **3.1 GB on average, 3.7 GB at most** (a limit of 1 GiB each would allow 40 GB), about 500 processes |
| The machine | 41% busy (the cap holds the slice to 400% of 1,200), CPU pressure 96 to 99%, load average 31 on average and 73 at most, **46 GB available all along** |

**What it means.** The *shape* of the exam held for half an hour with every container compiling: nothing failed, nothing was lost, the server and the runner used a small part of one core each, and the
autosave stayed under the plan's target at the 95th percentile (and, narrowly, at the 99th) with the machine as busy as it can be made. **What it does not show** is capacity without the cap: the cap, not the hardware, set
the `make` times, as Discovery found for its own first run (132 s against 30 s). The number to expect without the cap is Discovery's: forty compiles of a small program at once took 30 s on eight threads; this project
costs 14 CPU-seconds a build, so about 80 to 140 s for forty at once on twelve.


## Acceptance (plan, Phase 8)

- [~] **Load test meets the latency target, with the results recorded.** **On the host (the loopback), yes:** p95 161 ms and p99 273 ms for 17,938 autosaves with 40 students and every core of the slice busy, and the results are in `reports/phase-8-load/`. **Not yet on the router:** the plan's target is for the network the class uses, which for this deployment is the owner's Wi-Fi router (P0-25), and the run through it is the owner's (`load-test.md` says how: `cbe-loadtest` from another device). **And the run was under the backstop**, which is a harder condition for the control path than the production limits, but is not the production configuration.
- [x] **Security checklist complete.** All ten checks have a test that runs and passed (`security-checklist.md`); three of them did not before this phase and now do (the runner's surface, the student application's isolation, the student's disk), and links, mounts, privileges and the file count are now checked in a real container. The half of check 9 about `userns-remap` does not apply to rootless Podman.
- [x] **Question-exam regression diff is empty.**
- [ ] **Teacher guide reviewed by the owner.** `TEACHER-GUIDE.md` is written; it waits for the owner.

Not in the plan's four, but in its tasks: **fuzzing** (task 2) done; **Windows build and smoke test** (task 6): the build and `go vet` for Windows pass here, and the test is the owner's on their VM (`windows-smoke-test.md`).

## What the work found

1. **The proctor's CPU figure was wrong on the real runtime** (found reading the load test's own numbers: forty containers each using a core added up to 30%). Rootless Podman gives no previous sample (`precpu_stats` is empty), so the runner's division gave **0.03% for a container using a whole core**. Fixed: the runner keeps the last sample of each container and gives the percentage between two samples at least a second apart (`CPUKnown` says there was one; the roster shows memory alone for the first reading). Checked in a real container (a loop on its one core: 100%) and by a unit test. The runner's figure and the exam server's were fine in the fake; no test could have found it.
2. **A workspace that failed did not say why.** The reason was in the API (`failure`) and nowhere on the proctor's screen: the guide was going to say "read the reason" and there was none to read. The review window's Container row shows it now (as text, with a test that a `<b>` in it is not markup).
3. **The memory budget did not exist** (Discovery proposed it in Phase 0 and no phase built it): forty students at 2 GiB would have been 80 GiB of limits on a machine of 64. Built (P8-4).
4. **The spike's backstop makes the control path slow**, which is the likeliest reason for the slowest autosaves. The slice (the containers, **the runner that relays every request to them**, the runtime) is held to 400% of 1,200 and every process in it waits for the next 100 ms period when the quota is gone, with a load average of 31 to 73. The server's own handling is not it: **a 7-minute rerun that logged any reply taking over 500 ms between the agent's answer and the student (the history's database write and the send) logged none**, while the slowest autosave of that run was 6.9 s. So the time went before the server got the reply (the runner, the agent, the container's own one-core quota, the volume's disk), or in the generator (it is outside the slice and the machine was 41% busy, so unlikely). **This was not proved**: it needs the same run with the production limits (`limits.sh`) and, if it remains, a timer in the agent. Until then the number to quote is p99, 273 ms.
5. **The generator had three faults, fixed:** a write in flight when a context is cancelled closes the WebSocket (three students' final saves failed, in the seven-minute run, that way); two goroutines of a student shared a random-number generator (a data race, found by the CI's `-race` after the runs; it did not touch what the server was asked or how long it took); and the exit status of the load test's script was swallowed in my own invocation (the log said FAIL, my wrapper said 0; the script itself exits with the test's status).
6. **Rootless Podman keeps the agent's folder on whatever file system `/data/cbe-platform/run` is on** (btrfs here) and `/work` on the XFS volume: the test reads the mount table and only demands that they are disks, not tmpfs or overlay.
7. **A container that starts creating files stops at 19,979 of 25,000**: the limit is 20,000 inodes of the project quota, and the agent's own files in `/work` count (a couple).

## Deviations from the plan, and conflicts found (standing rule 1)

- **The load test ran under the spike's backstop** (above), not the production limits, because applying them was refused (`limits.sh`, a system-wide change). The plan did not say under which limits; Discovery said the backstop must go before production (P0-11).
- **The plan's "40 simulated students each ... running `make` in a loop"** is a loop of stretches of work (an edit, saves, a build, a pause), **with a typist alongside**, because a student who is waiting for a build that takes minutes (under the cap) would otherwise stop saving and the autosave would not be measured when the machine is busiest.
- **The router test and the "pull the network for 30 seconds" test** are not run: they are the owner's (P0-25), and the tooling to run them is built (`cmd/cbe-loadtest`).
- **The plan says "Teacher guide: ... reading the proctor views"**: written; the owner reviews it (acceptance).
- **Production code changed in this phase beyond the plan:** the runner's memory budget and its CPU figure (found by the load test), the failure reason on the proctor's screen, the relay's control rate as an option, and a warning in the relay when a reply takes over half a second. Each has a test.

## Not run, and what is unverified

- **The production limits** (`limits.sh`) on this host, and so **a load test under them** (30 minutes; `deploy/workspace/test-load.sh` runs it as it is).
- **The router**: the autosave p95 over the owner's Wi-Fi, the 30-second network cut with a real device.
- **Windows**: the smoke test on a clean machine (the exe is cross-built into `build/phase8/`, and it was never run on Windows).
- **macOS** (deferred by the owner), **rootful Docker** (not the chosen runtime), **a real reboot during an exam** (Phase 3 simulated it and a real one passed in Phase 0; not repeated with a class).
- **Heartbeats, screenshots and focus events of the monitoring** are not simulated by the load test: a class of 40 on a question exam is the load they were built against, and they come on top of this.
- **A class whose build is much heavier than `lab-carga`**, and **a student whose program fills the 20,000 files while 39 compile**: the budget and the limits are what is there for them.
- **The student application on a machine that is slow** (the IDE's own latency in WebView2).

## Left behind

Nothing from the runs: the scripts remove their containers, folders and private runners and report what they find (`test-security.sh`, `test-load.sh`, `test-grading.sh` and `test-image.sh` each ended clean); the runtime holds only the old `snapprobe`, and `podman images` the course image and the spike's three. **One thing the image-build tests do leave** is untagged layers (`<none>:<none>`; 17 of them, from today's Phase 7 and Phase 8 test builds): I removed them with `podman image prune` after looking that all were from today and none had a child; `images.md` says the same of any exam's builds. A probe container of my own (`statprobe-p8`, to read what Podman says of a busy container) was removed. **Nothing was installed or changed on the host**: the installed runner is still the Phase 7 build, the slice still has the backstop, no unit or file under `/etc` was touched.

## For the owner

1. **May I apply the production limits?** `sudo deploy/workspace/limits.sh` replaces the spike's backstop on `cbe-runner`'s slice (`/etc/systemd/system/user-1001.slice.d/spike-backstop.conf`, kept as `/etc/cbe-platform/spike-backstop.conf.removed`) with `CPUWeight=50`, `MemoryMax=54,054M`, `TasksMax=12000` (P8-3). It is a drop-in file and `daemon-reload`; undoing it is moving the file back. With it, I would **run the 30 minutes again** and say whether the slow autosaves were the cap.
2. **May I upgrade the installed runner** (`/usr/local/bin/cbe-runner`, the old one kept as `cbe-runner.phase7`)? It has the memory budget and the CPU figure; only its user unit would be restarted; nothing is running on it now.
3. **The router test:** when you have the router set up, run `cbe-loadtest` from another device (`load-test.md`), or tell me when it is ready and I will run it from here if this machine is on the router.
4. **The Windows smoke test** on your VM (`windows-smoke-test.md`; the exe will be in `build/phase8/`, your own `build/cbe-portal-student.exe` is untouched).
5. **Review `TEACHER-GUIDE.md`** (it is the plan's last acceptance item), and `STUDENT-NOTICE.md` is still yours.
6. **Confirm or change P8-1 to P8-8** (`DECISIONS.md`), among them P8-7: images are removed by hand; do you want a `cbe-runner images` command?
7. **May I push the Phase 8 branch?** And do you want **one pull request** to `main` for the whole workspace work (Phases 0 to 8 are stacked branches, all pushed except this one, and no pull request has been opened)?
