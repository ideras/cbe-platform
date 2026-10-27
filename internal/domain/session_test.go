package domain

import (
	"testing"
	"time"
)

func TestEvaluateSessionBoundaries(t *testing.T) {
	start := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	for _, grace := range []time.Duration{30 * time.Second, 7 * time.Second} {
		session := Session{Status: Active, StartedAt: start, DeadlineAt: start.Add(time.Minute), GraceUntil: start.Add(time.Minute + grace)}
		for _, test := range []struct {
			name      string
			now       time.Time
			status    SessionStatus
			remaining int64
			pending   bool
		}{
			{"start", start, Active, 60, false},
			{"fractional second", start.Add(time.Nanosecond), Active, 60, false},
			{"before deadline", session.DeadlineAt.Add(-time.Nanosecond), Active, 1, false},
			{"at deadline", session.DeadlineAt, Grace, 0, false},
			{"after deadline", session.DeadlineAt.Add(time.Nanosecond), Grace, 0, false},
			{"before grace end", session.GraceUntil.Add(-time.Nanosecond), Grace, 0, false},
			{"at grace end", session.GraceUntil, Submitted, 0, true},
			{"after grace end", session.GraceUntil.Add(time.Nanosecond), Submitted, 0, true},
		} {
			t.Run(grace.String()+"/"+test.name, func(t *testing.T) {
				got := EvaluateSession(session, test.now)
				if got.Status != test.status || got.SecondsRemaining != test.remaining || got.FinalizationPending != test.pending {
					t.Fatalf("lifecycle = %+v", got)
				}
			})
		}
	}
}

func TestTerminalStateIsImmutable(t *testing.T) {
	start := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	for _, status := range []SessionStatus{Submitted, ForceSubmitted} {
		session := Session{Status: status, DeadlineAt: start.Add(time.Hour), GraceUntil: start.Add(2 * time.Hour)}
		for _, now := range []time.Time{session.DeadlineAt.Add(-time.Hour), session.GraceUntil.Add(time.Hour)} {
			if got := EvaluateSession(session, now); got.Status != status || got.SecondsRemaining != 0 || got.FinalizationPending {
				t.Fatalf("final state changed: %+v", got)
			}
		}
	}
}
