package httpapi

import (
	"context"
	"log/slog"
	"net/http"

	"exam-platform/internal/service"
)

// LoginGateAPI is kept apart from ProctorAPI, like TimingAPI, so an embedded proctor implementation without it still serves
// its other routes.
type LoginGateAPI interface {
	ReleaseLogin(context.Context, string, string) (service.ReleaseResult, error)
}

// releaseLoginHandler lets the next login of a session in, however recently the application that has it open was heard
// from. Only a proctor can do it.
func releaseLoginHandler(api LoginGateAPI, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := proctorQuery(w, r); !ok {
			return
		}
		input, ok := decodeJSON[struct {
			Reason string `json:"reason"`
		}](w, r, 8<<10, false, "")
		if !ok {
			return
		}
		result, err := api.ReleaseLogin(r.Context(), r.PathValue("id"), input.Reason)
		if err != nil {
			serviceError(w, logger, "release session login", err)
			return
		}
		logger.Info("session released for login", "session_id", result.SessionID)
		writeJSON(w, http.StatusOK, result)
	}
}
