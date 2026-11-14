//go:build linux

package e2e

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"exam-platform/internal/runner"
	"exam-platform/internal/workspace/workspacetest"
)

// Phase E is Phase 6's real test: grading with nothing replaced. deploy/workspace/test-grading.sh starts a private cbe-runner (its own
// socket and folders, the same runtime and quota helper) so that nobody's containers are touched, and runs this against it, with
//
//	CBE_E2E_SUBMISSIONS   testdata/workspace/lab-listas-submissions, copied where this account can read it
//
// It does two things. First, through the exam server and the proctor's routes: three students of the example package (one who solves
// it, one who half does, one who does nothing) are graded by the example's own grade script in real containers, and what the plan's
// acceptance asks of it is checked. Second, straight at the runner, with grade programs that misbehave: what a grading container is
// and is not allowed, a run that does not end, one that eats memory, one that floods its output, and forty runs at once.

// overlay writes the files of a fixture folder into a student's workspace, through the student's own connection.
func overlay(t *testing.T, c *workspacetest.Client, dir string) {
	t.Helper()
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		reply := c.Call(map[string]any{"type": "fs.create", "path": rel, "content": string(data)})
		if workspacetest.Code(reply) == "EXISTS" {
			read := c.Call(map[string]any{"type": "fs.read", "path": rel})
			reply = c.Call(map[string]any{"type": "fs.write", "path": rel, "content": string(data), "base_version": read["version"]})
		}
		if !workspacetest.OK(reply) {
			t.Fatalf("putting %s in the workspace: %v", rel, reply)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

type gradingView struct {
	ID          int64    `json:"id"`
	State       string   `json:"state"`
	Failure     string   `json:"failure"`
	FailureText string   `json:"failure_text"`
	Points      *float64 `json:"points"`
	Max         *float64 `json:"max_points"`
	Confirmed   bool     `json:"confirmed"`
	DurationMS  int64    `json:"duration_ms"`
	Stdout      string   `json:"stdout"`
	Stderr      string   `json:"stderr"`
	Results     []struct {
		TodoID string  `json:"todo_id"`
		Passed bool    `json:"passed"`
		Points float64 `json:"points"`
		Log    string  `json:"log"`
	} `json:"results"`
}

func (r *rig) gradings(session string) []gradingView {
	r.t.Helper()
	var out struct {
		Gradings []gradingView `json:"gradings"`
	}
	if err := json.Unmarshal(r.admin("GET", "/admin/workspace/session/"+session+"/gradings", "", nil, 200), &out); err != nil {
		r.t.Fatal(err)
	}
	return out.Gradings
}

// tarBundle makes a bundle of name -> content (a name ending in .sh, or grade, can be run).
func tarBundle(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := tar.NewWriter(&buf)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		mode := int64(0o644)
		if name == "grade" || strings.HasSuffix(name, ".sh") {
			mode = 0o755
		}
		if err := w.WriteHeader(&tar.Header{Name: name, Mode: mode, Size: int64(len(files[name])), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(files[name]))
	}
	_ = w.Close()
	return buf.Bytes()
}

func phaseGrading(t *testing.T, r *rig) {
	submissions := env(t, "CBE_E2E_SUBMISSIONS")
	dir, err := os.MkdirTemp(env(t, "CBE_E2E_STATE"), "grading-")
	if err != nil {
		t.Fatal(err)
	}
	r.dir = dir
	if err := os.WriteFile(filepath.Join(dir, "admin-token"), []byte(adminTokn+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	r.start()
	var class struct {
		Class struct {
			ID string `json:"class_id"`
		} `json:"class"`
	}
	if err := json.Unmarshal(r.admin("POST", "/admin/classes?name=Grading", "text/csv", strings.NewReader(classCSV), 201), &class); err != nil || class.Class.ID == "" {
		t.Fatalf("class: %v", err)
	}
	examID := r.importExam(class.Class.ID, "Grade the example", 0, 0)
	r.admin("POST", "/admin/exam/"+examID+"/publish", "application/json", strings.NewReader(`{"expected_current":""}`), 200)
	defer r.removeWorkspaces(examID)

	t.Run("the example package's grade script, in real containers", func(t *testing.T) { gradeTheExample(t, r, examID, submissions) })
	t.Run("what a grading container is and is not allowed", func(t *testing.T) { whatAGradingContainerIs(t, r) })
	t.Run("a run that does not end, runs out of memory, floods or fails", func(t *testing.T) { runsThatGoWrong(t, r) })
	t.Run("forty runs at once", func(t *testing.T) { fortyRuns(t, r) })
}

func gradeTheExample(t *testing.T, r *rig, examID, submissions string) {
	type student struct{ account, overlay string }
	students := []student{{"0001", "good"}, {"0002", "partial"}, {"0003", ""}}
	sessions := map[string]sessionState{}
	clients := map[string]*workspacetest.Client{}
	for _, s := range students {
		session := r.login(s.account)
		sessions[s.account] = session
		r.waitRunning(session)
		c := workspacetest.Dial(t, r.base, session.Token)
		defer c.Close()
		clients[s.account] = c
		if s.overlay != "" {
			overlay(t, c, filepath.Join(submissions, s.overlay))
		}
		if reply := c.Call(map[string]any{"type": "submit"}); !workspacetest.OK(reply) {
			t.Fatalf("submit %s: %v", s.account, reply)
		}
	}
	// What each student submitted, as the proctor downloads it: it must be the same after grading.
	zipBefore := map[string][]byte{}
	for account, session := range sessions {
		var view struct {
			Submissions []struct {
				ID int64 `json:"id"`
			} `json:"submissions"`
		}
		if err := json.Unmarshal(r.admin("GET", "/admin/workspace/session/"+session.ID, "", nil, 200), &view); err != nil || len(view.Submissions) == 0 {
			t.Fatalf("submissions of %s: %v", account, err)
		}
		zipBefore[account] = r.admin("GET", fmt.Sprintf("/admin/workspace/submission/%d/zip", view.Submissions[len(view.Submissions)-1].ID), "", nil, 200)
	}

	var started struct {
		Queued  []int64           `json:"queued"`
		Skipped []json.RawMessage `json:"skipped"`
	}
	if err := json.Unmarshal(r.admin("POST", "/admin/workspace/exam/"+examID+"/grade", "", nil, 202), &started); err != nil || len(started.Queued) != 3 || len(started.Skipped) != 0 {
		t.Fatalf("grading the class: %+v %v", started, err)
	}
	want := map[string]struct {
		points float64
		state  string
	}{"0001": {40, "done"}, "0002": {16, "done"}, "0003": {0, "done"}}
	got := map[string]gradingView{}
	workspacetest.Eventually(t, 5*time.Minute, "the three gradings to end", func() bool {
		for account, session := range sessions {
			list := r.gradings(session.ID)
			if len(list) == 0 || (list[0].State != "done" && list[0].State != "failed") {
				return false
			}
			got[account] = list[0]
		}
		return true
	})
	for account, expected := range want {
		g := got[account]
		if g.State != expected.state || g.Points == nil || *g.Points != expected.points || g.Max == nil || *g.Max != 40 || g.Confirmed || len(g.Results) != 3 {
			t.Errorf("%s: %+v", account, g)
		}
		t.Logf("%s: %v of 40 in %d ms", account, *g.Points, g.DurationMS)
	}
	if l := got["0002"].Results; len(l) == 3 && (l[1].TodoID != "t2" || l[1].Points != 6 || l[1].Log != "2/5 tests passed" || l[2].Log != "tests/mine.cpp is not there") {
		t.Errorf("the partial student's results: %+v", l)
	}
	detail := func(account string) gradingView {
		var g gradingView
		if err := json.Unmarshal(r.admin("GET", fmt.Sprintf("/admin/workspace/grading/%d", got[account].ID), "", nil, 200), &g); err != nil {
			t.Fatal(err)
		}
		return g
	}
	if g := detail("0001"); !strings.Contains(g.Stderr, "ok: insert_sorted 3 9 1 5 -> 1 3 5 9") || strings.Contains(g.Stdout, "ok:") {
		t.Errorf("the good student's run: stdout %q, stderr %q", g.Stdout, g.Stderr)
	}
	if g := detail("0003"); !strings.Contains(g.Stderr, "FAILED: push_front 1 2 3 -> 3 2 1") {
		t.Errorf("the student who did nothing: stderr %q", g.Stderr)
	}

	// A grading run cannot change the stored submission, nor the student's own files.
	for account, session := range sessions {
		var view struct {
			Submissions []struct {
				ID int64 `json:"id"`
			} `json:"submissions"`
		}
		_ = json.Unmarshal(r.admin("GET", "/admin/workspace/session/"+session.ID, "", nil, 200), &view)
		after := r.admin("GET", fmt.Sprintf("/admin/workspace/submission/%d/zip", view.Submissions[len(view.Submissions)-1].ID), "", nil, 200)
		if !bytes.Equal(after, zipBefore[account]) {
			t.Errorf("%s: the stored submission changed", account)
		}
	}
	// The grading files are never in a student's container: not the folder, not the files, not in what the server keeps of the student.
	for account, c := range clients {
		term := openTerminal(t, c)
		if code, out := sh(t, c, term, "ls /grading"); code == 0 || !strings.Contains(out, "No such file") {
			t.Errorf("%s: /grading is there: %d %s", account, code, out)
		}
		if code, out := sh(t, c, term, "echo count=$(grep -rl HIDDEN-CASES-MARKER /work /home /tmp /run /usr/local 2>/dev/null | wc -l)"); code != 0 || !strings.Contains(out, "count=0") {
			t.Errorf("%s: the hidden cases are in the container: %d %s", account, code, out)
		}
	}
	for account, session := range sessions {
		snapshots := r.adminSession(session).Snapshots
		files := r.snapshotFiles(snapshots[len(snapshots)-1].ID)
		for name, content := range files {
			if strings.Contains(content, "HIDDEN-CASES-MARKER") || strings.Contains(name, "hidden_cases") || name == "grade" || strings.HasPrefix(name, "tests/driver") {
				t.Errorf("%s: the snapshot has a grading file: %s", account, name)
			}
		}
	}
	// And the student's own workspace is as it was: the good student's list is still the one they wrote.
	read := clients["0001"].Call(map[string]any{"type": "fs.read", "path": "src/list.cpp"})
	if !strings.Contains(fmt.Sprint(read["content"]), "head_ = new Node{value, head_};") {
		t.Errorf("the good student's file changed: %v", read)
	}

	// Grading again is another run; the proctor confirms one.
	r.admin("POST", "/admin/workspace/session/"+sessions["0002"].ID+"/grade", "", nil, 202)
	workspacetest.Eventually(t, 3*time.Minute, "the second run", func() bool {
		list := r.gradings(sessions["0002"].ID)
		return len(list) == 2 && list[0].State == "done"
	})
	first := r.gradings(sessions["0002"].ID)[0]
	r.admin("POST", fmt.Sprintf("/admin/workspace/grading/%d/confirm", first.ID), "", nil, 200)
	var roster struct {
		Students []struct {
			Account string `json:"account_number"`
			Grade   *struct {
				Points    float64 `json:"points"`
				Confirmed bool    `json:"confirmed"`
			} `json:"grade"`
		} `json:"students"`
	}
	if err := json.Unmarshal(r.admin("GET", "/admin/workspace/exam/"+examID+"/roster", "", nil, 200), &roster); err != nil {
		t.Fatal(err)
	}
	for _, s := range roster.Students {
		if s.Account == "0002" && (s.Grade == nil || !s.Grade.Confirmed || s.Grade.Points != 16) {
			t.Errorf("the roster's grade of 0002: %+v", s.Grade)
		}
		if s.Account == "0001" && (s.Grade == nil || s.Grade.Confirmed || s.Grade.Points != 40) {
			t.Errorf("the roster's grade of 0001: %+v", s.Grade)
		}
	}
}

// grade runs a grade program of the given text against a submission of the given files, in a real grading container.
func (r *rig) gradeWith(t *testing.T, id string, script string, submission map[string]string, timeout int, memoryMB int) (runner.GradeResult, error) {
	t.Helper()
	req := runner.GradeRequest{Grading: id, Exam: "e2e-probe", Image: "cbe/cpp20", TimeoutSeconds: timeout, Resources: runner.Resources{CPU: 1, MemoryMB: memoryMB, DiskMB: 50}}
	return r.runner.Grade(context.Background(), req, bytes.NewReader(tarBundle(t, submission)), bytes.NewReader(tarBundle(t, map[string]string{"grade": script, "data/secret.txt": "S3CRET\n"})))
}

func whatAGradingContainerIs(t *testing.T, r *rig) {
	probe := `#!/bin/sh
echo "uid=$(id -u) gid=$(id -g) home=$HOME pwd=$(pwd) grading=$CBE_GRADING" >&2
echo "work=$(ls /work | tr '\n' ' ')" >&2
if touch /grading/x 2>/dev/null; then echo "grading_writable=yes" >&2; else echo "grading_writable=no" >&2; fi
if rm /grading/data/secret.txt 2>/dev/null; then echo "grading_removable=yes" >&2; else echo "grading_removable=no" >&2; fi
if touch /usr/x 2>/dev/null; then echo "rootfs_writable=yes" >&2; else echo "rootfs_writable=no" >&2; fi
if cp /bin/true /tmp/t 2>/dev/null && /tmp/t 2>/dev/null; then echo "tmp_exec=yes" >&2; else echo "tmp_exec=no" >&2; fi
if timeout 5 bash -c 'exec 3<>/dev/tcp/1.1.1.1/53' 2>/dev/null; then echo "network=yes" >&2; else echo "network=no" >&2; fi
echo "interfaces=$(ls /sys/class/net | tr '\n' ' ')" >&2
echo "outside=$(ls /data /home 2>&1 | tr '\n' ' ')" >&2
grep -E 'CapEff|NoNewPrivs' /proc/self/status | tr '\t' ' ' | tr '\n' ' ' >&2; echo >&2
echo "work_writable=$(touch /work/mine 2>&1 && echo yes)" >&2
echo '{}'
`
	// Two different submissions at the same time: each run sees its own, and only its own.
	var wg sync.WaitGroup
	results := make([]runner.GradeResult, 2)
	errs := make([]error, 2)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errs[i] = r.gradeWith(t, fmt.Sprintf("probe%d", i), probe, map[string]string{fmt.Sprintf("only-%d.txt", i): "x"}, 60, 256)
		}()
	}
	wg.Wait()
	for i, result := range results {
		if errs[i] != nil {
			t.Fatalf("probe %d: %v", i, errs[i])
		}
		out := result.Stderr
		t.Logf("probe %d:\n%s", i, out)
		for _, want := range []string{"uid=1000 gid=1000 home=/work pwd=/work grading=1", fmt.Sprintf("work=only-%d.txt ", i), "grading_writable=no", "grading_removable=no", "rootfs_writable=no", "tmp_exec=no", "network=no",
			"interfaces=lo ", "NoNewPrivs:", "work_writable=yes"} {
			if !strings.Contains(out, want) {
				t.Errorf("probe %d: lacks %q", i, want)
			}
		}
		if strings.Contains(out, fmt.Sprintf("only-%d.txt", 1-i)) || strings.Contains(out, "outside=data") || strings.Contains(out, "outside=home") && strings.Contains(out, "student") {
			t.Errorf("probe %d sees what it should not:\n%s", i, out)
		}
		if !strings.Contains(out, "CapEff: 0000000000000000") {
			t.Errorf("probe %d has capabilities:\n%s", i, out)
		}
		if result.ExitCode != 0 || result.TimedOut || strings.TrimSpace(result.Stdout) != "{}" {
			t.Errorf("probe %d: %+v", i, result)
		}
	}
}

func runsThatGoWrong(t *testing.T, r *rig) {
	// A run that does not end is stopped at its timeout, and what it printed is returned.
	started := time.Now()
	result, err := r.gradeWith(t, "slow", "#!/bin/sh\necho started\nsleep 600\n", map[string]string{"a": "x"}, 10, 256)
	took := time.Since(started)
	if err != nil || !result.TimedOut || !strings.Contains(result.Stdout, "started") || took < 10*time.Second || took > 40*time.Second {
		t.Errorf("a run that does not end: %+v %v after %v", result, err, took)
	}
	// A run that eats memory is killed for it. When it is the program the container runs (grade becomes the hog) the runtime says it was killed
	// for memory; when it is a child of grade, the child is killed and grade goes on, ending with its own exit code, which fails the run all
	// the same.
	result, err = r.gradeWith(t, "hungry", "#!/bin/sh\nexec perl -e '$x = \"a\" x 900_000_000; print length($x)'\n", map[string]string{"a": "x"}, 60, 128)
	// (The runtime of this host does not always set its OOM flag; the kill, exit code 137, is what it always shows.)
	if err != nil || (!result.OOMKilled && result.ExitCode != 137) || result.TimedOut {
		t.Errorf("a run that eats memory: %+v %v", result, err)
	}
	result, err = r.gradeWith(t, "hungrychild", "#!/bin/sh\nperl -e '$x = \"a\" x 900_000_000; print length($x)'\n", map[string]string{"a": "x"}, 60, 128)
	if err != nil || result.ExitCode == 0 || !strings.Contains(result.Stderr, "Killed") {
		t.Errorf("a child that eats memory: %+v %v", result, err)
	}
	// A run that floods its output is cut at the limit, and ends.
	result, err = r.gradeWith(t, "flood", "#!/bin/sh\nyes xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx | head -c 20000000\necho done >&2\n", map[string]string{"a": "x"}, 60, 256)
	if err != nil || !result.StdoutTruncated || len(result.Stdout) != 256<<10 || result.TimedOut {
		t.Errorf("a run that floods: truncated %v, %d bytes, %+v %v", result.StdoutTruncated, len(result.Stdout), result.TimedOut, err)
	}
	// A run that fails has its exit code and its error output.
	result, err = r.gradeWith(t, "fails", "#!/bin/sh\necho 'it broke' >&2\nexit 3\n", map[string]string{"a": "x"}, 60, 256)
	if err != nil || result.ExitCode != 3 || result.Stderr != "it broke\n" {
		t.Errorf("a run that fails: %+v %v", result, err)
	}
	// A fork bomb is held by the limit on processes, and the run ends.
	result, err = r.gradeWith(t, "forks", "#!/bin/sh\nf() { f | f & }\nf\nsleep 3\necho survived >&2\n", map[string]string{"a": "x"}, 20, 256)
	if err != nil || result.OOMKilled {
		t.Errorf("a fork bomb: %+v %v", result, err)
	}
	// A bundle with no grade, and an image that is not approved, are refused before a container is made.
	req := runner.GradeRequest{Grading: "refused", Exam: "e2e-probe", Image: "cbe/cpp20", TimeoutSeconds: 10}
	if _, err := r.runner.Grade(context.Background(), req, bytes.NewReader(tarBundle(t, map[string]string{"a": "x"})), bytes.NewReader(tarBundle(t, map[string]string{"data": "no grade here"}))); err == nil {
		t.Error("a bundle with no grade was run")
	}
	req.Image = "docker.io/library/alpine"
	if _, err := r.runner.Grade(context.Background(), req, bytes.NewReader(tarBundle(t, map[string]string{"a": "x"})), bytes.NewReader(tarBundle(t, map[string]string{"grade": "#!/bin/sh\n"}))); err == nil {
		t.Error("an image that is not approved was run")
	}
}

// memory reads what the host has free, in MiB, and its load.
func hostNow(t *testing.T) (availableMB int, load string) {
	t.Helper()
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "MemAvailable:") {
			kb, _ := strconv.Atoi(strings.Fields(line)[1])
			availableMB = kb / 1024
		}
	}
	loadavg, _ := os.ReadFile("/proc/loadavg")
	return availableMB, strings.TrimSpace(string(loadavg))
}

