package client

import (
	"maps"
	"net/http"
	"testing"
	"time"
)

func forwardOnly(t *testing.T) (*fixture, *Service) {
	t.Helper()
	f := newFixture(t)
	f.navigation = "forward_only"
	s := newClient(t, f, nil)
	if view := loginClient(t, s, f); view.Exam.Navigation != "forward_only" || view.Exercise != 0 || len(view.Exam.Content.Exercises) != 2 {
		t.Fatalf("view = %+v", view)
	}
	return f, s
}

func TestAdvanceSavesTheSnapshotAndLocksEditingUntilConfirmed(t *testing.T) {
	f, s := forwardOnly(t)
	entered, release := make(chan struct{}), make(chan struct{})
	f.onWrite = func(_ http.ResponseWriter, r *http.Request, _ writeRecord, _ int) bool {
		if r.URL.Path == "/advance" {
			close(entered)
			<-release
		}
		return false
	}
	answers := map[string]string{"q1": "leaving now", "q2": ""}
	if view, err := s.Advance(answers, 1, 0); err != nil || !view.Advancing {
		t.Fatalf("advance = %+v, %v", view, err)
	}
	<-entered
	if _, err := s.UpdateAnswers(map[string]string{"q1": "edited while leaving", "q2": ""}, 2); err == nil {
		t.Fatal("editing was allowed while moving on")
	}
	if _, err := s.Submit(answers, 1); err == nil {
		t.Fatal("submitting was allowed while moving on")
	}
	if view, err := s.Advance(answers, 1, 0); err != nil || !view.Advancing {
		t.Fatalf("a repeated click = %+v, %v", view, err)
	}
	close(release)
	view := waitView(t, s, func(v View) bool { return !v.Advancing })
	if view.Exercise != 1 || view.AdvanceError != "" || view.AcknowledgedRevision != 1 || view.Saved.Sequence != 1 {
		t.Fatalf("after advancing = %+v", view)
	}
	f.mu.Lock()
	if len(f.writes) != 1 || f.writes[0].path != "/advance" || !maps.Equal(f.writes[0].answers, answers) {
		t.Fatalf("writes = %+v", f.writes)
	}
	f.mu.Unlock()
	if _, err := s.Advance(answers, 1, 1); err == nil {
		t.Fatal("moved past the last exercise")
	}
	if _, err := s.Advance(answers, 1, 0); err == nil {
		t.Fatal("left a closed exercise again")
	}
}

func TestAdvanceRetriesTheSameSnapshotAfterAnUnclearFailure(t *testing.T) {
	f, s := forwardOnly(t)
	f.onWrite = func(w http.ResponseWriter, _ *http.Request, _ writeRecord, n int) bool {
		if n == 1 {
			http.Error(w, "temporarily unavailable", 503)
			return true
		}
		return false
	}
	if _, err := s.Advance(map[string]string{"q1": "once", "q2": ""}, 1, 0); err != nil {
		t.Fatal(err)
	}
	waitView(t, s, func(v View) bool { return v.Advancing && v.AdvanceError != "" })
	view := waitView(t, s, func(v View) bool { return !v.Advancing })
	if view.Exercise != 1 || view.AdvanceError != "" {
		t.Fatalf("view = %+v", view)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.writes) != 2 || !maps.Equal(f.writes[0].answers, f.writes[1].answers) || f.writes[1].path != "/advance" {
		t.Fatalf("retry changed the snapshot: %+v", f.writes)
	}
}

func TestARefusedAdvanceResumesEditingAndTheServerPositionWins(t *testing.T) {
	f, s := forwardOnly(t)
	// Another window already moved this student on: the server refuses, and the next status read shows where they are.
	f.mu.Lock()
	f.status.CurrentExercise = 1
	f.mu.Unlock()
	if _, err := s.Advance(map[string]string{"q1": "late", "q2": ""}, 1, 0); err != nil {
		t.Fatal(err)
	}
	view := waitView(t, s, func(v View) bool { return !v.Advancing && v.Exercise == 1 })
	if view.AdvanceError == "" {
		t.Fatal("the refusal was not reported")
	}
	if _, err := s.UpdateAnswers(map[string]string{"q1": "", "q2": "editing again"}, 2); err != nil {
		t.Fatal(err)
	}
}

func TestStatusNeverMovesAStudentBack(t *testing.T) {
	f, s := forwardOnly(t)
	f.mu.Lock()
	f.status.CurrentExercise = 1
	f.mu.Unlock()
	waitView(t, s, func(v View) bool { return v.Exercise == 1 })
	f.mu.Lock()
	f.status.CurrentExercise = 0
	f.mu.Unlock()
	time.Sleep(60 * time.Millisecond)
	if view := s.Snapshot(); view.Exercise != 1 {
		t.Fatalf("exercise went back to %d", view.Exercise)
	}
}

func TestFreeNavigationCannotAdvance(t *testing.T) {
	f := newFixture(t)
	s := newClient(t, f, nil)
	loginClient(t, s, f)
	if _, err := s.Advance(map[string]string{"q1": "", "q2": ""}, 0, 0); err == nil {
		t.Fatal("advanced under free navigation")
	}
	f.mu.Lock()
	f.status.CurrentExercise = 1
	f.mu.Unlock()
	time.Sleep(60 * time.Millisecond)
	if view := s.Snapshot(); view.Exercise != 0 {
		t.Fatalf("free navigation followed the server's exercise: %d", view.Exercise)
	}
}
