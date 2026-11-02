package httpapi

import (
	"log/slog"
	"net"
	"net/http"
	"time"

	"exam-platform/internal/domain"
	"exam-platform/internal/service"
)

func heartbeatHandler(sessions SessionAPI, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		input, ok := decodeBody[struct {
			Hostname      string `json:"hostname"`
			VolumePercent *int   `json:"volume_percent"`
			VolumeMuted   *bool  `json:"volume_muted"`
		}](w, r, 8<<10)
		if !ok {
			return
		}
		principal, ok := PrincipalFromContext(r.Context())
		if !ok {
			serviceError(w, logger, "heartbeat", service.ErrInvalidToken)
			return
		}
		result, err := sessions.Heartbeat(r.Context(), principal.Session.Token, input.Hostname, peerAddress(r), domain.Sound{VolumePercent: input.VolumePercent, Muted: input.VolumeMuted})
		if err != nil {
			serviceError(w, logger, "heartbeat", err)
			return
		}
		writeJSON(w, http.StatusOK, struct {
			ServerTime time.Time `json:"server_time"`
		}{result.ServerTime})
	}
}

func eventHandler(sessions SessionAPI, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		input, ok := decodeBody[service.EventInput](w, r, 8<<10)
		if !ok {
			return
		}
		principal, ok := PrincipalFromContext(r.Context())
		if !ok {
			serviceError(w, logger, "event", service.ErrInvalidToken)
			return
		}
		result, err := sessions.ClientEvent(r.Context(), principal.Session.Token, *input)
		if err != nil {
			serviceError(w, logger, "event", err)
			return
		}
		writeJSON(w, http.StatusOK, struct {
			ID         int64     `json:"event_log_id"`
			ServerTime time.Time `json:"server_time"`
			Duplicate  bool      `json:"duplicate,omitempty"`
		}{result.EventLogID, result.ServerTime, result.Duplicate})
	}
}

// peerAddress is the address the request came from, without its port.
func peerAddress(r *http.Request) string {
	peer, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return peer
}
