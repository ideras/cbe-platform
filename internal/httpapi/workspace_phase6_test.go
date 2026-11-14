//go:build linux

package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"exam-platform/internal/runner"
	"exam-platform/internal/workspace"
	"exam-platform/internal/workspace/workspacetest"
)

// ---------------------------------------------------------------------------------------------------------------------------------
// Phase 6: grading. These go through the proctor's routes and the real database, with the fake runner standing in for cbe-runner (the
// real one is run by deploy/workspace/test-server.sh).

func points(f ...float64) []*float64 {
	out := make([]*float64, len(f))
	for i := range f {
		v := f[i]
		out[i] = &v
	}
	return out
}

// withPoints makes the two TODOs of the fixture's exam worth 10 and 15.
func withPoints(d *workspace.Document) {
	p := points(10, 15)
	d.Content.Todos[0].Points, d.Content.Todos[1].Points = p[0], p[1]
}

func (f *workspaceFixture) post(t *testing.T, path, body string) (int, []byte) {
	t.Helper()
	w := request(f.router, "POST", path, body, "Bearer "+testAdminToken)
	return w.Code, w.Body.Bytes()
}

// waitEvent waits for an event of a kind to be in a session's history.
func (f *workspaceFixture) waitEvent(t *testing.T, session, kind string) {
	t.Helper()
	eventually(t, "the "+kind+" event of "+session, func() bool {
		for _, e := range f.history(t, session) {
			if e.Type == kind {
				return true
			}
		}
		return false
	})
}

func (f *workspaceFixture) gradingsOf(t *testing.T, session string) workspace.AdminGradings {
	t.Helper()
	w := request(f.router, "GET", "/admin/workspace/session/"+session+"/gradings", "", "Bearer "+testAdminToken)
	if w.Code != http.StatusOK {
		t.Fatalf("gradings = %d %s", w.Code, w.Body.String())
	}
	return decoded[workspace.AdminGradings](t, w.Body.Bytes())
}

// gradedWith waits for the latest grading of a session to be over and returns it.
func (f *workspaceFixture) gradedWith(t *testing.T, session string) workspace.AdminGrading {
	t.Helper()
	var got workspace.AdminGrading
	eventually(t, "the grading of "+session, func() bool {
		list := f.gradingsOf(t, session).Gradings
		if len(list) == 0 || (list[0].State != workspace.GradingDone && list[0].State != workspace.GradingFailed) {
			return false
		}
		got = list[0]
		return true
	})
	return got
}

// submitted has a student log in, write a file and press Save: a submission to grade.
func (f *workspaceFixture) submitted(t *testing.T, account, content string) loginResponse {
	t.Helper()
	login := f.loginReady(t, account)
	writeWork(t, f, login.SessionID, "src/answer.c", content)
	c := f.dial(t, login.SessionToken)
	if reply := c.Call(map[string]any{"type": "submit"}); !ok(reply) {
		t.Fatal(reply)
	}
	return login
}

const goodOutput = `{"t1": {"passed": true, "points": 10, "log": "4/4 tests passed"}, "t2": {"passed": false, "points": 6, "log": "3/5 tests passed"}}`

