package client

import (
	"context"
	"errors"
	"net/http"
)

// Rules are the times the pre-login rules show: the alarm, the screenshots and the locks of the published exam, or of the
// server's settings when no exam is published.
type Rules struct {
	// AlarmMillis is how long a student may be out of the exam window before the alarm starts.
	AlarmMillis int64 `json:"alarm_millis"`
	// CaptureMillis is how often the screen is captured during an absence.
	CaptureMillis int64 `json:"capture_millis"`
	// LockAwayMillis is how long one absence may last before the exam locks; 0 means the lock is off.
	LockAwayMillis int64 `json:"lock_away_millis"`
	// LockNoContactMillis is how long the application may be out of contact before the exam locks; 0 means off.
	LockNoContactMillis int64 `json:"lock_no_contact_millis"`
}

// Rules reads the times the pre-login rules show from the server at address, the one the student typed. It needs no
// token: the server answers it to anyone who knows its address. A server that does not answer leaves the caller with the
// fallback wording.
func (s *Service) Rules(ctx context.Context, address string) (Rules, error) {
	base, err := validateServer(address)
	if err != nil {
		return Rules{}, err
	}
	request, cancel := s.callContext(ctx)
	defer cancel()
	var reply Rules
	if err := requestJSON(request, s.http, base, "", http.MethodGet, "/rules", nil, &reply); err != nil {
		return Rules{}, err
	}
	if reply.AlarmMillis <= 0 || reply.CaptureMillis <= 0 {
		return Rules{}, errors.New("The server's rules are invalid.")
	}
	return reply, nil
}
