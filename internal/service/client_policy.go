package service

import (
	"time"

	"exam-platform/internal/domain"
)

// ClientPolicy is what the student application follows for one session.
type ClientPolicy struct {
	HeartbeatMillis int64 `json:"heartbeat_interval_ms"`
	OverdueMillis   int64 `json:"overdue_threshold_ms"`
	FocusMillis     int64 `json:"focus_threshold_ms"`
	// AlertMillis is how often the alert repeats while a sustained focus loss lasts.
	AlertMillis int64 `json:"alert_interval_ms"`
	// CaptureMillis is how often the screen is captured after the first capture at the threshold.
	CaptureMillis int64 `json:"capture_interval_ms"`
	// LockAwayAfterMillis is how long one absence may last before the exam locks; 0 means the lock is off.
	LockAwayAfterMillis int64 `json:"lock_away_after_ms"`
	// LockNoContactAfterMillis is how long the application may be out of contact before the exam locks; 0 means off.
	LockNoContactAfterMillis int64 `json:"lock_no_contact_after_ms"`
	IncidentCaptures         int   `json:"incident_captures"`
	SessionCaptures          int   `json:"session_captures"`
}

// clientPolicy is the policy a session follows, as the student application needs it.
func clientPolicy(policy domain.Policy) ClientPolicy {
	// Round up for local scheduling; server duration comparisons keep full precision.
	millis := func(d time.Duration) int64 {
		if d <= 0 {
			return 0
		}
		return int64((d-1)/time.Millisecond) + 1
	}
	return ClientPolicy{millis(policy.HeartbeatInterval), millis(policy.OverdueThreshold), millis(policy.FocusThreshold),
		millis(policy.AlertInterval), millis(policy.CaptureInterval), millis(policy.LockAwayAfter), millis(policy.LockNoContactAfter),
		policy.IncidentCaptures, policy.SessionCaptures}
}