// A grading gives the runner the submission and the exam's hidden bundle, and gives the proctor the result of each TODO, labeled as
// advisory until confirmed; the bundle is in no answer the server gives.
func TestAStudentIsGradedFromTheirSubmissionAndTheBundleStaysOnTheServer(t *testing.T) {
	f := newWorkspaceFixture(t, withPoints)
	f.runner.GradeHook = func(_ context.Context, call workspacetest.GradeCall) (runner.GradeResult, error) {
		return runner.GradeResult{Stdout: goodOutput, Stderr: "compiled with warnings", DurationMS: 1234}, nil
	}
	login := f.submitted(t, "0012", "int answer = 42;\n")
	submission := submissionsOf(t, f, login.SessionID)[0]
	before := request(f.router, "GET", fmt.Sprintf("/admin/workspace/submission/%d/zip", submission.ID), "", "Bearer "+testAdminToken).Body.Bytes()

	code, body := f.post(t, "/admin/workspace/session/"+login.SessionID+"/grade", "")
	if code != http.StatusAccepted {
		t.Fatalf("grade = %d %s", code, body)
	}
	grading := f.gradedWith(t, login.SessionID)
	if grading.State != workspace.GradingDone || grading.Points == nil || *grading.Points != 16 || grading.Max == nil || *grading.Max != 25 || grading.Confirmed {
		t.Fatalf("grading = %+v", grading)
	}
	if len(grading.Results) != 2 || grading.Results[0].TodoID != "t1" || !grading.Results[0].Passed || grading.Results[1].Points != 6 || grading.Results[1].Log != "3/5 tests passed" {
		t.Errorf("results = %+v", grading.Results)
	}
	if grading.SubmissionID != submission.ID {
		t.Errorf("graded submission %d, want %d", grading.SubmissionID, submission.ID)
	}

	// The runner was given the files the exam submits, as the submission, and the hidden bundle, as the bundle; nothing else.
	if len(f.runner.Gradings) != 1 {
		t.Fatalf("%d runs", len(f.runner.Gradings))
	}
	call := f.runner.Gradings[0]
	if call.Files["src/answer.c"] != "int answer = 42;\n" || call.Files["Makefile"] == "" || call.Request.Image != "cbe/cpp20" || call.Request.TimeoutSeconds != 300 ||
		call.Request.Exam != f.examID || call.Request.Resources.MemoryMB != 256 {
		t.Errorf("the run's request or files: %+v %v", call.Request, call.Files)
	}
	if call.Bundle["tests/hidden.txt"] != "HIDDEN-GRADING-MARKER\n" || call.Bundle["grade"] == "" {
		t.Errorf("the bundle = %v", call.Bundle)
	}
	for name, content := range call.Files {
		if strings.Contains(name, "hidden") || strings.Contains(content, "HIDDEN-GRADING-MARKER") {
			t.Errorf("the grading bundle is in the submission: %s", name)
		}
	}

	// What the proctor reads says what the run printed, and says nothing of the hidden files.
	detail := request(f.router, "GET", fmt.Sprintf("/admin/workspace/grading/%d", grading.ID), "", "Bearer "+testAdminToken)
	one := decoded[workspace.AdminGrading](t, detail.Body.Bytes())
	if one.Stdout != goodOutput || one.Stderr != "compiled with warnings" || one.DurationMS != 1234 {
		t.Errorf("detail = %+v", one)
	}
	for _, path := range []string{"/admin/workspace/session/" + login.SessionID + "/gradings", fmt.Sprintf("/admin/workspace/grading/%d", grading.ID), "/admin/workspace/exam/" + f.examID + "/roster",
		"/admin/workspace/session/" + login.SessionID, "/admin/workspace/session/" + login.SessionID + "/report"} {
		if w := request(f.router, "GET", path, "", "Bearer "+testAdminToken); strings.Contains(w.Body.String(), "HIDDEN-GRADING-MARKER") || strings.Contains(w.Body.String(), "tests/hidden") {
			t.Errorf("%s shows the grading bundle", path)
		}
	}
	// The student's own view has no grade, and no grading.
	view := request(f.router, "GET", "/workspace", "", "Bearer "+login.SessionToken)
	if strings.Contains(strings.ToLower(view.Body.String()), "grad") || strings.Contains(view.Body.String(), "HIDDEN") {
		t.Errorf("the student's workspace view mentions grading: %s", view.Body.String())
	}

	// The roster has the grade, advisory.
	roster := decoded[struct {
		Students []workspace.RosterEntry `json:"students"`
	}](t, request(f.router, "GET", "/admin/workspace/exam/"+f.examID+"/roster", "", "Bearer "+testAdminToken).Body.Bytes()).Students
	if len(roster) != 1 || roster[0].Grade == nil || roster[0].Grade.State != workspace.GradingDone || roster[0].Grade.Confirmed || *roster[0].Grade.Points != 16 {
		t.Errorf("roster = %+v", roster)
	}
	// The history records that it ended.
	f.waitEvent(t, login.SessionID, "grading.finished")

	// A grading run cannot change the stored submission: the same bytes, and the student's own files are what they were.
	after := request(f.router, "GET", fmt.Sprintf("/admin/workspace/submission/%d/zip", submission.ID), "", "Bearer "+testAdminToken).Body.Bytes()
	if string(before) != string(after) {
		t.Error("the stored submission changed")
	}
	if got := readFile(t, f.work(login.SessionID)+"/src/answer.c"); got != "int answer = 42;\n" {
		t.Errorf("the student's file = %q", got)
	}
	for _, name := range []string{"grade", "tests/hidden.txt"} {
		if _, err := readFileIfThere(f.work(login.SessionID) + "/" + name); err == nil {
			t.Errorf("%s is in the student's /work", name)
		}
	}
}

