package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"exam-platform/internal/service"
)

type ProctorAPI interface {
	ProctorSessions(context.Context, string) (service.SessionList, error)
	ProctorTimeline(context.Context, string, int, string) (service.TimelinePage, error)
	Submission(context.Context, string) (service.Submission, error)
	ForceSubmit(context.Context, string, string) (service.SubmitResult, error)
	Unlock(context.Context, string, string, bool, *time.Time) (service.UnlockResult, error)
	Settings(context.Context) (service.Settings, error)
	SaveSettings(context.Context, service.SettingsUpdate) (service.Settings, error)
}

func proctorQuery(w http.ResponseWriter, r *http.Request, allowed ...string) (url.Values, bool) {
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid query")
		return nil, false
	}
	for key, value := range values {
		known := false
		for _, name := range allowed {
			if key == name {
				known = true
				break
			}
		}
		if !known || len(value) != 1 || value[0] == "" {
			writeError(w, http.StatusBadRequest, "invalid query")
			return nil, false
		}
	}
	return values, true
}
func proctorListHandler(proctor ProctorAPI, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		query, ok := proctorQuery(w, r, "exam_id")
		if !ok {
			return
		}
		result, err := proctor.ProctorSessions(r.Context(), query.Get("exam_id"))
		if err != nil {
			serviceError(w, logger, "list proctor sessions", err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}
func proctorTimelineHandler(proctor ProctorAPI, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		query, ok := proctorQuery(w, r, "limit", "cursor")
		if !ok {
			return
		}
		limit := service.DefaultTimelineLimit
		if raw := query.Get("limit"); raw != "" {
			n, err := strconv.ParseUint(raw, 10, 32)
			if err != nil || n < 1 || n > service.MaxTimelineLimit {
				writeError(w, http.StatusBadRequest, "invalid timeline limit")
				return
			}
			limit = int(n)
		}
		result, err := proctor.ProctorTimeline(r.Context(), r.PathValue("id"), limit, query.Get("cursor"))
		if err != nil {
			serviceError(w, logger, "read proctor timeline", err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}
func proctorSubmissionHandler(proctor ProctorAPI, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := proctorQuery(w, r); !ok {
			return
		}
		result, err := proctor.Submission(r.Context(), r.PathValue("id"))
		if err != nil {
			serviceError(w, logger, "read submission", err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}
func forceSubmitHandler(proctor ProctorAPI, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := proctorQuery(w, r); !ok {
			return
		}
		input, ok := decodeBody[struct {
			Reason string `json:"reason"`
		}](w, r, 8<<10)
		if !ok {
			return
		}
		result, err := proctor.ForceSubmit(r.Context(), r.PathValue("id"), input.Reason)
		if err != nil {
			serviceError(w, logger, "force submit", err)
			return
		}
		writeJSON(w, http.StatusOK, submitResponse{Status: result.Status, SubmittedAt: result.SubmittedAt, FinalVersionID: result.FinalVersionID, Sequence: result.Sequence})
	}
}
