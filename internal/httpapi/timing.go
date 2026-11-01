package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"exam-platform/internal/service"
)

// Kept separate from ProctorAPI so older embedded proctor implementations can
// continue to serve their read-only routes.
type TimingAPI interface {
	SetEndTime(context.Context, string, *time.Time) error
	ExtendGrace(context.Context, string, int, string) (service.GraceExtension, error)
	ExtendExamGrace(context.Context, string, int, string) (service.GraceExtension, error)
}

func endTimeHandler(api TimingAPI, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		input, ok := decodeBody[struct {
			EndsAt *time.Time `json:"ends_at"`
		}](w, r, 1024)
		if !ok {
			return
		}
		if err := api.SetEndTime(r.Context(), r.PathValue("id"), input.EndsAt); err != nil {
			serviceError(w, logger, "change end time", err)
			return
		}
		writeJSON(w, http.StatusOK, struct {
			EndsAt *time.Time `json:"ends_at"`
		}{input.EndsAt})
	}
}
func extendGraceHandler(api TimingAPI, logger *slog.Logger, everyone bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		input, ok := decodeBody[struct {
			Minutes int    `json:"minutes"`
			Reason  string `json:"reason"`
		}](w, r, 8<<10)
		if !ok {
			return
		}
		method := api.ExtendGrace
		if everyone {
			method = api.ExtendExamGrace
		}
		result, err := method(r.Context(), r.PathValue("id"), input.Minutes, input.Reason)
		if err != nil {
			serviceError(w, logger, "extend grace", err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}
