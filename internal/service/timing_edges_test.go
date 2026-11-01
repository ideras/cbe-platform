package service_test

import (
	"testing"
	"time"

	"exam-platform/internal/domain"
	"exam-platform/internal/service"
)

func TestEndCapSurvivesGraceAndLockedGiveBack(t *testing.T) {
	f := newFixture(t, 30)
	if _, err := f.raw.Exec(`UPDATE exams SET duration_seconds=600`); err != nil {
		t.Fatal(err)
	}
	end := epoch.Add(5 * time.Minute)
	if err := f.sessions.SetEndTime(t.Context(), testExamID, &end); err != nil {
		t.Fatal(err)
	}
	login := f.login(t, "0012")
	if !login.DeadlineAt.Equal(end) {
		t.Fatal("login not capped")
	}
	result, err := f.sessions.ExtendGrace(t.Context(), login.SessionID, 5, "extra grace")
	if err != nil || !result.Capped || !result.GraceUntil.Equal(end) {
		t.Fatalf("%+v %v", result, err)
	}
	if _, err := f.raw.Exec(`UPDATE sessions SET locked_at=?, lock_reason='away' WHERE session_id=?`, epoch.Format(time.RFC3339Nano), login.SessionID); err != nil {
		t.Fatal(err)
	}
	f.clock.Set(epoch.Add(time.Minute))
	unlocked, err := f.sessions.Unlock(t.Context(), login.SessionID, "back", true, nil)
	if err != nil || !unlocked.DeadlineAt.Equal(end) || !unlocked.GraceUntil.Equal(end) {
		t.Fatalf("%+v %v", unlocked, err)
	}
	// The raw entitlement still grows underneath the cap.
	stored := f.storedSession(t, login.Token)
	if !stored.DeadlineAt.Equal(epoch.Add(11*time.Minute)) || !stored.GraceUntil.Equal(epoch.Add(16*time.Minute+30*time.Second)) {
		t.Fatalf("%+v", stored)
	}
	if _, err := f.raw.Exec(`UPDATE sessions SET locked_at=?, lock_reason='away' WHERE session_id=?`, epoch.Format(time.RFC3339Nano), login.SessionID); err != nil {
		t.Fatal(err)
	}
	f.clock.Set(end)
	p, err := f.sessions.Authenticate(t.Context(), login.Token)
	if err != nil || p.Session.Status != domain.Submitted || p.Locked != nil || p.FinalReason != "end_time_reached" {
		t.Fatalf("%+v %v", p, err)
	}
}

func TestRepublishingWithLaterEndCannotResurrectAnExpiredSession(t *testing.T) {
	f := newFixture(t, 30)
	if _, err := f.raw.Exec(`UPDATE exams SET duration_seconds=600`); err != nil {
		t.Fatal(err)
	}
	login := f.login(t, "0012")
	end := epoch.Add(2 * time.Minute)
	if err := f.sessions.SetEndTime(t.Context(), testExamID, &end); err != nil {
		t.Fatal(err)
	}
	f.clock.Set(end)
	later := end.Add(3 * time.Minute)
	p := service.NewPublisher(f.store, f.clock)
	if _, err := p.PublishWithEndTime(t.Context(), testExamID, testExamID, true, &later); err != nil {
		t.Fatal(err)
	}
	stored := f.storedSession(t, login.Token)
	if stored.Status != domain.Submitted || stored.EndsAt == nil || !stored.EndsAt.Equal(end) {
		t.Fatalf("resurrected: %+v", stored)
	}
}
