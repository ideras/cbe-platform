40 of 40 students worked for 30m0s.

| Operation | Count | Failures | min | p50 | p95 | p99 | max |
|---|---:|---:|---:|---:|---:|---:|---:|
| login | 40 | 0 | 1.9 ms | 5.4 ms | 6.7 ms | 6.9 ms | 6.9 ms |
| workspace ready after login | 40 | 0 | 217 ms | 340 ms | 898 ms | 949 ms | 949 ms |
| heartbeat (HTTP) | 14527 | 0 | 1.2 ms | 14.0 ms | 99.0 ms | 121 ms | 199 ms |
| open a folder (fs.list) | 105 | 0 | 0.3 ms | 1.5 ms | 15.3 ms | 58.9 ms | 72.0 ms |
| open a file (fs.read) | 111 | 0 | 0.3 ms | 10.8 ms | 74.7 ms | 81.4 ms | 106 ms |
| autosave (fs.write) | 17938 | 0 | 1.6 ms | 51.0 ms | 161 ms | 273 ms | 16.4 s |
| new file (fs.create) | 50 | 0 | 3.0 ms | 34.0 ms | 93.0 ms | 119 ms | 119 ms |
| delete a file (fs.delete) | 10 | 0 | 2.5 ms | 11.8 ms | 85.0 ms | 85.0 ms | 85.0 ms |
| tick a TODO | 8 | 0 | 5.0 ms | 10.7 ms | 27.1 ms | 27.1 ms | 27.1 ms |
| terminal echo | 70 | 0 | 0.3 ms | 34.0 ms | 160 ms | 279 ms | 279 ms |
| first make of the project (all of it) | 16 | 24 | 401.1 s | 544.9 s | 599.3 s | 599.3 s | 599.3 s |
| make after an edit | 23 | 0 | 36.0 s | 78.1 s | 118.0 s | 121.3 s | 121.3 s |
| make of everything, all at once | 14 | 34 | 442.0 s | 527.3 s | 599.0 s | 599.0 s | 599.0 s |
| submit (a snapshot) | 40 | 0 | 1.73 s | 3.55 s | 4.14 s | 4.23 s | 4.23 s |
| terminal open | 40 | 0 | 3.0 ms | 35.0 ms | 183 ms | 218 ms | 218 ms |

Failures of *first make of the project (all of it)*: the build did not end ×24

Failures of *make of everything, all at once*: the build did not end ×34

| Event | Times |
|---|---:|
| a stretch of work failed: the build did not end | 24 |

| Minute of the test | Autosaves | p50 | p95 | max |
|---:|---:|---:|---:|---:|
| 0 | 769 | 34.1 ms | 112 ms | 464 ms |
| 1 | 576 | 44.7 ms | 145 ms | 337 ms |
| 2 | 596 | 50.4 ms | 158 ms | 391 ms |
| 3 | 588 | 45.9 ms | 155 ms | 575 ms |
| 4 | 592 | 55.0 ms | 174 ms | 522 ms |
| 5 | 578 | 45.5 ms | 150 ms | 2.50 s |
| 6 | 608 | 47.7 ms | 158 ms | 1.18 s |
| 7 | 597 | 53.9 ms | 167 ms | 660 ms |
| 8 | 607 | 50.2 ms | 150 ms | 925 ms |
| 9 | 629 | 46.3 ms | 143 ms | 1.20 s |
| 10 | 601 | 53.7 ms | 163 ms | 2.20 s |
| 11 | 590 | 48.1 ms | 150 ms | 4.96 s |
| 12 | 588 | 50.1 ms | 144 ms | 485 ms |
| 13 | 584 | 49.5 ms | 155 ms | 3.45 s |
| 14 | 582 | 48.3 ms | 157 ms | 1.31 s |
| 15 | 596 | 51.2 ms | 164 ms | 3.13 s |
| 16 | 603 | 49.7 ms | 141 ms | 7.62 s |
| 17 | 578 | 45.4 ms | 161 ms | 16.4 s |
| 18 | 596 | 48.9 ms | 163 ms | 677 ms |
| 19 | 596 | 49.8 ms | 165 ms | 1.01 s |
| 20 | 581 | 53.2 ms | 162 ms | 352 ms |
| 21 | 585 | 55.2 ms | 170 ms | 2.13 s |
| 22 | 589 | 48.6 ms | 176 ms | 9.29 s |
| 23 | 585 | 56.9 ms | 165 ms | 9.97 s |
| 24 | 573 | 52.8 ms | 168 ms | 11.6 s |
| 25 | 584 | 56.6 ms | 162 ms | 4.21 s |
| 26 | 583 | 56.9 ms | 184 ms | 6.21 s |
| 27 | 592 | 63.4 ms | 169 ms | 1.24 s |
| 28 | 605 | 58.2 ms | 179 ms | 1.18 s |
| 29 | 607 | 57.3 ms | 171 ms | 2.80 s |

| Reading | mean | 95th percentile | max | readings |
|---|---:|---:|---:|---:|
| containers | 40.0 | 40.0 | 40.0 | 181 |
| containers_cpu_percent_sum | 29.7 | 55.2 | 58.0 | 181 |
| containers_memory_mb_sum | 3087.0 | 3508.9 | 3699.5 | 181 |
| containers_pids_sum | 494.8 | 507.0 | 509.0 | 181 |
| containers_slice_cpu_percent | 397.4 | 400.4 | 402.0 | 181 |
| containers_slice_mem_mb | 3282.7 | 3692.3 | 3851.6 | 181 |
| containers_slice_pids | 571.6 | 587.0 | 606.0 | 181 |
| cpu_pressure_some_avg10 | 95.9 | 98.8 | 99.2 | 181 |
| exam_server_cpu_percent | 35.8 | 45.9 | 49.2 | 181 |
| exam_server_rss_mb | 31.6 | 32.5 | 32.9 | 181 |
| load1 | 30.8 | 67.5 | 72.6 | 181 |
| machine_cpu_busy_percent | 41.0 | 42.5 | 44.3 | 181 |
| mem_available_mb | 46419.0 | 46838.1 | 49315.0 | 181 |
| runner_cpu_percent | 13.9 | 16.5 | 17.6 | 181 |
| runner_rss_mb | 32.4 | 33.0 | 33.3 | 181 |
