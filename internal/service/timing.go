package service

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"exam-platform/internal/domain"
)

func validEndTime(end *time.Time, now time.Time) bool {
	return end == nil || (!end.Before(now.Add(time.Minute)) && !end.After(now.Add(24*time.Hour)))
}

// SetEndTime changes the reversible cap, never reopening a finished session.
func (s *Sessions) SetEndTime(ctx context.Context, examID string, end *time.Time) error {
	if !validProctorID(examID) {
		return ErrInvalidInput
	}
	return s.repository.WithSessionTransaction(ctx, func(tx SessionTransaction) error {
		now := s.clock.Now().UTC()
		if !validEndTime(end, now) {
			return ErrInvalidInput
		}
		if _, err := tx.Exam(ctx, examID); err != nil {
			return err
		}
		records, err := tx.ProctorSessions(ctx, examID)
		if err != nil {
			return err
		}
		for _, record := range records {
			if terminal(record.Session) {
				continue
			}
			principal, _, err := s.reconcileSession(ctx, tx, record.Session, now)
			if err != nil {
				return err
			}
			if terminal(principal.Session) {
				continue
			}
			detail, _ := json.Marshal(struct {
				EndsAt *time.Time `json:"ends_at"`
			}{end})
			if err := tx.AddEvent(ctx, domain.Event{SessionID: record.Session.ID, Type: "end_time_changed", OccurredAt: now, Detail: detail}); err != nil {
				return err
			}
		}
		return tx.SetExamEndTime(ctx, examID, end)
	})
}

// GraceExtension reports actual affected/skipped counts, not a stale roster estimate.
type GraceExtension struct {
	Changed    int        `json:"changed"`
	Skipped    int        `json:"skipped"`
	GraceUntil *time.Time `json:"grace_until,omitempty"`
	Capped     bool       `json:"capped"`
}

func (s *Sessions) ExtendGrace(ctx context.Context, sessionID string, minutes int, reason string) (GraceExtension, error) {
	return s.extendGrace(ctx, sessionID, minutes, reason, false)
}
func (s *Sessions) ExtendExamGrace(ctx context.Context, examID string, minutes int, reason string) (GraceExtension, error) {
	return s.extendGrace(ctx, examID, minutes, reason, true)
}
func (s *Sessions) extendGrace(ctx context.Context, id string, minutes int, reason string, everyone bool) (GraceExtension, error) {
	reason = strings.TrimSpace(reason)
	if !validProctorID(id) || minutes < 1 || minutes > 60 || !validText(reason, 1024) {
		return GraceExtension{}, ErrInvalidInput
	}
	var result GraceExtension
	var decisionErr error
	err := s.repository.WithSessionTransaction(ctx, func(tx SessionTransaction) error {
		now := s.clock.Now().UTC()
		extra := time.Duration(minutes) * time.Minute
		var sessions []domain.Session
		scope := "session"
		if everyone {
			scope = "exam"
			exam, err := tx.Exam(ctx, id)
			if err != nil {
				return err
			}
			exam.ExtraGraceSeconds += int64(extra / time.Second)
			if _, _, err := examTiming(exam); err != nil {
				return ErrInvalidInput
			}
			records, err := tx.ProctorSessions(ctx, id)
			if err != nil {
				return err
			}
			for _, record := range records {
				sessions = append(sessions, record.Session)
			}
			if err := tx.AddExamGrace(ctx, id, int64(extra/time.Second)); err != nil {
				return err
			}
		} else {
			session, err := tx.SessionByID(ctx, id)
			if err != nil {
				return err
			}
			sessions = []domain.Session{session}
		}
		for _, session := range sessions {
			principal, _, err := s.reconcileSession(ctx, tx, session, now)
			if err != nil {
				return err
			}
			session = principal.Session
			if terminal(session) {
				result.Skipped++
				continue
			}
			session.GraceUntil = session.GraceUntil.Add(extra)
			if err := tx.SetGraceUntil(ctx, session.ID, session.GraceUntil); err != nil {
				return err
			}
			detail, _ := json.Marshal(struct {
				Reason   string `json:"operator_reason"`
				Extended int64  `json:"extended_ms"`
				Scope    string `json:"scope"`
			}{reason, extra.Milliseconds(), scope})
			if err := tx.AddEvent(ctx, domain.Event{SessionID: session.ID, Type: "grace_extended", OccurredAt: now, Detail: detail}); err != nil {
				return err
			}
			_, grace := session.EffectiveTimes()
			result.GraceUntil = &grace
			result.Capped = result.Capped || grace.Before(session.GraceUntil)
			result.Changed++
		}
		if !everyone && result.Changed == 0 {
			decisionErr = ErrAlreadySubmitted
		}
		return nil
	})
	if err != nil {
		return GraceExtension{}, err
	}
	return result, decisionErr
}
