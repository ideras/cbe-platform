40 of 40 students worked for 30m0s.

| Operation | Count | Failures | min | p50 | p95 | p99 | max |
|---|---:|---:|---:|---:|---:|---:|---:|
| login | 40 | 0 | 1.8 ms | 2.3 ms | 3.8 ms | 4.9 ms | 4.9 ms |
| workspace ready after login | 40 | 0 | 161 ms | 211 ms | 252 ms | 261 ms | 261 ms |
| heartbeat (HTTP) | 14503 | 0 | 1.3 ms | 3.5 ms | 14.1 ms | 29.5 ms | 275 ms |
| open a folder (fs.list) | 1448 | 0 | 0.3 ms | 0.7 ms | 3.2 ms | 7.2 ms | 31.0 ms |
| open a file (fs.read) | 4096 | 0 | 0.3 ms | 0.6 ms | 3.1 ms | 10.8 ms | 79.9 ms |
| autosave (fs.write) | 36207 | 0 | 1.5 ms | 2.7 ms | 18.2 ms | 98.7 ms | 276 ms |
| new file (fs.create) | 732 | 0 | 1.4 ms | 2.3 ms | 36.3 ms | 97.1 ms | 217 ms |
| delete a file (fs.delete) | 692 | 0 | 1.3 ms | 2.1 ms | 7.2 ms | 14.4 ms | 190 ms |
| tick a TODO | 649 | 0 | 1.4 ms | 2.8 ms | 15.6 ms | 119 ms | 175 ms |
| terminal echo | 4045 | 0 | 0.3 ms | 0.6 ms | 4.1 ms | 8.5 ms | 151 ms |
| first make of the project (all of it) | 40 | 0 | 61.0 s | 77.8 s | 83.2 s | 84.4 s | 84.4 s |
| make after an edit | 3997 | 0 | 1.16 s | 1.87 s | 3.09 s | 7.41 s | 15.1 s |
| make of everything, all at once | 80 | 0 | 65.5 s | 78.1 s | 90.6 s | 92.2 s | 92.2 s |
| submit (a snapshot) | 40 | 0 | 223 ms | 622 ms | 735 ms | 751 ms | 751 ms |
| proctor: sessions | 599 | 0 | 21.3 ms | 101 ms | 207 ms | 239 ms | 287 ms |
| proctor: workspace roster | 599 | 0 | 1.9 ms | 67.3 ms | 215 ms | 263 ms | 300 ms |
| terminal open | 40 | 0 | 3.4 ms | 9.1 ms | 34.5 ms | 37.3 ms | 37.3 ms |

| Minute of the test | Autosaves | p50 | p95 | max |
|---:|---:|---:|---:|---:|
| 0 | 772 | 4.5 ms | 53.5 ms | 118 ms |
| 1 | 1021 | 3.2 ms | 14.6 ms | 83.5 ms |
| 2 | 1276 | 2.6 ms | 9.0 ms | 95.3 ms |
| 3 | 1319 | 2.3 ms | 4.9 ms | 44.2 ms |
| 4 | 1287 | 2.7 ms | 12.6 ms | 72.7 ms |
| 5 | 1296 | 2.8 ms | 10.8 ms | 84.8 ms |
| 6 | 1283 | 2.8 ms | 12.5 ms | 81.5 ms |
| 7 | 1314 | 2.6 ms | 9.2 ms | 58.9 ms |
| 8 | 1308 | 2.6 ms | 10.9 ms | 61.5 ms |
| 9 | 1310 | 2.6 ms | 10.3 ms | 92.1 ms |
| 10 | 619 | 5.7 ms | 45.3 ms | 153 ms |
| 11 | 1024 | 3.6 ms | 23.3 ms | 150 ms |
| 12 | 1314 | 2.8 ms | 12.4 ms | 83.3 ms |
| 13 | 1295 | 2.4 ms | 10.0 ms | 119 ms |
| 14 | 1305 | 2.9 ms | 14.7 ms | 118 ms |
| 15 | 1293 | 2.9 ms | 13.1 ms | 139 ms |
| 16 | 1323 | 2.9 ms | 14.1 ms | 116 ms |
| 17 | 1281 | 3.3 ms | 25.9 ms | 181 ms |
| 18 | 1285 | 2.6 ms | 13.9 ms | 156 ms |
| 19 | 1282 | 2.8 ms | 16.5 ms | 221 ms |
| 20 | 630 | 6.4 ms | 111 ms | 276 ms |
| 21 | 1044 | 3.6 ms | 28.0 ms | 210 ms |
| 22 | 1273 | 2.6 ms | 21.1 ms | 207 ms |
| 23 | 1265 | 2.7 ms | 24.4 ms | 267 ms |
| 24 | 1288 | 2.5 ms | 20.2 ms | 205 ms |
| 25 | 1290 | 2.6 ms | 16.6 ms | 243 ms |
| 26 | 1298 | 2.5 ms | 26.6 ms | 204 ms |
| 27 | 1305 | 2.4 ms | 39.0 ms | 195 ms |
| 28 | 1316 | 2.5 ms | 22.9 ms | 219 ms |
| 29 | 1291 | 2.4 ms | 27.1 ms | 260 ms |

| Reading | mean | 95th percentile | max | readings |
|---|---:|---:|---:|---:|
| containers | 40.0 | 40.0 | 40.0 | 181 |
| containers_cpu_percent_sum | 561.0 | 956.6 | 992.6 | 181 |
| containers_memory_mb_sum | 956.0 | 3315.4 | 3759.3 | 181 |
| containers_pids_sum | 397.3 | 485.0 | 495.0 | 181 |
| containers_slice_cpu_percent | 570.8 | 983.7 | 1011.3 | 181 |
| containers_slice_mem_mb | 1134.4 | 3573.7 | 3883.6 | 181 |
| containers_slice_pids | 466.5 | 553.0 | 565.0 | 181 |
| cpu_pressure_some_avg10 | 15.4 | 68.1 | 70.7 | 181 |
| exam_server_cpu_percent | 17.7 | 21.2 | 23.7 | 181 |
| exam_server_rss_mb | 32.4 | 33.1 | 34.9 | 181 |
| load1 | 15.8 | 37.1 | 40.4 | 181 |
| machine_cpu_busy_percent | 67.6 | 99.9 | 99.9 | 181 |
| mem_available_mb | 48582.7 | 49586.3 | 49811.7 | 181 |
| runner_cpu_percent | 5.6 | 6.2 | 6.6 | 181 |
| runner_rss_mb | 32.2 | 32.8 | 33.3 | 181 |
