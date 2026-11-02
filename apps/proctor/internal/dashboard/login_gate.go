package dashboard

import (
	"errors"
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ReleaseLogin lets the next login of a student's session in at once, even though the application that has it open was heard
// from a moment ago. That login replaces the session's token, so the other application is cut off. Only a proctor can do it,
// and the reason is always recorded.
func (s *Service) ReleaseLogin(sessionID, reason string, confirmed bool) (ReleaseResult, error) {
	reason = strings.TrimSpace(reason)
	if !confirmed {
		return ReleaseResult{}, errors.New("Explicit confirmation is required; nothing was sent.")
	}
	if !validID(sessionID) || len(reason) == 0 || len(reason) > 1024 || !utf8.ValidString(reason) || strings.ContainsFunc(reason, unicode.IsControl) {
		return ReleaseResult{}, errors.New("A selected session and a reason of 1–1024 UTF-8 bytes are required.")
	}
	ctx, base, token, _, gen, err := s.beginAction()
	if err != nil {
		return ReleaseResult{}, err
	}
	defer s.endAction()
	s.mu.Lock()
	found := false
	for _, row := range s.view.Roster.Sessions {
		found = found || row.ID == sessionID
	}
	s.mu.Unlock()
	if !found {
		return ReleaseResult{}, errors.New("Session is not in this exam's loaded roster.")
	}
	var result ReleaseResult
	if err := getJSON(ctx, s.client, base, token, http.MethodPost, "/admin/session/"+sessionID+"/release-login", map[string]any{"reason": reason}, &result); err != nil {
		return ReleaseResult{}, err
	}
	if err := s.stillCurrent(gen); err != nil {
		return ReleaseResult{}, err
	}
	if result.SessionID != sessionID || result.ReleasedAt.IsZero() {
		return ReleaseResult{}, errors.New("Release acknowledgment invalid; refresh to establish the outcome.")
	}
	return result, nil
}
