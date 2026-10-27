package domain

import (
	"encoding/json"
	"fmt"
	"time"
)

// Policy is the proctor's monitoring settings: how often the student application reports in, when an absence starts to
// alarm and to be captured, when the exam is locked, and how many screenshots are kept.
//
// It is frozen when an exam is published and copied into every session at login, so a running exam always follows the
// rules it was published with and a later change of the settings never reaches it.
type Policy struct {
	// HeartbeatInterval is how often the student application reports in.
	HeartbeatInterval time.Duration
	// OverdueThreshold is how long without contact before the connection counts as lost.
	OverdueThreshold time.Duration
	// FocusThreshold is how long a student must be out of the exam window before the alarm and the screenshots start.
	FocusThreshold time.Duration
	// AlertInterval is how often the alarm repeats during an absence.
	AlertInterval time.Duration
	// CaptureInterval is how often the screen is captured during an absence.
	CaptureInterval time.Duration
	// LockAwayAfter locks the exam after one absence lasts this long; zero turns the away lock off.
	LockAwayAfter time.Duration
	// LockNoContactAfter locks the exam after this long without any contact from the application; zero turns it off.
	LockNoContactAfter time.Duration
	// DefaultGrace is the grace period a new exam gets when its file names none.
	DefaultGrace time.Duration
	// IncidentCaptures is how many screenshots one absence keeps.
	IncidentCaptures int
	// SessionCaptures is how many screenshots one student keeps over the whole exam.
	SessionCaptures int
}

// DefaultPolicy is the settings a server without a stored row uses, with the per-absence screenshot count worked out.
func DefaultPolicy() Policy {
	return Policy{
		HeartbeatInterval:  15 * time.Second,
		OverdueThreshold:   30 * time.Second,
		FocusThreshold:     5 * time.Second,
		AlertInterval:      2 * time.Second,
		CaptureInterval:    5 * time.Second,
		LockAwayAfter:      5 * time.Minute,
		LockNoContactAfter: 5 * time.Minute,
		DefaultGrace:       30 * time.Second,
		IncidentCaptures:   30,
		SessionCaptures:    200,
	}.Normalized()
}

// IsZero says whether the policy carries no settings at all, which is how a stored NULL column is read.
func (p Policy) IsZero() bool {
	return p == Policy{}
}

// Normalized returns the policy with the per-absence screenshot count worked out: with the away lock on, the screenshots
// must cover the whole absence up to the lock, so the count is (lock - start) / interval + 1.
func (p Policy) Normalized() Policy {
	if p.LockAwayAfter > 0 {
		p.IncidentCaptures = p.EffectiveIncidentCaptures()
	}
	return p
}

// EffectiveIncidentCaptures is how many screenshots one absence keeps. With the away lock on, the count covers the whole
// absence up to the lock; otherwise it is what the proctor chose.
func (p Policy) EffectiveIncidentCaptures() int {
	if p.LockAwayAfter <= 0 || p.CaptureInterval <= 0 {
		return p.IncidentCaptures
	}
	span := p.LockAwayAfter - p.FocusThreshold
	if span < 0 {
		span = 0
	}
	return int(span/p.CaptureInterval) + 1
}

