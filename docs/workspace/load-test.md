# The load test

Phase 8. What was measured, how, and how to measure again. The results of the run for the phase are in
[`reports/phase-8.md`](reports/phase-8.md) and the files in [`reports/phase-8-load/`](reports/phase-8-load).

## What it simulates

A class of **40 students** (`internal/loadtest`) taking a workspace exam through the exam server's own routes, the ones the student's application uses:
`POST /login`, `POST /heartbeat` every 5 seconds, the WebSocket `/workspace/ws` and everything the IDE sends on it. Each student, after waiting for
their workspace to come up:

| Does | How often | Measured as |
|---|---|---|
| Opens a folder and a file (`fs.list`, `fs.read`) | every stretch of work | *open a folder*, *open a file* |
| Edits the file and **autosaves** it (`fs.write`, 3 to 6 times, about a second apart, each one a different content) | every stretch | **autosave** (the plan's 300 ms target) |
| Types a few characters in a terminal and waits for the echo | every stretch | *terminal echo* |
| Types `make` into the terminal (the edit made one unit newer, so one unit is recompiled and the program linked) and waits for it to end | every stretch | *make after an edit* |
| Makes a file and deletes it, ticks a TODO | one stretch in six | *new file*, *delete a file*, *tick a TODO* |
| **A storm**: removes `build/` and runs `make`, all forty at once (twice in the half hour: at a third and at two thirds) | the teacher saying "now run make" | *make of everything, all at once* |
| Presses **Save** at the end (a snapshot of `/work`) | once | *submit* |

A stretch of work is followed by 4 to 14 seconds of thinking, so each student builds about once every 20 seconds and the class, on average, about twice a second.
The project is `testdata/workspace/lab-carga`: eight C++ units with STL containers and algorithms, built with `-O2`, **1.6 CPU-seconds each** (13.8 s for all of them on this machine alone).
It is a heavier build than the sample lab (2.8 s for everything at `-O0`), on purpose (Discovery section 7: "the Phase 8 load test should include an optimized build").

While it runs, the machine is read every 10 seconds (`internal/loadtest/host_linux.go`): the load average, the CPU pressure (PSI), the memory available, the exam server's and
the runner's CPU and memory, the control group the containers run in (CPU, memory, processes), and the runner's own figures for the containers.

**Not simulated:** the screenshots and focus events of the monitoring (they are the question exams' load, measured when those were built), the
proctor's windows, a student reading the instructions or looking at the screen (the think time stands for it), and the students' own computers and Wi-Fi.

## Running it

On the container host, with a throwaway exam server of its own (its own database; the installed runner, quota helper and Podman):

```bash
deploy/workspace/test-load.sh                        # 40 students, 30 minutes; results in docs/workspace/reports/phase-8-load/
CBE_LOAD_STUDENTS=8 CBE_LOAD_DURATION=2m deploy/workspace/test-load.sh /tmp/short     # a short one
```

It uses `sudo`, makes forty containers and removes them at the end (whatever happens). **For half an hour the machine is busy:** the containers' slice has a CPU
weight (`limits.sh`), so the desktop and the exam server win under contention, but the students use every core the machine is not using.

Against **any running exam server**, from any machine (this is how the router is put in the middle):

```bash
go build -o cbe-loadtest ./cmd/cbe-loadtest
build/examc -json -bundles pkg testdata/workspace/lab-carga > pkg/document.json
./cbe-loadtest -server http://192.168.1.10:8080 -admin-token-file token -package pkg -students 40 -duration 30m -out results
```

With `-admin-token-file` and `-package` it first makes a class of `LT001` to `LT040` and the exam, published, with a PIN it prints; without them it takes `-pin` of an exam that is
there. It exits with status 3 when the 95th percentile of an autosave is over 300 ms. Everything it needs the server to have is what a real exam needs: the runner, the image `cbe/cpp20`.
**After it,** delete the exam in the proctor application; the containers go after the retention time (`-workspace-retention`, 48 hours by default; start the server with a short
one for the test).

## Running it with the router in the middle (done on 2026-11-15)

*Done once, from the owner's second laptop: `reports/phase-8-load-5-router/`. The steps are what was run.*


The plan's 300 ms target is for the lab network, which for this deployment is **the owner's own Wi-Fi router** (decision P0-25). The loopback run measures what the server, the
runner and the agent take; the router adds its round trip to every request and every echo. The simulated students must **come from another device on the Wi-Fi**: a client that
connects to this machine's own address never leaves it, whatever the address is.

1. **On the machine of the exam server** (connected to the router, by cable or by Wi-Fi), start a throwaway server on all its addresses, with the installed runner:

   ```bash
   deploy/workspace/test-server.sh --manual 0.0.0.0:8090
   ```

   It prints the admin token (a line `echo '…' > ~/manual-admin-token`) and runs until Ctrl+C; it has its own database. Find the machine's address on the router (`ip -4 addr`), say `192.168.1.10`.
2. **On another device on the Wi-Fi** (a laptop; the Windows VM is not it unless it is bridged to the Wi-Fi), with the files `cbe-loadtest` (or `cbe-loadtest.exe`) and the folder `lab-carga/` from `build/phase8/`,
   and the token saved in a file `token`:

   ```bash
   ./cbe-loadtest -server http://192.168.1.10:8090 -admin-token-file token -package lab-carga -students 40 -duration 30m -out results
   ```

   It makes the class of `LT001` to `LT040` and the exam, published, and runs the class. It exits with status 3 if the 95th percentile of an autosave is over 300 ms. The 40 simulated students share
   the laptop's one Wi-Fi link, which is harsher than 40 devices each with their own radio time and kinder than a crowded channel; if you have the devices, also run the real student application on a few
   of them meanwhile and look at their **Last activity** in the monitoring window.
3. **Read `results/summary.json` and `results/machine.csv`:** *autosave* p95 and p99, *terminal echo*, and the *connection lost and made again* events. Ctrl+C the server on the machine of the exam server: it
   removes everything the students made.
4. The plan's other network test, **pulling the network for 30 seconds loses no edits**, is done by hand with the real application: work in the IDE on a laptop, turn the Wi-Fi off for 30 seconds,
   turn it on; the *Reconnecting…* banner goes, and the text typed meanwhile is saved. (It passed on the Windows VM on 2026-11-15, step 9 of `windows-smoke-test.md`, with the VM's adapter.)

## Reading the results

- `load-report.md`: the table of every operation (count, failures, min, p50, p95, p99, max), the autosave p95 minute by minute (to see a drift), the events (reconnections, restarts, dropped
  keystrokes) and the machine's readings during the steady state (mean, 95th percentile, max).
- `load-summary.json`: the same as data. `load-machine.csv`: every reading, one line each, for a chart; the time is seconds from the start of the steady state (negative while the class logged in).
- A **failure** is counted by reason (an autosave refused, a reply that never came, a make that failed). The test fails on any failed login, workspace start, folder or file read,
  autosave or submission, and on an autosave p95 over 300 ms.
