# Phase 0 spike (throwaway, kept for reference)

This folder is a **separate Go module** (`cbe-spike`). Nothing imports it, `go test ./...` at the repository root does not
see it, and CI does not list it. It is not product code and is never merged into the product. What it found is in
`../DISCOVERY.md`; the decisions it led to are in `../DECISIONS.md` (entries `P0-n`).

It answers one question for plan D10: can **rootless Podman**, driven only through the **Docker Engine API** with the moby Go SDK, do
everything the runner needs (limits, a hard disk quota, pause, stats, archive) on the real host?

| File | What it is |
|------|------------|
| `setup-host.sh` | Prepares this machine (as root): the `cbe-runner` user, cgroup delegation, the XFS loop volume, the quota helper, the Podman socket. Idempotent. |
| `teardown-host.sh` | Undoes it. `--purge` also removes the user, the volume and the fstab line. |
| `quota-helper.sh` | Prototype of the root-owned quota helper: `create <session> <MiB> [files]`, `remove`, `report`. |
| `main.go` | The driver. `spike check` runs the checks through the moby client; `spike load <mode>` is the workload it starts inside containers. Linux only. |
| `run-spike.sh` | Builds the driver, makes the quota folders, runs every check as `cbe-runner`, then the reboot simulation. |
| `sizing.sh` | Task 4: one compile alone, then N containers compiling C++ at the same instant, plus autosave-shaped write latency. |
| `results/` | The raw output of the runs the report quotes. |

Checks: `api`, `delegation`, `pids`, `memory`, `cpu`, `disk`, `inodes`, `pause`, `stats`, `archive`, `socket`, then
`reboot-prep` and `reboot-verify` (asked for by name) and `density` (also by name, `-n` containers).

```bash
sudo docs/workspace/spike/setup-host.sh
docs/workspace/spike/run-spike.sh               # everything, ~2 minutes
docs/workspace/spike/run-spike.sh pids,disk     # only these
```

Caveats: the quota helper is a shell script run by hand with `sudo`, not the socket service the plan describes; the harness opts
into `0755` session folders so it can read files from the host (the helper's default is `0700`); and the setup installs a
**temporary resource backstop** on `cbe-runner`'s slice that must be removed before production.
