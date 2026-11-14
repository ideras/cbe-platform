# Phase 3 report: runner, orchestrator and relay

**Date:** 2026-11-13. **Branch:** `feat/workspace-phase-3-runner-relay` (from `feat/workspace-phase-2-image-agent`, which is pushed). **Plan:** `.agents/WORKSPACE-PLAN.md` v1.1.
**Status:** done; the acceptance items are met on the one runtime the plan chose (rootless Podman), with the two things that were not
possible here named under "Not run". Decisions `P3-1` to `P3-21` are proposed and wait for the owner. **The branch is not pushed.**

## What changed

| Path | What |
|------|------|
| `internal/quota/`, `cmd/cbe-quota-helper/` | The quota helper as a Go service (P3-3): XFS project-quota folders, served over a Unix socket to `cbe-runner` only |
| `internal/runner/`, `cmd/cbe-runner/` | The runner (Appendix E): API, policy (`BuildSpec`), seeding, agent links, reconcile after a restart, client |
| `internal/runner/dockerrt/` | The one package that imports the Docker SDK (moby client). The exam server's binary links none of it |
| `internal/storage/` (`migrations/020_workspace.sql`, `workspace.go`) | `exams.kind`; tables `workspace_exams`, `workspace_sessions`, `workspace_snapshots`; the repository |
| `internal/workspace/` | The exam server's side: importer, manager (provisioning, supervisor, freeze, retention), relay, limiter, snapshots, blob store, facade |
| `internal/workspace/workspacetest/` | Test support: a fake runner whose containers are real agents, and the WebSocket test client |
| `internal/workspace/e2e/` | The real end-to-end test (needs the container host) |
| `internal/httpapi/workspace.go`, `internal/config`, `internal/server/run.go` | The routes, the flags (`-runner-socket` and seven more) and the wiring; **without `-runner-socket` the server is exactly what it was** |
| `internal/agent/pty.go` | A finished terminal makes room when the table of four is full (P3-9) |
| `deploy/workspace/` | `install.sh`, the two services' units, the server's unit (not installed), `runner.json`, `test-runner.sh`, `test-server.sh` |
| `docs/workspace/` | `deployment.md`, `relay-protocol.md`, `DECISIONS.md` (P3-1 to P3-21), this report and its logs |
| `go.mod` | `github.com/moby/moby/client v0.6.3`, `github.com/moby/moby/api v1.57.0`, `github.com/coder/websocket v1.8.15` (P0-17, P0-24; `go` directive unchanged) |

55 files, about 9,900 lines added, 12 removed.

## Tests run and results

