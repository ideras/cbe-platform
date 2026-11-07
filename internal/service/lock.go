package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"exam-platform/internal/domain"
)

// The reasons a session can be locked.
const (
	// LockReasonAway means the student was out of the exam window for too long.
	LockReasonAway = "away"
	// LockReasonNoContact means nothing reached the server from the student application for too long.
	LockReasonNoContact = "no_contact"
	// LockReasonNetwork means the student locked the exam themselves, with the server out of reach, to reconnect to the
	// exam network without the alarm.
	LockReasonNetwork = "network"
)

// lockWatchedByScreenshots says whether the student application gives the student the run of the computer during a lock of
// this reason, to reconnect to the network, and so takes screenshots meanwhile.
func lockWatchedByScreenshots(reason string) bool {
	return reason == LockReasonNetwork || reason == LockReasonNoContact
}

// NetworkLocksLeft is how many more times the student may lock the exam themselves to reconnect to the network.
func NetworkLocksLeft(session domain.Session) int {
	return max(0, domain.NetworkLockLimit-session.NetworkLocks)
}

var (
	// ErrNotLocked means the session is not locked, or is over, so it cannot be unlocked.
	ErrNotLocked = errors.New("the session is not locked")
	// ErrLockChanged means the session was locked again since the proctor's page loaded it.
	ErrLockChanged = errors.New("the session was locked again; reload it and try again")
)

// lockSession locks a running session and records the lock in its history. The record is made now; since is when the lock
// began, which is earlier only for a lock the student application made while the server was out of reach. It answers the
// record of the session's lock, the new one or the one it already had, and whether this call locked it.
func lockSession(ctx context.Context, tx SessionTransaction, session domain.Session, reason string, now, since time.Time, source string) (int64, bool, error) {
	locked, err := tx.LockSession(ctx, session.ID, since, reason, reason == LockReasonNetwork)
	if err != nil {
		return 0, false, err
	}
	if !locked {
		existing, err := tx.LatestLockEvent(ctx, session.ID)
		return existing.ID, false, err
	}
	// A lock the student asked for says which of the allowed ones it is, so the proctor sees a student past the limit.
	request, limit := 0, 0
	if reason == LockReasonNetwork {
		request, limit = session.NetworkLocks+1, domain.NetworkLockLimit
	}
	detail, err := json.Marshal(struct {
		Reason  string    `json:"reason"`
		Source  string    `json:"source"`
		Since   time.Time `json:"since"`
		Request int       `json:"request,omitempty"`
		Limit   int       `json:"limit,omitempty"`
	}{reason, source, since, request, limit})
	if err != nil {
		return 0, false, err
	}
	id, err := tx.RecordEvent(ctx, domain.Event{SessionID: session.ID, Type: "locked", OccurredAt: now, Detail: detail})
	return id, err == nil, err
}

// lockReportGrace is how long after contact came back a lock the application made while out of reach can still be reported as
// belonging to that gap. The application reports at once, a heartbeat interval after the connection returns, and tries again
// every one; a gap that closed longer ago than this has nothing to do with a lock reported now.
const lockReportGrace = 2 * time.Minute

// clientLockSince is when a lock the student application made for the network began, the one the student asked for or the one
// for lack of contact. The application makes it while the server is out of reach, so the report arrives some time after the
// lock; it says how long ago the lock began, and an age is used because the student's clock may be off the server's. The server
// believes it no further than what it knows: the lock cannot have begun before the exam, before the latest unlock, or before the
// network gap the student was in, and without a gap that is still open or has just closed there is nothing to say the student was
// out of reach, so it began when the server heard of it.
func clientLockSince(ctx context.Context, tx SessionTransaction, session domain.Session, now time.Time, age time.Duration) (time.Time, error) {
	gap, err := tx.LatestGap(ctx, session.ID)
	if errors.Is(err, domain.ErrNotFound) {
		return now, nil
	}
	if err != nil {
		return time.Time{}, err
	}
	if gap.End != nil && now.Sub(*gap.End) > lockReportGrace {
		return now, nil
	}
	since := now.Add(-age)
	for _, floor := range []time.Time{gap.Start, session.StartedAt} {
		if since.Before(floor) {
			since = floor
		}
	}
	if session.UnlockedAt != nil && since.Before(*session.UnlockedAt) {
		since = *session.UnlockedAt
	}
	if since.After(now) {
		since = now
	}
	return since, nil
}