func readFileIfThere(path string) (string, error) {
	data, err := os.ReadFile(path)
	return string(data), err
}

// A grade is advisory until a proctor confirms it; one confirmation of a student stands at a time; only a finished run can be confirmed.
func TestAGradeIsAdvisoryUntilAProctorConfirmsIt(t *testing.T) {
	f := newWorkspaceFixture(t, withPoints)
	var run atomic.Int32
	f.runner.GradeHook = func(context.Context, workspacetest.GradeCall) (runner.GradeResult, error) {
		if run.Add(1) == 2 {
			return runner.GradeResult{Stdout: `{"t1": {"passed": true, "points": 10}, "t2": {"passed": true, "points": 15}}`}, nil
		}
		if run.Load() == 3 {
			return runner.GradeResult{Stdout: "not json"}, nil
		}
		return runner.GradeResult{Stdout: goodOutput}, nil
	}
	login := f.submitted(t, "0012", "x\n")
	grade := func() workspace.AdminGrading {
		if code, body := f.post(t, "/admin/workspace/session/"+login.SessionID+"/grade", ""); code != http.StatusAccepted {
			t.Fatalf("grade = %d %s", code, body)
		}
		return f.gradedWith(t, login.SessionID)
	}
	first := grade()
	confirm := func(id int64, undo bool) int {
		path := fmt.Sprintf("/admin/workspace/grading/%d/", id)
		if undo {
			path += "unconfirm"
		} else {
			path += "confirm"
		}
		code, _ := f.post(t, path, "")
		return code
	}
	rosterGrade := func() *workspace.RosterGrade {
		roster := decoded[struct {
			Students []workspace.RosterEntry `json:"students"`
		}](t, request(f.router, "GET", "/admin/workspace/exam/"+f.examID+"/roster", "", "Bearer "+testAdminToken).Body.Bytes()).Students
		return roster[0].Grade
	}
	if code := confirm(first.ID, false); code != http.StatusOK {
		t.Fatalf("confirm = %d", code)
	}
	if g := rosterGrade(); !g.Confirmed || g.GradingID != first.ID || *g.Points != 16 {
		t.Errorf("roster grade after confirming = %+v", g)
	}
	f.waitEvent(t, login.SessionID, "grading.confirmed")
	// A second run is another grading; the confirmed one still stands in the roster until the proctor chooses.
	second := grade()
	if second.ID == first.ID || *second.Points != 25 || second.Confirmed {
		t.Fatalf("second = %+v", second)
	}
	if g := rosterGrade(); g.GradingID != first.ID || !g.Confirmed {
		t.Errorf("a new run took the place of the confirmed grade: %+v", g)
	}
	if confirm(second.ID, false) != http.StatusOK {
		t.Fatal("the second run was not confirmed")
	}
	list := f.gradingsOf(t, login.SessionID).Gradings
	if len(list) != 2 || !list[0].Confirmed || list[1].Confirmed {
		t.Errorf("one confirmation at a time: %+v", list)
	}
	// A run that failed (malformed output) cannot be confirmed, and an unknown one is not found.
	third := grade()
	if third.State != workspace.GradingFailed || confirm(third.ID, false) != http.StatusConflict || confirm(9999, false) != http.StatusNotFound {
		t.Errorf("third = %+v", third)
	}
	// And a confirmation is taken back.
	if confirm(second.ID, true) != http.StatusOK {
		t.Fatal("unconfirm failed")
	}
	if g := rosterGrade(); g.Confirmed {
		t.Errorf("still confirmed: %+v", g)
	}
	f.waitEvent(t, login.SessionID, "grading.unconfirmed")
	// Nothing here is open without the proctor's token.
	for _, path := range []string{"/admin/workspace/grading/1", "/admin/workspace/session/" + login.SessionID + "/gradings"} {
		if w := request(f.router, "GET", path, "", ""); w.Code != http.StatusUnauthorized {
			t.Errorf("GET %s without the token = %d", path, w.Code)
		}
	}
	for _, path := range []string{"/admin/workspace/grading/1/confirm", "/admin/workspace/session/" + login.SessionID + "/grade", "/admin/workspace/exam/" + f.examID + "/grade"} {
		if w := request(f.router, "POST", path, "", ""); w.Code != http.StatusUnauthorized {
			t.Errorf("POST %s without the token = %d", path, w.Code)
		}
	}
}

