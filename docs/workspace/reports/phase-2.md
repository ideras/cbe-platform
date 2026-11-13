# Phase 2 report: course image and agent

**Date:** 2026-11-13. **Branch:** `feat/workspace-phase-2-image-agent` (from `feat/workspace-phase-1-package-format`, which is pushed). **Plan:** `.agents/WORKSPACE-PLAN.md` v1.1.
**Status:** done; all five acceptance items are met. Decisions `P2-1` to `P2-15` are recorded and wait for the owner. **Not pushed.**

## What changed

New code and an image; **no server, student or proctor code changed, and no new dependency** (`golang.org/x/sys` was already a direct dependency).

| Path | What |
|------|------|
| `internal/agentproto/` | The agent's language: frames, the messages of section 6 as strict typed requests, the path rules, a small client, and the fuzz targets |
| `internal/agent/` | The agent (Linux only): `fs.go` (every file operation, through `openat2` and `RESOLVE_BENEATH`), `pty.go` (terminals), `watch.go` (inotify), `reaper.go`, `ring.go`, `agent.go` (connections, heartbeat, dispatch) |
| `cmd/cbe-agent/` | The static binary that is the container's first process |
| `cmd/cbe-agent-cli/` | The harness: drives the agent through its socket without the server (task 4) |
| `images/` | `cpp20/Containerfile`, `build.sh` (builds the agent and the image; `--check` proves a rebuild is identical), `test.sh` (31 checks through the socket, with the hardening the runner will use), `README.md` (the contract; how to add a course image) |
| `docs/workspace/agent-protocol.md` | The wire, as built |
| `Makefile` | an `agent` target (not run: it writes to `build/`) |
| `docs/workspace/DECISIONS.md`, `reports/phase-2*.{md,log}` | P2-1 to P2-15, this report, and the logs it quotes |

## Tests run and results

