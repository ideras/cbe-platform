40 of 40 students worked for 30m0s.

| Operation | Count | Failures | min | p50 | p95 | p99 | max |
|---|---:|---:|---:|---:|---:|---:|---:|
| login | 40 | 0 | 1.9 ms | 2.2 ms | 2.8 ms | 3.9 ms | 3.9 ms |
| workspace ready after login | 40 | 0 | 156 ms | 210 ms | 261 ms | 283 ms | 283 ms |
| heartbeat (HTTP) | 14503 | 0 | 1.2 ms | 3.5 ms | 21.5 ms | 94.0 ms | 502 ms |
| open a folder (fs.list) | 1441 | 0 | 0.3 ms | 0.7 ms | 3.0 ms | 5.7 ms | 183 ms |
| open a file (fs.read) | 4072 | 0 | 0.3 ms | 0.6 ms | 3.5 ms | 8.9 ms | 129 ms |
| autosave (fs.write) | 36106 | 0 | 1.4 ms | 2.9 ms | 31.2 ms | 202 ms | 523 ms |
| new file (fs.create) | 729 | 0 | 1.5 ms | 2.3 ms | 47.1 ms | 120 ms | 320 ms |
| delete a file (fs.delete) | 689 | 0 | 1.3 ms | 2.2 ms | 7.8 ms | 23.0 ms | 120 ms |
| tick a TODO | 643 | 0 | 1.3 ms | 3.0 ms | 35.6 ms | 226 ms | 417 ms |
| terminal echo | 4023 | 0 | 0.2 ms | 0.6 ms | 4.2 ms | 8.9 ms | 159 ms |
| first make of the project (all of it) | 40 | 0 | 53.6 s | 77.4 s | 80.6 s | 81.1 s | 81.1 s |
| make after an edit | 3976 | 0 | 1.18 s | 1.92 s | 3.10 s | 5.95 s | 16.7 s |
| make of everything, all at once | 80 | 0 | 61.4 s | 82.6 s | 93.4 s | 97.5 s | 97.5 s |
| submit (a snapshot) | 40 | 0 | 202 ms | 548 ms | 632 ms | 638 ms | 638 ms |
| proctor: sessions | 599 | 0 | 19.4 ms | 118 ms | 338 ms | 409 ms | 465 ms |
| proctor: workspace roster | 599 | 0 | 2.1 ms | 110 ms | 358 ms | 444 ms | 575 ms |
| terminal open | 40 | 0 | 3.2 ms | 9.4 ms | 27.7 ms | 44.1 ms | 44.1 ms |

| Minute of the test | Autosaves | p50 | p95 | max |
|---:|---:|---:|---:|---:|
| 0 | 771 | 4.5 ms | 33.3 ms | 110 ms |
| 1 | 1047 | 3.0 ms | 19.2 ms | 94.6 ms |
| 2 | 1270 | 2.8 ms | 15.8 ms | 46.3 ms |
| 3 | 1288 | 3.0 ms | 20.5 ms | 85.8 ms |
| 4 | 1274 | 3.3 ms | 20.0 ms | 122 ms |
| 5 | 1281 | 2.9 ms | 21.4 ms | 127 ms |
| 6 | 1275 | 2.9 ms | 15.9 ms | 59.0 ms |
| 7 | 1316 | 2.9 ms | 18.2 ms | 118 ms |
| 8 | 1300 | 2.7 ms | 15.9 ms | 106 ms |
| 9 | 1290 | 3.0 ms | 15.1 ms | 93.0 ms |
| 10 | 608 | 5.3 ms | 51.6 ms | 166 ms |
| 11 | 1051 | 3.1 ms | 17.9 ms | 182 ms |
| 12 | 1329 | 2.5 ms | 8.5 ms | 90.2 ms |
| 13 | 1312 | 2.7 ms | 11.8 ms | 145 ms |
| 14 | 1304 | 2.5 ms | 10.3 ms | 143 ms |
| 15 | 1330 | 2.5 ms | 12.9 ms | 274 ms |
| 16 | 1312 | 3.0 ms | 22.1 ms | 231 ms |
| 17 | 1305 | 2.5 ms | 12.0 ms | 168 ms |
| 18 | 1302 | 2.5 ms | 23.9 ms | 169 ms |
| 19 | 1295 | 2.9 ms | 49.8 ms | 330 ms |
| 20 | 615 | 6.6 ms | 153 ms | 387 ms |
| 21 | 993 | 4.0 ms | 131 ms | 349 ms |
| 22 | 1245 | 2.7 ms | 76.8 ms | 237 ms |
| 23 | 1293 | 2.7 ms | 96.7 ms | 328 ms |
| 24 | 1283 | 2.7 ms | 139 ms | 358 ms |
| 25 | 1258 | 2.8 ms | 129 ms | 523 ms |
| 26 | 1285 | 3.6 ms | 184 ms | 423 ms |
| 27 | 1284 | 2.9 ms | 175 ms | 494 ms |
| 28 | 1286 | 3.2 ms | 168 ms | 440 ms |
| 29 | 1304 | 2.6 ms | 109 ms | 370 ms |

| Reading | mean | 95th percentile | max | readings |
|---|---:|---:|---:|---:|
| containers | 40.0 | 40.0 | 40.0 | 181 |
| containers_cpu_percent_sum | 565.1 | 968.0 | 1010.4 | 181 |
| containers_memory_mb_sum | 951.2 | 3134.6 | 3666.7 | 181 |
| containers_pids_sum | 398.2 | 488.0 | 494.0 | 181 |
| containers_slice_cpu_percent | 576.9 | 987.6 | 1023.8 | 181 |
| containers_slice_mem_mb | 1130.4 | 3434.6 | 3876.9 | 181 |
| containers_slice_pids | 466.5 | 557.0 | 563.0 | 181 |
| cpu_pressure_some_avg10 | 15.9 | 68.7 | 73.9 | 181 |
| exam_server_cpu_percent | 19.6 | 25.2 | 29.6 | 181 |
| exam_server_rss_mb | 32.5 | 33.2 | 35.6 | 181 |
| load1 | 14.8 | 33.2 | 39.5 | 181 |
| machine_cpu_busy_percent | 69.6 | 99.9 | 99.9 | 181 |
| mem_available_mb | 47969.9 | 48968.5 | 49223.1 | 181 |
| runner_cpu_percent | 5.7 | 6.4 | 6.8 | 181 |
| runner_rss_mb | 32.0 | 32.6 | 33.0 | 181 |
