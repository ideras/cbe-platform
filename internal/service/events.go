package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"exam-platform/internal/domain"
)

const MaxEventDetailBytes = 4 << 10

var ErrTelemetryTooLarge = errors.New("telemetry detail size limit exceeded")

type EventInput struct {
	Type   string          `json:"event_type"`
	Detail json.RawMessage `json:"detail,omitempty"`
}
type EventResult struct {
	EventLogID int64
	ServerTime time.Time
	Duplicate  bool
}

func (s *Sessions) ClientEvent(ctx context.Context, token string, input EventInput) (EventResult, error) {
	// The calculator's events (D12) record their client detail plainly, like the capture events: permitted while
	// the exam is locked and after its end, because closing the calculator at either moment is worth recording.
	toolEvent := strings.HasPrefix(input.Type, "calc_")
	captureEvent := input.Type == "capture_ready" || input.Type == "capture_failed" || input.Type == "capture_restored" || toolEvent
	switch input.Type {
	case "focus_lost", "focus_regained", "app_started", "app_closed", "capture_ready", "capture_failed", "capture_restored", "away_lock":
	case "calc_opened", "calc_closed", "calc_mode_changed", "calc_tab_changed", "calc_evaluated", "calc_inspected":
	default:
		return EventResult{}, ErrInvalidInput
	}
	if len(input.Detail) > MaxEventDetailBytes {
		return EventResult{}, ErrTelemetryTooLarge
	}
	if input.Detail != nil {
		if !utf8.Valid(input.Detail) {
			return EventResult{}, ErrInvalidInput
		}
		var object map[string]json.RawMessage
		if err := json.Unmarshal(input.Detail, &object); err != nil || object == nil {
			return EventResult{}, ErrInvalidInput
		}
	}
	var result EventResult
	err := s.withStudent(ctx, token, captureEvent, true, func(tx SessionTransaction, principal Principal) error {
		if captureEvent {
			if toolEvent {
				// A modified application cannot fill the record of an exam without a calculator: the event is
				// refused when the session's exam names none (plan Phase 4).
				exam, err := tx.Exam(ctx, principal.Session.ExamID)
				if err != nil {
					return err
				}
				if !exam.Tools.On() {
					return &ValidationError{Problems: []string{"the exam has no calculator"}}
				}
			}
			encoded, err := json.Marshal(struct {
				Client json.RawMessage `json:"client,omitempty"`
			}{Client: input.Detail})
			if err != nil {
				return err
			}
			id, err := tx.RecordEvent(ctx, domain.Event{SessionID: principal.Session.ID, Type: input.Type, OccurredAt: principal.ServerTime, Detail: encoded})
			result = EventResult{EventLogID: id, ServerTime: principal.ServerTime}
			return err
		}
		session, _, err := s.ensureGap(ctx, tx, principal.Session, principal.ServerTime)
		if err != nil {
			return err
		}
		if input.Type == "away_lock" {
			// The student application locked itself because its own measurement reached a limit. Only the sender can be
			// hurt by this, so the server takes it at face value and locks the session too. The acknowledgment is the
			// record of the lock, whether this report made it or the server had locked the session first.
			reason := LockReasonAway
			var reported struct {
				Reason string `json:"reason"`
			}
			if len(input.Detail) > 0 && json.Unmarshal(input.Detail, &reported) == nil && reported.Reason == LockReasonNoContact {
				reason = LockReasonNoContact
			}
			id, _, err := lockSession(ctx, tx, session, reason, principal.ServerTime, "client")
			if err != nil {
				return err
			}
			result = EventResult{EventLogID: id, ServerTime: principal.ServerTime}
			return nil
		}
		previous, err := tx.LatestFocusEvent(ctx, session.ID)
		if err != nil && !errors.Is(err, domain.ErrNotFound) {
			return err
		}
		if (input.Type == "focus_lost" || input.Type == "focus_regained") && previous.Type == input.Type {
			result = EventResult{EventLogID: previous.ID, ServerTime: principal.ServerTime, Duplicate: true}
			return nil
		}
		detail := struct {
			Client                json.RawMessage `json:"client,omitempty"`
			LostEventID           *int64          `json:"focus_lost_event_log_id,omitempty"`
			DurationMillis        *int64          `json:"unfocused_duration_ms,omitempty"`
			ThresholdExceeded     bool            `json:"threshold_exceeded,omitempty"`
			Unmatched             bool            `json:"unmatched,omitempty"`
			ConnectionUnconfirmed bool            `json:"connection_unconfirmed,omitempty"`
		}{Client: input.Detail, ConnectionUnconfirmed: session.LastSeen == nil || session.NetworkGapSince != nil}
		if input.Type == "focus_regained" {
			if previous.Type == "focus_lost" && session.LastSeen != nil && session.NetworkGapSince == nil {
				elapsed := domain.NonnegativeElapsed(principal.ServerTime, previous.OccurredAt)
				duration := elapsed.Milliseconds()
				detail.LostEventID, detail.DurationMillis = &previous.ID, &duration
				detail.ThresholdExceeded = elapsed > session.Policy.FocusThreshold
			} else {
				detail.Unmatched = true
			}
		}
		encoded, err := json.Marshal(detail)
		if err != nil {
			return err
		}
		id, err := tx.RecordEvent(ctx, domain.Event{SessionID: session.ID, Type: input.Type, OccurredAt: principal.ServerTime, Detail: encoded})
		if err != nil {
			return err
		}
		result = EventResult{EventLogID: id, ServerTime: principal.ServerTime}
		return nil
	})
	if err != nil {
		return EventResult{}, err
	}
	return result, nil
}