// lockReason is the reason a session should be locked now, or "" when it should not be. Both clocks start again when a
// proctor unlocks the session.
//
// The server measures absences from the events it received, so it cannot tell how long a student was really away across a
// network gap: the no-contact rule covers that case instead.
func lockReason(ctx context.Context, tx SessionTransaction, session domain.Session, now time.Time) (string, error) {
	policy := session.Policy
	if session.LockedAt != nil || terminal(session) {
		return "", nil
	}
	since := func(start time.Time) time.Duration {
		if session.UnlockedAt != nil && session.UnlockedAt.After(start) {
			start = *session.UnlockedAt
		}
		return domain.NonnegativeElapsed(now, start)
	}
	if policy.LockAwayAfter > 0 && session.NetworkGapSince == nil {
		event, err := tx.LatestFocusEvent(ctx, session.ID)
		if err != nil && !errors.Is(err, domain.ErrNotFound) {
			return "", err
		}
		if err == nil && event.Type == "focus_lost" && since(event.OccurredAt) >= policy.LockAwayAfter {
			return LockReasonAway, nil
		}
	}
	if policy.LockNoContactAfter > 0 {
		reference := session.StartedAt
		if session.LastSeen != nil && session.LastSeen.After(reference) {
			reference = *session.LastSeen
		}
		if since(reference) >= policy.LockNoContactAfter {
			return LockReasonNoContact, nil
		}
	}
	return "", nil
}

// UnlockResult is what lifting a lock does to a session.
type UnlockResult struct {
	SessionID    string    `json:"session_id"`
	DeadlineAt   time.Time `json:"deadline_at"`
	GraceUntil   time.Time `json:"grace_until"`
	SecondsAdded int64     `json:"seconds_added"`
	LockedMillis int64     `json:"locked_ms"`
}

// Unlock lifts the lock of a session. Only a proctor can do it, and only while the exam is still running.
func (s *Sessions) Unlock(ctx context.Context, sessionID, operatorReason string, giveBackTime bool, expectedLockedAt *time.Time) (UnlockResult, error) {
	operatorReason = strings.TrimSpace(operatorReason)
	if !validProctorID(sessionID) || !validText(operatorReason, 1024) {
		return UnlockResult{}, ErrInvalidInput
	}
	var result UnlockResult
	err := s.repository.WithSessionTransaction(ctx, func(tx SessionTransaction) error {
		now := s.clock.Now().UTC()
		session, err := tx.SessionByID(ctx, sessionID)
		if err != nil {
			return err
		}
		principal, _, err := s.reconcileSession(ctx, tx, session, now)
		if err != nil {
			return err
		}
		session = principal.Session
		if session.LockedAt == nil || terminal(session) {
			return ErrNotLocked
		}
		if expectedLockedAt != nil && !expectedLockedAt.Equal(*session.LockedAt) {
			return ErrLockChanged
		}
		locked := domain.NonnegativeElapsed(now, *session.LockedAt)
		extra := time.Duration(0)
		if giveBackTime {
			extra = locked
		}
		deadline := session.DeadlineAt.Add(extra)
		grace := session.GraceUntil.Add(extra)
		unlocked, err := tx.UnlockSession(ctx, session.ID, deadline, grace, now)
		if err != nil {
			return err
		}
		if !unlocked {
			return ErrNotLocked
		}
		detail, err := json.Marshal(struct {
			OperatorReason string `json:"operator_reason"`
			LockedMillis   int64  `json:"locked_ms"`
			ExtendedMillis int64  `json:"extended_ms"`
		}{operatorReason, locked.Milliseconds(), extra.Milliseconds()})
		if err != nil {
			return err
		}
		if err := tx.AddEvent(ctx, domain.Event{SessionID: session.ID, Type: "unlocked", OccurredAt: now, Detail: detail}); err != nil {
			return err
		}
		session.DeadlineAt, session.GraceUntil = deadline, grace
		deadline, grace = session.EffectiveTimes()
		result = UnlockResult{SessionID: session.ID, DeadlineAt: deadline, GraceUntil: grace, SecondsAdded: int64(extra / time.Second), LockedMillis: locked.Milliseconds()}
		return nil
	})
	if err != nil {
		return UnlockResult{}, err
	}
	return result, nil
}
