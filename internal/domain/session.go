package domain

import (
	"encoding/json"
	"errors"
	"time"
)

var ErrNotFound = errors.New("record not found")

type Session struct {
	ID             string
	AccountNumber  string
	ExamID         string
	Token          string
	StartedAt      time.Time
	DeadlineAt     time.Time
	GraceUntil     time.Time
	LastSeen       *time.Time
	SubmittedAt    *time.Time
	Status         SessionStatus
	FinalVersionID *int64
	// CurrentExercise is the index of the exercise a student of a forward-only exam is on; earlier ones are closed to them.
	CurrentExercise int
	// Policy is the monitoring settings this session follows; it was copied from the exam at login.
	Policy Policy
	// LockedAt is when the session was locked; nil while it is not locked.
	LockedAt *time.Time
	// LockReason says why the session was locked: "away" or "no_contact".
	LockReason string
	// UnlockedAt is when a proctor last unlocked the session; the absence and no-contact clocks start again from it.
	UnlockedAt      *time.Time
	Hostname        string
	RemoteAddr      string
	NetworkGapSince *time.Time
	Sound           Sound
}

// LockRecord is one lock or unlock of a session, as the proctor and the downloads see it.
type LockRecord struct {
	// Type is "locked" or "unlocked".
	Type string `json:"type"`
	// At is when the lock or the unlock happened.
	At time.Time `json:"at"`
	// Reason says why the exam was locked: "away" or "no_contact".
	Reason string `json:"reason,omitempty"`
	// OperatorReason is the proctor's reason for an unlock.
	OperatorReason string `json:"operator_reason,omitempty"`
	// LockedMillis is how long the exam was locked before the unlock.
	LockedMillis int64 `json:"locked_ms,omitempty"`
	// ExtendedMillis is the time the proctor gave back, if any.
	ExtendedMillis int64 `json:"extended_ms,omitempty"`
}

// Sound is the output level the student's computer last reported; nil means it was not reported.
type Sound struct {
	VolumePercent *int
	Muted         *bool
}

type Event struct {
	ID         int64
	SessionID  string
	Type       string
	OccurredAt time.Time
	Detail     json.RawMessage
}

type Lifecycle struct {
	Status              SessionStatus
	SecondsRemaining    int64
	FinalizationPending bool
}

func EvaluateSession(session Session, now time.Time) Lifecycle {
	if session.Status == Submitted || session.Status == ForceSubmitted {
		return Lifecycle{Status: session.Status}
	}
	if !now.Before(session.GraceUntil) {
		// Student operations finalize before returning; observational admin views expose pending expiry.
		return Lifecycle{Status: Submitted, FinalizationPending: true}
	}
	if !now.Before(session.DeadlineAt) {
		return Lifecycle{Status: Grace}
	}
	remaining := session.DeadlineAt.Sub(now)
	seconds := int64(remaining / time.Second)
	if remaining%time.Second != 0 {
		seconds++
	}
	return Lifecycle{Status: Active, SecondsRemaining: seconds}
}
