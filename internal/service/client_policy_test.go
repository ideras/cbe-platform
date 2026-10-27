package service

import (
	"testing"
	"time"

	"exam-platform/internal/domain"
)

func TestClientPolicyPositiveMillisecondsAndConfiguredDurations(t *testing.T) {
	for _, policy := range []domain.Policy{domain.DefaultPolicy(), {HeartbeatInterval: time.Nanosecond, OverdueThreshold: 2 * time.Nanosecond, FocusThreshold: time.Nanosecond, AlertInterval: time.Nanosecond, CaptureInterval: time.Nanosecond, IncidentCaptures: 1, SessionCaptures: 1}, {HeartbeatInterval: 1250 * time.Millisecond, OverdueThreshold: 2501 * time.Millisecond, FocusThreshold: 3333 * time.Millisecond, AlertInterval: time.Second, CaptureInterval: 3 * time.Second, IncidentCaptures: 30, SessionCaptures: 200}} {
		got := clientPolicy(policy)
		if got.HeartbeatMillis < 1 || got.FocusMillis < 1 || got.OverdueMillis < got.HeartbeatMillis || got.AlertMillis < 1 || got.CaptureMillis < 1 || got.IncidentCaptures < 1 || got.SessionCaptures < got.IncidentCaptures {
			t.Fatalf("invalid client policy: %+v", got)
		}
		if time.Duration(got.HeartbeatMillis)*time.Millisecond < policy.HeartbeatInterval || time.Duration(got.FocusMillis)*time.Millisecond < policy.FocusThreshold {
			t.Fatal("rounded scheduling policy down")
		}
	}
}

// A session keeps the rules it was published with; the lock times travel with them.
func TestClientPolicyCarriesTheLockTimes(t *testing.T) {
	policy := domain.Policy{HeartbeatInterval: time.Second, OverdueThreshold: 2 * time.Second, FocusThreshold: 5 * time.Second,
		LockAwayAfter: 5 * time.Minute, LockNoContactAfter: 6 * time.Minute}
	got := clientPolicy(policy)
	if got.LockAwayAfterMillis != 300000 || got.LockNoContactAfterMillis != 360000 {
		t.Fatalf("policy = %+v", got)
	}
	if got := clientPolicy(domain.Policy{HeartbeatInterval: time.Second, OverdueThreshold: 2 * time.Second, FocusThreshold: 5 * time.Second}); got.LockAwayAfterMillis != 0 || got.LockNoContactAfterMillis != 0 {
		t.Fatalf("a lock that is off must not be advertised: %+v", got)
	}
}
