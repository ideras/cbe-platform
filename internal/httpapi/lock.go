package httpapi

import (
	"log/slog"
	"net/http"
	"time"
)

// unlockHandler lifts the lock of a session. Only a proctor can do it.
func unlockHandler(proctor ProctorAPI, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := proctorQuery(w, r); !ok {
			return
		}
		input, ok := decodeJSON[struct {
			Reason           string     `json:"reason"`
			GiveBackTime     bool       `json:"give_back_time"`
			ExpectedLockedAt *time.Time `json:"expected_locked_at"`
		}](w, r, 8<<10, false, "")
		if !ok {
			return
		}
		result, err := proctor.Unlock(r.Context(), r.PathValue("id"), input.Reason, input.GiveBackTime, input.ExpectedLockedAt)
		if err != nil {
			serviceError(w, logger, "unlock session", err)
			return
		}
		logger.Info("session unlocked", "session_id", r.PathValue("id"), "seconds_added", result.SecondsAdded)
		writeJSON(w, http.StatusOK, result)
	}
}
