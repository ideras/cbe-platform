package dashboard

import (
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// nextEndTime deliberately uses the proctor computer's local zone, not the server
// or student's zone. AddDate handles a midnight crossing (including DST).
func nextEndTime(hhmm string, now time.Time) (time.Time, error) {
	parsed, err := time.Parse("15:04", hhmm)
	if err != nil || parsed.Format("15:04") != hhmm {
		return time.Time{}, errors.New("Use HH:MM for the end time.")
	}
	end := time.Date(now.Year(), now.Month(), now.Day(), parsed.Hour(), parsed.Minute(), 0, 0, now.Location())
	if !end.After(now) {
		end = end.AddDate(0, 0, 1)
	}
	if end.Sub(now) < time.Minute || end.Sub(now) > 24*time.Hour {
		return time.Time{}, errors.New("Choose an end time at least one minute away and within 24 hours.")
	}
	return end, nil
}

type EndTimePreview struct {
	EndsAt       time.Time `json:"ends_at"`
	Label        string    `json:"label"`
	ClockWarning bool      `json:"clock_warning"`
}

func (s *Service) PreviewEndTime(hhmm string) (EndTimePreview, error) {
	now := time.Now()
	end, err := nextEndTime(hhmm, now)
	if err != nil {
		return EndTimePreview{}, err
	}
	s.mu.Lock()
	server, observed := s.view.Catalog.ServerTime, s.view.LastSuccess
	s.mu.Unlock()
	if observed != nil {
		server = server.Add(time.Since(*observed))
	}
	day := "today"
	if end.Day() != now.Day() {
		day = "tomorrow"
	}
	diff := now.Sub(server)
	return EndTimePreview{end.UTC(), "Ends " + day + " at " + end.Format("15:04 MST (Jan 2)"), !server.IsZero() && (diff > time.Minute || diff < -time.Minute)}, nil
}

func (s *Service) SetEndTime(examID, hhmm string, confirmed bool) error {
	if !confirmed {
		return errors.New("Explicit confirmation is required; nothing was sent.")
	}
	if !validID(examID) {
		return errors.New("Choose an exam.")
	}
	var end *time.Time
	if hhmm != "" {
		value, err := nextEndTime(hhmm, time.Now())
		if err != nil {
			return err
		}
		end = &value
	}
	ctx, base, token, err := s.beginAdmin()
	if err != nil {
		return err
	}
	defer s.endAction()
	var result struct {
		EndsAt *time.Time `json:"ends_at"`
	}
	if err := getJSON(ctx, s.client, base, token, http.MethodPut, "/admin/exam/"+examID+"/end-time", map[string]any{"ends_at": end}, &result); err != nil {
		return err
	}
	s.signal()
	return nil
}

type GraceExtension struct {
	Changed    int        `json:"changed"`
	Skipped    int        `json:"skipped"`
	GraceUntil *time.Time `json:"grace_until"`
	Capped     bool       `json:"capped"`
}

func (s *Service) ExtendGrace(id string, minutes int, reason string, confirmed bool) (GraceExtension, error) {
	return s.extendGrace(id, minutes, reason, confirmed, false)
}
func (s *Service) ExtendExamGrace(id string, minutes int, reason string, confirmed bool) (GraceExtension, error) {
	return s.extendGrace(id, minutes, reason, confirmed, true)
}
func (s *Service) extendGrace(id string, minutes int, reason string, confirmed, everyone bool) (GraceExtension, error) {
	if !confirmed {
		return GraceExtension{}, errors.New("Explicit confirmation is required; nothing was sent.")
	}
	reason = strings.TrimSpace(reason)
	if !validID(id) || minutes < 1 || minutes > 60 || len(reason) == 0 || len(reason) > 1024 || !utf8.ValidString(reason) || strings.ContainsFunc(reason, unicode.IsControl) {
		return GraceExtension{}, errors.New("Choose 1–60 minutes and a reason of 1–1024 UTF-8 bytes.")
	}
	ctx, base, token, _, gen, err := s.beginAction()
	if err != nil {
		return GraceExtension{}, err
	}
	defer s.endAction()
	s.mu.Lock()
	found := everyone && s.view.ExamID == id
	if !everyone {
		for _, row := range s.view.Roster.Sessions {
			if row.ID == id {
				found = true
			}
		}
	}
	s.mu.Unlock()
	if !found {
		return GraceExtension{}, errors.New("Selection is not in the loaded exam.")
	}
	path := "/admin/session/" + id + "/extend-grace"
	if everyone {
		path = "/admin/exam/" + id + "/extend-grace"
	}
	var result GraceExtension
	if err := getJSON(ctx, s.client, base, token, http.MethodPost, path, map[string]any{"minutes": minutes, "reason": reason}, &result); err != nil {
		return result, err
	}
	if err := s.stillCurrent(gen); err != nil {
		return result, err
	}
	s.signal()
	return result, nil
}

func (s *Service) PublishTimed(examID, expectedExam string, allowActive, confirmed bool, hhmm string) (PublishOutcome, error) {
	if !confirmed {
		return PublishOutcome{}, errors.New("Explicit confirmation is required; nothing was sent.")
	}
	if !validID(examID) || !validID(expectedExam) && expectedExam != "" {
		return PublishOutcome{}, errors.New("Choose an exam.")
	}
	end, err := nextEndTime(hhmm, time.Now())
	if err != nil {
		return PublishOutcome{}, err
	}
	outcome, err := s.changeAdmission("/admin/exam/"+examID+"/publish", examID, expectedExam, allowActive, &end)
	if err != nil {
		return PublishOutcome{}, err
	}
	s.mu.Lock()
	if !s.stopped && s.view.Connected {
		s.selectLocked(examID)
	}
	s.mu.Unlock()
	s.signal()
	return outcome, nil
}
