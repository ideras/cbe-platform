package client

import (
	"testing"
	"time"
)

func TestTimingNoticesAndMonotonicReportAge(t *testing.T) {
	s := &Service{}
	now := time.Now()
	initial := Status{State: "active", DeadlineAt: now.Add(time.Hour), GraceUntil: now.Add(65 * time.Minute)}
	s.observeStatus(initial, now.Add(-time.Second))
	if s.statusObservation != 1 || len(s.timeNotices) != 0 || time.Since(s.statusObservedAt) < time.Second {
		t.Fatal("initial report")
	}
	extended := initial
	extended.GraceUntil = extended.GraceUntil.Add(5 * time.Minute)
	s.observeStatus(extended, now)
	if len(s.timeNotices) != 1 || s.timeNotices[0].Kind != "grace" || s.timeNotices[0].AddedSeconds != 300 {
		t.Fatal(s.timeNotices)
	}
	sequence := s.timeNotices[0].Sequence
	s.observeStatus(extended, now)
	if s.timeNotices[0].Sequence != sequence {
		t.Fatal("repeated notice")
	}
	unlocked := extended
	unlocked.DeadlineAt = unlocked.DeadlineAt.Add(time.Minute)
	unlocked.GraceUntil = unlocked.GraceUntil.Add(time.Minute)
	s.observeStatus(unlocked, now)
	if s.timeNoticeSequence != sequence {
		t.Fatal("unlock duplicated")
	}
	end := now.Add(30 * time.Minute)
	unlocked.EndsAt = &end
	unlocked.DeadlineAt = end
	unlocked.GraceUntil = end
	s.observeStatus(unlocked, now)
	if s.timeNotices[0].Kind != "end_time" || s.timeNoticeSequence != sequence+1 {
		t.Fatal(s.timeNotices)
	}
	if err := validateStatus(Status{State: "active", ServerTime: now, DeadlineAt: end, GraceUntil: end, EndsAt: &end}); err != nil {
		t.Fatal(err)
	}
	unlocked.EndsAt = nil
	s.observeStatus(unlocked, now)
	if s.timeNotices[0].EndsAt != nil {
		t.Fatal("clear missing")
	}
}
