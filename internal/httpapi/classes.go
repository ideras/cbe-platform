package httpapi

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"

	"exam-platform/internal/service"
)

// DefaultMaxRosterBytes bounds a roster upload; a few thousand students fit with room to spare.
const DefaultMaxRosterBytes = 4 << 20

type ClassAPI interface {
	Create(ctx context.Context, name string, input io.Reader, apply bool) (service.ClassReport, error)
	List(context.Context) (service.ClassList, error)
	Students(ctx context.Context, classID string) (service.ClassRoster, error)
	Rename(ctx context.Context, classID, name string) (service.ClassEntry, error)
	Delete(ctx context.Context, classID string) (service.DeletedClass, error)
	ReplaceRoster(ctx context.Context, classID string, input io.Reader, apply bool) (service.RosterChange, error)
}

func classListHandler(classes ClassAPI, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := proctorQuery(w, r); !ok {
			return
		}
		list, err := classes.List(r.Context())
		if err != nil {
			serviceError(w, logger, "list classes", err)
			return
		}
		writeJSON(w, http.StatusOK, list)
	}
}

func classStudentsHandler(classes ClassAPI, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := proctorQuery(w, r); !ok {
			return
		}
		roster, err := classes.Students(r.Context(), r.PathValue("id"))
		if err != nil {
			serviceError(w, logger, "list class students", err)
			return
		}
		writeJSON(w, http.StatusOK, roster)
	}
}

// classCreateHandler stores the uploaded roster CSV as a new class named by the `name` query parameter, or with
// dry_run=true only reports what the file holds.
func classCreateHandler(classes ClassAPI, logger *slog.Logger, limit int64) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		query, ok := proctorQuery(w, r, "dry_run", "name")
		if !ok {
			return
		}
		var preview bool
		switch query.Get("dry_run") {
		case "", "false":
		case "true":
			preview = true
		default:
			writeError(w, http.StatusBadRequest, "invalid query")
			return
		}
		body, ok := readCSV(w, r, limit)
		if !ok {
			return
		}
		report, err := classes.Create(r.Context(), query.Get("name"), bytes.NewReader(body), !preview)
		if err != nil {
			serviceError(w, logger, "create class", err)
			return
		}
		if report.Applied {
			logger.Info("class created", "class_id", report.Class.ID, "students", report.Students, "warnings", report.WarningCount)
			writeJSON(w, http.StatusCreated, report)
			return
		}
		writeJSON(w, http.StatusOK, report)
	}
}

// readCSV reads an uploaded roster, or answers why it cannot.
func readCSV(w http.ResponseWriter, r *http.Request, limit int64) ([]byte, bool) {
	if mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mediaType != "text/csv" {
		writeError(w, http.StatusUnsupportedMediaType, "Content-Type must be text/csv")
		return nil, false
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
		} else {
			writeError(w, http.StatusBadRequest, "invalid request")
		}
		return nil, false
	}
	return body, true
}

func classRenameHandler(classes ClassAPI, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		input, ok := decodeJSON[struct {
			Name string `json:"name"`
		}](w, r, 2<<10, true, "")
		if !ok {
			return
		}
		renamed, err := classes.Rename(r.Context(), r.PathValue("id"), input.Name)
		if err != nil {
			serviceError(w, logger, "rename class", err)
			return
		}
		logger.Info("class renamed", "class_id", renamed.ID)
		writeJSON(w, http.StatusOK, renamed)
	}
}

func classDeleteHandler(classes ClassAPI, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := proctorQuery(w, r); !ok {
			return
		}
		deleted, err := classes.Delete(r.Context(), r.PathValue("id"))
		if err != nil {
			serviceError(w, logger, "delete class", err)
			return
		}
		logger.Info("class deleted", "class_id", deleted.ID, "students", deleted.Students, "pruned", deleted.Pruned)
		writeJSON(w, http.StatusOK, deleted)
	}
}

// classRosterHandler replaces the roster of a class with an uploaded CSV, or with dry_run=true only reports what that would do.
func classRosterHandler(classes ClassAPI, logger *slog.Logger, limit int64) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		preview, ok := dryRun(w, r)
		if !ok {
			return
		}
		body, ok := readCSV(w, r, limit)
		if !ok {
			return
		}
		change, err := classes.ReplaceRoster(r.Context(), r.PathValue("id"), bytes.NewReader(body), !preview)
		if err != nil {
			serviceError(w, logger, "replace class roster", err)
			return
		}
		if change.Applied {
			logger.Info("class roster replaced", "class_id", r.PathValue("id"), "students", change.Students, "added", change.Added, "removed", change.Removed, "pruned", change.Pruned)
		}
		writeJSON(w, http.StatusOK, change)
	}
}