func fortyRuns(t *testing.T, r *rig) {
	const total = 40
	// Each run says when it began and ended, in milliseconds; from the 40 intervals the most that were going at once is read.
	script := "#!/bin/sh\necho \"$(date +%s%3N)\"\nsleep 1\necho \"$(date +%s%3N)\"\n"
	beforeMB, beforeLoad := hostNow(t)
	var mu sync.Mutex
	var peakMB int
	stopSampling := make(chan struct{})
	sampled := make(chan struct{})
	go func() {
		defer close(sampled)
		lowest := beforeMB
		for {
			select {
			case <-stopSampling:
				mu.Lock()
				peakMB = beforeMB - lowest
				mu.Unlock()
				return
			case <-time.After(500 * time.Millisecond):
				if now, _ := hostNow(t); now < lowest {
					lowest = now
				}
			}
		}
	}()
	type interval struct{ start, end int64 }
	intervals := make([]interval, total)
	errs := make([]error, total)
	began := time.Now()
	var wg sync.WaitGroup
	for i := 0; i < total; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := r.gradeWith(t, fmt.Sprintf("bulk%02d", i), script, map[string]string{fmt.Sprintf("f%d.txt", i): "x"}, 60, 128)
			if err != nil {
				errs[i] = err
				return
			}
			var s, e int64
			if _, err := fmt.Sscanf(strings.ReplaceAll(strings.TrimSpace(result.Stdout), "\n", " "), "%d %d", &s, &e); err != nil || result.ExitCode != 0 {
				errs[i] = fmt.Errorf("run %d: %+v %v", i, result, err)
				return
			}
			intervals[i] = interval{s, e}
		}()
	}
	wg.Wait()
	elapsed := time.Since(began)
	close(stopSampling)
	<-sampled
	for i, err := range errs {
		if err != nil {
			t.Errorf("run %d: %v", i, err)
		}
	}
	// The most runs going at once, from the starts and the ends.
	type event struct {
		at    int64
		delta int
	}
	var events []event
	for _, iv := range intervals {
		if iv.start > 0 {
			events = append(events, event{iv.start, 1}, event{iv.end, -1})
		}
	}
	sort.Slice(events, func(i, j int) bool {
		if events[i].at != events[j].at {
			return events[i].at < events[j].at
		}
		return events[i].delta < events[j].delta
	})
	running, peak := 0, 0
	for _, e := range events {
		if running += e.delta; running > peak {
			peak = running
		}
	}
	afterMB, afterLoad := hostNow(t)
	mu.Lock()
	dropMB := peakMB
	mu.Unlock()
	t.Logf("forty runs of one second: %v in all, at most %d at once; host memory available %d MiB before, %d MiB lowest (%d MiB less), %d MiB after; load %s before, %s after", elapsed.Round(time.Second), peak, beforeMB, beforeMB-dropMB, dropMB, afterMB, beforeLoad, afterLoad)
	if peak > 2 || peak < 2 {
		t.Errorf("the runner was set to go two at once; the most going at once was %d", peak)
	}
	if elapsed < 15*time.Second {
		t.Errorf("forty runs of a second each, two at once, took %v", elapsed)
	}
	if dropMB > 3072 {
		t.Errorf("the host lost %d MiB of free memory while grading", dropMB)
	}
}
