# The load test over the router (2026-11-15, 19:27 to 19:58)

The exam server's machine is on the owner's router **by cable** (`eno1`, 192.168.1.10). The 40 simulated students ran in **one process on a laptop** (a Precision M4700, Linux Mint, 8 threads, 32 GiB) that is on the router's
**Wi-Fi** (a link of 270 Mbit/s, signal 80%), 192.168.1.11, with `cbe-loadtest` of this branch (`build/phase8/`): they logged in, edited, saved, ran `make` and submitted over the air, to a throwaway exam server of the tree's build
(`deploy/workspace/test-server.sh --manual 192.168.1.10:8090`, snapshots every 60 s, the installed runner and the production limits), with the same project and the same behaviour as the other runs, two storms
(at 10 and 20 minutes), and **no proctor application reading** (the other runs of the phase had its two reads going).

The round trip between the two, `ping` 40 times at 0.2 s: **min 1.8 ms, mean 3.3 ms, max 5.7 ms, no loss**.

| File | What |
|---|---|
| `load-report.md`, `summary.json` | What the students measured: the time from a request sent to its answer, **as the laptop saw it, over the Wi-Fi** |
| `client-machine.csv` | The laptop's own readings (its CPU, memory and load), every 10 s |
| `server-machine.csv` | The exam server's process and the machine of the server, every 10 s (`server_cpu_pct` is of one core; `containers_slice_cpu_pct` of the containers' control group) |

**The plan's target (300 ms at the 95th percentile of an autosave) is met by a factor of 27: p95 11.2 ms, p99 24.4 ms, max 279 ms** (the slowest are the saves of the minutes of the storms, 150 to 280 ms).
The exam server's CPU was **flat at 12.5% of a core for the half hour** (12.3% in the first five minutes, 13.3% in the last) with 22 to 33 MB of memory: the creep of the earlier runs (15% to 21%) is not there without the proctor's reads.

The simulated students are **one device's** share of the air (one radio for 40 students), which is harsher in bandwidth and kinder in contention than 40 phones or laptops; the real application on a few devices at the same time, and a crowded channel, are what this does not say.

The 40 containers this made were removed afterwards through the runner (the throwaway server's own clean-up only knew the example exam's students; it takes every workspace exam's now, `phaseManual`).
