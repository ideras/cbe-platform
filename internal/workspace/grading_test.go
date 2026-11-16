//go:build linux

package workspace_test

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"exam-platform/internal/runner"
	"exam-platform/internal/workspace"
	"exam-platform/internal/workspace/workspacetest"
)

func pts(v float64) *float64 { return &v }

var todos = []workspace.Todo{{ID: "t1", Points: pts(10)}, {ID: "t2", Points: pts(15)}, {ID: "t3", Points: pts(15)}}

// ParseGrade takes what the plan's 5.3 says and refuses everything else with a reason a teacher can act on.
func TestParseGradeReadsTheContractAndRefusesTheRest(t *testing.T) {
	good, total, max, err := workspace.ParseGrade(` {"t1": {"passed": true, "points": 10, "log": "4/4"}, "t2": {"passed": false, "points": 6, "log": "3/5"}} `, todos)
	if err != nil || total != 16 || max == nil || *max != 40 || len(good) != 2 || good[0].TodoID != "t1" || !good[0].Known || good[1].Points != 6 || good[1].Log != "3/5" {
		t.Fatalf("%+v %v %v %v", good, total, max, err)
	}
	// Points that are left out are what the TODO is worth when it passed, and nothing when it did not.
	got, total, _, err := workspace.ParseGrade(`{"t1": {"passed": true}, "t2": {"passed": false}}`, todos)
	if err != nil || total != 10 || got[0].Points != 10 || got[1].Points != 0 {
		t.Errorf("default points: %+v %v %v", got, total, err)
	}
	// Points over what a TODO is worth are capped, and the log says so.
	got, total, _, err = workspace.ParseGrade(`{"t1": {"passed": true, "points": 99, "log": "x"}}`, todos)
	if err != nil || total != 10 || got[0].Points != 10 || !strings.Contains(got[0].Log, "worth 10") {
		t.Errorf("capped points: %+v %v %v", got, total, err)
	}
	// A key that is not a TODO is shown and not counted; a TODO that is not mentioned has no result.
	got, total, _, err = workspace.ParseGrade(`{"t1": {"passed": true, "points": 10}, "t9": {"passed": true, "points": 50, "log": "?"}}`, todos)
	if err != nil || total != 10 || len(got) != 2 || got[1].TodoID != "t9" || got[1].Known {
		t.Errorf("unknown key: %+v %v %v", got, total, err)
	}
	// TODOs with no points have no maximum.
	_, _, max, err = workspace.ParseGrade(`{"a": {"passed": true}}`, []workspace.Todo{{ID: "a"}})
	if err != nil || max != nil {
		t.Errorf("max = %v %v", max, err)
	}
	// An empty object is a valid answer: nothing was graded.
	if got, total, _, err = workspace.ParseGrade(`{}`, todos); err != nil || len(got) != 0 || total != 0 {
		t.Errorf("{}: %+v %v %v", got, total, err)
	}
	// A log that is very long is cut, at a whole character.
	got, _, _, _ = workspace.ParseGrade(`{"t1": {"passed": true, "log": "`+strings.Repeat("é", 10000)+`"}}`, todos)
	if len(got[0].Log) > 8<<10+64 || !strings.HasSuffix(got[0].Log, "(the log was cut here)") {
		t.Errorf("a long log: %d bytes", len(got[0].Log))
	}

	for what, bad := range map[string]struct{ output, mention string }{
		"nothing":                        {"", "printed nothing"},
		"only spaces":                    {" \n\t ", "printed nothing"},
		"noise before":                   {"make: entering directory\n" + `{"t1": {"passed": true}}`, "not one JSON object"},
		"text":                           {"all tests passed", "not one JSON object"},
		"an array":                       {`[{"passed": true}]`, "not one JSON object"},
		"noise after":                    {`{"t1": {"passed": true}}` + "\nbye", "after its JSON object"},
		"two objects":                    {`{"t1": {"passed": true}} {"t2": {"passed": true}}`, "after its JSON object"},
		"no passed":                      {`{"t1": {"points": 10}}`, `"t1"`},
		"passed as a string":             {`{"t1": {"passed": "yes"}}`, `"t1"`},
		"a result that is not an object": {`{"t1": true}`, `"t1"`},
		"points as a string":             {`{"t1": {"passed": true, "points": "10"}}`, `"t1"`},
		"negative points":                {`{"t1": {"passed": true, "points": -1}}`, "zero or more"},
		"log as a number":                {`{"t1": {"passed": true, "log": 5}}`, `"t1"`},
		"truncated":                      {`{"t1": {"passed": tr`, "not one JSON object"},
	} {
		if _, _, _, err := workspace.ParseGrade(bad.output, todos); err == nil || !strings.Contains(err.Error(), bad.mention) {
			t.Errorf("%s: %v (wanted a mention of %q)", what, err, bad.mention)
		}
	}
}

