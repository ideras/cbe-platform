package service_test

import (
	"testing"
	"time"

	"exam-platform/internal/domain"
	"exam-platform/internal/service"
)

func TestCurrentExamSelectionAndOldTokenAuthority(t *testing.T) {
	f := newFixture(t, 0)
	old := f.login(t, "0012")
	const otherExam = "00000000-0000-4000-8000-000000000002"
	if err := f.store.CreateExam(t.Context(), domain.Exam{ID: otherExam, ClassID: testClassID, Title: "Another", PIN: "exam-secret", DurationSeconds: 120, GraceSeconds: 7, CreatedAt: epoch}); err != nil {
		t.Fatal(err)
	}
	f.publish(t, otherExam)
	other, err := f.sessions.Login(t.Context(), "0012", "exam-secret")
	if err != nil {
		t.Fatal(err)
	}
	if other.SessionID == old.SessionID || other.Token == old.Token || !other.DeadlineAt.Equal(epoch.Add(2*time.Minute)) {
		t.Fatal("shared PIN silently selected wrong exam")
	}
	if got := f.storedSession(t, other.Token).ExamID; got != otherExam {
		t.Fatalf("selected exam = %q", got)
	}
	for _, closed := range []bool{false, true} {
		if closed {
			if _, err := service.NewPublisher(f.store, f.clock).Unpublish(t.Context(), otherExam, true); err != nil {
				t.Fatal(err)
			}
		}
		principal, err := f.sessions.Authenticate(t.Context(), old.Token)
		if err != nil || principal.Session.ID != old.SessionID {
			t.Fatal("current-exam selection revoked another session's bearer authority")
		}
	}
	// Admission moving to another class's exam does not revoke the token either.
	const thirdExam, otherClass = "00000000-0000-4000-8000-000000000003", "00000000-0000-4000-8000-0000000000c2"
	f.addClass(t, otherClass, "Another class", domain.Student{AccountNumber: "0056"})
	if err := f.store.CreateExam(t.Context(), domain.Exam{ID: thirdExam, ClassID: otherClass, Title: "Third", PIN: "exam-secret", DurationSeconds: 120, GraceSeconds: 7, CreatedAt: epoch}); err != nil {
		t.Fatal(err)
	}
	f.publish(t, thirdExam)
	if _, err := f.sessions.Authenticate(t.Context(), old.Token); err != nil {
		t.Fatal("publishing the exam of another class revoked an existing token")
	}
}
