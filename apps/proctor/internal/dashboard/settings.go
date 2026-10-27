package dashboard

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
)

// settingsRequest is what the Settings page sends: the values and the update time it loaded.
type settingsRequest struct {
	SettingsValues
	ExpectedUpdatedAt *time.Time `json:"expected_updated_at"`
}

// Settings reads the monitoring settings of the server. They are the same on every proctor computer. It is a read: it
// must not hold the dashboard busy, or the poll would stop and every action would be refused.
func (s *Service) Settings() (Settings, error) {
	ctx, base, token, _, err := s.adminConnection()
	if err != nil {
		return Settings{}, err
	}
	var result Settings
	if err := getJSON(ctx, s.client, base, token, http.MethodGet, "/admin/settings", nil, &result); err != nil {
		return Settings{}, err
	}
	return result, nil
}

// SaveSettings stores the monitoring settings of the server. expectedUpdatedAt is the update time the page loaded: when
// another proctor saved in between, the server refuses the change and the page reloads.
func (s *Service) SaveSettings(values SettingsValues, expectedUpdatedAt string) (Settings, error) {
	request := settingsRequest{SettingsValues: values}
	if strings.TrimSpace(expectedUpdatedAt) != "" {
		at, err := time.Parse(time.RFC3339Nano, expectedUpdatedAt)
		if err != nil {
			return Settings{}, errors.New("The update time of the settings could not be read; reload the page and try again.")
		}
		request.ExpectedUpdatedAt = &at
	}
	ctx, base, token, err := s.beginAdmin()
	if err != nil {
		return Settings{}, err
	}
	// The settings change no session, so the rows on screen stay current.
	defer s.endAdmin()
	var result Settings
	if err := adminSend(ctx, s.client, base, token, http.MethodPut, "/admin/settings", "application/json", mustJSON(request), &result); err != nil {
		if refused(err, http.StatusConflict) {
			// The page looks for this sentence to reload the values the server holds.
			return Settings{}, errors.New("Another proctor saved the settings first. The values were reloaded; check them and save again.")
		}
		return Settings{}, err
	}
	return result, nil
}

// mustJSON encodes a request body that cannot fail to encode.
func mustJSON(value any) []byte {
	data, err := json.Marshal(value)
	if err != nil {
		return []byte("{}")
	}
	return data
}
