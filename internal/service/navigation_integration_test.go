package service_test

import (
	"errors"
	"testing"

	"exam-platform/internal/domain"
	"exam-platform/internal/service"
)

// forwardFixture is a forward-only exam of three exercises: q1 and q2, then q3 (with starter code), then q4.
func forwardFixture(t *testing.T) *fixture {
	t.Helper()
	f := newFixture(t, 0)
	content := examContent([]string{"q1", "q2"}, []string{"q3"}, []string{"q4"})
	content.Exercises[1].Slots[0] = domain.Slot{ID: "q3", Kind: domain.CodeSlot, Language: "mips", Starter: "main:\n"}
	setContent(t, f, domain.ForwardOnly, content)
	return f
}

func latestAnswers(t *testing.T, f *fixture, token string) domain.Answers {
	t.Helper()
	view, err := f.sessions.GetExam(t.Context(), token)
	if err != nil {
		t.Fatal(err)
	}
	return view.Answers
}

func TestForwardOnlyStudentsCannotChangeClosedOrLaterExercises(t *testing.T) {
	f := forwardFixture(t)
	login := f.login(t, "0012")
	view, err := f.sessions.GetExam(t.Context(), login.Token)
	if err != nil || view.Navigation != domain.ForwardOnly || view.CurrentExercise != 0 || len(view.Content.Exercises) != 3 || view.Latest != nil {
		t.Fatalf("view = %+v, %v", view, err)
	}
	if view.Answers["q3"] != "main:\n" || view.Answers["q1"] != "" || len(view.Answers) != 4 {
		t.Fatalf("starting answers = %q", view.Answers)
	}

	// Answers to a later exercise are not taken early; missing answers keep their saved value.
	if _, err := f.sessions.Autosave(t.Context(), login.Token, domain.Answers{"q1": "draft", "q3": "early", "q4": "early"}); err != nil {
		t.Fatal(err)
	}
	if answers := latestAnswers(t, f, login.Token); answers["q1"] != "draft" || answers["q2"] != "" || answers["q3"] != "main:\n" || answers["q4"] != "" {
		t.Fatalf("after autosave = %q", answers)
	}

	moved, err := f.sessions.Advance(t.Context(), login.Token, 0, domain.Answers{"q1": "final 世界", "q2": "b", "q3": "early"})
	if err != nil || moved.CurrentExercise != 1 || moved.Sequence != 2 {
		t.Fatalf("advance = %+v, %v", moved, err)
	}
	if f.storedSession(t, login.Token).CurrentExercise != 1 {
		t.Fatal("the session did not move on")
	}
	if answers := latestAnswers(t, f, login.Token); answers["q1"] != "final 世界" || answers["q2"] != "b" || answers["q3"] != "main:\n" {
		t.Fatalf("after advance = %q", answers)
	}

	// The closed exercise keeps its answers whatever the student sends.
	if _, err := f.sessions.Autosave(t.Context(), login.Token, domain.Answers{"q1": "changed", "q2": "", "q3": "main:\n  jr $ra\n"}); err != nil {
		t.Fatal(err)
	}
	if answers := latestAnswers(t, f, login.Token); answers["q1"] != "final 世界" || answers["q2"] != "b" || answers["q3"] != "main:\n  jr $ra\n" {
		t.Fatalf("closed exercise changed: %q", answers)
	}

	// Repeating a request that already moved on saves nothing; going back is refused.
	before := f.count(t, `SELECT count(*) FROM submission_versions`)
	again, err := f.sessions.Advance(t.Context(), login.Token, 0, domain.Answers{"q1": "replay"})
	if err != nil || again.CurrentExercise != 1 || again.Sequence != 3 || f.count(t, `SELECT count(*) FROM submission_versions`) != before {
		t.Fatalf("replayed advance = %+v, %v", again, err)
	}
	if _, err := f.sessions.Advance(t.Context(), login.Token, 1, domain.Answers{}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.sessions.Advance(t.Context(), login.Token, 0, domain.Answers{}); !errors.Is(err, service.ErrExerciseChanged) {
		t.Fatalf("advance from a closed exercise = %v", err)
	}
	if _, err := f.sessions.Advance(t.Context(), login.Token, 2, domain.Answers{}); !errors.Is(err, service.ErrInvalidInput) {
		t.Fatalf("advance past the last exercise = %v", err)
	}
	if _, err := f.sessions.Advance(t.Context(), login.Token, 2, domain.Answers{"unknown": "x"}); !errors.Is(err, service.ErrInvalidInput) {
		t.Fatalf("advance with an unknown slot = %v", err)
	}

	// Submitting keeps the closed exercises too, and locks the session.
	if _, err := f.sessions.Submit(t.Context(), login.Token, domain.Answers{"q1": "x", "q3": "x", "q4": "last"}); err != nil {
		t.Fatal(err)
	}
	final := finalVersion(t, f, login.Token).Answers
	if final["q1"] != "final 世界" || final["q3"] != "main:\n  jr $ra\n" || final["q4"] != "last" {
		t.Fatalf("final = %q", final)
	}
	if _, err := f.sessions.Advance(t.Context(), login.Token, 2, domain.Answers{}); !errors.Is(err, service.ErrSessionLocked) {
		t.Fatalf("advance after submitting = %v", err)
	}
}

func TestAdvanceBeforeAnySaveKeepsTheStarters(t *testing.T) {
	f := forwardFixture(t)
	login := f.login(t, "0012")
	if _, err := f.sessions.Advance(t.Context(), login.Token, 0, domain.Answers{"q1": "a"}); err != nil {
		t.Fatal(err)
	}
	if answers := latestAnswers(t, f, login.Token); answers["q1"] != "a" || answers["q2"] != "" || answers["q3"] != "main:\n" {
		t.Fatalf("answers = %q", answers)
	}
}

func TestFreeNavigationHasNoAdvanceAndSavesWhatIsSent(t *testing.T) {
	f := newFixture(t, 0)
	setContent(t, f, domain.FreeNavigation, examContent([]string{"q1"}, []string{"q2"}))
	login := f.login(t, "0012")
	if _, err := f.sessions.Advance(t.Context(), login.Token, 0, domain.Answers{}); !errors.Is(err, service.ErrInvalidInput) {
		t.Fatalf("advance under free navigation = %v", err)
	}
	if _, err := f.sessions.Autosave(t.Context(), login.Token, domain.Answers{"q2": "later first"}); err != nil {
		t.Fatal(err)
	}
	if answers := latestAnswers(t, f, login.Token); answers["q2"] != "later first" || f.storedSession(t, login.Token).CurrentExercise != 0 {
		t.Fatalf("answers = %q", answers)
	}
}