- **Unit and integration tests with the race detector, all green:** the runner (13 tests, a fake runtime and a fake quota), the quota helper (7), the Docker translation (1), and **21 test functions (about 60 cases with their subtests) in `internal/httpapi/workspace_test.go`** that drive the real routes and a real WebSocket against real agents: every import refusal (26), login making the container with the starter, the student's view with nothing hidden in it, the relay (files, terminals, Run and Check, refusals, takeover by a second connection), floods, snapshots at the D8 moments, restoring byte for byte, hostile files in a snapshot, snapshot limits, a lost agent, the deadline and the retention period, a restarted server, a stuck provisioning, the supervisor loop, a submit that fails. They ran three times in a row for flakiness. `reports/phase-3-tests.log`.
- **The runner against the real runtime** (`deploy/workspace/test-runner.sh`, as `cbe-server`): the fork bomb stops at the process limit; a full disk is `QUOTA`; no network; one container cannot see another's files; killing the agent ends the container and a restart keeps the files; archives of a running and a paused container are byte for byte; the host account cannot reach the runtime's socket or a student's folder; the runner is restarted and finds its containers again.
- **The whole thing, with nothing replaced** (`deploy/workspace/test-server.sh`, `reports/phase-3-e2e.log`): the server's binary as `cbe-server`, the installed runner and quota helper, rootless Podman, the real image and the example package, in three phases. **A:** class, import, publish, two logins; the student's files and the Run and Check buttons through the real WebSocket; a terminal's account, network, read-only root, no exec from `/tmp`, no capabilities; a connection that idles 40 s (longer than the server's timeouts) and a dropped one finding its terminal and its output; a periodic snapshot; Submit and the snapshot's files; **a fork storm, four spinning processes and a full disk in one student's container while the other's terminal answers within 5 s**; another student's file not findable; the student ending their own container with `kill -TERM 1` and the files back after the restart; the server restarted with the containers untouched. **B** (the runner restarted in between): the same containers (same start time), the files, a terminal. **C** (the runtime's user manager restarted, which takes every container down as a boot does): the workspaces come back on their own `/work` with an `agent_loss` snapshot and the loss counted; then a 25-second exam with a 5-second grace runs out: the connection closes, the container is paused, and the `deadline` snapshot has the file written last. Everything it made is removed at the end (checked). About four minutes.
- **The image test again** (`images/test.sh cpp20`): 31 of 31 on the image rebuilt with the agent fix.
- **Baseline (`reports/phase-3-tests.log`), identical to Phases 0 to 2:** `scripts/ci/test.py --race` over the 7 modules passes; student unit 45/45; proctor unit 74/74; student Playwright 131; proctor Playwright 294. The existing question-exam tests (storage, service, httpapi) pass untouched.
- **Cross-vet:** `GOOS=windows` and `GOOS=darwin go vet ./...` clean (after making `internal/runner` independent of the Linux-only quota code, which would have stopped the server from building there).
- **Do the tests catch what they claim?** The end-to-end test found, and the lower tests had missed, five things (below), which is the check that matters: none of them showed in a green run of the faster suites.

## Acceptance (plan, Phase 3)

- [x] **The exam server process has no access to the runtime socket.** `checkNoRuntimeAccess` in every phase of the end-to-end test: the account's `connect` to the runtime's socket is refused, the running server holds no descriptor to it, and the server runs as that account. The runner's own test shows the same. The runtime socket is `0700` in `cbe-runner`'s runtime folder; the server's binary links no runtime client.
- [x] **The runner rejects an unknown operation, an unapproved image, an extra mount, a capability and a network setting.** `TestTheAPIRefusesWhatIsNotInAppendixE` (a `mounts`, `binds`, `cap_add`, `network_mode`, `privileged`, `user` and an extra limit are each a 400; an image off the list is 403; every path outside Appendix E is 404 or 405), `TestEveryContainerHasTheSameHardening` and `TestTheDockerTranslationKeepsTheHardening` (every container has exactly two mounts and the same options whatever the request).
- [x] **The same integration tests pass with the runner pointed at the chosen runtime.** Rootless Podman, as D10 chose. See "Not run" for the second runtime.
- [x] **Integration test: login creates a container, files round-trip, a terminal runs `make`, a reconnect reattaches to the same container.** End-to-end phase A (`make` through Run and Check; the reconnect lists the terminal and replays its output; the container's start count and time are unchanged).
- [x] **Abuse tests: a fork bomb hits the PID limit, a full disk returns `QUOTA`, `curl` fails, another student's volume is unreachable, and none affects the others.** The runner's real test and end-to-end phase A (process count at the limit, CPU at one core, `No space left` and the quota error, no network, no other student's file, the neighbour's terminal answering throughout).
- [x] **Killing the agent leads to a restart with no lost files.** `kill -TERM 1` from the student's own terminal, in the end-to-end test; and a killed agent in the fake-runner tests, including that the file written just before is in the `agent_loss` snapshot.
- [x] **An exam-server restart and a runner restart during an exam each reattach to every running container.** End-to-end phases A (server) and B (runner and server): same containers, same start times, files and terminals intact. Phase C goes further (a restart that takes the containers down).
- [x] **Snapshots are taken at the D8 moments, through the archive endpoint, from running and paused containers, and restore byte for byte.** Periodic (only when files changed and the interval passed), Submit, agent loss and deadline, each with its reason; a snapshot of a rich tree (1 MiB, Unicode names, an empty file and folder, a link, modes) comes back as the same bytes, twice the same tar; the archive of a paused container is checked in the runner's real test.

## What the work found, and what later phases must know

1. **Four Run or Check clicks left the student unable to open anything.** Each click makes a terminal that stays in the agent's table of four after its program ends. Found by the end-to-end test. The agent now drops the oldest finished terminal when the table is full (P3-9; the agent was changed, so the image was rebuilt and its 31 checks run again). The IDE should still close the tab of a finished Run when the student does.
2. **Two ways to notice a lost agent made two behaviours.** A reconnecting student found the dead container and restarted it, without the snapshot or the count; the supervisor did both. They are now one path (P3-17).
3. **A message that arrives while the container is starting was refused.** A student's application speaks as soon as the socket opens. The relay now waits up to 8 s for the agent.
4. **A failed Submit told the student the runner's socket path.** It now says to try again; the cause goes to the log (P3-16).
5. **`ignore` and `submit` do not apply to snapshots.** The plan applies them to the submission (Phase 5). A snapshot is everything, build products included, so a student's recovery never depends on a glob (P3-14). Phase 5 must filter when it records the submission.
6. **A frozen (paused) container does not survive the user manager's restart cleanly.** The runtime keeps the state "paused" for a container whose process is gone and `rm -f` fails ("container state improper") until it refreshes its runtime state. A real boot clears that state (the runtime folder is a fresh tmpfs); I removed the markers by hand to clear it here. I did not run a real boot with a frozen workspace; Phase 8 should (the owner's real reboot test of Phase 0 was with a running container).
7. **The restart policy is none (P3-5), not the `always` of the Phase 0 spike.** After a boot nothing restarts a container until the exam server is up and its supervisor sees it. The spike's `podman-restart.service` is not needed and is not installed.
8. **The WebSocket survives the server's 10 s and 15 s timeouts** (tested with 40 s of silence); the library clears the connection's deadlines. The ordinary requests still have them.
9. **The student's account can end PID 1 with `kill -TERM 1`** (a signal from inside is ignored for PID 1 unless it has a handler, and SIGKILL is). It restarts and is counted, as the plan's risk list expects. The count is in `GET /admin/workspace/session/{id}` for Phase 5's roster.
10. **The deployment's one flag day:** the exam server needs `-runner-socket` and the secret to serve workspace exams; without them the import refuses and nothing else changes.

## Deviations from the plan, and conflicts found (standing rule 1)

None was settled silently; each is a numbered decision.

1. **`container.seed` is added to Appendix E (P3-4).** Without it the starter cannot get into `/work`.
2. **The quota helper is a service, not the shell prototype (P3-3).**
3. **A snapshot is unfiltered (P3-14);** the plan's wording ("Submit takes a snapshot filtered by `submit` and `ignore`") is Phase 5's.
4. **The runner API is HTTP over a Unix socket with a shared secret (P3-1),** as the owner chose.
5. **No restart policy (P3-5)** and **one code path for lost agents (P3-17).**
6. **The agent's table of terminals changed (P3-9, amends P2-7).**
7. **Containers are named `cbe-<exam>-<session>`** (the plan: "name containers by exam and session"), with labels the runner finds them by.

## Not run, and what is unverified

- **The second runtime (rootful Docker).** Only rootless Podman exists on this machine, and the plan says the acceptance is "with the runner pointed at the chosen runtime". The runner is written against an interface and the SDK package is the only place that would change.
- **A real reboot with a workspace exam running**, and the exam server started by its unit after it. Simulated (phase C), and the unit was run against the real runner as a transient service and answered, but the unit is not installed and the machine was not rebooted. Phase 8.
- **The Windows and macOS CI jobs and the Linux CI job.** The new test packages are Linux-only and need PTYs, `openat2` and sockets; GitHub's runners should have them, unverified. The end-to-end and runner scripts need this machine and are not part of CI.
- **The proctor application and the student application:** unchanged, as the plan has them change in Phases 4 and 5.
- **More than two or three students at once.** The load test is Phase 8.
- **Deleting a workspace exam** does not delete its snapshots and blobs (P3-21).

## Left behind

On the container host (this machine), by this phase, all of it idempotent through `deploy/workspace/install.sh`:

- accounts `cbe-server` (uid 799, system, no login) and group `cbe-ipc` (with `cbe-runner` and `cbe-server` in it);
- folders under `/data/cbe-platform`: `ipc`, `run`, `runner`, `server`;
- `/etc/cbe-platform/runner.json`; `/usr/local/bin/cbe-runner`; `/usr/local/sbin/cbe-quota-helperd` (beside the Phase 0 shell prototype, which is unused);
- the system service `cbe-quota-helper.service` and `cbe-runner`'s user service `cbe-runner.service` with lingering, both enabled and running;
- the image `cbe/cpp20:latest` rebuilt with the agent fix (the Phase 2 image is untagged now);
- the Phase 0 leftovers (`snapprobe`, `snap-probe`, the spike's quota folders, the temporary resource backstop on `cbe-runner`'s slice) are still there. **The backstop must go before production.**

Nothing from the end-to-end tests is left: no containers, quota folders, run folders, runner files, state folders or processes. The exam server's unit file (`deploy/workspace/cbe-server.service`) is not installed and the owner's server on `:8080` was not touched.

## For the owner

1. **Confirm or change P3-1 to P3-21**, first P3-9 (the agent change), P3-14 (snapshots are unfiltered; Phase 5 filters submissions), P3-17 (the student can end their own container and it is counted, not prevented) and P3-18 (48 hours of retention, then the container and its folder go and the snapshots stay).
2. **May I push the branch?** (Phases 0 to 2 are pushed.)
3. **Go for Phase 4** (the student IDE: Svelte, Monaco-class editor, terminal, TODO panel, status bar; it speaks `docs/workspace/relay-protocol.md`).
