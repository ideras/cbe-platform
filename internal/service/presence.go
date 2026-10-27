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

type HeartbeatResult struct{ ServerTime time.Time }

func (s *Sessions) ensureGap(ctx context.Context, tx SessionTransaction, session domain.Session, now time.Time) (domain.Session, bool, error) {
	if terminal(session) || session.LastSeen == nil || session.NetworkGapSince != nil {
		return session, false, nil
	}
	policy := session.Policy
	observedUntil := now
	if observedUntil.After(session.GraceUntil) {
		observedUntil = session.GraceUntil
	}
	age := domain.NonnegativeElapsed(observedUntil, *session.LastSeen)
	if age <= policy.OverdueThreshold {
		return session, false, nil
	}
	since := session.LastSeen.Add(policy.OverdueThreshold)
	if err := tx.SetNetworkGap(ctx, session.ID, since); err != nil {
		return domain.Session{}, false, err
	}
	detail, err := json.Marshal(struct {
		LastSeen        time.Time `json:"last_seen"`
		OverdueAfter    time.Time `json:"overdue_after"`
		ObservedUntil   time.Time `json:"observed_until"`
		AgeMillis       int64     `json:"observed_age_ms"`
		ThresholdMillis int64     `json:"overdue_threshold_ms"`
		IntervalMillis  int64     `json:"heartbeat_interval_ms"`
	}{*session.LastSeen, since, observedUntil, age.Milliseconds(), policy.OverdueThreshold.Milliseconds(), policy.HeartbeatInterval.Milliseconds()})
	if err != nil {
		return domain.Session{}, false, err
	}
	if err := tx.AddEvent(ctx, domain.Event{SessionID: session.ID, Type: "network_gap", OccurredAt: now, Detail: detail}); err != nil {
		return domain.Session{}, false, err
	}
	session.NetworkGapSince = &since
	return session, true, nil
}

func (s *Sessions) Heartbeat(ctx context.Context, token, hostname, remoteAddr string, sound domain.Sound) (HeartbeatResult, error) {
	hostname = strings.TrimSpace(hostname)
	if !validText(hostname, 255) || !validText(remoteAddr, 128) || strings.ContainsFunc(hostname, unicode.IsControl) || strings.ContainsFunc(remoteAddr, unicode.IsControl) || sound.VolumePercent != nil && (*sound.VolumePercent < 0 || *sound.VolumePercent > 100) {
		return HeartbeatResult{}, ErrInvalidInput
	}
	var result HeartbeatResult
	err := s.withStudent(ctx, token, false, true, func(tx SessionTransaction, principal Principal) error {
		session, _, err := s.ensureGap(ctx, tx, principal.Session, principal.ServerTime)
		if err != nil {
			return err
		}
		now := principal.ServerTime
		if session.LastSeen == nil {
			detail, err := json.Marshal(struct {
				Hostname   string `json:"hostname"`
				RemoteAddr string `json:"remote_addr"`
			}{hostname, remoteAddr})
			if err != nil {
				return err
			}
			if err := tx.AddEvent(ctx, domain.Event{SessionID: session.ID, Type: "network_connected", OccurredAt: now, Detail: detail}); err != nil {
				return err
			}
		} else if session.NetworkGapSince != nil {
			detail, err := json.Marshal(struct {
				LastSeen      *time.Time `json:"last_seen_before_gap"`
				OverdueAfter  time.Time  `json:"overdue_after"`
				GapMillis     int64      `json:"gap_duration_ms"`
				OverdueMillis int64      `json:"overdue_duration_ms"`
				Hostname      string     `json:"hostname"`
				RemoteAddr    string     `json:"remote_addr"`
			}{session.LastSeen, *session.NetworkGapSince, domain.NonnegativeElapsed(now, *session.LastSeen).Milliseconds(), domain.NonnegativeElapsed(now, *session.NetworkGapSince).Milliseconds(), hostname, remoteAddr})
			if err != nil {
				return err
			}
			if err := tx.AddEvent(ctx, domain.Event{SessionID: session.ID, Type: "network_reconnected", OccurredAt: now, Detail: detail}); err != nil {
				return err
			}
		} else if session.LastSeen != nil && (session.Hostname != hostname || session.RemoteAddr != remoteAddr) {
			detail, err := json.Marshal(struct {
				PreviousHostname   string `json:"previous_hostname"`
				PreviousRemoteAddr string `json:"previous_remote_addr"`
				Hostname           string `json:"hostname"`
				RemoteAddr         string `json:"remote_addr"`
			}{session.Hostname, session.RemoteAddr, hostname, remoteAddr})
			if err != nil {
				return err
			}
			if err := tx.AddEvent(ctx, domain.Event{SessionID: session.ID, Type: "connection_changed", OccurredAt: now, Detail: detail}); err != nil {
				return err
			}
		}
		if err := tx.TouchHeartbeat(ctx, session.ID, hostname, remoteAddr, sound, now); err != nil {
			return err
		}
		result.ServerTime = now
		return nil
	})
	if err != nil {
		return HeartbeatResult{}, err
	}
	return result, nil
}