- **New tests, all green with `-race`:** `internal/agentproto` 85, `internal/agent` 64 (one skip: the reaper helper, which only runs when its test starts it), `cmd/cbe-agent-cli` 4 (with their subtests), about 2,000 lines of test code. They cover every file operation, the path-safety cases of the plan (`..`, absolute paths, a link to `/etc`, a loop, a hundred hops, a race against a student who keeps replacing a folder with a link), a terminal that runs `make`, resizes and reports its exit code, a program killed by a signal, the memory kill, the limit of four terminals, a terminal that survives a reconnect, a runaway program held back by a connection that does not read, and the events from files made in a terminal.
- **Fuzz (acceptance: 60 s on the decoder), `reports/phase-2-fuzz.log`:** `FuzzDecodeRequest` **60 s, 6.6 million runs, no panic**; `FuzzFilePaths` (every file operation on made-up paths, in a `/work` full of links to a folder outside it, checking that folder is never read, changed or added to) **60 s**; `FuzzReadFrame` 30 s; `FuzzCheckPath` 20 s. All pass.
- **The image, on the container host (`reports/phase-2-image-test.log`):** `images/test.sh cpp20` runs the image with `--network none --read-only`, a tmpfs at `/tmp`, all capabilities dropped, `no-new-privileges`, 256 processes, 256 MiB, one CPU and a 20 MiB XFS-quota `/work`, and tests it from outside through the agent's socket as `cbe-runner`: **31 of 31 pass**. Among them: the agent is PID 1 and runs as uid 1000; `openat2` works under Podman's default seccomp profile; there is no sudo, no setuid file and no network; the files are uid 1000 inside and cannot be read from the host; links to `/etc` made in the terminal lead nowhere; `make`, `cmake`, `flex`, `bison` and `gdb` work; no zombies; **a full disk is `QUOTA`** for a create and for a write and frees up again (P0-7 on the real XFS volume); **the memory limit's kill is reported** from the container's own `memory.events` (P0-8, in a rootless container); a fork storm hits the limit and the agent still answers; `podman stop` ends the agent in 0.16 s with exit code 0 and no socket left.
- **Reproducible build (`reports/phase-2-reproducible.log`):** `images/build.sh cpp20 --check` builds the image twice from nothing and gets **the same image ID both times** (`869b4a3488ee`, 482 MB), twice (once with each version of the agent).
- **Baseline (`reports/phase-2-tests.log`), identical to Phase 0 and 1:** `scripts/ci/test.py --race` over 7 modules pass (the packages that did not change came from Go's test cache; the agent's ran fresh); student unit 45/45; proctor unit 74/74; student Playwright 131 passed; proctor Playwright 294 passed.
- **Do the tests catch what they claim?** I broke five rules on purpose (paths resolved without `RESOLVE_BENEATH`; a reaper that never reaps; deletion that follows links; the exit event for a program that ends at once; links in a bundle, in Phase 1). Each made tests fail and I reverted each.
- **Not run:** the Windows and macOS CI jobs (the agent packages are Linux-only and are skipped there; `go vet` for both is clean), **the Linux CI job** (the new tests need PTYs, inotify and `openat2`, which GitHub's Ubuntu runners should have; unverified), an `arm64` image, and the full path through the exam server (Phase 3).

## Acceptance (plan, Phase 2)

- [x] **Unit tests for every file operation, including path safety (`..`, absolute, a symlink to `/etc`, a symlink loop).** `internal/agent/fs_test.go`: list, read, write, create, mkdir, rename and delete, each with its refusals, plus `TestSymbolicLinks`, `TestLinksAreRemovedNotFollowed`, `TestWithoutOpenat2` and `TestRacingWithLinkSwaps`; `FuzzFilePaths`; and the same cases from outside in the image test.
- [x] **A PTY session runs `make`, streams output, handles resize, and reports the exit code.** `TestMakeRunsInATerminal`, `TestAShellTakesInputAndResizes`, `TestACommandRunsAndReportsItsExitCode`, `TestASignalIsReported`; in the image, `make` on the example project (`2` for a failing `make check`).
- [x] **Files created from the terminal produce `fs.event` messages.** `TestFilesCreatedInATerminalProduceEvents`; in the image, `touch` in a terminal.
- [x] **Fuzz test on the agent's message decoder: 60 seconds, no panic.** `FuzzDecodeRequest`, above.
- [x] **The image builds reproducibly, and the agent runs as a non-root user inside it.** Same image ID from two clean builds; PID 1 is `cbe-agent` with uid 1000:1000.

## What the work found, and what Phase 3 and 4 must know

1. **Three races, all found by running the whole baseline and then a load test, none by the first green runs.** (a) A program that ends before the connection that started it is attached got **no exit event**: fixed, and every attached connection hears it once. (b) `openat2` with `RESOLVE_BENEATH` returns **`EAGAIN` when any rename happens anywhere on the system** while it resolves a path: a student renaming files in the terminal would have made the agent fail requests; the agent now asks again (up to 100 times) and says `UNAVAILABLE` after that. (c) Shutdown removed whatever file was at the socket path, including a successor's: now once, and only its own.
2. **Finished terminals count toward the limit of four until they are closed**, like tabs. The IDE and the relay must close a Run or Check terminal when its tab goes (Phase 4); the harness closes its own.
3. **`run.start` is the client's message to the server; the agent has `pty.open` with a `cmd`.** The relay turns one into the other, so the command comes from the exam, never from the client.
4. **`/tmp` has no exec** (a size-limited tmpfs, as the plan asks), so a student builds and runs programs in `/work`. The image test builds in `/work`.
5. **The socket is mode 0666 in a folder the runner mounts.** For `cbe-runner` to reach it the host folder must be world-writable with a private parent (DISCOVERY 4.6); Phase 3's runner creates it that way.
6. **The image build needs the Internet and takes 10 to 12 minutes**, because the Ubuntu snapshot service is slow; the owner never has to build during an exam. The base image has no CA certificates, so `build.sh` lends the host's for the download step as a bind mount that leaves nothing in the image.
7. **A paused container freezes the agent too**; Phase 3 must not read its missing heartbeat as a lost agent (DISCOVERY 4.4, in `agent-protocol.md`).

## Deviations from the plan, and conflicts found (standing rule 1)

None was settled silently; each is a numbered decision.

1. **"Packages by version" (D11) became a dated snapshot of the Ubuntu archive (P2-12).** A list of versions goes stale and conflicts as the archive moves; a snapshot pins every package at once.
2. **"Builds reproducibly" is checked as "two builds from nothing give the same image ID"** (P2-12), which is stronger than "the same toolchain".
3. **The protocol adds three error codes (P2-3)** and a few rules the plan does not list (a reserved name prefix, no control characters in names, P2-4).
4. **Writing through a symbolic link is refused (P2-5);** reading and listing follow links that stay inside `/work`.
5. **The agent enforces the limit of four terminals and counts finished ones (P2-7).**
6. **The agent is built outside the image and copied in (P2-13)**, not in a multi-stage build.

## Left behind

On the container host: the image `cbe/cpp20:latest` (482 MB) and the base image in `cbe-runner`'s storage (`/home/cbe-runner`, ext4); the harness at `/home/cbe-runner/phase2/bin/cbe-agent-cli`; and the Phase 0 leftovers (`snapprobe`, `snap-probe`, the temporary resource cap) unchanged. `images/test.sh` removes its own container, quota folder and socket folder, and did.
