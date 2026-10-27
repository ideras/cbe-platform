package httpapi

import (
	"context"
	"log/slog"
	"net/http"

	"exam-platform/internal/service"
)

type PublishAPI interface {
	Catalog(context.Context) (service.ExamCatalog, error)
	Publish(ctx context.Context, examID, expected string, allowActive bool) (service.PublishResult, error)
	Unpublish(ctx context.Context, expected string, allowActive bool) (service.PublishResult, error)
}

// publishRequest names the exam the caller saw published, so a stale decision cannot overwrite a newer one.
type publishRequest struct {
	ExpectedCurrent string `json:"expected_current"`
	AllowActive     bool   `json:"allow_active"`
}

func examCatalogHandler(publisher PublishAPI, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := proctorQuery(w, r); !ok {
			return
		}
		catalog, err := publisher.Catalog(r.Context())
		if err != nil {
			serviceError(w, logger, "list exams", err)
			return
		}
		writeJSON(w, http.StatusOK, catalog)
	}
}

func publishHandler(publisher PublishAPI, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		input, ok := decodeBody[publishRequest](w, r, 1<<10)
		if !ok {
			return
		}
		result, err := publisher.Publish(r.Context(), r.PathValue("id"), input.ExpectedCurrent, input.AllowActive)
		if err != nil {
			serviceError(w, logger, "publish exam", err)
			return
		}
		logger.Info("exam published", "exam_id", result.CurrentExamID, "class_id", result.CurrentClassID, "replaced_exam_id", result.ReplacedExamID, "active_sessions", result.ActiveSessions)
		writeJSON(w, http.StatusOK, result)
	}
}

func unpublishHandler(publisher PublishAPI, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		input, ok := decodeBody[publishRequest](w, r, 1<<10)
		if !ok {
			return
		}
		result, err := publisher.Unpublish(r.Context(), input.ExpectedCurrent, input.AllowActive)
		if err != nil {
			serviceError(w, logger, "unpublish exam", err)
			return
		}
		logger.Info("exam admission closed", "replaced_exam_id", result.ReplacedExamID, "active_sessions", result.ActiveSessions)
		writeJSON(w, http.StatusOK, result)
	}
}
