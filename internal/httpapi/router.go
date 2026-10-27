package httpapi

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"exam-platform/internal/clock"
)

func New(clock clock.Clock, sessions SessionAPI, logger *slog.Logger, options ...Options) http.Handler {
	cfg := Options{MaxJSONBytes: 1 << 20, MaxRosterBytes: DefaultMaxRosterBytes}
	if len(options) > 0 {
		cfg = options[0]
		if cfg.MaxJSONBytes <= 0 {
			cfg.MaxJSONBytes = 1 << 20
		}
		if cfg.MaxRosterBytes <= 0 {
			cfg.MaxRosterBytes = DefaultMaxRosterBytes
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, struct {
			Status     string `json:"status"`
			ServerTime string `json:"server_time"`
		}{Status: "ok", ServerTime: clock.Now().UTC().Format(time.RFC3339Nano)})
	})
	if cfg.Rules != nil {
		mux.HandleFunc("GET /rules", rulesHandler(cfg.Rules, logger))
	}
	mux.Handle("POST /login", loginHandler(sessions, logger))
	mux.Handle("GET /session-status", StudentAuth(sessions, logger, http.HandlerFunc(statusHandler)))
	mux.Handle("GET /exam", StudentAuth(sessions, logger, examHandler(sessions, logger)))
	mux.Handle("POST /autosave", StudentAuth(sessions, logger, answerHandler(sessions, logger, cfg.MaxJSONBytes, false)))
	mux.Handle("POST /submit", StudentAuth(sessions, logger, answerHandler(sessions, logger, cfg.MaxJSONBytes, true)))
	mux.Handle("POST /advance", StudentAuth(sessions, logger, advanceHandler(sessions, logger, cfg.MaxJSONBytes)))
	mux.Handle("POST /heartbeat", StudentAuth(sessions, logger, heartbeatHandler(sessions, logger)))
	mux.Handle("POST /event", StudentAuth(sessions, logger, eventHandler(sessions, logger)))
	admin := http.NewServeMux()
	if cfg.Screenshots != nil {
		mux.Handle("POST /screenshot", StudentAuth(sessions, logger, screenshotUploadHandler(cfg.Screenshots, logger)))
		admin.Handle("GET /admin/screenshot/{id}", screenshotGetHandler(cfg.Screenshots, logger, false))
		admin.Handle("GET /admin/screenshot/{id}/thumbnail", screenshotGetHandler(cfg.Screenshots, logger, true))
		admin.Handle("POST /admin/purge-screenshots/{id}", screenshotPurgeHandler(cfg.Screenshots, logger))
	}
	if cfg.Exams != nil {
		admin.Handle("POST /admin/class/{id}/exam", createExamHandler(cfg.Exams, logger, cfg.MaxJSONBytes))
		admin.Handle("GET /admin/exam/{id}", examDetailHandler(cfg.Exams, logger))
		admin.Handle("DELETE /admin/exam/{id}", deleteExamHandler(cfg.Exams, logger))
	}
	if cfg.Publisher != nil {
		admin.Handle("GET /admin/exams", examCatalogHandler(cfg.Publisher, logger))
		admin.Handle("POST /admin/exam/{id}/publish", publishHandler(cfg.Publisher, logger))
		admin.Handle("POST /admin/unpublish", unpublishHandler(cfg.Publisher, logger))
	}
	if cfg.Classes != nil {
		admin.Handle("GET /admin/classes", classListHandler(cfg.Classes, logger))
		admin.Handle("POST /admin/classes", classCreateHandler(cfg.Classes, logger, cfg.MaxRosterBytes))
		admin.Handle("GET /admin/class/{id}/students", classStudentsHandler(cfg.Classes, logger))
		admin.Handle("PATCH /admin/class/{id}", classRenameHandler(cfg.Classes, logger))
		admin.Handle("DELETE /admin/class/{id}", classDeleteHandler(cfg.Classes, logger))
		admin.Handle("PUT /admin/class/{id}/roster", classRosterHandler(cfg.Classes, logger, cfg.MaxRosterBytes))
	}
	if cfg.Proctor != nil {
		admin.Handle("GET /admin/sessions", proctorListHandler(cfg.Proctor, logger))
		admin.Handle("GET /admin/session/{id}/timeline", proctorTimelineHandler(cfg.Proctor, logger))
		admin.Handle("GET /admin/session/{id}/submission", proctorSubmissionHandler(cfg.Proctor, logger))
		admin.Handle("POST /admin/force-submit/{id}", forceSubmitHandler(cfg.Proctor, logger))
		admin.Handle("POST /admin/session/{id}/unlock", unlockHandler(cfg.Proctor, logger))
		admin.Handle("GET /admin/settings", settingsHandler(cfg.Proctor, logger))
		admin.Handle("PUT /admin/settings", saveSettingsHandler(cfg.Proctor, logger))
	}
	guardedAdmin := AdminAuth(cfg.AdminToken, admin)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Authorize before ServeMux's method checks and path-normalization redirects.
		if r.URL.Path == "/admin" || strings.HasPrefix(r.URL.Path, "/admin/") {
			guardedAdmin.ServeHTTP(w, r)
			return
		}
		mux.ServeHTTP(w, r)
	})
}
