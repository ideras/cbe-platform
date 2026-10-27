package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"exam-platform/internal/domain"
	"exam-platform/internal/service"
)

type SessionAPI interface {
	Login(context.Context, string, string) (service.LoginResult, error)
	Authenticate(context.Context, string) (service.Principal, error)
	GetExam(context.Context, string) (service.ExamView, error)
	Autosave(context.Context, string, domain.Answers) (service.SaveResult, error)
	Submit(context.Context, string, domain.Answers) (service.SubmitResult, error)
	Advance(context.Context, string, int, domain.Answers) (service.AdvanceResult, error)
	Heartbeat(context.Context, string, string, string, domain.Sound) (service.HeartbeatResult, error)
	ClientEvent(context.Context, string, service.EventInput) (service.EventResult, error)
}

type loginRequest struct {
	AccountNumber string `json:"account_number"`
	ExamPIN       string `json:"exam_pin"`
}

type loginResponse struct {
	Policy       service.ClientPolicy `json:"telemetry_policy"`
	SessionToken string               `json:"session_token"`
	SessionID    string               `json:"session_id"`
	DeadlineAt   time.Time            `json:"deadline_at"`
	ServerTime   time.Time            `json:"server_time"`
	// MinVolumePercent is the exam's minimum system volume for the student application; 0 means leave it alone.
	MinVolumePercent int `json:"min_volume_percent"`
	// Locked says that the exam is locked, since when and why; null while it is not.
	Locked *service.LockInfo `json:"locked"`
}

type statusResponse struct {
	Policy              service.ClientPolicy `json:"telemetry_policy"`
	Status              domain.SessionStatus `json:"status"`
	ServerTime          time.Time            `json:"server_time"`
	DeadlineAt          time.Time            `json:"deadline_at"`
	GraceUntil          time.Time            `json:"grace_until"`
	SecondsRemaining    int64                `json:"seconds_remaining"`
	FinalizationPending bool                 `json:"finalization_pending,omitempty"`
	// CurrentExercise is the exercise a forward-only student is on; earlier ones are closed to them.
	CurrentExercise int `json:"current_exercise"`
	// Locked says that the exam is locked, since when and why; null while it is not.
	Locked *service.LockInfo `json:"locked"`
}

func loginHandler(sessions SessionAPI, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		input, ok := decodeBody[loginRequest](w, r, 8<<10)
		if !ok {
			return
		}
		result, err := sessions.Login(r.Context(), input.AccountNumber, input.ExamPIN)
		if err != nil {
			serviceError(w, logger, "login", err)
			return
		}
		writeJSON(w, http.StatusOK, loginResponse{Policy: result.Policy, SessionToken: result.Token, SessionID: result.SessionID, DeadlineAt: result.DeadlineAt, ServerTime: result.ServerTime, MinVolumePercent: result.MinVolumePercent, Locked: result.Locked})
	}
}

func statusHandler(w http.ResponseWriter, r *http.Request) {
	principal, ok := PrincipalFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	writeJSON(w, http.StatusOK, statusResponse{
		Policy: principal.Policy,
		Status: principal.Lifecycle.Status, ServerTime: principal.ServerTime, DeadlineAt: principal.Session.DeadlineAt,
		GraceUntil: principal.Session.GraceUntil, SecondsRemaining: principal.Lifecycle.SecondsRemaining,
		FinalizationPending: principal.Lifecycle.FinalizationPending, CurrentExercise: principal.Session.CurrentExercise,
		Locked: principal.Locked,
	})
}

type principalKey struct{}

func PrincipalFromContext(ctx context.Context) (service.Principal, bool) {
	principal, ok := ctx.Value(principalKey{}).(service.Principal)
	return principal, ok
}

// StudentAuth identifies the token's session; mutating services recheck time in their write transaction.
func StudentAuth(sessions SessionAPI, logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		values := r.Header.Values("Authorization")
		if len(values) != 1 {
			serviceError(w, logger, "authenticate", service.ErrInvalidToken)
			return
		}
		parts := strings.Fields(values[0])
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			serviceError(w, logger, "authenticate", service.ErrInvalidToken)
			return
		}
		principal, err := sessions.Authenticate(r.Context(), parts[1])
		if err != nil {
			serviceError(w, logger, "authenticate", err)
			return
		}
		terminal := principal.Lifecycle.Status == domain.Submitted || principal.Lifecycle.Status == domain.ForceSubmitted
		if terminal && !terminalAccess(r) {
			writeError(w, http.StatusForbidden, "session is locked")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), principalKey{}, principal)))
	})
}

