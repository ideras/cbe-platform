# The Phase 8 load test, as it ran

| File | What |
|---|---|
| `load-report.md`, `load-summary.json`, `load-machine.csv` | **The run of the phase**: 40 students for 30 minutes, 2026-11-15 11:01 to 11:33 (`deploy/workspace/test-load.sh`), a throwaway exam server of this tree's build, a private runner of this tree's build, rootless Podman, 12 threads and 64 GiB. The machine's readings are every 10 s (`seconds_from_steady_state` is negative while the class logged in). |
| `diagnostic-7min-report.md`, `diagnostic-7min-server-warnings.log` | **A second run of seven minutes**, made to find where the slowest autosaves spent their time: the relay logged any reply that took over 500 ms between the agent's answer and the student (the history's database write and the send). It logged none. (Its three failed *submit* lines are the generator's: see below.) |

**Conditions:** both runs were made **under the spike's backstop** on the containers' control group (`CPUQuota=400%`, 16 GiB, 6,000 tasks, `P0-11`), because the production limits (`deploy/workspace/limits.sh`) were not applied (the change is the owner's to allow). The slice was held at 400% of the machine's 1,200% all along (`containers_slice_cpu_percent`), CPU pressure at 96 to 99%. The `make` times are therefore **not** a measure of capacity (`reports/phase-8.md`).

**What was wrong with the generator, and is fixed since** (`internal/loadtest`): the note-typing goroutine of each student shared a random-number generator with the student's own work (a data race, found by `go test -race` after the runs; the seed is now passed to it), and a WebSocket write in flight when the end of the steady state cancelled its context closed the connection (three final saves of the seven-minute run). Neither touches what the server was asked to do or how long it took; the 30-minute run had all 40 submissions succeed.

**`containers_cpu_percent_sum`** in both runs is the runner's figure of the time, **which was wrong** (0.03% for a container using a whole core: Podman gives no previous sample); it is the reason the runner now makes the figure from two samples of its own (`reports/phase-8.md`, "What the work found"). Use `containers_slice_cpu_percent` for the CPU of the containers in these files.
