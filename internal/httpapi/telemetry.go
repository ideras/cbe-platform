package httpapi

import (
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strings"
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

// peerAddress is the address the request came from, without its port. A request that arrives from this computer itself
// came through the reverse proxy in front of the server (Caddy, when the server is on the internet); the address it names in
// the last X-Forwarded-For entry, the one the proxy added, is the student's. A request from any other peer is never given
// the benefit of that header, since a client could write it.
func peerAddress(r *http.Request) string {
	peer, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	if ip, err := netip.ParseAddr(peer); err == nil && ip.IsLoopback() {
		forwarded := r.Header.Values("X-Forwarded-For")
		if len(forwarded) > 0 {
			entries := strings.Split(forwarded[len(forwarded)-1], ",")
			if client, err := netip.ParseAddr(strings.TrimSpace(entries[len(entries)-1])); err == nil {
				return client.WithZone("").String()
			}
		}
	}
	return peer
}
