40 of 40 students worked for 7m0s.

| Operation | Count | Failures | min | p50 | p95 | p99 | max |
|---|---:|---:|---:|---:|---:|---:|---:|
| login | 40 | 0 | 1.9 ms | 5.3 ms | 7.3 ms | 7.3 ms | 7.3 ms |
| workspace ready after login | 40 | 0 | 194 ms | 326 ms | 900 ms | 940 ms | 940 ms |
| heartbeat (HTTP) | 3499 | 0 | 1.2 ms | 7.9 ms | 32.3 ms | 39.2 ms | 60.6 ms |
| open a folder (fs.list) | 94 | 0 | 0.3 ms | 1.1 ms | 8.1 ms | 8.6 ms | 8.6 ms |
| open a file (fs.read) | 80 | 0 | 0.3 ms | 7.4 ms | 76.1 ms | 98.0 ms | 98.0 ms |
| autosave (fs.write) | 4292 | 0 | 1.6 ms | 49.0 ms | 162 ms | 306 ms | 6.90 s |
| new file (fs.create) | 40 | 0 | 4.8 ms | 47.1 ms | 88.2 ms | 103 ms | 103 ms |
| terminal echo | 40 | 0 | 0.4 ms | 3.2 ms | 78.6 ms | 82.7 ms | 82.7 ms |
| submit (a snapshot) | 37 | 3 | 2.08 s | 4.92 s | 5.13 s | 5.13 s | 5.13 s |
| terminal open | 40 | 0 | 3.0 ms | 19.1 ms | 86.0 ms | 111 ms | 111 ms |

Failures of *submit (a snapshot)*: the connection ended ×2; write: failed to write msg: use of closed network connection ×1

| Minute of the test | Autosaves | p50 | p95 | max |
|---:|---:|---:|---:|---:|
| 0 | 771 | 34.6 ms | 132 ms | 411 ms |
| 1 | 574 | 51.8 ms | 167 ms | 1.10 s |
| 2 | 592 | 52.6 ms | 159 ms | 6.90 s |
| 3 | 589 | 51.0 ms | 160 ms | 943 ms |
| 4 | 591 | 55.0 ms | 198 ms | 1.02 s |
| 5 | 577 | 48.8 ms | 169 ms | 1.42 s |
| 6 | 598 | 48.5 ms | 154 ms | 1.38 s |

| Reading | mean | 95th percentile | max | readings |
|---|---:|---:|---:|---:|
| containers | 40.0 | 40.0 | 40.0 | 44 |
| containers_cpu_percent_sum | 288.3 | 352.3 | 368.9 | 44 |
| containers_memory_mb_sum | 3025.6 | 3595.2 | 3671.2 | 44 |
| containers_pids_sum | 480.4 | 493.0 | 493.0 | 44 |
| containers_slice_cpu_percent | 389.8 | 401.0 | 401.3 | 44 |
| containers_slice_mem_mb | 3226.5 | 3763.9 | 3919.0 | 44 |
| containers_slice_pids | 557.1 | 572.0 | 580.0 | 44 |
| cpu_pressure_some_avg10 | 92.6 | 98.1 | 98.5 | 44 |
| exam_server_cpu_percent | 31.3 | 37.9 | 38.4 | 44 |
| exam_server_rss_mb | 31.0 | 32.5 | 32.8 | 44 |
| load1 | 26.0 | 70.4 | 70.6 | 44 |
| machine_cpu_busy_percent | 40.7 | 42.7 | 42.9 | 44 |
| mem_available_mb | 46504.8 | 47886.6 | 49275.0 | 44 |
| runner_cpu_percent | 14.4 | 16.6 | 17.0 | 44 |
| runner_rss_mb | 32.3 | 33.2 | 33.8 | 44 |
