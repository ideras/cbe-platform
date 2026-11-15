package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"exam-platform/internal/service"
	"exam-platform/internal/workspace"
)

type PublishAPI interface {
	Catalog(context.Context) (service.ExamCatalog, error)
	Publish(ctx context.Context, examID, expected string, allowActive bool) (service.PublishResult, error)
	Unpublish(ctx context.Context, expected string, allowActive bool) (service.PublishResult, error)
}

// publishRequest names the exam the caller saw published, so a stale decision cannot overwrite a newer one.
type publishRequest struct {
	ExpectedCurrent string     `json:"expected_current"`
	AllowActive     bool       `json:"allow_active"`
	EndsAt          *time.Time `json:"ends_at,omitempty"`
}

// examCatalogHandler lists the exams for the proctor dashboard, with the server's release, which it reads on every poll: the
// dashboard compares it with the release each student's application reports.
func examCatalogHandler(publisher PublishAPI, version string, logger *slog.Logger) http.HandlerFunc {
	return catalogHandler(publisher, version, nil, logger)
}

// catalogEntry is an exam of the catalog with what the exam list needs to show of a workspace exam (Phase 7): that it is one, how many TODO
// items it has, and the state of its own image. A question exam has none of the three.
type catalogEntry struct {
	service.ExamEntry
	Kind  string               `json:"kind,omitempty"`
	Todos *int                 `json:"todos,omitempty"`
	Image *workspace.ImageView `json:"image,omitempty"`
}

// catalogResponse is the catalog with its exams as catalogEntry; the outer Exams hides the embedded one.
type catalogResponse struct {
	service.ExamCatalog
	Exams []catalogEntry `json:"exams"`
}

// catalogHandler lists the exams; when workspace exams are enabled it adds, from the database alone, what the list shows of each.
func catalogHandler(publisher PublishAPI, version string, workspaces WorkspaceAPI, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := proctorQuery(w, r); !ok {
			return
		}
		catalog, err := publisher.Catalog(r.Context())
		if err != nil {
			serviceError(w, logger, "list exams", err)
			return
		}
		catalog.ServerVersion = version
		if workspaces == nil {
			writeJSON(w, http.StatusOK, catalog)
			return
		}
		states, err := workspaces.ImageStates(r.Context())
		if err != nil {
			logger.Error("list exams: the state of the workspace exams", "error", err)
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		response := catalogResponse{ExamCatalog: catalog, Exams: make([]catalogEntry, 0, len(catalog.Exams))}
		for _, entry := range catalog.Exams {
			item := catalogEntry{ExamEntry: entry}
			if state, ok := states[entry.ID]; ok {
				todos := state.Todos
				image := state.Image
				item.Kind, item.Todos, item.Image = "workspace", &todos, &image
			}
			response.Exams = append(response.Exams, item)
		}
		writeJSON(w, http.StatusOK, response)
	}
}

func publishHandler(publisher PublishAPI, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		input, ok := decodeBody[publishRequest](w, r, 1<<10)
		if !ok {
			return
		}
		var result service.PublishResult
		var err error
		if input.EndsAt != nil {
			api, ok := publisher.(interface {
				PublishWithEndTime(context.Context, string, string, bool, *time.Time) (service.PublishResult, error)
			})
			if !ok {
				writeError(w, http.StatusBadRequest, "end time is not supported")
				return
			}
			result, err = api.PublishWithEndTime(r.Context(), r.PathValue("id"), input.ExpectedCurrent, input.AllowActive, input.EndsAt)
		} else {
			result, err = publisher.Publish(r.Context(), r.PathValue("id"), input.ExpectedCurrent, input.AllowActive)
		}
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
