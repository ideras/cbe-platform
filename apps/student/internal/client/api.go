package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"exam-platform/shared/calc"
	"exam-platform/shared/clientwire"
	"io"
	"net/http"
	"time"
	"unicode/utf8"
)

// Slot is one answer field; its ID is the key of the student's answer.
type Slot struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Language string `json:"language,omitempty"`
	Lines    int    `json:"lines,omitempty"`
	Starter  string `json:"starter,omitempty"`
}

// Item is a part of an exercise. Its body is HTML in which each slot is an element with a data-slot attribute.
type Item struct {
	ID     string   `json:"id"`
	Title  string   `json:"title"`
	Points *float64 `json:"points,omitempty"`
	Body   string   `json:"body,omitempty"`
	Slots  []Slot   `json:"slots,omitempty"`
}

// Exercise is one page of the exam; its body is the context its items share.
type Exercise struct {
	ID     string   `json:"id"`
	Title  string   `json:"title"`
	Points *float64 `json:"points,omitempty"`
	Body   string   `json:"body,omitempty"`
	Slots  []Slot   `json:"slots,omitempty"`
	Items  []Item   `json:"items,omitempty"`
}

type Content struct {
	Instructions string     `json:"instructions,omitempty"`
	Exercises    []Exercise `json:"exercises"`
}
type Saved struct {
	Sequence int64     `json:"version_seq"`
	SavedAt  time.Time `json:"saved_at"`
}
type Exam struct {
	ID    string `json:"exam_id"`
	Title string `json:"title"`
	// Navigation is free, or forward_only when a student cannot return to an exercise they left.
	Navigation string  `json:"navigation"`
	Content    Content `json:"content"`
	// Tools is the exam's effective tools configuration; nil when the exam has no calculator.
	Tools *calc.Tools `json:"tools,omitempty"`
}

// slotExercises maps every slot of the exam to the index of its exercise.
func (e *Exam) slotExercises() map[string]int {
	exercises := map[string]int{}
	for i, exercise := range e.Content.Exercises {
		for _, slot := range exercise.Slots {
			exercises[slot.ID] = i
		}
		for _, item := range exercise.Items {
			for _, slot := range item.Slots {
				exercises[slot.ID] = i
			}
		}
	}
	return exercises
}

func (e *Exam) forwardOnly() bool { return e.Navigation == "forward_only" }

// Lock says that the exam is locked, since when and why.
type Lock struct {
	Since  time.Time `json:"since"`
	Reason string    `json:"reason"`
}

type Status struct {
	Policy              *Policy   `json:"telemetry_policy,omitempty"`
	State               string    `json:"status"`
	ServerTime          time.Time `json:"server_time"`
	DeadlineAt          time.Time `json:"deadline_at"`
	GraceUntil          time.Time `json:"grace_until"`
	SecondsRemaining    int64     `json:"seconds_remaining"`
	FinalizationPending bool      `json:"finalization_pending,omitempty"`
	// CurrentExercise is the exercise the server has a forward-only student on.
	CurrentExercise int `json:"current_exercise"`
	// Locked says that the exam is locked, since when and why; absent while it is not.
	Locked *Lock `json:"locked,omitempty"`
}
type Final = clientwire.Final
type loginReply struct {
	Policy     *Policy   `json:"telemetry_policy,omitempty"`
	Token      string    `json:"session_token"`
	SessionID  string    `json:"session_id"`
	DeadlineAt time.Time `json:"deadline_at"`
	ServerTime time.Time `json:"server_time"`
	// MinVolumePercent is the exam's minimum system volume; absent (an older server) or 0 leaves the volume alone.
	MinVolumePercent *int `json:"min_volume_percent,omitempty"`
	// Locked says that the exam is locked, since when and why; absent while it is not.
	Locked *Lock `json:"locked,omitempty"`
}
type examReply struct {
	Exam
	CurrentExercise int               `json:"current_exercise"`
	Answers         map[string]string `json:"answers"`
	Latest          *Saved            `json:"latest_version"`
}
type advanceReply struct {
	CurrentExercise int       `json:"current_exercise"`
	Sequence        int64     `json:"version_seq"`
	SavedAt         time.Time `json:"saved_at"`
}

type apiFailure struct {
	code    int
	message string
}

func (e *apiFailure) Error() string { return e.message }
func failureCode(err error) int {
	var failure *apiFailure
	if errors.As(err, &failure) {
		return failure.code
	}
	return 0
}
func retryable(err error) bool {
	code := failureCode(err)
	return code == 0 || code == 408 || code == 429 || code >= 500
}

func validateServer(value string) (string, error)      { return clientwire.ValidateServer(value) }
func newHTTPClient(timeout time.Duration) *http.Client { return clientwire.NewHTTPClient(timeout) }
func requestJSON(ctx context.Context, httpClient *http.Client, base, token, method, path string, input, output any) error {
	var body io.Reader
	if input != nil {
		data, err := json.Marshal(input)
		if err != nil {
			return errors.New("Cannot encode the request.")
		}
		body = bytes.NewReader(data)
	}
	r, err := http.NewRequestWithContext(ctx, method, base+path, body)
	if err != nil {
		return errors.New("Cannot create the request.")
	}
	if input != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := httpClient.Do(r)
	if err != nil {
		return &apiFailure{message: "Server could not be reached. Work remains in memory; retrying does not guarantee it was saved."}
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		messages := map[int]string{400: "The server rejected the request. Check your input or contact the proctor.", 401: "Credentials were rejected. Check account/PIN or contact the proctor.", 403: "The session is locked or admission is closed.", 409: "This session has already been submitted, or it is no longer on that exercise.", 413: "Answer size exceeds the server limit. Shorten the answer and try again.", 423: "Your exam is locked. Call your instructor.", 503: "No exam is available. Contact the proctor."}
		message := messages[response.StatusCode]
		if message == "" {
			message = "Server request failed. Contact the proctor if it persists."
		}
		return &apiFailure{code: response.StatusCode, message: message}
	}
	const limit = 16 << 20
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil || len(data) > limit || !utf8.Valid(data) {
		return &apiFailure{message: "Server response is incomplete or too large. Confirmation is unavailable."}
	}
	if err := json.Unmarshal(data, output); err != nil {
		return &apiFailure{message: "Server response is invalid. Confirmation is unavailable."}
	}
	return nil
}
func finalStatus(value string) bool { return value == "submitted" || value == "force_submitted" }
func validateStatus(status Status) error {
	if status.State != "active" && status.State != "grace" && !finalStatus(status.State) || status.CurrentExercise < 0 || status.ServerTime.IsZero() || status.DeadlineAt.IsZero() || !status.GraceUntil.After(status.DeadlineAt) || status.SecondsRemaining < 0 || status.FinalizationPending {
		return &apiFailure{message: "Server timing response is invalid. Editing is not enabled."}
	}
	if status.State != "active" && status.SecondsRemaining != 0 {
		return &apiFailure{message: "Server timing response is invalid. Editing is not enabled."}
	}
	return nil
}
