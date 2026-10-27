package domain

import "time"

type Screenshot struct {
	ScreenshotMetadata
	SessionID string
	ExamID    string
	Path      string
	SHA256    string
	State     string
}

type ScreenshotInfo struct {
	Width            int        `json:"width,omitempty"`
	Height           int        `json:"height,omitempty"`
	Bytes            int64      `json:"byte_size,omitempty"`
	ClientCapturedAt *time.Time `json:"client_captured_at_untrusted,omitempty"`
}
