package dashboard

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func ptr(v float64) *float64 { return &v }

func gradedRoster() WorkspaceRoster {
	r := workspaceRoster()
	r.Students[0].Grade = &WorkspaceGrade{GradingID: 4, State: "done", Points: ptr(16), Max: ptr(40), Confirmed: false, At: at}
	return r
}

// The roster carries each student's grade, advisory until it is confirmed; a grade that makes no sense is an error.
func TestTheRosterCarriesEachStudentsGrade(t *testing.T) {
	served := gradedRoster()
	s, _ := start(t, sessionsHandler(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/roster") {
			reply(w, served)
			return
		}
		http.NotFound(w, r)
	}))
	wait(t, func() bool { return s.Snapshot().Workspace != nil })
	grade := s.Snapshot().Workspace.Students[0].Grade
	if grade == nil || grade.State != "done" || *grade.Points != 16 || *grade.Max != 40 || grade.Confirmed {
		t.Fatalf("grade = %+v", grade)
	}
	served.Students[0].Grade.Confirmed = true
	wait(t, func() bool { g := s.Snapshot().Workspace.Students[0].Grade; return g != nil && g.Confirmed })
	for name, bad := range map[string]*WorkspaceGrade{
		"an unknown state":                 {GradingID: 1, State: "great"},
		"negative points":                  {GradingID: 1, State: "done", Points: ptr(-1)},
		"a confirmed run that is not done": {GradingID: 1, State: "failed", Confirmed: true},
		"no run number":                    {State: "done"},
	} {
		if validGrade(bad) {
			t.Errorf("%s is taken for a grade", name)
		}
	}
	if !validGrade(nil) {
		t.Error("no grade is a valid answer")
	}
}

func TestAStudentsGradingsAreReadForTheReviewWindow(t *testing.T) {
	good := Grading{ID: 4, SubmissionID: 7, State: "done", QueuedAt: at, Points: ptr(16), Max: ptr(40), Results: []GradingResult{{TodoID: "t1", Known: true, Passed: true, Points: 10, Log: "4/4"}, {TodoID: "t2", Known: true, Points: 6, Log: "2/5"}},
		Stdout: `{"t1":{}}`, Stderr: "ok: x"}
	s, _ := start(t, sessionsHandler(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/admin/workspace/session/" + sessionID + "/gradings":
			list := good
			list.Stdout, list.Stderr = "", ""
			reply(w, WorkspaceGradings{SessionID: sessionID, Todos: []GradingTodo{{ID: "t1", Text: "Do `one`", Points: ptr(10)}, {ID: "t2", Text: "Do two"}}, Gradings: []Grading{list}})
		case "/admin/workspace/grading/4":
			reply(w, good)
		case "/admin/workspace/grading/5":
			bad := good
			bad.ID, bad.Confirmed, bad.State = 5, true, "failed"
			reply(w, bad)
		case "/admin/workspace/grading/6":
			wrong := good
			wrong.ID = 99
			reply(w, wrong)
		default:
			http.NotFound(w, r)
		}
	}))
	list, err := s.WorkspaceGradings(sessionID)
	if err != nil || len(list.Gradings) != 1 || len(list.Todos) != 2 || list.Gradings[0].Results[1].Points != 6 || list.Gradings[0].Stdout != "" {
		t.Fatalf("list = %+v, %v", list, err)
	}
	one, err := s.WorkspaceGrading(4)
	if err != nil || one.Stdout != `{"t1":{}}` || one.Stderr != "ok: x" || *one.Points != 16 {
		t.Fatalf("one = %+v, %v", one, err)
	}
	// What makes no sense is refused here: a confirmed run that failed, a run that is not the one asked for, a selection that is nothing.
	for name, call := range map[string]func() error{
		"a confirmed failure": func() error { _, err := s.WorkspaceGrading(5); return err },
		"another run":         func() error { _, err := s.WorkspaceGrading(6); return err },
		"no run":              func() error { _, err := s.WorkspaceGrading(0); return err },
		"not found":           func() error { _, err := s.WorkspaceGrading(77); return err },
		"a bad session":       func() error { _, err := s.WorkspaceGradings("../x"); return err },
	} {
		if err := call(); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}

