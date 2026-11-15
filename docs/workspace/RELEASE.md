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

- [ ] **The limits of the containers' control group are the production ones** (`sudo deploy/workspace/limits.sh`; it replaces the spike's backstop of 400% CPU, 16 GiB and 6,000 tasks). The load test of this phase was run **under the backstop**.
- [ ] `host_memory_mb` in `/etc/cbe-platform/runner.json` is what the machine can give the students (the default is 70% of its memory; 40 students at 1 GiB need 40 GiB).
- [ ] The **load test with the router in the middle** ([load-test.md](load-test.md)): the 300 ms target is for the Wi-Fi the class will use, and the "pull the network for 30 seconds" check.
- [ ] The **Windows smoke test** on a clean machine ([windows-smoke-test.md](windows-smoke-test.md)).
- [ ] The teacher's own `grade` run on a solution and on a bad submission, in a test class ([TEACHER-GUIDE.md](TEACHER-GUIDE.md)).
- [ ] The exam server runs as `cbe-server` under its unit (`deploy/workspace/cbe-server.service`), not as a person.
- [ ] The student's notice (`docs/tools/STUDENT-NOTICE.md`) is the owner's own.

## Known limits (said, not hidden)

- **No packages or network for students**, by design; an exam that needs a library builds its own image.
- **TODO ticks are the student's word**; a grade is advisory until the proctor confirms it.
- **Grading measures; it is not a lock**: the student's code runs in the container `grade` runs in (P6-13).
- **An exam's image is the proctor's own code** (it builds with network, as root in the build container, P7-12), and images are removed by hand (P8-7).
- **One host**: no second runtime, no several exam servers; the container host is the exam server's machine.
- **macOS** was never run with the IDE (deferred by the owner).
