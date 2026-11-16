40 of 40 students worked for 30m0s, from a laptop on the Wi-Fi (the exam server's machine is on the router by cable).

| Operation | Count | Failures | min | p50 | p95 | p99 | max |
|---|---:|---:|---:|---:|---:|---:|---:|
| login | 40 | 0 | 4.2 ms | 9.9 ms | 11.4 ms | 14.1 ms | 14.1 ms |
| workspace ready after login | 40 | 0 | 187 ms | 316 ms | 898 ms | 926 ms | 926 ms |
| heartbeat (HTTP) | 14502 | 0 | 3.1 ms | 6.7 ms | 9.8 ms | 20.9 ms | 375 ms |
| open a folder (fs.list) | 1510 | 0 | 2.5 ms | 3.8 ms | 6.7 ms | 12.5 ms | 118 ms |
| open a file (fs.read) | 4429 | 0 | 2.2 ms | 3.9 ms | 7.1 ms | 13.7 ms | 151 ms |
| autosave (fs.write) | 37701 | 0 | 3.1 ms | 6.4 ms | 11.2 ms | 24.4 ms | 279 ms |
| new file (fs.create) | 759 | 0 | 3.4 ms | 6.4 ms | 16.9 ms | 71.2 ms | 96.3 ms |
| delete a file (fs.delete) | 719 | 0 | 3.4 ms | 6.3 ms | 8.1 ms | 13.6 ms | 17.7 ms |
| tick a TODO | 704 | 0 | 3.3 ms | 6.7 ms | 9.2 ms | 14.8 ms | 167 ms |
| terminal echo | 4378 | 0 | 2.1 ms | 3.7 ms | 6.2 ms | 10.0 ms | 270 ms |
| first make of the project (all of it) | 40 | 0 | 44.7 s | 59.2 s | 61.5 s | 61.9 s | 61.9 s |
| make after an edit | 4336 | 0 | 1.06 s | 1.32 s | 1.90 s | 4.58 s | 12.4 s |
| make of everything, all at once | 80 | 0 | 50.0 s | 61.0 s | 66.8 s | 68.0 s | 68.0 s |
| submit (a snapshot) | 40 | 0 | 113 ms | 379 ms | 440 ms | 443 ms | 443 ms |
| terminal open | 40 | 0 | 6.4 ms | 10.1 ms | 23.4 ms | 49.6 ms | 49.6 ms |

| Minute of the test | Autosaves | p50 | p95 | max |
|---:|---:|---:|---:|---:|
| 0 | 782 | 8.4 ms | 40.6 ms | 150 ms |
| 1 | 1219 | 6.2 ms | 10.2 ms | 51.8 ms |
| 2 | 1317 | 6.2 ms | 9.1 ms | 20.0 ms |
| 3 | 1334 | 6.4 ms | 8.6 ms | 29.7 ms |
| 4 | 1302 | 5.4 ms | 9.1 ms | 17.5 ms |
| 5 | 1346 | 6.5 ms | 14.1 ms | 279 ms |
| 6 | 1331 | 6.2 ms | 8.8 ms | 22.7 ms |
| 7 | 1353 | 5.5 ms | 9.3 ms | 25.6 ms |
| 8 | 1314 | 6.6 ms | 9.7 ms | 28.8 ms |
| 9 | 1320 | 5.5 ms | 9.3 ms | 33.1 ms |
| 10 | 643 | 8.8 ms | 36.2 ms | 187 ms |
| 11 | 1305 | 6.8 ms | 10.9 ms | 29.0 ms |
| 12 | 1335 | 5.3 ms | 9.4 ms | 27.1 ms |
| 13 | 1324 | 6.3 ms | 9.5 ms | 27.7 ms |
| 14 | 1303 | 5.7 ms | 9.0 ms | 28.4 ms |
| 15 | 1323 | 5.4 ms | 13.5 ms | 189 ms |
| 16 | 1341 | 6.5 ms | 9.2 ms | 26.1 ms |
| 17 | 1329 | 5.6 ms | 9.5 ms | 21.9 ms |
| 18 | 1307 | 5.5 ms | 9.0 ms | 29.0 ms |
| 19 | 1308 | 6.3 ms | 8.4 ms | 32.2 ms |
| 20 | 636 | 8.8 ms | 31.6 ms | 172 ms |
| 21 | 1320 | 6.9 ms | 11.6 ms | 31.5 ms |
| 22 | 1322 | 5.5 ms | 8.7 ms | 33.6 ms |
| 23 | 1317 | 5.4 ms | 9.1 ms | 30.6 ms |
| 24 | 1329 | 6.4 ms | 8.8 ms | 29.5 ms |
| 25 | 1352 | 5.6 ms | 13.8 ms | 200 ms |
| 26 | 1313 | 5.5 ms | 9.0 ms | 31.4 ms |
| 27 | 1296 | 6.4 ms | 9.7 ms | 29.4 ms |
| 28 | 1330 | 5.5 ms | 12.9 ms | 28.9 ms |
| 29 | 1350 | 6.6 ms | 13.7 ms | 32.9 ms |

