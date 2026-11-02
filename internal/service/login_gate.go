package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode"

	"exam-platform/internal/domain"
)

// An account opens an exam on one computer at a time. A login that finds the session in use, because the application that
// opened it was heard from within the policy's overdue threshold, is refused until that time has passed or a proctor has
// released the session. A login that is accepted on an existing session replaces its token, so the application that
// held the old one is cut off.

// SessionOpenError says that the account is open on another computer, and for how many more seconds that is assumed.
type SessionOpenError struct{ Seconds int64 }

func (e *SessionOpenError) Error() string { return "this account is open on another computer" }

// ErrSessionOpen is what a SessionOpenError matches with errors.Is.
var ErrSessionOpen = errors.New("this account is open on another computer")

func (e *SessionOpenError) Is(target error) bool { return target == ErrSessionOpen }

type remoteAddrKey struct{}

// WithRemoteAddr says, for a login, which address the request came from, so a refusal can tell the proctor.
func WithRemoteAddr(ctx context.Context, address string) context.Context {
	return context.WithValue(ctx, remoteAddrKey{}, address)
}

func remoteAddrFrom(ctx context.Context) string {
	address, _ := ctx.Value(remoteAddrKey{}).(string)
	return address
}

// sessionOpenFor says for how long a session is still assumed to be open on the computer that last used it: the time left
// until that computer has been silent for the overdue threshold, or zero when it has. The last sign of life is the later
// of its latest heartbeat and the latest accepted login, since a computer that has just logged in has not reported yet. An
// application that announced it was closing is gone at once: a student who closes the application and opens it again does not wait.
func sessionOpenFor(session domain.Session, gate domain.LoginGate, now time.Time) time.Duration {
	if gate.AppClosed {
		return 0
	}
	last := session.LastSeen
	if gate.LastLoginAt != nil && (last == nil || gate.LastLoginAt.After(*last)) {
		last = gate.LastLoginAt
	}
	if last == nil {
		return 0
	}
	threshold := session.Policy.OverdueThreshold
	if threshold <= 0 {
		threshold = domain.DefaultPolicy().OverdueThreshold
	}
	silent := domain.NonnegativeElapsed(now, *last)
	if silent > threshold {
		return 0
	}
	return max(threshold-silent, time.Second)
}

// LoginGateView is what the proctor sees of a session that someone tried to open while it was in use, or that a proctor
// has released. It is nil while there is nothing to report.
type LoginGateView struct {
	// Refusals counts the logins refused since the latest accepted one.
	Refusals int `json:"refusals"`
	// LastRefusedAt and LastRefusedAddr describe the latest refused login.
	LastRefusedAt   *time.Time `json:"last_refused_at"`
	LastRefusedAddr string     `json:"last_refused_addr"`
	// ReleasedAt is when a proctor allowed the next login, if none has used the release yet.
	ReleasedAt *time.Time `json:"released_at"`
}

func loginGateView(gate domain.LoginGate) *LoginGateView {
	if gate.Refusals == 0 && gate.ReleasedAt == nil {
		return nil
	}
	return &LoginGateView{gate.Refusals, gate.LastRefusedAt, gate.LastRefusedAddr, gate.ReleasedAt}
}

// ReleaseResult is the outcome of a proctor allowing the next login of a session.
type ReleaseResult struct {
	SessionID  string    `json:"session_id"`
	ReleasedAt time.Time `json:"released_at"`
}

// ReleaseLogin lets the next login of a session in at once, whether or not the computer that has it open is still heard
// from. That login replaces the session's token, so anything still using the old one stops. Only a proctor can do it, and
// the reason is recorded.
func (s *Sessions) ReleaseLogin(ctx context.Context, sessionID, operatorReason string) (ReleaseResult, error) {
	operatorReason = strings.TrimSpace(operatorReason)
	if !validProctorID(sessionID) || !validText(operatorReason, 1024) || strings.ContainsFunc(operatorReason, unicode.IsControl) {
		return ReleaseResult{}, ErrInvalidInput
	}
	var result ReleaseResult
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
		if terminal(principal.Session) {
			return ErrAlreadySubmitted
		}
		gate, err := tx.LoginGate(ctx, session.ID)
		if err != nil {
			return err
		}
		if gate.ReleasedAt != nil {
			// Already released and not yet used: the second click changes nothing.
			result = ReleaseResult{SessionID: session.ID, ReleasedAt: *gate.ReleasedAt}
			return nil
		}
		detail, err := json.Marshal(struct {
			OperatorReason string `json:"operator_reason"`
		}{operatorReason})
		if err != nil {
			return err
		}
		if err := tx.AddEvent(ctx, domain.Event{SessionID: session.ID, Type: "login_released", OccurredAt: now, Detail: detail}); err != nil {
			return err
		}
		result = ReleaseResult{SessionID: session.ID, ReleasedAt: now}
		return nil
	})
	if err != nil {
		return ReleaseResult{}, err
	}
	return result, nil
}
