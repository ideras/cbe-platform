# Workspace exams: Phase 0 discovery

**Plan:** `.agents/WORKSPACE-PLAN.md` v1.1. **Date:** 2026-11-13. **Branch:** `feat/workspace-phase-0-discovery`.
**Host:** the owner's computer, which is also the exam server in production (a local network with a Wi-Fi router).

No feature code was written. The spike lives in `docs/workspace/spike/` and is its own Go module that nothing imports.
Decisions made along the way are in `DECISIONS.md` (entries `P0-n`).

## 1. Summary

**Rootless Podman passes all five spike checks, on this machine, in one day.** The plan's preferred runtime (D10) is viable,
so the rootful Docker fallback is not needed. It also passed the extra checks I added (host-to-agent socket, neighbour
isolation, logout, a simulated reboot). The runner drives it through the Docker-compatible API with the maintained moby SDK.

The spike also found things the plan has to absorb before Phase 3. The ones that change a design are:

1. **Snapshots must ask the archive endpoint for `/work/.`.** With `/work`, a running container names entries `work/x` and a
   paused one names them `/x`, with a leading slash (section 4.3).
2. **`State.OOMKilled` is never set** by Podman 4.9.3, even when the memory limit killed the main process. OOM kills have to
   be read from the cgroup's `memory.events` (section 4.2).
3. **XFS reports a full project quota as `ENOSPC`, not `EDQUOT`.** The `QUOTA` error must cover both (section 4.1).
4. **The server and the client are not ready for a WebSocket.** There is no WebSocket library in `go.mod`, the HTTP server has
   a 10 s read and 15 s write timeout, and the student interface has no network access of its own: everything goes through
   the Go client by polling every 500 ms (section 5).
5. **A database migration is needed**, and the server validates exam documents with its own code, not with the compiler
   (section 5).

What I need from the owner is in section 9: confirm the runtime, confirm or change D6 to D9 and D12 to D14, and answer eight new
questions the code and the host raised (Q1 to Q8).

## 2. The host