// memRepo is the part of the repository the grader uses, in memory. The rest is not there: a call to it panics, and so shows the grader
// asking for something it should not.
type memRepo struct {
	workspace.Repository
	mu          sync.Mutex
	exam        workspace.ExamRecord
	sessions    map[string]workspace.SessionInfo
	submissions map[string][]workspace.SubmissionRecord
	gradings    map[int64]*workspace.GradingRecord
	next        int64
	events      []string
}

func (r *memRepo) Exam(context.Context, string) (workspace.ExamRecord, error) { return r.exam, nil }
func (r *memRepo) Session(_ context.Context, id string) (workspace.SessionInfo, error) {
	if info, ok := r.sessions[id]; ok {
		return info, nil
	}
	return workspace.SessionInfo{}, workspace.ErrNotFound
}
func (r *memRepo) Roster(context.Context, string) ([]workspace.RosterRow, error) {
	var out []workspace.RosterRow
	for id, info := range r.sessions {
		out = append(out, workspace.RosterRow{SessionID: id, Account: info.Account})
	}
	return out, nil
}
func (r *memRepo) Submissions(_ context.Context, id string) ([]workspace.SubmissionRecord, error) {
	return r.submissions[id], nil
}
func (r *memRepo) Submission(_ context.Context, id int64) (workspace.SubmissionRecord, error) {
	for _, list := range r.submissions {
		for _, s := range list {
			if s.ID == id {
				return s, nil
			}
		}
	}
	return workspace.SubmissionRecord{}, workspace.ErrNotFound
}
func (r *memRepo) Gradings(_ context.Context, id string) ([]workspace.GradingRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []workspace.GradingRecord
	for _, g := range r.gradings {
		if g.SessionID == id {
			out = append(out, *g)
		}
	}
	return out, nil
}
func (r *memRepo) Grading(_ context.Context, id int64) (workspace.GradingRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if g, ok := r.gradings[id]; ok {
		return *g, nil
	}
	return workspace.GradingRecord{}, workspace.ErrNotFound
}
func (r *memRepo) CreateGrading(_ context.Context, session string, submission int64, now time.Time) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.next++
	r.gradings[r.next] = &workspace.GradingRecord{ID: r.next, SessionID: session, SubmissionID: submission, State: workspace.GradingQueued, QueuedAt: now}
	return r.next, nil
}
func (r *memRepo) StartGrading(_ context.Context, id int64, _ time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.gradings[id].State = workspace.GradingRunning
	return nil
}
func (r *memRepo) FinishGrading(_ context.Context, record workspace.GradingRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	copy := record
	r.gradings[record.ID] = &copy
	return nil
}
func (r *memRepo) InterruptGradings(context.Context, time.Time) (int, error) { return 0, nil }
func (r *memRepo) RecordEvent(_ context.Context, _ string, kind string, _ time.Time, _ any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, kind)
	return nil
}

