package httpapi

import (
	"context"
	"log/slog"
	"net/http"

	"exam-platform/internal/service"
)

// SettingsAPI reads and stores the monitoring settings of the server.
type SettingsAPI interface {
	Settings(context.Context) (service.Settings, error)
	SaveSettings(context.Context, service.SettingsUpdate) (service.Settings, error)
}

// RulesAPI reads the times the student rules show before the exam starts.
type RulesAPI interface {
	Rules(context.Context) (service.Rules, error)
}

// settingsHandler answers the proctor's Settings page.
func settingsHandler(settings SettingsAPI, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		result, err := settings.Settings(r.Context())
		if err != nil {
			serviceError(w, logger, "read settings", err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}

// saveSettingsHandler stores the monitoring settings of the server. A save that would overwrite another proctor's change is
// refused with 409.
func saveSettingsHandler(settings SettingsAPI, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		input, ok := decodeJSON[service.SettingsUpdate](w, r, 8<<10, true, "")
		if !ok {
			return
		}
		result, err := settings.SaveSettings(r.Context(), *input)
		if err != nil {
			serviceError(w, logger, "save settings", err)
			return
		}
		logger.Info("monitoring settings saved")
		writeJSON(w, http.StatusOK, result)
	}
}

// rulesHandler answers the times the student rules show. It needs no token: the student application asks for it as soon as
// it knows the server's address.
func rulesHandler(rules RulesAPI, logger *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		result, err := rules.Rules(r.Context())
		if err != nil {
			serviceError(w, logger, "read rules", err)
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}
