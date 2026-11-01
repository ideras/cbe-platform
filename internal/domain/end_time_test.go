package domain

import (
	"testing"
	"time"
)

func TestEffectiveEndTimeBounds(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, minutes := range []int{0, 30, 65, 80} {
		s := Session{Status: Active, DeadlineAt: start.Add(time.Hour), GraceUntil: start.Add(70 * time.Minute)}
		if minutes != 0 {
			end := start.Add(time.Duration(minutes) * time.Minute)
			s.EndsAt = &end
		}
		deadline, grace := s.EffectiveTimes()
		if minutes == 30 && (!deadline.Equal(*s.EndsAt) || !grace.Equal(*s.EndsAt)) {
			t.Fatal("missing cap")
		}
		if minutes == 65 && (!deadline.Equal(s.DeadlineAt) || !grace.Equal(*s.EndsAt)) {
			t.Fatal("grace cap")
		}
		got := EvaluateSession(s, grace)
		if !got.FinalizationPending || got.EndedByEndTime != (minutes == 30 || minutes == 65) {
			t.Fatalf("%d: %+v", minutes, got)
		}
		if minutes == 65 && EvaluateSession(s, s.DeadlineAt).Status != Grace {
			t.Fatal("lost grace")
		}
	}
}
