# Workspace exams: what is released, and what is to be done before the first real exam

The state of the work at the end of Phase 8 (2026-11-15), for the person who deploys it. How each part works is in the document linked from it; the reasons
for each choice are in [`DECISIONS.md`](DECISIONS.md) and the results of each phase in [`reports/`](reports).

## What is in it

| | Part | Read |
|---|---|---|
| Exam kind | **Workspace exams**: each student gets a project, an editor and terminals in a container on the exam server; TODO items, Save, hidden tests run by the proctor | [TEACHER-GUIDE.md](TEACHER-GUIDE.md) |
| Package | `workspace.md`, `project/`, `grading/`, `image/`; compiled by `build/examc` and imported from the proctor application | [`docs/workspace-format.md`](../workspace-format.md) |
| Student | The IDE in the student application (file tree, CodeMirror, xterm.js terminals, TODO, Save, Finish), over a WebSocket owned by the Go client | [student-ide.md](student-ide.md), [relay-protocol.md](relay-protocol.md) |
| Server | The relay, snapshots, submissions, the history of what students did, grading, image builds; migrations 020 to 023 | [deployment.md](deployment.md) |
| Runner | `cbe-runner` (the only holder of the container runtime), the quota helper, the agent in each container, the course image `cbe/cpp20` | [deployment.md](deployment.md), [`images/README.md`](../../images/README.md) |
| Proctor | The Workspace column, the review window (history, files, terminals and their replay, grading), grading and confirmation, importing a package and building its image | [proctor-view.md](proctor-view.md), [grading.md](grading.md), [images.md](images.md) |
| Checks | Eight fuzz targets, the security checklist, the load test, the question-exam regression | [security-checklist.md](security-checklist.md), [load-test.md](load-test.md) |

After the manual test of 2026-11-16 (P8-15 to P8-18, confirmed): the terminals have their own A− and A+ and a splitter, typing `exit` closes a terminal's tab, and the student application reports (report only) that it runs inside a virtual machine, which the owner tried on their Windows virtual machine (VirtualBox). **What the release archives hold:** the exam server, and the student and proctor applications, for each platform. The container host's parts (`cbe-runner`, `cbe-agent`, the quota helper, `examc` and the course image) are not in them: they are built from the tagged source with `deploy/workspace/install.sh` and `images/build.sh` ([deployment.md](deployment.md)).

Question exams are unchanged: the regression (`scripts/regression/qexam-diff.sh`) gives the same record on the server before the workspace work and on this one.

## What the host needs

Linux with **rootless Podman** (4.9 was used), systemd with lingering for the `cbe-runner` account, an **XFS volume with project quotas** for `/work`, Go to build, and the
course image built on it. `docs/workspace/spike/setup-host.sh` makes the account, the delegation and the volume; `deploy/workspace/install.sh` makes the services and the folders,
and sets the limits of the containers' control group (`limits.sh`). It is the machine of the exam server: students reach only the exam server's port.

## Upgrading a server that runs question exams

1. Build the server (`make server`) and **copy the database first** (the server does it itself on the first start of a new version: `UpgradeBackup`).
2. Start it as before. Without `-runner-socket` and `-runner-secret-file` it serves question exams exactly as it did, and the workspace routes do not exist. With them, workspace exams are on.
3. Install the host's part (`deployment.md`), then the student and proctor applications of this release (Windows: [windows-smoke-test.md](windows-smoke-test.md) says what to check first).

## Before the first real exam

- [x] **The limits of the containers' control group are the production ones** (`deploy/workspace/limits.sh`; applied on this host on 2026-11-15 with the owner's go: it replaced the spike's backstop of 400% CPU, 16 GiB and 6,000 tasks, which is kept as `/etc/cbe-platform/spike-backstop.conf.removed`). The load test was run under the backstop and again under these limits (`reports/phase-8-load*`).
- [x] The installed runner is the Phase 8 build (2026-11-15; the Phase 7 one is `/usr/local/bin/cbe-runner.phase7`).
- [ ] `host_memory_mb` in `/etc/cbe-platform/runner.json` is what the machine can give the students (the default is 70% of its memory: 44,838 MiB here; 40 students at 1 GiB need 40 GiB).
- [x] The **load test with the router in the middle** ([load-test.md](load-test.md)): over the owner's Wi-Fi from a second laptop, autosave p95 11 ms (target 300 ms), 2026-11-15; and the "pull the network for 30 seconds" check passed on the Windows VM. Repeat it with the class's real devices if the room or the router changes.
- [x] The **Windows smoke test** on a clean machine ([windows-smoke-test.md](windows-smoke-test.md)): run by the owner on 2026-11-15, every step passed.
- [x] The teacher guide was read by the owner (2026-11-15); what it lacked, a download of the grades and a switch for the image warnings, was added.
- [ ] The teacher's own `grade` run on a solution and on a bad submission, in a test class ([TEACHER-GUIDE.md](TEACHER-GUIDE.md)).
- [ ] The exam server runs as `cbe-server` under its unit (`deploy/workspace/cbe-server.service`), not as a person.
- [ ] The student's notice (`docs/tools/STUDENT-NOTICE.md`) is the owner's own.

## Known limits (said, not hidden)

- **No packages or network for students**, by design; an exam that needs a library builds its own image.
- **TODO ticks are the student's word**; a grade is advisory until the proctor confirms it.
- **Grading measures; it is not a lock**: the student's code runs in the container `grade` runs in (P6-13).
- **A class image is installed by the administrator** (`cbe-portalctl image install`, D-CB-7): an instructor who is not the server's administrator brings the image in each exam's package. A library of images in the proctor application (import, build, list and pin to an exam) is recorded and deferred (D-CB-8).
- **An exam's image is the proctor's own code** (it builds with network, as root in the build container, P7-12), and images are removed by hand (P8-7).
- **One host**: no second runtime, no several exam servers; the container host is the exam server's machine.
- **macOS** was never run with the IDE (deferred by the owner).
