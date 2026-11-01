package service_test

import (
	"errors"
	"testing"
	"time"

	"exam-platform/internal/domain"
	"exam-platform/internal/service"
)

func TestEndTimeCapsRestoresAndFinalizesWithoutGrace(t *testing.T) {
	f := newFixture(t, 30)
	if _, err := f.raw.Exec(`UPDATE exams SET duration_seconds=3600`); err != nil {
		t.Fatal(err)
	}
	login := f.login(t, "0012")
	end := epoch.Add(50 * time.Minute)
	if err := f.sessions.SetEndTime(t.Context(), testExamID, &end); err != nil {
		t.Fatal(err)
	}
	p, err := f.sessions.Authenticate(t.Context(), login.Token)
	if err != nil || p.Lifecycle.SecondsRemaining != 3000 {
		t.Fatalf("%+v %v", p, err)
	}
	later := epoch.Add(55 * time.Minute)
	if err := f.sessions.SetEndTime(t.Context(), testExamID, &later); err != nil {
		t.Fatal(err)
	}
	if err := f.sessions.SetEndTime(t.Context(), testExamID, nil); err != nil {
		t.Fatal(err)
	}
	p, _ = f.sessions.Authenticate(t.Context(), login.Token)
	if p.Lifecycle.SecondsRemaining != 3600 {
		t.Fatal("clearing did not restore duration")
	}
	if err := f.sessions.SetEndTime(t.Context(), testExamID, &end); err != nil {
		t.Fatal(err)
	}
	f.clock.Set(end)
	p, err = f.sessions.Authenticate(t.Context(), login.Token)
	if err != nil || p.Session.Status != domain.Submitted {
		t.Fatalf("%+v %v", p, err)
	}
	if f.count(t, `SELECT count(*) FROM event_log WHERE event_type='grace_started'`) != 0 {
		t.Fatal("spurious grace transition")
	}
	submission, err := f.sessions.Submission(t.Context(), login.SessionID)
	if err != nil || submission.Reason != "end_time_reached" {
		t.Fatalf("%+v %v", submission, err)
	}
	if _, err := f.sessions.Login(t.Context(), "0034", "exam-secret"); !errors.Is(err, service.ErrExamEnded) {
		t.Fatalf("late login: %v", err)
	}
}

func TestGraceExtensionIsIndividualAndInheritedAndSkipsFinals(t *testing.T) {
	f := newFixture(t, 30)
	first := f.login(t, "0012")
	original := f.storedSession(t, first.Token)
	result, err := f.sessions.ExtendGrace(t.Context(), first.SessionID, 5, "interruption")
	if err != nil || result.Changed != 1 {
		t.Fatalf("%+v %v", result, err)
	}
	got := f.storedSession(t, first.Token)
	if !got.GraceUntil.Equal(original.GraceUntil.Add(5*time.Minute)) || !got.DeadlineAt.Equal(original.DeadlineAt) {
		t.Fatal("wrong times")
	}
	if _, err := f.sessions.ForceSubmit(t.Context(), first.SessionID, "done"); err != nil {
		t.Fatal(err)
	}
	result, err = f.sessions.ExtendExamGrace(t.Context(), testExamID, 2, "class interruption")
	if err != nil || result.Changed != 0 || result.Skipped != 1 {
		t.Fatalf("%+v %v", result, err)
	}
	second := f.login(t, "0034")
	if got := f.storedSession(t, second.Token); !got.GraceUntil.Equal(epoch.Add(210 * time.Second)) {
		t.Fatalf("not inherited: %+v", got)
	}
	f.clock.Set(epoch.Add(65 * time.Second))
	result, err = f.sessions.ExtendExamGrace(t.Context(), testExamID, 1, "more time")
	if err != nil || result.Changed != 1 || result.Skipped != 1 {
		t.Fatalf("%+v %v", result, err)
	}
	if f.count(t, `SELECT count(*) FROM event_log WHERE event_type='grace_extended'`) != 2 {
		t.Fatal("missing events")
	}
}

func TestTimingValidationAndNoResurrection(t *testing.T) {
	f := newFixture(t, 30)
	login := f.login(t, "0012")
	for _, minutes := range []int{0, -1, 61} {
		if _, err := f.sessions.ExtendGrace(t.Context(), login.SessionID, minutes, "reason"); !errors.Is(err, service.ErrInvalidInput) {
			t.Fatal(err)
		}
	}
	for _, offset := range []time.Duration{30 * time.Second, 25 * time.Hour} {
		end := epoch.Add(offset)
		if err := f.sessions.SetEndTime(t.Context(), testExamID, &end); !errors.Is(err, service.ErrInvalidInput) {
			t.Fatal(err)
		}
	}
	f.clock.Set(epoch.Add(91 * time.Second))
	if _, err := f.sessions.ExtendGrace(t.Context(), login.SessionID, 1, "too late"); !errors.Is(err, service.ErrAlreadySubmitted) {
		t.Fatal(err)
	}
	if f.storedSession(t, login.Token).Status != domain.Submitted {
		t.Fatal("expiry was rolled back")
	}
}