// A run that goes wrong fails with its reason and what it printed, and never holds up another student's.
func TestARunThatGoesWrongFailsWithItsReasonAndNoOtherStudentWaits(t *testing.T) {
	f := newWorkspaceFixture(t, withPoints)
	outcomes := map[string]func() (runner.GradeResult, error){
		"timeout": func() (runner.GradeResult, error) {
			return runner.GradeResult{TimedOut: true, ExitCode: 137, Stdout: "partial output"}, nil
		},
		"crash": func() (runner.GradeResult, error) {
			return runner.GradeResult{ExitCode: 139, Stderr: "Segmentation fault"}, nil
		},
		"oom": func() (runner.GradeResult, error) { return runner.GradeResult{OOMKilled: true, ExitCode: 137}, nil },
		"runner": func() (runner.GradeResult, error) {
			return runner.GradeResult{}, &runner.APIError{Status: 503, Message: "busy"}
		},
		"malformed": func() (runner.GradeResult, error) {
			return runner.GradeResult{Stdout: "make: entering directory\n" + goodOutput}, nil
		},
	}
	var which atomic.Value
	which.Store("")
	f.runner.GradeHook = func(_ context.Context, call workspacetest.GradeCall) (runner.GradeResult, error) {
		if outcome := outcomes[which.Load().(string)]; outcome != nil {
			return outcome()
		}
		return runner.GradeResult{Stdout: goodOutput}, nil
	}
	login := f.submitted(t, "0012", "x\n")
	for reason := range outcomes {
		which.Store(reason)
		if code, body := f.post(t, "/admin/workspace/session/"+login.SessionID+"/grade", ""); code != http.StatusAccepted {
			t.Fatalf("%s: grade = %d %s", reason, code, body)
		}
		got := f.gradedWith(t, login.SessionID)
		if got.State != workspace.GradingFailed || got.Failure != reason || got.FailureText == "" || got.Points != nil || got.Confirmed {
			t.Errorf("%s: %+v", reason, got)
		}
		full := decoded[workspace.AdminGrading](t, request(f.router, "GET", fmt.Sprintf("/admin/workspace/grading/%d", got.ID), "", "Bearer "+testAdminToken).Body.Bytes())
		switch reason {
		case "timeout":
			if !full.TimedOut || full.Stdout != "partial output" {
				t.Errorf("a timeout keeps what was printed: %+v", full)
			}
		case "crash":
			if full.Stderr != "Segmentation fault" || full.ExitCode == nil || *full.ExitCode != 139 {
				t.Errorf("a crash keeps what was printed: %+v", full)
			}
		case "malformed":
			if !strings.Contains(full.FailureText, "not one JSON object") || !strings.Contains(full.Stdout, "entering directory") {
				t.Errorf("malformed output is explained and kept: %+v", full)
			}
		}
		f.waitEvent(t, login.SessionID, "grading.finished")
	}
	// A student whose run failed does not stop the next student's: the second student is graded after the first one's failure.
	which.Store("crash")
	other := f.submitted(t, "0034", "y\n")
	if code, _ := f.post(t, "/admin/workspace/session/"+login.SessionID+"/grade", ""); code != http.StatusAccepted {
		t.Fatal("grade 1")
	}
	which.Store("")
	if code, _ := f.post(t, "/admin/workspace/session/"+other.SessionID+"/grade", ""); code != http.StatusAccepted {
		t.Fatal("grade 2")
	}
	if got := f.gradedWith(t, other.SessionID); got.State != workspace.GradingDone {
		t.Errorf("the other student = %+v", got)
	}
}