// Presence observes committed signals without changing lifecycle or incident history.
func (s *Sessions) Presence(ctx context.Context, sessionID string) (domain.Presence, error) {
	var result domain.Presence
	err := s.repository.WithSessionTransaction(ctx, func(tx SessionTransaction) error {
		now := s.clock.Now().UTC()
		session, err := tx.SessionByID(ctx, sessionID)
		if err != nil {
			return err
		}
		result, err = s.presence(ctx, tx, session, now)
		return err
	})
	if err != nil {
		return domain.Presence{}, err
	}
	return result, nil
}

func (s *Sessions) presence(ctx context.Context, tx SessionTransaction, session domain.Session, now time.Time) (domain.Presence, error) {
	ever, err := tx.EverOverdue(ctx, session.ID)
	if err != nil {
		return domain.Presence{}, err
	}
	policy := session.Policy
	lifecycle := domain.EvaluateSession(session, now)
	monitoring := lifecycle.Status == domain.Active || lifecycle.Status == domain.Grace
	result := domain.Presence{SessionID: session.ID, ServerTime: now, Monitoring: monitoring, Connectivity: "no_heartbeat", Hostname: session.Hostname, RemoteAddr: session.RemoteAddr, LastSeen: session.LastSeen, EverOverdue: ever, Focus: domain.Focus{State: domain.FocusUnknown}, VolumePercent: session.Sound.VolumePercent, VolumeMuted: session.Sound.Muted}
	if session.LastSeen != nil {
		// Monitoring ends with the session: a finished one is measured up to its submission (or the end of grace, when
		// it expired), so its heartbeat age and missed intervals stop growing.
		until := now
		if !monitoring {
			end := session.GraceUntil
			if session.SubmittedAt != nil && session.SubmittedAt.Before(end) {
				end = *session.SubmittedAt
			}
			if end.Before(until) {
				until = end
			}
		}
		age := domain.NonnegativeElapsed(until, *session.LastSeen)
		millis := age.Milliseconds()
		result.HeartbeatAgeMillis = &millis
		result.MissedIntervals = int64(age / policy.HeartbeatInterval)
		result.Overdue = monitoring && (session.NetworkGapSince != nil || age > policy.OverdueThreshold)
		result.Connectivity = "healthy"
		if result.Overdue {
			result.Connectivity = "overdue"
			result.EverOverdue = true
		}
	}
	if !monitoring {
		result.Connectivity = "terminal"
	}
	if monitoring && session.LastSeen != nil && !result.Overdue {
		event, err := tx.LatestFocusEvent(ctx, session.ID)
		if err != nil && !errors.Is(err, domain.ErrNotFound) {
			return domain.Presence{}, err
		}
		if err == nil {
			switch event.Type {
			case "focus_regained":
				result.Focus = domain.Focus{State: domain.FocusFocused, Since: &event.OccurredAt}
			case "focus_lost":
				elapsed := domain.NonnegativeElapsed(now, event.OccurredAt)
				duration := elapsed.Milliseconds()
				result.Focus = domain.Focus{State: domain.FocusUnfocused, Since: &event.OccurredAt, IncidentID: &event.ID, DurationMillis: &duration, ThresholdExceeded: elapsed > policy.FocusThreshold}
			}
		}
	}
	return result, nil
}

func (s *Sessions) StudentsWithoutSession(ctx context.Context, examID string) ([]domain.Student, error) {
	var students []domain.Student
	err := s.repository.WithSessionTransaction(ctx, func(tx SessionTransaction) error {
		if _, err := tx.Exam(ctx, examID); err != nil {
			return err
		}
		var err error
		students, err = tx.StudentsWithoutSession(ctx, examID)
		return err
	})
	return students, err
}
