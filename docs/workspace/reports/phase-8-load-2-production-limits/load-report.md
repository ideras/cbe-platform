40 of 40 students worked for 30m0s.

| Operation | Count | Failures | min | p50 | p95 | p99 | max |
|---|---:|---:|---:|---:|---:|---:|---:|
| login | 40 | 0 | 1.8 ms | 2.3 ms | 2.9 ms | 7.0 ms | 7.0 ms |
| workspace ready after login | 40 | 0 | 178 ms | 239 ms | 298 ms | 315 ms | 315 ms |
| heartbeat (HTTP) | 14508 | 0 | 1.4 ms | 8.7 ms | 176 ms | 246 ms | 490 ms |
| open a folder (fs.list) | 1352 | 0 | 0.3 ms | 1.0 ms | 6.1 ms | 11.1 ms | 73.6 ms |
| open a file (fs.read) | 3768 | 0 | 0.3 ms | 0.9 ms | 5.5 ms | 18.6 ms | 109 ms |
| autosave (fs.write) | 34687 | 0 | 1.7 ms | 4.5 ms | 110 ms | 182 ms | 548 ms |
| new file (fs.create) | 682 | 0 | 1.7 ms | 3.6 ms | 104 ms | 164 ms | 299 ms |
| delete a file (fs.delete) | 642 | 0 | 1.4 ms | 3.1 ms | 16.8 ms | 33.8 ms | 321 ms |
| tick a TODO | 602 | 0 | 1.7 ms | 4.3 ms | 110 ms | 179 ms | 482 ms |
| terminal echo | 3722 | 0 | 0.3 ms | 1.2 ms | 6.9 ms | 14.3 ms | 98.5 ms |
| first make of the project (all of it) | 40 | 0 | 77.3 s | 88.5 s | 91.1 s | 91.6 s | 91.6 s |
| make after an edit | 3673 | 0 | 1.40 s | 2.49 s | 4.83 s | 10.9 s | 20.0 s |
| make of everything, all at once | 80 | 0 | 78.9 s | 99.4 s | 112.6 s | 117.3 s | 117.3 s |
| submit (a snapshot) | 40 | 0 | 737 ms | 1.40 s | 1.67 s | 1.69 s | 1.69 s |
| terminal open | 40 | 0 | 3.6 ms | 8.1 ms | 41.5 ms | 55.8 ms | 55.8 ms |

| Minute of the test | Autosaves | p50 | p95 | max |
|---:|---:|---:|---:|---:|
| 0 | 770 | 4.4 ms | 46.4 ms | 173 ms |
| 1 | 900 | 3.6 ms | 18.5 ms | 129 ms |
| 2 | 1259 | 3.1 ms | 11.6 ms | 75.5 ms |
| 3 | 1289 | 3.1 ms | 11.3 ms | 47.1 ms |
| 4 | 1287 | 3.3 ms | 13.2 ms | 40.5 ms |
| 5 | 1262 | 3.1 ms | 14.7 ms | 54.1 ms |
| 6 | 1264 | 3.2 ms | 12.5 ms | 54.1 ms |
| 7 | 1289 | 3.3 ms | 21.7 ms | 257 ms |
| 8 | 1308 | 3.0 ms | 16.4 ms | 87.2 ms |
| 9 | 1223 | 6.7 ms | 56.2 ms | 182 ms |
| 10 | 611 | 10.1 ms | 106 ms | 548 ms |
| 11 | 783 | 7.4 ms | 63.5 ms | 180 ms |
| 12 | 1200 | 5.1 ms | 56.9 ms | 153 ms |
| 13 | 1271 | 6.6 ms | 71.3 ms | 202 ms |
| 14 | 1239 | 6.5 ms | 88.2 ms | 229 ms |
| 15 | 1258 | 5.3 ms | 88.5 ms | 193 ms |
| 16 | 1299 | 4.5 ms | 72.1 ms | 140 ms |
| 17 | 1283 | 4.0 ms | 87.5 ms | 214 ms |
| 18 | 1238 | 6.2 ms | 106 ms | 202 ms |
| 19 | 1300 | 3.1 ms | 77.1 ms | 136 ms |
| 20 | 630 | 9.4 ms | 152 ms | 302 ms |
| 21 | 780 | 27.5 ms | 230 ms | 424 ms |
| 22 | 1231 | 6.9 ms | 151 ms | 288 ms |
| 23 | 1212 | 4.4 ms | 128 ms | 243 ms |
| 24 | 1229 | 5.6 ms | 157 ms | 345 ms |
| 25 | 1232 | 6.7 ms | 177 ms | 351 ms |
| 26 | 1251 | 3.5 ms | 134 ms | 202 ms |
| 27 | 1282 | 3.9 ms | 138 ms | 350 ms |
| 28 | 1233 | 6.5 ms | 166 ms | 297 ms |
| 29 | 1274 | 4.5 ms | 159 ms | 313 ms |

| Reading | mean | 95th percentile | max | readings |
|---|---:|---:|---:|---:|
| containers | 40.0 | 40.0 | 40.0 | 181 |
| containers_cpu_percent_sum | 604.9 | 863.2 | 927.2 | 181 |
| containers_memory_mb_sum | 1119.4 | 3408.4 | 3746.2 | 181 |
| containers_pids_sum | 407.1 | 489.0 | 495.0 | 181 |
| containers_slice_cpu_percent | 626.0 | 880.3 | 946.1 | 181 |
| containers_slice_mem_mb | 1302.6 | 3641.8 | 3964.7 | 181 |
| containers_slice_pids | 475.7 | 559.0 | 686.0 | 181 |
| cpu_pressure_some_avg10 | 28.6 | 77.3 | 79.2 | 181 |
| exam_server_cpu_percent | 27.0 | 43.9 | 46.3 | 181 |
| exam_server_rss_mb | 32.0 | 32.7 | 33.0 | 181 |
| load1 | 20.6 | 44.2 | 48.0 | 181 |
| machine_cpu_busy_percent | 84.4 | 99.9 | 100.0 | 181 |
| mem_available_mb | 29597.3 | 30616.5 | 30925.6 | 181 |
| runner_cpu_percent | 6.1 | 7.0 | 7.2 | 181 |
| runner_rss_mb | 32.4 | 33.1 | 33.5 | 181 |