// The whole class is graded with one request; students who have nothing to grade, or are being graded, are named and left out.
func TestTheClassIsGradedWithOneRequestAndWhoWasLeftOutIsSaid(t *testing.T) {
	f := newWorkspaceFixture(t, withPoints)
	f.runner.GradeHook = func(context.Context, workspacetest.GradeCall) (runner.GradeResult, error) {
		return runner.GradeResult{Stdout: goodOutput}, nil
	}
	ada := f.submitted(t, "0012", "x\n")
	grace := f.loginReady(t, "0034") // logged in, never pressed Save
	code, body := f.post(t, "/admin/workspace/exam/"+f.examID+"/grade", "")
	if code != http.StatusAccepted {
		t.Fatalf("grade the class = %d %s", code, body)
	}
	started := decoded[workspace.Started](t, body)
	if len(started.Queued) != 1 || len(started.Skipped) != 1 || started.Skipped[0].SessionID != grace.SessionID || started.Skipped[0].Account != "0034" || !strings.Contains(started.Skipped[0].Reason, "not submitted") {
		t.Fatalf("started = %+v", started)
	}
	if got := f.gradedWith(t, ada.SessionID); got.State != workspace.GradingDone {
		t.Errorf("ada = %+v", got)
	}
	// Named students only.
	code, body = f.post(t, "/admin/workspace/exam/"+f.examID+"/grade", `{"sessions": ["`+grace.SessionID+`", "no-such-session"]}`)
	started = decoded[workspace.Started](t, body)
	if code != http.StatusAccepted || len(started.Queued) != 0 || len(started.Skipped) != 2 {
		t.Errorf("named = %d %+v", code, started)
	}
	// One student by their own route: no submission is a conflict, a wrong body is refused, and a submission of another student is not found.
	if code, _ := f.post(t, "/admin/workspace/session/"+grace.SessionID+"/grade", ""); code != http.StatusConflict {
		t.Errorf("a student with nothing to grade = %d", code)
	}
	if code, _ := f.post(t, "/admin/workspace/session/"+ada.SessionID+"/grade", `{"unknown": 1}`); code != http.StatusBadRequest {
		t.Errorf("an unknown field = %d", code)
	}
	if code, _ := f.post(t, "/admin/workspace/session/"+ada.SessionID+"/grade", fmt.Sprintf(`{"submission": %d}`, 99999)); code != http.StatusConflict {
		t.Errorf("a submission that does not exist = %d", code)
	}
	if code, _ := f.post(t, "/admin/workspace/exam/no-such-exam/grade", ""); code != http.StatusNotFound {
		t.Errorf("an exam that does not exist = %d", code)
	}
}

// An exam with no grading folder cannot be graded, and the answer says why.
func TestAnExamWithoutAGradingFolderCannotBeGraded(t *testing.T) {
	f := newWorkspaceFixture(t, func(d *workspace.Document) { d.GradingBundle = "" })
	login := f.submitted(t, "0012", "x\n")
	if code, body := f.post(t, "/admin/workspace/session/"+login.SessionID+"/grade", ""); code != http.StatusConflict || !strings.Contains(string(body), "no grading folder") {
		t.Errorf("grade = %d %s", code, body)
	}
	if code, _ := f.post(t, "/admin/workspace/exam/"+f.examID+"/grade", ""); code != http.StatusConflict {
		t.Errorf("grade the class = %d", code)
	}
}

