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
)

var (
	// ErrNotLocked means the session is not locked, or is over, so it cannot be unlocked.
	ErrNotLocked = errors.New("the session is not locked")
	// ErrLockChanged means the session was locked again since the proctor's page loaded it.
	ErrLockChanged = errors.New("the session was locked again; reload it and try again")
)

// lockSession locks a running session and records the lock in its history. It answers the record of the session's lock,
// the new one or the one it already had, and whether this call locked it.
func lockSession(ctx context.Context, tx SessionTransaction, session domain.Session, reason string, now time.Time, source string) (int64, bool, error) {
	locked, err := tx.LockSession(ctx, session.ID, now, reason)
	if err != nil {
		return 0, false, err
	}
	if !locked {
		existing, err := tx.LatestLockEvent(ctx, session.ID)
		return existing.ID, false, err
	}
	detail, err := json.Marshal(struct {
		Reason string    `json:"reason"`
		Source string    `json:"source"`
		Since  time.Time `json:"since"`
	}{reason, source, now})
	if err != nil {
		return 0, false, err
	}
	id, err := tx.RecordEvent(ctx, domain.Event{SessionID: session.ID, Type: "locked", OccurredAt: now, Detail: detail})
	return id, err == nil, err
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
		result = UnlockResult{SessionID: session.ID, DeadlineAt: deadline, GraceUntil: grace, SecondsAdded: int64(extra / time.Second), LockedMillis: locked.Milliseconds()}
		return nil
	})
	if err != nil {
		return UnlockResult{}, err
	}
	return result, nil
}