// Problems lists everything wrong with the policy, so a settings form can show all of them at once. Each problem names the
// values that are wrong, so the proctor knows what to change.
func (p Policy) Problems() []string {
	var problems []string
	add := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }
	if p.HeartbeatInterval < 5*time.Second || p.HeartbeatInterval > 60*time.Second {
		add("heartbeat every must be between 5s and 60s (it is %s)", p.HeartbeatInterval)
	}
	if p.OverdueThreshold < 2*p.HeartbeatInterval {
		add("connection lost after must be at least two heartbeats: it is %s and the heartbeat is %s", p.OverdueThreshold, p.HeartbeatInterval)
	}
	if p.OverdueThreshold > 10*time.Minute {
		add("connection lost after must be at most 10m (it is %s)", p.OverdueThreshold)
	}
	if p.FocusThreshold < time.Second || p.FocusThreshold > 10*time.Minute {
		add("alarm and screenshots start after must be between 1s and 10m (it is %s)", p.FocusThreshold)
	}
	if p.AlertInterval < time.Second || p.AlertInterval > 10*time.Second {
		add("alarm repeats every must be between 1s and 10s (it is %s)", p.AlertInterval)
	}
	if p.CaptureInterval < 3*time.Second || p.CaptureInterval > 60*time.Second {
		add("screenshot every must be between 3s and 60s (it is %s)", p.CaptureInterval)
	}
	if p.LockAwayAfter != 0 && (p.LockAwayAfter < time.Minute || p.LockAwayAfter > 60*time.Minute) {
		add("lock after away must be off or between 1m and 60m (it is %s)", p.LockAwayAfter)
	}
	if p.LockAwayAfter > 0 && p.LockAwayAfter < p.FocusThreshold+p.CaptureInterval {
		add("lock after away (%s) must come after the alarm starts (%s) plus one screenshot interval (%s): raise the lock or lower the alarm start",
			p.LockAwayAfter, p.FocusThreshold, p.CaptureInterval)
	}
	if p.LockNoContactAfter != 0 && (p.LockNoContactAfter < time.Minute || p.LockNoContactAfter > 60*time.Minute) {
		add("lock after no contact must be off or between 1m and 60m (it is %s)", p.LockNoContactAfter)
	}
	if p.LockNoContactAfter > 0 && p.LockNoContactAfter <= p.OverdueThreshold {
		add("lock after no contact (%s) must be longer than the connection lost threshold (%s)", p.LockNoContactAfter, p.OverdueThreshold)
	}
	if p.LockAwayAfter == 0 && (p.IncidentCaptures < 1 || p.IncidentCaptures > 500) {
		add("screenshots per absence must be between 1 and 500 (it is %d)", p.IncidentCaptures)
	}
	if p.SessionCaptures < p.EffectiveIncidentCaptures() || p.SessionCaptures > 2000 {
		add("screenshots per student (%d) must be at least the per-absence count (%d) and at most 2000", p.SessionCaptures, p.EffectiveIncidentCaptures())
	}
	// An exam's grace period is a positive whole number of seconds, so the default one is too.
	if p.DefaultGrace < time.Second || p.DefaultGrace > 30*time.Minute || p.DefaultGrace%time.Second != 0 {
		add("default grace period must be a whole number of seconds between 1s and 30m (it is %s)", p.DefaultGrace)
	}
	return problems
}

// PolicyMillis is the policy in whole milliseconds: the form it is stored in, sent to the proctor's Settings page and read
// back from it.
type PolicyMillis struct {
	HeartbeatInterval  int64 `json:"heartbeat_interval_ms"`
	OverdueThreshold   int64 `json:"overdue_threshold_ms"`
	FocusThreshold     int64 `json:"focus_threshold_ms"`
	AlertInterval      int64 `json:"alert_interval_ms"`
	CaptureInterval    int64 `json:"capture_interval_ms"`
	LockAwayAfter      int64 `json:"lock_away_after_ms"`
	LockNoContactAfter int64 `json:"lock_no_contact_after_ms"`
	DefaultGrace       int64 `json:"default_grace_ms"`
	IncidentCaptures   int   `json:"incident_captures"`
	SessionCaptures    int   `json:"session_captures"`
}

// Millis is the policy in whole milliseconds.
func (p Policy) Millis() PolicyMillis {
	return PolicyMillis{
		HeartbeatInterval: p.HeartbeatInterval.Milliseconds(), OverdueThreshold: p.OverdueThreshold.Milliseconds(),
		FocusThreshold: p.FocusThreshold.Milliseconds(), AlertInterval: p.AlertInterval.Milliseconds(),
		CaptureInterval: p.CaptureInterval.Milliseconds(), LockAwayAfter: p.LockAwayAfter.Milliseconds(),
		LockNoContactAfter: p.LockNoContactAfter.Milliseconds(), DefaultGrace: p.DefaultGrace.Milliseconds(),
		IncidentCaptures: p.IncidentCaptures, SessionCaptures: p.SessionCaptures,
	}
}

// Policy reads the milliseconds back as a policy.
func (m PolicyMillis) Policy() Policy {
	millis := func(value int64) time.Duration { return time.Duration(value) * time.Millisecond }
	return Policy{
		HeartbeatInterval: millis(m.HeartbeatInterval), OverdueThreshold: millis(m.OverdueThreshold),
		FocusThreshold: millis(m.FocusThreshold), AlertInterval: millis(m.AlertInterval),
		CaptureInterval: millis(m.CaptureInterval), LockAwayAfter: millis(m.LockAwayAfter),
		LockNoContactAfter: millis(m.LockNoContactAfter), DefaultGrace: millis(m.DefaultGrace),
		IncidentCaptures: m.IncidentCaptures, SessionCaptures: m.SessionCaptures,
	}
}

// MarshalJSON stores the policy as whole milliseconds, so a stored policy reads the same everywhere.
func (p Policy) MarshalJSON() ([]byte, error) { return json.Marshal(p.Millis()) }

// UnmarshalJSON reads a stored policy; a missing or empty document leaves the policy unset.
func (p *Policy) UnmarshalJSON(data []byte) error {
	if len(data) == 0 || string(data) == "null" {
		return nil
	}
	var raw PolicyMillis
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*p = raw.Policy()
	return nil
}