func terminalAccess(r *http.Request) bool {
	if r.Method == http.MethodPost && r.URL.Path == "/event" {
		if r.Body == nil {
			return false
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, (8<<10)+1))
		if err != nil || len(body) > 8<<10 {
			return false
		}
		r.Body = struct {
			io.Reader
			io.Closer
		}{bytes.NewReader(body), r.Body}
		var input struct {
			Type string `json:"event_type"`
		}
		if json.Unmarshal(body, &input) != nil {
			return false
		}
		return input.Type == "capture_ready" || input.Type == "capture_failed" || input.Type == "capture_restored"
	}
	return ((r.Method == http.MethodGet || r.Method == http.MethodHead) && r.URL.Path == "/session-status") ||
		(r.Method == http.MethodPost && (r.URL.Path == "/submit" || r.URL.Path == "/screenshot"))
}

func serviceError(w http.ResponseWriter, logger *slog.Logger, operation string, err error) {
	var wait *service.ScreenshotWait
	var invalid *service.ValidationError
	var active *service.ActiveSessionsError
	switch {
	case errors.As(err, &invalid):
		writeJSON(w, http.StatusBadRequest, problemResponse{Error: "invalid request", Problems: invalid.Problems})
	case errors.As(err, &active):
		writeJSON(w, http.StatusConflict, struct {
			Error          string `json:"error"`
			ActiveSessions int    `json:"active_sessions"`
		}{"students are still working in the current exam", active.Count})
	case errors.Is(err, domain.ErrClassExists):
		writeError(w, http.StatusConflict, "a class with this name already exists")
	case errors.Is(err, domain.ErrClassHasExams):
		writeError(w, http.StatusConflict, "the class has exams; delete its exams first (exams students have taken cannot be deleted)")
	case errors.Is(err, domain.ErrExamInUse):
		writeError(w, http.StatusConflict, "the exam is published or students have taken it, so it cannot be deleted")
	case errors.Is(err, service.ErrSelectionChanged):
		writeError(w, http.StatusConflict, "the current exam changed; refresh and retry")
	case errors.Is(err, service.ErrSettingsChanged):
		writeError(w, http.StatusConflict, "the settings changed; reload them and try again")
	case errors.Is(err, service.ErrLockChanged):
		writeError(w, http.StatusConflict, "the session was locked again; reload it and try again")
	case errors.Is(err, service.ErrNotLocked):
		writeError(w, http.StatusConflict, "the session is not locked")
	case errors.As(err, &wait):
		w.Header().Set("Retry-After", strconv.FormatInt(wait.Seconds, 10))
		writeError(w, 425, "screenshot threshold has not elapsed")
	case errors.Is(err, service.ErrInvalidCredentials):
		writeError(w, http.StatusUnauthorized, "invalid credentials")
	case errors.Is(err, service.ErrInvalidToken):
		w.Header().Set("WWW-Authenticate", `Bearer realm="student"`)
		writeError(w, http.StatusUnauthorized, "unauthorized")
	case errors.Is(err, service.ErrAdmissionClosed):
		writeError(w, http.StatusForbidden, "exam admission is closed")
	case errors.Is(err, service.ErrAlreadySubmitted):
		writeError(w, http.StatusConflict, "session already submitted")
	case errors.Is(err, service.ErrExerciseChanged):
		writeError(w, http.StatusConflict, "the student is no longer on that exercise")
	case errors.Is(err, service.ErrSessionLocked):
		writeError(w, http.StatusForbidden, "session is locked")
	case errors.Is(err, service.ErrSessionOnHold):
		writeError(w, http.StatusLocked, "the exam is locked until your instructor unlocks it")
	case errors.Is(err, service.ErrScreenshotIneligible), errors.Is(err, service.ErrScreenshotsPurged):
		writeError(w, http.StatusForbidden, "screenshot incident is not eligible or retention has ended")
	case errors.Is(err, service.ErrScreenshotTooLarge):
		writeError(w, http.StatusRequestEntityTooLarge, "screenshot size or dimensions exceed limits")
	case errors.Is(err, service.ErrScreenshotUnavailable):
		writeError(w, http.StatusGone, "screenshot is unavailable")
	case errors.Is(err, service.ErrInvalidInput):
		writeError(w, http.StatusBadRequest, "invalid request")
	case errors.Is(err, service.ErrTelemetryTooLarge):
		writeError(w, http.StatusRequestEntityTooLarge, "telemetry detail size limit exceeded")
	case errors.Is(err, service.ErrAnswerTooLarge):
		writeError(w, http.StatusRequestEntityTooLarge, "answer size limit exceeded")
	case errors.Is(err, domain.ErrNotFound):
		writeError(w, http.StatusNotFound, "not found")
	case errors.Is(err, service.ErrNoCurrentExam):
		writeError(w, http.StatusServiceUnavailable, "no current exam is available")
	default:
		logger.Error("API request failed", "operation", operation, "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
	}
}

func bodyError(w http.ResponseWriter, err error) {
	var limit *http.MaxBytesError
	if errors.As(err, &limit) {
		writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
		return
	}
	writeError(w, http.StatusBadRequest, "invalid JSON request")
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(value)
}

// problemResponse is a rejected administrator document with everything wrong in it.
type problemResponse struct {
	Error    string   `json:"error"`
	Problems []string `json:"problems"`
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, struct {
		Error string `json:"error"`
	}{message})
}
