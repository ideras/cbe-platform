package httpapi

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"net/http"
	"strconv"
	"time"

	"exam-platform/internal/domain"
	"exam-platform/internal/workspace"
)

// WorkspaceAPI is what the routes of workspace exams need; internal/workspace's Service is it.
type WorkspaceAPI interface {
	OnLogin(session string)
	Serve(w http.ResponseWriter, r *http.Request, session string)
	Import(ctx context.Context, classID string, document, starter, grading []byte) (string, error)
	Student(ctx context.Context, session string) (workspace.StudentView, error)
	Admin(ctx context.Context, session string) (workspace.AdminSession, error)
	SnapshotTar(ctx context.Context, id int64, w io.Writer) error
}

// maxImportBytes is the most a package import may be: the document and the two bundles.
const maxImportBytes = workspace.MaxDocumentBytes + 2*workspace.MaxBundleBytes + 1<<20

func workspaceError(w http.ResponseWriter, logger *slog.Logger, operation string, err error) {
	var invalid *workspace.ValidationError
	switch {
	case errors.As(err, &invalid):
		writeJSON(w, http.StatusBadRequest, problemResponse{Error: "invalid workspace exam", Problems: invalid.Problems})
	case errors.Is(err, workspace.ErrNotFound), errors.Is(err, domain.ErrNotFound):
		writeError(w, http.StatusNotFound, "not found")
	case errors.Is(err, workspace.ErrOver):
		writeError(w, http.StatusGone, "the exam is over")
	case errors.Is(err, workspace.ErrLocked):
		writeError(w, http.StatusLocked, "the exam is locked")
	default:
		logger.Error("workspace", "operation", operation, "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
	}
}

// importWorkspaceHandler takes a workspace package: multipart form data with the compiled document (document), the starter bundle
// (starter) and, when the package has one, the grading bundle (grading).
func importWorkspaceHandler(api WorkspaceAPI, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// A package can be 40 MiB, which the server's usual limits of seconds for a request are too short for on a slow link.
		controller := http.NewResponseController(w)
		_ = controller.SetReadDeadline(time.Now().Add(5 * time.Minute))
		_ = controller.SetWriteDeadline(time.Now().Add(5 * time.Minute))
		mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || mediaType != "multipart/form-data" || params["boundary"] == "" {
			writeError(w, http.StatusUnsupportedMediaType, "Content-Type must be multipart/form-data")
			return
		}
		reader := multipart.NewReader(http.MaxBytesReader(w, r.Body, maxImportBytes), params["boundary"])
		parts := map[string][]byte{}
		limits := map[string]int64{"document": workspace.MaxDocumentBytes, "starter": workspace.MaxBundleBytes, "grading": workspace.MaxBundleBytes}
		for {
			part, err := reader.NextPart()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				writeError(w, http.StatusBadRequest, "the form could not be read")
				return
			}
			limit, known := limits[part.FormName()]
			if !known {
				writeError(w, http.StatusBadRequest, "unknown part "+strconv.Quote(part.FormName())+"; the parts are document, starter and grading")
				return
			}
			if _, again := parts[part.FormName()]; again {
				writeError(w, http.StatusBadRequest, "the part "+part.FormName()+" is there twice")
				return
			}
			data, err := io.ReadAll(io.LimitReader(part, limit+1))
			if err != nil || int64(len(data)) > limit {
				writeError(w, http.StatusRequestEntityTooLarge, "the part "+part.FormName()+" is too large")
				return
			}
			parts[part.FormName()] = data
		}
		id, err := api.Import(r.Context(), r.PathValue("id"), parts["document"], parts["starter"], parts["grading"])
		if err != nil {
			workspaceError(w, logger, "import", err)
			return
		}
		writeJSON(w, http.StatusCreated, struct {
			ID string `json:"exam_id"`
		}{id})
	}
}

func workspaceViewHandler(api WorkspaceAPI, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := PrincipalFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		view, err := api.Student(r.Context(), principal.Session.ID)
		if err != nil {
			workspaceError(w, logger, "view", err)
			return
		}
		writeJSON(w, http.StatusOK, view)
	}
}

func workspaceSocketHandler(api WorkspaceAPI, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := PrincipalFromContext(r.Context())
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		api.Serve(w, r, principal.Session.ID)
	}
}

func workspaceAdminHandler(api WorkspaceAPI, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		view, err := api.Admin(r.Context(), r.PathValue("id"))
		if err != nil {
			workspaceError(w, logger, "admin", err)
			return
		}
		writeJSON(w, http.StatusOK, view)
	}
}

func workspaceSnapshotHandler(api WorkspaceAPI, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil || id <= 0 {
			writeError(w, http.StatusBadRequest, "invalid snapshot")
			return
		}
		_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(5 * time.Minute))
		w.Header().Set("Content-Type", "application/x-tar")
		w.Header().Set("Cache-Control", "no-store")
		if err := api.SnapshotTar(r.Context(), id, w); err != nil {
			if errors.Is(err, workspace.ErrNotFound) {
				w.Header().Del("Content-Type")
				writeError(w, http.StatusNotFound, "not found")
				return
			}
			logger.Error("workspace snapshot download", "error", err)
		}
	}
}