// graderRig has n students, each with a submission of one file naming them, and an exam with a grading bundle.
func graderRig(t *testing.T, n, parallel int) (*workspace.Grader, *memRepo, *workspacetest.Runner) {
	t.Helper()
	dir := t.TempDir()
	runnerFake := workspacetest.New(dir)
	t.Cleanup(runnerFake.Close)
	blobs, err := workspace.NewBlobs(dir + "/blobs")
	if err != nil {
		t.Fatal(err)
	}
	var bundle bytes.Buffer
	archive := tar.NewWriter(&bundle)
	_ = archive.WriteHeader(&tar.Header{Name: "grade", Mode: 0o755, Size: 4, Typeflag: tar.TypeReg})
	_, _ = archive.Write([]byte("#!sh"))
	_ = archive.Close()
	bundleHash, err := blobs.PutBytes(bundle.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	repo := &memRepo{sessions: map[string]workspace.SessionInfo{}, submissions: map[string][]workspace.SubmissionRecord{}, gradings: map[int64]*workspace.GradingRecord{},
		exam: workspace.ExamRecord{ExamID: "exam", GradingHash: bundleHash, Document: workspace.Document{Workspace: workspace.Settings{Image: "cbe/cpp20", Resources: workspace.Resources{CPU: 1, MemoryMB: 256, DiskMB: 50}},
			Content: workspace.Content{Todos: []workspace.Todo{{ID: "t1", Points: pts(10)}}}}}}
	for i := 0; i < n; i++ {
		session := fmt.Sprintf("s%02d", i)
		repo.sessions[session] = workspace.SessionInfo{SessionID: session, ExamID: "exam", Account: fmt.Sprintf("%04d", i)}
		file, err := blobs.PutBytes([]byte(session))
		if err != nil {
			t.Fatal(err)
		}
		manifest, _ := json.Marshal(workspace.Manifest{Version: 1, Entries: []workspace.Entry{{Path: "who.txt", Type: "file", Size: int64(len(session)), Mode: 0o644, Hash: file}}})
		manifestHash, err := blobs.PutBytes(manifest)
		if err != nil {
			t.Fatal(err)
		}
		repo.submissions[session] = []workspace.SubmissionRecord{{ID: int64(i + 1), SessionID: session, Kind: workspace.SubmissionSubmit, SubmittedAt: time.Now(), ManifestHash: manifestHash, Files: 1}}
	}
	manager := workspace.NewManager(workspace.Options{Repo: repo, Runner: runnerFake, Blobs: blobs})
	t.Cleanup(manager.Close)
	return workspace.NewGrader(manager, workspace.GraderOptions{Parallel: parallel, DefaultTimeout: time.Minute}), repo, runnerFake
}

func (r *memRepo) allOver() (done, failed, other int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, g := range r.gradings {
		switch g.State {
		case workspace.GradingDone:
			done++
		case workspace.GradingFailed:
			failed++
		default:
			other++
		}
	}
	return
}

// Forty students are graded with at most the bound going at once, each from their own files, and a student whose run fails costs the
// others nothing.
func TestFortySubmissionsAreGradedWithBoundedParallelism(t *testing.T) {
	const students, parallel = 40, 4
	grader, repo, fake := graderRig(t, students, parallel)
	var inFlight, peak atomic.Int32
	fake.GradeHook = func(ctx context.Context, call workspacetest.GradeCall) (runner.GradeResult, error) {
		now := inFlight.Add(1)
		defer inFlight.Add(-1)
		for {
			old := peak.Load()
			if now <= old || peak.CompareAndSwap(old, now) {
				break
			}
		}
		time.Sleep(15 * time.Millisecond)
		who := call.Files["who.txt"]
		if strings.HasSuffix(who, "7") {
			return runner.GradeResult{ExitCode: 3, Stderr: "crashed for " + who}, nil
		}
		return runner.GradeResult{Stdout: fmt.Sprintf(`{"t1": {"passed": true, "points": 10, "log": %q}}`, who)}, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { grader.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()

	started, err := grader.StartExam(ctx, "exam", nil)
	if err != nil || len(started.Queued) != students || len(started.Skipped) != 0 {
		t.Fatalf("started = %+v %v", started, err)
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		if _, _, other := repo.allOver(); other == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the runs did not end")
		}
		time.Sleep(10 * time.Millisecond)
	}
	good, failed, _ := repo.allOver()
	// s07, s17 and s27 and s37 end in 7: four students crash; the other thirty-six are graded.
	if good != 36 || failed != 4 {
		t.Errorf("done %d, failed %d", good, failed)
	}
	if peak.Load() > parallel || fake.GradePeak > parallel || peak.Load() < 2 {
		t.Errorf("peak in flight %d (runner saw %d), bound %d", peak.Load(), fake.GradePeak, parallel)
	}
	// Every student got their own files and their own result.
	for _, g := range repo.gradings {
		who := fmt.Sprintf("s%02d", g.SubmissionID-1)
		if g.State == workspace.GradingDone && (len(g.Results) != 1 || g.Results[0].Log != who || g.SessionID != who) {
			t.Errorf("grading %d of %s has %+v", g.ID, g.SessionID, g.Results)
		}
		if g.State == workspace.GradingFailed && (g.Failure != workspace.FailCrash || !strings.Contains(g.Stderr, who)) {
			t.Errorf("grading %d of %s failed as %q (%s) with %q", g.ID, g.SessionID, g.Failure, g.FailureText, g.Stderr)
		}
	}
	// The same request again queues nobody twice while none is going, but a new run each: a student can be graded again.
	again, err := grader.StartExam(ctx, "exam", []string{"s00", "s01"})
	if err != nil || len(again.Queued) != 2 {
		t.Errorf("again = %+v %v", again, err)
	}
}

// A run that was going when the server was stopped ends as failed, not as a run that never ends.
func TestARunGoingWhenTheServerStopsIsRecordedAsInterrupted(t *testing.T) {
	grader, repo, fake := graderRig(t, 1, 1)
	entered := make(chan struct{})
	fake.GradeHook = func(ctx context.Context, call workspacetest.GradeCall) (runner.GradeResult, error) {
		close(entered)
		<-ctx.Done()
		return runner.GradeResult{}, ctx.Err()
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { grader.Run(ctx); close(done) }()
	if _, err := grader.StartSession(ctx, "s00", 0); err != nil {
		t.Fatal(err)
	}
	<-entered
	cancel()
	<-done
	got := repo.gradings[1]
	if got.State != workspace.GradingFailed || got.Failure != workspace.FailInterrupted {
		t.Errorf("got = %+v", got)
	}
}
