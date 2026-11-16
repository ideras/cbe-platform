package e2e

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"exam-platform/internal/loadtest"
)

// phaseLoad is the Phase 8 load test: a class of simulated students (internal/loadtest) takes the load test's workspace exam
// (testdata/workspace/lab-carga) on the real exam server, the real runner and rootless Podman, one container each, for CBE_LOAD_DURATION
// (30 minutes), with the machine read every ten seconds. deploy/workspace/test-load.sh runs it and keeps the results (docs/workspace/load-test.md).
//
//	CBE_LOAD_STUDENTS   how many students (40)       CBE_LOAD_DURATION   the steady state (30m)
//	CBE_E2E_LOAD_PACKAGE   the folder examc compiled lab-carga into
//
// It fails if a student never got a workspace, if an autosave, a file read or a submission failed, or if the 95th percentile of an autosave is
// over the plan's 300 ms (here on the loopback: what the server, the runner and the agent take; the router adds its own, docs/workspace/load-test.md).
func phaseLoad(t *testing.T, r *rig) {
	dir, err := os.MkdirTemp(env(t, "CBE_E2E_STATE"), "load-")
	if err != nil {
		t.Fatal(err)
	}
	r.dir = dir
	if err := os.WriteFile(filepath.Join(dir, "admin-token"), []byte(adminTokn+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r.state.Dir = dir
	r.save()
	students, duration := 40, 30*time.Minute
	if v := os.Getenv("CBE_LOAD_STUDENTS"); v != "" {
		if students, err = strconv.Atoi(v); err != nil || students < 1 {
			t.Fatalf("CBE_LOAD_STUDENTS = %q", v)
		}
	}
	if v := os.Getenv("CBE_LOAD_DURATION"); v != "" {
		if duration, err = time.ParseDuration(v); err != nil {
			t.Fatalf("CBE_LOAD_DURATION = %q", v)
		}
	}
	r.start()
	accounts := loadtest.Accounts("LT", students)
	examID, err := loadtest.Setup(context.Background(), r.base, adminTokn, env(t, "CBE_E2E_LOAD_PACKAGE"), "Load test: "+strconv.Itoa(students)+" students", pin, accounts, duration+time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	defer r.removeWorkspaces(examID)

	// What the machine does: the exam server and the runner, the control group the containers run in, and the runner's own figures for the containers.
	uid := strings.Split(strings.TrimPrefix(env(t, "CBE_E2E_RUNTIME_SOCKET"), "/run/user/"), "/")[0]
	pids := map[string]int{"exam_server": r.cmd.Process.Pid}
	if pid, err := strconv.Atoi(os.Getenv("CBE_E2E_RUNNER_PID")); err == nil {
		pids["runner"] = pid
	}
	host := &loadtest.Host{Pids: pids,
		Groups: map[string]string{"containers_slice": "/sys/fs/cgroup/user.slice/user-" + uid + ".slice"},
		Extra: func() map[string]float64 {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			stats, err := r.runner.ExamStats(ctx, examID)
			if err != nil {
				return nil
			}
			var cpu, memory float64
			var pids uint64
			for _, s := range stats {
				if s.CPUKnown {
					cpu += s.CPUPercent
				}
				memory += float64(s.MemoryBytes) / (1 << 20)
				pids += s.Pids
			}
			return map[string]float64{"containers": float64(len(stats)), "containers_cpu_percent_sum": cpu, "containers_memory_mb_sum": memory, "containers_pids_sum": float64(pids)}
		}}
	// With CBE_LOAD_PROFILE the server (built with -tags pprof, listening on CBE_PPROF) is asked for 30 seconds of its CPU early in the run and again late
	// in it, to find where a server whose load does not change spends more time as the run goes on.
	if os.Getenv("CBE_LOAD_PROFILE") != "" {
		for name, at := range map[string]time.Duration{"early": 2 * time.Minute, "late": duration - 2*time.Minute} {
			name, at := name, at
			go func() {
				time.Sleep(at + 30*time.Second) // the steady state starts about when the logins are done
				resp, err := (&http.Client{Timeout: 2 * time.Minute}).Get("http://" + os.Getenv("CBE_PPROF") + "/debug/pprof/profile?seconds=30")
				if err != nil {
					t.Logf("the %s profile: %v", name, err)
					return
				}
				defer resp.Body.Close()
				data, _ := io.ReadAll(resp.Body)
				_ = os.WriteFile(filepath.Join(dir, "profile-"+name+".pb.gz"), data, 0o644)
			}()
		}
	}
	var started time.Time
	result, err := loadtest.Run(context.Background(), loadtest.Config{Server: r.base, PIN: pin, Accounts: accounts, Duration: duration, Ramp: 30 * time.Second,
		Storms: []time.Duration{duration / 3, duration * 2 / 3}, Sample: host.Sample,
		// The proctor's application is open on the exam, as it is in a class: its two reads, as often as it makes them.
		Poll: []loadtest.Poll{{Name: "sessions", Path: "/admin/sessions?exam_id=" + examID, Token: adminTokn, Every: 3 * time.Second}, {Name: "workspace roster", Path: "/admin/workspace/exam/" + examID + "/roster", Token: adminTokn, Every: 3 * time.Second}},
		Logf: func(format string, args ...any) {
			if started.IsZero() {
				started = time.Now()
			}
			t.Logf("%s "+format, append([]any{time.Since(started).Round(time.Second)}, args...)...)
		}})
	if err != nil {
		t.Fatal(err)
	}

	// The results, for deploy/workspace/test-load.sh to keep.
	data, _ := json.MarshalIndent(map[string]any{"students": result.Students, "ready": result.Ready, "steady_seconds": result.Steady.Seconds(), "summary": result.Summary, "machine": result.Machine()}, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "load-summary.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	series, err := os.Create(filepath.Join(dir, "load-machine.csv"))
	if err != nil {
		t.Fatal(err)
	}
	if err := result.WriteSeries(csv.NewWriter(series)); err != nil {
		t.Fatal(err)
	}
	series.Close()
	var report strings.Builder
	fmt.Fprintf(&report, "%d of %d students worked for %v.\n\n", result.Ready, result.Students, result.Steady.Round(time.Second))
	result.Summary.Markdown(&report)
	report.WriteString("\n")
	result.MachineMarkdown(&report)
	if err := os.WriteFile(filepath.Join(dir, "load-report.md"), []byte(report.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	// What the server said while it went (its warnings and errors), for whoever has to find out why a save was slow.
	if logText, err := os.ReadFile(filepath.Join(dir, "server.log")); err == nil {
		var kept []string
		for _, line := range strings.Split(string(logText), "\n") {
			if strings.Contains(line, "WARN") || strings.Contains(line, "ERROR") {
				kept = append(kept, line)
			}
		}
		if err := os.WriteFile(filepath.Join(dir, "load-server-warnings.log"), []byte(strings.Join(kept, "\n")+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("\n%s", report.String())

	if result.Ready != result.Students {
		t.Errorf("%d of %d students got a workspace", result.Ready, result.Students)
	}
	for _, op := range result.Summary.Ops {
		switch op.Op {
		case loadtest.OpLogin, loadtest.OpReady, loadtest.OpList, loadtest.OpOpen, loadtest.OpSave, loadtest.OpSubmit:
			if op.Failures > 0 {
				t.Errorf("%s failed %d times: %v", op.Op, op.Failures, op.Errors)
			}
		}
		if op.Op == loadtest.OpSave {
			if op.Count == 0 {
				t.Error("no autosave was measured")
			}
			if op.P95MS > 300 {
				t.Errorf("the 95th percentile of an autosave is %.0f ms, over the plan's 300 ms", op.P95MS)
			}
		}
	}
}
