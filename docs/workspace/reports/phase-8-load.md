# The load test of Phase 8: three runs, and what each found

All with `deploy/workspace/test-load.sh`: 40 simulated students (`internal/loadtest`) on a throwaway exam server of the tree's build, a private runner of the tree's build and rootless Podman,
on this machine (12 threads, 64 GiB, the owner's desktop on it), the project `testdata/workspace/lab-carga` (eight `-O2` units). How the students behave and what is measured: [`../load-test.md`](../load-test.md).

| Run | Folder | Conditions | What it showed |
|---|---|---|---|
| **1** | [`phase-8-load-1-backstop/`](phase-8-load-1-backstop) | 30 minutes, under **the spike's backstop** on the containers' slice (CPU capped at 400% of 1,200, 16 GiB, 6,000 tasks) | Autosave p95 **161 ms**, p99 273 ms, **max 16.4 s**; every `make` of the whole project took 7 to 10 minutes or did not end (24 of 40 first builds); the slice was held at 400% and CPU pressure was 96 to 99% all along. A shorter run of seven minutes with the relay logging replies over 500 ms (`diagnostic-7min-*`) logged none, so the server's handling of a reply was not the delay. **Found:** the cap throttles the whole slice, runner included. |
| **2** | [`phase-8-load-2-production-limits/`](phase-8-load-2-production-limits) | 30 minutes, under the **production limits** (`limits.sh`: `CPUWeight=50`, no cap), with the owner's go | Autosave p95 **110 ms**, p99 182 ms, **max 548 ms**; `make` of the whole project 77 to 117 s for forty at once, an edit's `make` p50 2.5 s; no failure. **Found:** the exam server's CPU **rose from 13% to 44% of a core over the half hour at a constant load**, and the autosave's p95 with it (11 ms in the third minute, 177 ms in the 26th). |
| profile | `reports/phase-8-load-2-production-limits` is run 2; the profile of the next run was read in the scratchpad (a 15-minute run built with `-tags pprof`) | a CPU profile of the exam server early and late | **40% of its CPU, late, was one query**: `LatestFocusEvent`, asked of every session every second by the lifecycle worker, which SQLite planned as a walk back through the session's events (thousands of file events, none a focus event) until it found a match. Fixed in `internal/storage/telemetry.go` (one seek of the index for each type: `TestTheNewestEventOfAFewTypesIsASeekOfTheIndexAndTheSameRowAsBefore`); the roster's "last activity" had the same shape and is fixed the same way. |
| **3** | [`phase-8-load-3-fixed/`](phase-8-load-3-fixed) | 30 minutes, production limits, the fixed server, **and the proctor's application's two reads going every 3 seconds** (the sessions and the workspace roster, which a class has open) | The numbers of the phase: see `reports/phase-8.md`. |

**The conditions of each run are in its own `load-report.md`'s numbers:** `containers_slice_cpu_percent` (the CPU of the containers; the cap shows as a flat 400), `exam_server_cpu_percent` (the drift of run 2), `cpu_pressure_some_avg10`.

**`containers_cpu_percent_sum`** in runs 1 and 2 is the runner's figure of the time, which was wrong (0.03% for a container using a whole core: Podman gives no previous sample); the runner makes it from two samples of
its own now (P8-11). Use `containers_slice_cpu_percent` for the CPU of the containers in those files.

**What was wrong with the generator, and is fixed since** (`internal/loadtest`): the note-typing goroutine of each student shared a random-number generator with the student's own work (a data race, found by `go test -race`
after run 1), and a WebSocket write in flight when the end of the steady state cancelled its context closed the connection (three final saves of the seven-minute diagnostic). Neither touches what the server was asked to do or how long it took.
