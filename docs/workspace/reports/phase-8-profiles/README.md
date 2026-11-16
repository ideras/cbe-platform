# CPU profiles of the exam server during the load test (30 seconds each)

Taken by `CBE_LOAD_PROFILE=1 CBE_LOAD_DURATION=15m deploy/workspace/test-load.sh` (the server is built with `-tags pprof`: `cmd/server/pprof.go`). Read them with `go tool pprof -top -cum FILE`.

| File | When | What |
|---|---|---|
| `before-fix-early.pb.gz` | 2.5 minutes in | 3.7 s of CPU in 30 s (12%); the SQLite step is a quarter of it |
| `before-fix-late.pb.gz` | 12.5 minutes in | 6.1 s (20%): `Sessions.Reconcile → lockReason → LatestFocusEvent` **2.4 s, 40%** |
| `after-fix-early.pb.gz`, `after-fix-late.pb.gz` | the same, with the fix and the proctor's two reads going | late: 5.4 s (18%), `LatestFocusEvent` 0.7 s, `ProctorSessions` 1.1 s, `RecordEvent` 0.7 s |

## before-fix-late, by cumulative time (the server's own functions)
```
Duration: 30s, Total samples = 6.09s (20.30%)
   focus=exam-platform
         0     0%  0.16%      3.60s 59.11%  exam-platform/internal/storage.(*Store).withTx
         0     0%  5.09%      2.82s 46.31%  exam-platform/internal/storage.(*Store).WithSessionTransaction
         0     0%  5.09%      2.63s 43.19%  exam-platform/internal/storage.(*Store).WithSessionTransaction.func1
         0     0%  5.09%      2.52s 41.38%  exam-platform/internal/server.Run.func2
         0     0%  5.09%      2.52s 41.38%  exam-platform/internal/service.(*Sessions).Reconcile
         0     0%  5.09%      2.52s 41.38%  exam-platform/internal/service.(*Sessions).RunWorker
         0     0%  5.09%      2.51s 41.22%  exam-platform/internal/service.(*Sessions).Reconcile.func1
         0     0%  5.09%      2.44s 40.07%  exam-platform/internal/service.lockReason
         0     0%  5.09%      2.44s 40.07%  exam-platform/internal/storage.(*sessionTransaction).LatestFocusEvent
         0     0% 37.44%      1.25s 20.53%  exam-platform/internal/httpapi.New.func2
         0     0% 37.44%      1.25s 20.53%  exam-platform/internal/httpapi.StudentAuth.func1
         0     0% 37.44%      0.94s 15.44%  exam-platform/internal/httpapi.workspaceSocketHandler.func1
         0     0% 37.44%      0.94s 15.44%  exam-platform/internal/workspace.(*Manager).Serve
         0     0% 37.44%      0.94s 15.44%  exam-platform/internal/workspace.(*Service).Serve
```

## after-fix-late
```
Duration: 30s, Total samples = 5.37s (17.90%)
   focus=exam-platform
         0     0%  0.19%      2.68s 49.91%  exam-platform/internal/storage.(*Store).withTx
         0     0%  0.19%      2.25s 41.90%  exam-platform/internal/httpapi.New.func2
         0     0%  0.19%      1.97s 36.69%  exam-platform/internal/storage.(*Store).WithSessionTransaction
         0     0%  0.19%      1.83s 34.08%  exam-platform/internal/storage.(*Store).WithSessionTransaction.func1
         0     0%  3.91%      1.16s 21.60%  exam-platform/internal/httpapi.AdminAuth.func1
         0     0%  5.77%      1.10s 20.48%  exam-platform/internal/httpapi.proctorListHandler.func1
         0     0%  5.77%      1.09s 20.30%  exam-platform/internal/httpapi.StudentAuth.func1
         0     0%  5.77%      1.09s 20.30%  exam-platform/internal/service.(*Sessions).ProctorSessions
         0     0%  5.77%      1.09s 20.30%  exam-platform/internal/service.(*Sessions).ProctorSessions.func1
         0     0% 25.88%      1.05s 19.55%  exam-platform/internal/service.(*Sessions).proctorSession
         0     0% 26.26%      0.88s 16.39%  exam-platform/internal/httpapi.workspaceSocketHandler.func1
         0     0% 26.26%      0.88s 16.39%  exam-platform/internal/workspace.(*Manager).Serve
         0     0% 26.26%      0.88s 16.39%  exam-platform/internal/workspace.(*Service).Serve
         0     0% 26.26%      0.88s 16.39%  exam-platform/internal/workspace.(*relay).attach
```