| | |
|---|---|
| OS, kernel | KDE neon (Ubuntu 24.04 base), Linux 7.0.0-43, systemd 255 |
| CPU, memory | Intel i9-8950HK, 6 cores / 12 threads (a laptop part), 62 GiB, **no swap configured** |
| cgroups | v2 (`cgroup2fs`); controllers `cpuset cpu io memory hugetlb pids rdma misc dmem` |
| Container tools | Podman 4.9.3 with `crun` 1.14.1, `conmon` 2.1.10, overlay storage; rootful Docker 29.9.3 also present and left untouched (it runs the owner's other services) |
| Disk | `/data` is btrfs, 607 GB free. `/home` is ext4, 254 GB free (Podman's image storage lives there). |
| Work volume | 16 GiB sparse image `/data/cbe-platform/work.img`, XFS, loop-mounted with `prjquota` at `/data/cbe-platform/work` |
| Kernel features | `CONFIG_XFS_QUOTA=y`, `CONFIG_CGROUP_FREEZER=y`, `CONFIG_BLK_DEV_LOOP=y`; unprivileged user namespaces allowed (`apparmor_restrict_unprivileged_userns=1`, and Ubuntu's `podman` AppArmor profile permits them) |

Two facts about this host matter for the design:

- **It is a laptop-class machine that will run everything at once**: the exam server, the runner, up to 40 containers, the
  proctor application and the owner's other services. Section 7 sizes it for that.
- **The exam server will run as the owner's own user**, which can also use the rootful Docker socket. The runner split
  (rule 10) keeps the exam server away from the *Podman* socket, which I verified (section 3), but a compromised server
  running as that user could still reach Docker. Running the production server under its own account closes it (question Q6).

## 3. The spike: what was run and what it found

**Setup** (`spike/setup-host.sh`, idempotent, undone by `spike/teardown-host.sh`):

- user `cbe-runner` (uid 1001, subuid/subgid range 231072, 65536), lingering enabled;
- a drop-in `/etc/systemd/system/user@1001.service.d/delegate.conf`: `Delegate=cpu cpuset io memory pids` and
  `RequiresMountsFor=/data/cbe-platform/work`;
- the XFS image and its `/etc/fstab` line (`loop,prjquota,nofail,x-systemd.requires-mounts-for=...`);
- `podman.socket` and `podman-restart.service` enabled for `cbe-runner`; the socket is `/run/user/1001/podman/podman.sock`,
  mode 0660, in a directory the owner's user cannot even list;
- the quota helper prototype (`spike/quota-helper.sh`);
- a **spike-only backstop** on the user slice (`TasksMax=6000 MemoryMax=16G CPUQuota=400%`) so a limit that failed could not take the
  machine down. It must go before production.

**The driver** (`spike/main.go`, static, `CGO_ENABLED=0`) talks to the runtime only through `github.com/moby/moby/client`
v0.6.3, as the runner will. It runs as `cbe-runner`. The hostile workloads (`spike load ...`) are the same binary
bind-mounted read-only into the containers. Run everything with `spike/run-spike.sh`.

**Every container was created with the same fixed hardening** (this is what the runner will apply):

```text
User 1000:1000   NetworkMode none   ReadonlyRootfs true   CapDrop [ALL]   SecurityOpt [no-new-privileges]
Tmpfs /tmp rw,noexec,nosuid,nodev,size=16m   Binds: /spike (ro), /work (the quota folder)
Memory = MemorySwap (no swap)   NanoCPUs   PidsLimit   Labels cbe.spike=1
```

**Results** (final full run; the raw log is `spike/results/run-2.log`):

| # | Plan check | Result | Evidence |
|---|------------|--------|----------|
| 1a | Fork bomb stops at the PID limit | **PASS** | limit 64: `pids.peak=64`, `pids.events: max 34` (the limit was hit 34 times); killed and removed in 393 ms |
| 1b | Memory hog is killed at the limit | **PASS** | limit 128 MiB: last allocation 120 MiB, `memory.events oom_kill=1`, exit 137, `swap.max=0` (but see 4.2) |
| 1c | CPU loop stays at the CPU limit | **PASS** | 4 busy threads held to **1.00 cores** of a 1-core limit, throttled in 61 periods |
| 2 | Hard disk limit on `/work` | **PASS** | 50 MiB project quota: filling stopped at **50.0 MiB** with `no space left on device`; `statfs` inside the container reports the quota as the size. An inode limit of 2000 stopped file creation at 1998 files |
| 3 | Pause and resume | **PASS** | paused in 34 ms, `cgroup.freeze=1` on the container cgroup, a counter frozen for 2.5 s, resumed 39 ms after unpause |
| 4 | Live stats | **PASS** | a 5-frame stream reported 1.00 cores, 82 MiB of a 256 MiB limit and 6 pids; a one-shot sample works too |
| 5 | Archive from a running and a paused container | **PASS, with a gotcha** | `/work/.` gives identical tars (12 entries, content hashes and modes preserved) in both states; `/work` does not (4.3) |
| - | Containers survive logout | **PASS** | `Linger=yes`; no session exists for `cbe-runner`; the container's `conmon` runs under `user@1001.service/user.slice/libpod-conmon-*.scope`; it stayed up across separate `sudo` calls |
| - | Containers survive a reboot | **PASS (simulated)** | restarting `user@1001.service` kills every container as a boot does; `podman-restart.service` brought back the container with `RestartPolicy=always`, its `/work` intact and its process started a second time. A real reboot was **not** done (P0-12) |
| - | `delegation` | **PASS** | the container sees `cpu cpuset io memory pids` and a `cpuset` of `0-1` is applied |
| - | Host reaches an agent socket (D7) | **PASS** | `cbe-runner` talked to a Unix socket made by the container's uid 1000 through a bind-mounted folder |
| - | Neighbour isolation | **PASS** | a neighbour container (0.5 core, 32 MiB) kept ticking through the fork bomb, memory hog, CPU burner and both quota floods: 49 of 50 one-second ticks, not OOM-killed |
| - | Exam-server user cannot reach the runtime | **PASS** | the owner's user gets `Permission denied` on `/run/user/1001` and on the socket |
| - | API and SDK | **PASS** | the moby client negotiates API 1.41; image inspect, label-filtered list, create, start, wait, logs, pause, unpause, stats (stream and one-shot), kill, remove and copy-from-container all work |

The first full run had four failures (memory, disk, inodes, archive). Two were my wrong expectation of the errno (XFS gives
`ENOSPC`). One was my archive check mishandling hard links, which on the rerun exposed the real naming difference between
running and paused containers. One was real: `OOMKilled` is never set (4.2). I also fixed a reading that passed but showed
`cgroup.freeze=0`, because it looked at the wrong cgroup level. I changed a check only where the behaviour was right and my
expectation wrong, and every finding is in section 4. The first-run log is `spike/results/run-1.log`.

## 4. Things the plan has to absorb

### 4.1 Disk quota: `ENOSPC`, and a folder-permission rule

- XFS project quotas report "full" as `ENOSPC`. The agent and the runner must map **both** `ENOSPC` and `EDQUOT` to `QUOTA` (P0-7).
- The quota helper must create each session folder **`0700`, owned by the mapped uid** of the image's `student` user
  (host uid = first subuid + 999 for container uid 1000). I tested it: the container writes normally, **neither `cbe-runner`
  nor the exam-server user can read the files from the host**, and the archive API still reads them. This is the evidence for
  D14 and it means no host process ever needs to touch a student's files (P0-5).
- That makes the **`student` user's uid a contract** between the Phase 2 image and the helper: 1000:1000. If a course image
  uses another uid, the helper chowns to the wrong user and `/work` is read-only to the student.
- Set an **inode limit too** (the helper does: `ihard`). Without it a student can create millions of empty files.
- `work.img` must be `0600` root: it holds every student's files. The setup script does this now.

### 4.2 Out-of-memory kills are invisible to the API

Podman 4.9.3 reports `State.OOMKilled=false` in every case I tried: the memory-limited process was killed (`memory.events`
shows `oom_kill 1`, exit 137), whether it was the container's PID 1 or a child of a shell. The runner cannot use the flag to
tell a student "your program ran out of memory". The workaround is simple: **the agent reads `/sys/fs/cgroup/memory.events`
inside the container** (the cgroup namespace is private, so the file is the container's own) and compares `oom_kill` before and after
a command; the runner can read the same file on the host (P0-8).

In the real design the agent is PID 1 and the student's program is a child, so an OOM kill takes the *program*, not the
container. That is the case the second test covered. The first case (PID 1 itself killed) ends the container; the agent-loss
path in Phase 3 (restart on the same volume) handles it.

### 4.3 Archive: use `/work/.`, and treat the tar as hostile

| Source path | Running container | Paused container |
|-------------|-------------------|------------------|
| `/work` or `/work/` | `work/`, `work/src/main.cpp` | `/`, `/src/main.cpp` (**leading slash**) |
| `/work/.` | `src/main.cpp` | `src/main.cpp` (**identical**) |
| `/work/src` | `src/main.cpp` | `src/main.cpp` |

The snapshot code must always request **`/work/.`** (P0-6). The tar comes from a filesystem a student controls, so the
extractor in the exam server must be written for it. The spike's fixture contained what a student can create, and every item came
through as an entry and was **not followed** by the engine:

- a symlink to `/etc/passwd` and one to `../../../..`: archived as symlinks. Never follow them while extracting or building a zip.
- a hard link: archived as a **link entry** that points at the first name tar met. Which of two names is the "file" and which
  is the "link" depends on directory order, so resolve links before using content.
- a FIFO: archived as a FIFO entry. Reject it.
- names with spaces and non-ASCII characters: preserved. File modes: preserved (`0755` stays `0755`).
- Asking for the path `/work/link` (the symlink) is resolved inside the container, so it stays in the container's own
  filesystem. The runner must still refuse any path that is not under `/work`.

### 4.4 Pause freezes the agent, too

`ContainerPause` freezes every process in the container, including `cbe-agent`, so the agent's heartbeat stops. Agent-loss
detection (Phase 3, task 8) must not treat a paused container as a lost agent, and `ws.status` must say "frozen".

### 4.5 After a reboot, the volume must be there first

If the user manager (and so the containers) started before the XFS volume mounted, a container would bind-mount the **empty
directory underneath it** and the student would see an empty `/work`. The drop-in `RequiresMountsFor` now stops that, and the
runner must verify the mount before creating or starting any container (P0-4).

### 4.6 The agent socket (D7) works, with two conditions

A socket made by the container's uid 1000 in a bind-mounted folder is reachable from `cbe-runner`, as long as the **folder is
world-writable (0777) and its parent is private** (`/home/cbe-runner` is `0750`). Since the agent runs as `student`, a student
can kill it, delete its socket and listen in its place. **Everything arriving from an agent socket is student-controlled input**
and the relay must treat it that way. The plan already reads snapshots from the archive endpoint instead of from the agent,
which is the right call.

### 4.7 Smaller notes

- The compatible API version is **1.41**. The moby client negotiates it.
- Images are stored under `/home/cbe-runner`, on ext4. Plan for the image sizes there, not on `/data`.
- `podman system service` exits after 5 idle seconds and is socket-activated again; containers do not depend on it.
- `podman-restart.service` references `network-online.target`, which does not exist for a user manager. It worked anyway.
- Go 1.25 and later size `GOMAXPROCS` from the cgroup CPU quota. My CPU workload had to override it, or the test would have
  passed by itself. Worth remembering when the agent is built: it is inside a CPU-limited container.

## 5. The repository, and where the plan meets it

**Layout.** Go module `exam-platform` (the directory is `cbe-platform`), `go 1.26.8`. Seven modules:
the root (server), `shared/{calc,clientwire,examdoc,examimage}`, `apps/student` and `apps/proctor` (Wails v3 beta.29).
`scripts/ci/test.py` lists the modules by hand. The server's only third-party dependency is `modernc.org/sqlite`.

| Area | What is there | Plan impact |
|------|---------------|-------------|
| **Compiler** | `shared/examdoc` (1,733 LOC): `compile.go`, `frontmatter.go`, `picture.go`, goldmark with a custom answer-slot parser. `examc` takes a **file** (`examc exam.md`). | `examc` must learn to take a folder (Phase 1). |
| **Server routes** | `net/http` `ServeMux` in `internal/httpapi/router.go`: 3 public routes, 8 behind `StudentAuth`, about 30 admin routes behind `AdminAuth`. No WebSocket anywhere. No `ResponseWriter` wrappers, so a hijack-based upgrade would work. | A WebSocket library is a new dependency (Q4). |
| **HTTP timeouts** | `internal/server/run.go`: `ReadTimeout 10s`, `WriteTimeout 15s`, `IdleTimeout 60s`. | A WebSocket must take over its connection's deadlines, and long downloads (zips for 40 students) need a per-response write deadline. |
| **Exam storage** | SQLite, numbered migrations (latest `019_app_version.sql`), the compiled exam in `exams.questions_json` (JSONB). The server **does not import `examdoc`**: `internal/service/exam_content.go` validates the structure itself, and the request decoder uses `DisallowUnknownFields`. | **A migration is needed** (a `kind` column, workspace tables for sessions, snapshots, TODO state and grades), which contradicts "stored exams need no migration" for anything but the old rows. The server needs its own validator for `kind: workspace` documents. |
| **Sessions and deadlines** | `sessions` has `deadline_at`, `grace_until` and `status` (`active`, `grace`, `submitted`, `force_submitted`). A **background worker** (`internal/service/worker.go`, started in `internal/server/run.go`) finalizes expired sessions inside a database transaction. | Freezing a container and taking a snapshot is I/O that takes seconds and cannot run inside that transaction. Phase 5 needs a two-step finalize (mark finalizing, snapshot, commit). |
| **Events** | `event_log(session_id, event_type, occurred_at, detail JSON)`. The client `/event` endpoint accepts a fixed allowlist (`internal/service/events.go`). | Workspace events are written by the relay itself, so the client allowlist does not need to grow and a client cannot forge them. A `file.saved` per autosave could be tens of thousands of rows per exam; the proctor timeline needs to aggregate them. |
| **Student application** | Wails v3 shell. The Svelte frontend has **no `fetch` and no WebSocket**: all server traffic is in the Go client (`apps/student/internal/client`), and the frontend learns state by calling `api.Snapshot()` **every 500 ms**. Go can push events to the frontend (`Events.Emit`), used today only for focus alerts. | The WebSocket belongs in the **Go client** (it already owns the session token and the monitoring), and terminal and file traffic needs a push channel to the frontend, not the 500 ms poll (Q3). Throughput through Wails events on this beta must be proved early in Phase 4. |
| **Focus-loss** | Native: `WindowFocus` and `WindowLostFocus` in `cmd/student/main.go`, feeding `internal/client/monitor.go`. The DOM's own `blur` is used only for toasts. | Moving focus between the editor and a terminal, both inside the webview, cannot trigger it. Good for the Phase 4 focus acceptance test. |
| **CodeMirror** | Student only: `CodeEditor.svelte` and `languages.js` (C/C++, Python, MIPS), versions pinned, with a "highlighting only, never autocomplete" rule. Compartments already switch read-only. | Reusable for tabs. xterm.js is new. The **proctor frontend has only Svelte**: the read-only viewer needs CodeMirror added or plain text. |
| **Paste** | There is **no paste handling at all today**. | Paste control (Phase 4, task 8) is new behaviour, scoped to workspace mode. On Linux a middle-click and drag-and-drop paste differently from Ctrl+V and need testing in the webview on each OS. |
| **Calculator** | `shared/theme/calculator/` and `shared/calc`; the exam's `calculator:` front-matter key becomes `Tools`. | Needs a place in the IDE layout. |
| **Instructions rendering** | `shared/exam/sanitize.js` allows a fixed set of elements and one image form. | Reuse as is for the instructions panel and TODO text. |
| **Proctor application** | Wails shell, Svelte, no dependencies beyond Svelte. `Monitoring.svelte`, `MonitoringRow.svelte` and `roster.js` hold the roster; downloads are written by `apps/proctor/internal/dashboard/download*.go`. | New roster columns, a viewer window and a zip download fit the existing pattern. |
| **Docs the plan names** | `docs/exam-format.md` is here. `THEME-PROMPT-proctor-student.md`, its v2 amendment, `docs/tools/TEST-FOCUS.md` and `docs/tools/STUDENT-NOTICE.md` are in the **sibling repo** `../exam-platform/docs/`, not here (P0-13). | The owner decides whether to copy them. |

**Smaller plan notes.**

- The plan has no item literally marked `[DECIDE]`. I treated the decisions with status **Proposed** (D6 to D9, D12 to D14)
  as the open ones.
- The plan file is excluded from git (`.agents` is in `.git/info/exclude`), so reports can only refer to it by path and version.
- Section 5.1 says `navigation` is "not allowed" and `pin` and `focus_threshold` stay forbidden: those match the front-matter
  parser's existing cases (`frontmatter.go` handles `title`, `duration`, `grace`, `focus_threshold`, `min_volume`,
  `opens_at`, `closes_at`, `navigation`, `points`, `pin`, `calculator`), so the new keys (`image`, `terminal`, `run`,
  `check`, `submit`, `ignore`, `resources`) are additions to that switch.

## 6. Where workspace compilation lives (task 5)

**Extend `shared/examdoc`; do not create `shared/wsdoc`** (P0-9).

- The plan wants the question-exam rules for front matter, images, links and HTML reused (Phase 1, task 3). Those are
  unexported (`splitFrontMatter`, `parseFrontMatter`, the goldmark setup, the picture renderer). Reusing them from a new package
  would mean exporting them and freezing a public API for what is one team's code.
- A new shared module must be wired into **six places**: `Makefile` (test, vet, build), `.github/workflows/ci.yml`,
  `scripts/ci/test.py`'s module list, and `apps/proctor/go.mod`. Extending `examdoc` adds none.
- `examc` already lives in `examdoc/cmd/examc`; it can take a folder as well as a file.
- The question-exam document type `Exam` stays byte-for-byte as it is (rule 7). The workspace document is a separate type in
  new `workspace*.go` files, with its own tests, so the existing 1,700 lines are not at risk. Front-matter parsing gets a
  per-kind set of allowed keys; the existing tests are the guard that question exams do not change.

## 7. Sizing: a class of 40 compiling C++ at once

**Method** (`spike/sizing.sh`, results in `spike/results/sizing-*.log`). A C++20 lab with STL headers and three
translation units, built with `make` (`-O0 -g`) in a hardened container (1 core, 1 GiB, 256 processes, 100 MiB `/work`, no
network, read-only root) on an `ubuntu:24.04` + `g++` image of 343 MB. The machine was **not idle**: Chrome and Zoom were
running (load 2 to 3 before each run), which is how production will be. I allowed the `cbe-runner` slice 8 of the 12
threads, leaving the rest for the desktop.

| Measure | Result |
|---------|--------|
| One compile, alone (3 runs) | 2.8 s wall, 2.8 s CPU (2.5 user + 0.3 system), **biggest process 162 MiB** |
| 8 compiles at once | 6.1 s wall, 4.7 CPU-s each, 6.5 cores busy, slice peak 1.6 GiB |
| **40 compiles at the same instant** | **30.4 s wall** (median 24.1 s, slowest 28.6 s), 5.6 CPU-s each, 7.7 cores busy, **slice peak 4.2 GiB**, no failures |
| Autosave-shaped write (write + fsync + rename, 1 KiB), idle | p50 3.1 ms, p95 4.2 ms, max 5 ms |
| The same **during** the 40-compile storm | p50 4.0 ms, **p95 32.7 ms**, p99 42.6 ms, max 48 ms |
| 40 idle containers (the SDK, full hardening) | **3.3 MiB of host memory each**; create 40 ms, start 78 ms (medians); the whole class up in about 5 s, removed in 4.9 s |
| A snapshot of a compiled project through the archive API (10 files, 2.8 MiB) | median **37 ms**, p95 44 ms (includes `sudo` and `curl` start-up); 40 students once a minute is about **1.5 s** of serial work per minute |
| Disk | 40 x 100 MiB = 4 GiB at most, on a 16 GiB volume; each compiled project used under 3 MiB |

A first 40-way run with the slice capped at **4** threads (my spike backstop) took 132 s. It averaged exactly 4.0 cores, so the
cap, not the hardware, set the result; I discard it as a measure of capacity (its 12.5 CPU-s per compile I did not
investigate). The log is kept for honesty.

**What it means.**

- **The worst case is a synchronized one**: everyone presses `make` at the same moment (for example "now run `make check`").
  On 8 threads that took 30 s for this small program. Typical use is staggered: a build per student every two minutes is about
  0.33 builds a second, about 1.9 cores of work on average.
- **CPU per compile grows with concurrency** (2.8 s alone, 4.7 s at 8, 5.6 s at 40). It is a laptop CPU with hyper-threads,
  so each added busy thread is slower; I did not separate SMT from clock speed or heat. Plan with 5 to 6 CPU-seconds per
  sample compile.
- **The sample is small.** A lab built with `-O2`, heavy templates or more files will cost several times more (my
  expectation, not measured), and the synchronized worst case grows with it. The Phase 8 load test should include an optimized build.
- **Memory is not the constraint with the defaults**: 40 compiling containers peaked at 4.2 GiB (the figure includes page
  cache). If all 40 reached the 1 GiB limit that would be 40 GiB of 62 GiB, on a machine that also runs the server, the
  desktop and the owner's other services. A larger `memory=` per exam is what could exhaust it (about 20 students at 2 GiB).
- **Disk I/O is not a concern**: autosave stayed under 50 ms even during the storm, far from the 300 ms target. The real
  latency will come from the relay and the Wi-Fi (Q8), not from the volume.
- **Heat.** Sustained all-core load on a laptop part may throttle. A 30-second storm is not a 2-hour exam; Phase 8's 30-minute
  run is the first real test.

**Proposed server maximums for `resources`** (the owner confirms; none of these is enforced yet):

| Resource | Default (Appendix A) | Server maximum | Reason |
|----------|----------------------|----------------|--------|
| `cpu` | 1 core | **2** | A 40-way storm already oversubscribes 12 threads 3x; more per container only lengthens the queue |
| `memory` | 1 GiB | **2 GiB** | The sample needs 162 MiB. Above this one exam can starve the machine |
| `disk` | 100 MiB | **500 MiB** | The starter is at most 20 MiB; a compiled project used under 3 MiB |
| processes | 256 (fixed) | 512 | The fork bomb stopped at its limit in under 400 ms |
| files (inodes) | none in the plan | **20,000 (fixed)**, proposed addition | Without it a student can create millions of empty files |

**Host budget, to enforce in the runner** (replaces the spike's backstop): refuse to create a container once the memory
limits of all running containers add up to more than about **40 GiB**, and put the same ceiling on the slice
(`MemoryMax=40G`, `TasksMax=12000`). For CPU I would use `CPUWeight` (for example 50) on the runner's slice instead of a hard
quota, so the exam server, the proctor application and the desktop win under contention while an idle machine still gives
students everything. The spike used a hard quota; **`CPUWeight` is untested** and belongs in Phase 8.

## 8. Test baseline

Run on `main` at commit `3861680` plus only this phase's additions under `docs/workspace/`, with Go 1.27.4 (the module says
`go 1.26.8`), Node 26.13.0 and Playwright 1.64.1. The summary is `spike/results/baseline-summary.log` (the full Playwright output is not kept). It is what CI runs, minus the
desktop launch targets (P0-14).

| Step | Result |
|------|--------|
| Build the server (to the scratchpad, not `build/`), build both frontends | OK |
| `scripts/ci/test.py --race`: `go vet` and `go test -race` for all **7 modules** | **PASS**, 353 s, 19 packages `ok`, no failures |
| Student `npm test` (node unit tests) | **45 / 45** |
| Proctor `npm test` (unit tests and the theme contracts) | **74 / 74** |
| Student Playwright (`npm run test:ui`) | **131 passed** (2.7 min) |
| Proctor Playwright | **294 passed** (4.3 min) |

**Not run:** `make desktop-test`, `student-client-test` and `proctor-client-test`, because they launch the built applications and
the proctor application connects to the owner's real server; and the Windows and macOS CI jobs, which run only on GitHub (the last
documented green run, `docs/ci-verification.md`, is from an older commit).
`make test` in the root `Makefile` covers only four of the seven modules; `scripts/ci/test.py` covers all of them, so I used it.
No test was flaky in this run.

**Standing rule 7 asks that these keep passing unchanged.** Phase 1 should re-run exactly this list and compare.

## 9. What the owner has to decide

> **Answered on 2026-11-13** (recorded as `P0-17` to `P0-27` in `DECISIONS.md`). D6, D7, D9, D12, D13 and D14 are confirmed as the plan
> states them; D8 is confirmed with the interval made configurable; D10 is confirmed as rootless Podman. Q1 (real reboot): yes, and the
> owner does it. Q3: the Go client owns the WebSocket. Q4: the new dependency is accepted. Q6: yes, its own account. Q7: copy the
> documents (done). Q8: yes, on the Wi-Fi router. **Not answered**: Q2 and Q5 (defaults in `DECISIONS.md`). The questions and the
> recommendations are kept below as they were asked.

### Confirm or change (the plan asks for these before Phase 3)

| Item | Recommendation | Why |
|------|----------------|-----|
| **D10, runtime** | **Confirm rootless Podman** (P0-1), with the moby SDK (P0-2). | All five checks pass on this host. |
| **D6, one container per student** | Confirm. | Section 7 shows the idle cost is small and the limits isolate students. |
| **D7, agent on a Unix socket in a bind-mounted folder** | Confirm, with the two conditions in 4.6. | Works under rootless Podman; the agent is student-controlled, so its messages are hostile input. |
| **D8, snapshot every 60 s** | Confirm. | Snapshots go through the archive endpoint and cost little (section 7). |
| **D9, proctor sees a read-only viewer** | Confirm. | No dependency on the spike. |
| **D12, terminal on by default** | Confirm. | Needs xterm.js and a push channel (Q3), not the spike. |
| **D13, dedicated runner user** | Confirm; the user `cbe-runner` exists. | See Q6 about the exam server's own user. |
| **D14, snapshots through the archive endpoint** | Confirm, **with the refinements in 4.1 and 4.3** (`/work/.`, `0700` folders, a hostile-tar extractor). | Verified on running and paused containers. |

### New questions the code raised

- **Q1. Reboot.** Is it OK to do a real reboot of this machine to confirm containers and the volume come back (P0-12)?
- **Q2. Spike leftovers.** The user, the XFS volume, the fstab line, the drop-ins and the temporary backstop are still on this
  machine. Keep them for Phase 3 (recommended; the backstop should go once real limits are in), or tear them down with
  `spike/teardown-host.sh [--purge]`?
- **Q3. Who owns the WebSocket?** The student interface cannot reach the network; the Go client does. I recommend the **Go client terminates
  the WebSocket** and pushes file and terminal traffic to the Svelte interface through Wails events. The alternative is to let the
  webview open it directly, which breaks the rule that the client holds the session token and costs the monitoring a second path.
- **Q4. A WebSocket library.** The server has no WebSocket support and no dependency to give it. I recommend
  `github.com/coder/websocket` (maintained, minimal). Writing RFC 6455 by hand is the alternative. This is a new third-party
  dependency on both the server and the student client.
- **Q5. The `student` uid.** The runner's folder helper and the course images must agree that the student user is
  `1000:1000` (4.1). OK to make that a fixed contract in `images/README.md`?
- **Q6. The exam server's account.** In production the server will run as your own user, who can also use the Docker socket. The runner split protects
  you from a *student*, but not from a compromised server running as you. Run the production server under its own account?
- **Q7. The sibling-repo documents.** Copy `THEME-PROMPT-*`, `TEST-FOCUS.md` and `STUDENT-NOTICE.md` into this repo's `docs/`, or
  keep reading them from `../exam-platform/docs/`?
- **Q8. Wi-Fi.** Production is a local Wi-Fi router, not a wired lab LAN. Phase 8's 300 ms p95 target and its "pull the network for
  30 seconds" test should be run on that router. OK to amend the plan that way?

## 10. Reproduce and undo

```bash
sudo docs/workspace/spike/setup-host.sh          # user, delegation, XFS volume, helper, Podman socket
docs/workspace/spike/run-spike.sh                # every check, then the reboot simulation; ~2 minutes
docs/workspace/spike/sizing.sh 40                # section 7
sudo docs/workspace/spike/teardown-host.sh       # stop containers, remove helper and backstop (keeps user and volume)
sudo docs/workspace/spike/teardown-host.sh --purge   # also remove user, volume, fstab line
```
