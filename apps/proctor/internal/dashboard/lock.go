package dashboard

import (
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Unlock lifts the lock of a student's exam. Only a proctor can do it, and the reason is always recorded.
func (s *Service) Unlock(sessionID, reason string, giveBackTime, confirmed bool) (UnlockResult, error) {
	reason = strings.TrimSpace(reason)
	if !confirmed {
		return UnlockResult{}, errors.New("Explicit confirmation is required; nothing was sent.")
	}
	if !validID(sessionID) || len(reason) == 0 || len(reason) > 1024 || !utf8.ValidString(reason) || strings.ContainsFunc(reason, unicode.IsControl) {
		return UnlockResult{}, errors.New("A selected session and a reason of 1–1024 UTF-8 bytes are required.")
	}
	ctx, base, token, _, gen, err := s.beginAction()
	if err != nil {
		return UnlockResult{}, err
	}
	defer s.endAction()
	s.mu.Lock()
	var lockedAt *time.Time
	found := false
	for _, row := range s.view.Roster.Sessions {
		if row.ID == sessionID {
			found = true
			if row.Locked != nil {
				at := row.Locked.Since
				lockedAt = &at
			}
		}
	}
	s.mu.Unlock()
	if !found {
		return UnlockResult{}, errors.New("Session is not in this exam's loaded roster.")
	}
	body := map[string]any{"reason": reason, "give_back_time": giveBackTime}
	if lockedAt != nil {
		body["expected_locked_at"] = lockedAt
	}
	var result UnlockResult
	if err := getJSON(ctx, s.client, base, token, http.MethodPost, "/admin/session/"+sessionID+"/unlock", body, &result); err != nil {
		return UnlockResult{}, err
	}
	if err := s.stillCurrent(gen); err != nil {
		return UnlockResult{}, err
	}
	if result.SessionID != sessionID {
		return UnlockResult{}, errors.New("Unlock acknowledgment invalid; refresh to establish the outcome.")
	}
	return result, nil
}
