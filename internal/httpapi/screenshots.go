package httpapi

import (
	"bytes"
	"context"
	"exam-platform/internal/service"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"
)

type ScreenshotAPI interface {
	Upload(context.Context, string, int64, int, *time.Time, io.Reader) (service.ScreenshotResult, error)
	Get(context.Context, int64, bool) ([]byte, string, error)
	Purge(context.Context, string) (service.PurgeResult, error)
}

func screenshotUploadHandler(images ScreenshotAPI, logger *slog.Logger) http.HandlerFunc {
	budget := make(chan struct{}, 2)
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := PrincipalFromContext(r.Context())
		if !ok {
			serviceError(w, logger, "screenshot", service.ErrInvalidToken)
			return
		}
		select {
		case budget <- struct{}{}:
			defer func() { <-budget }()
		case <-r.Context().Done():
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, service.MaxScreenshotBytes+(64<<10))
		reader, err := r.MultipartReader()
		if err != nil {
			writeError(w, 415, "Content-Type must be multipart/form-data")
			return
		}
		var eventID int64
		var index int
		var clientAt *time.Time
		var data []byte
		seen := map[string]bool{}
		for count := 0; ; count++ {
			part, err := reader.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil || count >= 4 {
				writeError(w, 400, "invalid multipart screenshot")
				return
			}
			name := part.FormName()
			if seen[name] {
				writeError(w, 400, "duplicate multipart field")
				return
			}
			seen[name] = true
			limit := int64(128)
			if name == "image" {
				limit = service.MaxScreenshotBytes
			}
			value, err := io.ReadAll(io.LimitReader(part, limit+1))
			if err != nil {
				bodyError(w, err)
				return
			}
			if int64(len(value)) > limit {
				writeError(w, 413, "screenshot field too large")
				return
			}
			switch name {
			case "event_log_id":
				eventID, err = strconv.ParseInt(string(value), 10, 64)
				if err != nil || eventID <= 0 {
					writeError(w, 400, "invalid event reference")
					return
				}
			case "capture_index":
				// Which capture of the focus loss this is: 0 at the threshold, then one per capture interval.
				index, err = strconv.Atoi(string(value))
				if err != nil || index < 0 {
					writeError(w, 400, "invalid capture index")
					return
				}
			case "client_captured_at":
				at, e := time.Parse(time.RFC3339Nano, string(value))
				if e != nil {
					writeError(w, 400, "invalid diagnostic capture time")
					return
				}
				at = at.UTC()
				clientAt = &at
			case "image":
				if kind := part.Header.Get("Content-Type"); kind != "image/png" && kind != "image/jpeg" {
					writeError(w, 415, "image must be PNG or JPEG")
					return
				}
				data = value
			default:
				writeError(w, 400, "unexpected multipart field")
				return
			}
		}
		if eventID <= 0 || len(data) == 0 {
			writeError(w, 400, "event_log_id and image are required")
			return
		}
		result, err := images.Upload(r.Context(), principal.Session.Token, eventID, index, clientAt, bytes.NewReader(data))
		if err != nil {
			serviceError(w, logger, "upload screenshot", err)
			return
		}
		writeJSON(w, 200, result)
	}
}
func screenshotGetHandler(images ScreenshotAPI, logger *slog.Logger, thumbnail bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil || id <= 0 {
			writeError(w, 400, "invalid screenshot ID")
			return
		}
		data, contentType, err := images.Get(r.Context(), id, thumbnail)
		if err != nil {
			serviceError(w, logger, "read screenshot", err)
			return
		}
		extension := ".png"
		if contentType == "image/jpeg" {
			extension = ".jpg"
		}
		w.Header().Set("Content-Type", contentType)
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`inline; filename="screenshot-%d%s"`, id, extension))
		w.Header().Set("Content-Length", strconv.Itoa(len(data)))
		w.WriteHeader(200)
		_, _ = w.Write(data)
	}
}
func screenshotPurgeHandler(images ScreenshotAPI, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		input, ok := decodeBody[struct {
			Confirm bool `json:"confirm"`
		}](w, r, 1024)
		if !ok {
			return
		}
		if !input.Confirm {
			writeError(w, 400, "explicit purge confirmation required")
			return
		}
		result, err := images.Purge(r.Context(), r.PathValue("id"))
		if err != nil {
			serviceError(w, logger, "purge screenshots", err)
			return
		}
		writeJSON(w, 200, result)
	}
}
