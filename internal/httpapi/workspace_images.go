package httpapi

import (
	"log/slog"
	"net/http"
	"strconv"
)

// The routes of an exam's own image (Phase 7). The proctor builds it, reads its state and the log of a build, and the exam list shows it.

// workspaceImageBuildHandler queues a build of the exam's image. The body is optional: {"rebuild": true} builds again without the runner's cache.
func workspaceImageBuildHandler(api WorkspaceAPI, logger *slog.Logger) http.HandlerFunc {
	type request struct {
		Rebuild bool `json:"rebuild"`
	}
	return func(w http.ResponseWriter, r *http.Request) {
		var input request
		if r.ContentLength != 0 {
			body, ok := decodeBody[request](w, r, 1<<10)
			if !ok {
				return
			}
			input = *body
		}
		view, err := api.StartImageBuild(r.Context(), r.PathValue("id"), input.Rebuild)
		if err != nil {
			workspaceError(w, logger, "start an image build", err)
			return
		}
		writeJSON(w, http.StatusAccepted, view)
	}
}

// workspaceImageHandler reads the state of an exam's image with its builds; ?log=1 adds the log of the latest build.
func workspaceImageHandler(api WorkspaceAPI, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		view, err := api.ImageView(r.Context(), r.PathValue("id"), r.URL.Query().Get("log") == "1")
		if err != nil {
			workspaceError(w, logger, "read an exam's image", err)
			return
		}
		writeJSON(w, http.StatusOK, view)
	}
}

// workspaceImageBuildLogHandler reads one build of an exam's image with its log.
func workspaceImageBuildLogHandler(api WorkspaceAPI, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("build"), 10, 64)
		if err != nil || id < 1 {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		build, err := api.ImageBuildLog(r.Context(), r.PathValue("id"), id)
		if err != nil {
			workspaceError(w, logger, "read an image build", err)
			return
		}
		writeJSON(w, http.StatusOK, build)
	}
}
