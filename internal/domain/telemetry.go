package domain

import "time"

type FocusState string

const (
	FocusUnknown   FocusState = "unknown"
	FocusFocused   FocusState = "focused"
	FocusUnfocused FocusState = "unfocused"
)

type Focus struct {
	State             FocusState `json:"state"`
	Since             *time.Time `json:"since,omitempty"`
	IncidentID        *int64     `json:"event_log_id,omitempty"`
	DurationMillis    *int64     `json:"unfocused_duration_ms,omitempty"`
	ThresholdExceeded bool       `json:"threshold_exceeded"`
}

type Presence struct {
	SessionID          string     `json:"session_id"`
	ServerTime         time.Time  `json:"server_time"`
	Monitoring         bool       `json:"monitoring"`
	Connectivity       string     `json:"connectivity"`
	Hostname           string     `json:"hostname"`
	RemoteAddr         string     `json:"remote_addr"`
	LastSeen           *time.Time `json:"last_seen"`
	HeartbeatAgeMillis *int64     `json:"heartbeat_age_ms"`
	MissedIntervals    int64      `json:"missed_intervals"`
	Overdue            bool       `json:"overdue"`
	EverOverdue        bool       `json:"ever_overdue"`
	Focus              Focus      `json:"focus"`
	VolumePercent      *int       `json:"volume_percent"`
	VolumeMuted        *bool      `json:"volume_muted"`
}

func NonnegativeElapsed(now, since time.Time) time.Duration {
	if now.Before(since) {
		return 0
	}
	return now.Sub(since)
}