func TestTheProctorStartsGradingAndConfirmsAGrade(t *testing.T) {
	var gradedClass, gradedStudent, confirmed, unconfirmed atomic.Int32
	var classBody, studentBody atomic.Value
	s, _ := start(t, sessionsHandler(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		switch {
		case r.URL.Path == "/admin/workspace/exam/"+examID+"/roster":
			reply(w, workspaceRoster())
		case r.Method == http.MethodPost && r.URL.Path == "/admin/workspace/exam/"+examID+"/grade":
			gradedClass.Add(1)
			classBody.Store(string(data))
			w.WriteHeader(http.StatusAccepted)
			reply(w, GradeStarted{Queued: []int64{1, 2}, Skipped: []GradeSkip{{SessionID: sessionID, Account: "0012", Reason: "has not submitted anything"}}})
		case r.Method == http.MethodPost && r.URL.Path == "/admin/workspace/session/"+sessionID+"/grade":
			gradedStudent.Add(1)
			studentBody.Store(string(data))
			if strings.Contains(string(data), `"submission":99`) {
				w.WriteHeader(http.StatusConflict)
				reply(w, map[string]string{"error": "this student has not submitted anything"})
				return
			}
			w.WriteHeader(http.StatusAccepted)
			reply(w, map[string]int64{"grading": 9})
		case r.Method == http.MethodPost && r.URL.Path == "/admin/workspace/grading/4/confirm":
			confirmed.Add(1)
			reply(w, map[string]bool{"confirmed": true})
		case r.Method == http.MethodPost && r.URL.Path == "/admin/workspace/grading/4/unconfirm":
			unconfirmed.Add(1)
			reply(w, map[string]bool{"confirmed": false})
		case r.Method == http.MethodPost && r.URL.Path == "/admin/workspace/grading/5/confirm":
			reply(w, map[string]bool{"confirmed": false}) // an answer that is not what was asked
		default:
			http.NotFound(w, r)
		}
	}))
	wait(t, func() bool { return s.Snapshot().Workspace != nil })
	started, err := s.GradeExam(nil)
	if err != nil || len(started.Queued) != 2 || len(started.Skipped) != 1 || started.Skipped[0].Reason != "has not submitted anything" {
		t.Fatalf("started = %+v, %v", started, err)
	}
	var sent struct {
		Sessions []string `json:"sessions"`
	}
	if err := json.Unmarshal([]byte(classBody.Load().(string)), &sent); err != nil || len(sent.Sessions) != 0 {
		t.Errorf("the whole class is asked for with no names: %v %v", classBody.Load(), err)
	}
	if _, err := s.GradeExam([]string{sessionID}); err != nil || !strings.Contains(classBody.Load().(string), sessionID) {
		t.Errorf("named students: %v %v", err, classBody.Load())
	}
	id, err := s.GradeStudent(sessionID, 0)
	if err != nil || id != 9 || !strings.Contains(studentBody.Load().(string), `"submission":0`) {
		t.Errorf("one student: %d %v %v", id, err, studentBody.Load())
	}
	// The server's reason comes back in words the proctor reads; an invalid selection is refused here.
	if _, err := s.GradeStudent(sessionID, 99); err == nil || !strings.Contains(err.Error(), "this student has not submitted anything") {
		t.Errorf("a refusal: %v", err)
	}
	if _, err := s.GradeStudent("not-an-id", 0); err == nil || gradedStudent.Load() != 2 {
		t.Errorf("a bad session was sent: %v, %d", err, gradedStudent.Load())
	}
	if _, err := s.GradeExam([]string{"nope"}); err == nil || gradedClass.Load() != 2 {
		t.Errorf("a bad session in the class request was sent: %v %d", err, gradedClass.Load())
	}
	if err := s.ConfirmGrading(4, true); err != nil || confirmed.Load() != 1 {
		t.Errorf("confirm: %v", err)
	}
	if err := s.ConfirmGrading(4, false); err != nil || unconfirmed.Load() != 1 {
		t.Errorf("unconfirm: %v", err)
	}
	if err := s.ConfirmGrading(5, true); err == nil {
		t.Error("an answer that says it was not confirmed was taken for a confirmation")
	}
	if err := s.ConfirmGrading(0, true); err == nil {
		t.Error("run 0 was confirmed")
	}
}

// A question exam has nothing to grade, and the call says so without asking the server.
func TestAQuestionExamCannotBeGraded(t *testing.T) {
	var asked atomic.Int32
	s, _ := start(t, sessionsHandler(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/grade") {
			asked.Add(1)
		}
		http.NotFound(w, r)
	}))
	wait(t, func() bool { return s.Snapshot().Loaded })
	time.Sleep(50 * time.Millisecond)
	if _, err := s.GradeExam(nil); err == nil || asked.Load() != 0 {
		t.Errorf("a question exam was graded: %v %d", err, asked.Load())
	}
}