// A student already waiting or being graded is not queued twice.
func TestAStudentBeingGradedIsNotQueuedAgain(t *testing.T) {
	f := newWorkspaceFixture(t, withPoints)
	release := make(chan struct{})
	f.runner.GradeHook = func(ctx context.Context, call workspacetest.GradeCall) (runner.GradeResult, error) {
		select {
		case <-release:
		case <-ctx.Done():
		}
		return runner.GradeResult{Stdout: goodOutput}, nil
	}
	login := f.submitted(t, "0012", "x\n")
	if code, _ := f.post(t, "/admin/workspace/session/"+login.SessionID+"/grade", ""); code != http.StatusAccepted {
		t.Fatal("first")
	}
	eventually(t, "the run to be going", func() bool {
		list := f.gradingsOf(t, login.SessionID).Gradings
		return len(list) == 1 && list[0].State == workspace.GradingRunning
	})
	if code, _ := f.post(t, "/admin/workspace/session/"+login.SessionID+"/grade", ""); code != http.StatusConflict {
		t.Errorf("a second request while it is going = %d", code)
	}
	body := func() workspace.Started {
		_, b := f.post(t, "/admin/workspace/exam/"+f.examID+"/grade", "")
		return decoded[workspace.Started](t, b)
	}()
	if len(body.Queued) != 0 || len(body.Skipped) != 1 || !strings.Contains(body.Skipped[0].Reason, "already") {
		t.Errorf("the class while one is going: %+v", body)
	}
	close(release)
	f.gradedWith(t, login.SessionID)
}

// Runs a stop interrupted are marked failed when the server starts again; they are not left waiting for ever.
func TestRunsAStopInterruptedAreMarkedFailed(t *testing.T) {
	f := newWorkspaceFixture(t, withPoints)
	login := f.submitted(t, "0012", "x\n")
	sub := submissionsOf(t, f, login.SessionID)[0]
	id, err := f.repo.CreateGrading(context.Background(), login.SessionID, sub.ID, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := f.repo.StartGrading(context.Background(), id, time.Now()); err != nil {
		t.Fatal(err)
	}
	f.grader.Recover(context.Background())
	got := f.gradingsOf(t, login.SessionID).Gradings[0]
	if got.State != workspace.GradingFailed || got.Failure != workspace.FailInterrupted || !strings.Contains(got.FailureText, "run it again") {
		t.Errorf("got = %+v", got)
	}
}

// The proctor's reads of a grading that is not there, and a grading of a session that is not a workspace's.
func TestGradingReadsOfWhatIsNotThere(t *testing.T) {
	f := newWorkspaceFixture(t, withPoints)
	for path, want := range map[string]int{"/admin/workspace/grading/77": http.StatusNotFound, "/admin/workspace/grading/abc": http.StatusBadRequest,
		"/admin/workspace/session/no-such/gradings": http.StatusNotFound} {
		if w := request(f.router, "GET", path, "", "Bearer "+testAdminToken); w.Code != want {
			t.Errorf("GET %s = %d, want %d", path, w.Code, want)
		}
	}
	// A session with no gradings has an empty list, and the exam's TODOs to read the list against.
	login := f.loginReady(t, "0012")
	view := f.gradingsOf(t, login.SessionID)
	if len(view.Gradings) != 0 || len(view.Todos) != 2 || view.Todos[0].ID != "t1" || view.Todos[0].Text != "Do `one`" || *view.Todos[0].Points != 10 {
		t.Errorf("view = %+v", view)
	}
	raw := request(f.router, "GET", "/admin/workspace/session/"+login.SessionID+"/gradings", "", "Bearer "+testAdminToken).Body.String()
	var generic map[string]any
	if err := json.Unmarshal([]byte(raw), &generic); err != nil || generic["gradings"] == nil {
		t.Errorf("an empty list must be [], not null: %s", raw)
	}
}
