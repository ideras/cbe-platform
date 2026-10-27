package dashboard

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// The Settings page reads the settings when it opens. A read must not hold the dashboard busy: while it is busy the poll
// stops, every other action is refused, and the page's own save button is disabled.
func TestReadingTheSettingsLeavesTheDashboardIdle(t *testing.T) {
	f := newPublishFixture(t, "")
	fake := f.server
	fake.mu.Lock()
	fake.reply = func(w http.ResponseWriter, r *http.Request, _ []byte) bool {
		if r.URL.Path != "/admin/settings" {
			return false
		}
		reply(w, Settings{SettingsValues: SettingsValues{HeartbeatInterval: 15000, OverdueThreshold: 30000, FocusThreshold: 5000, AlertInterval: 2000,
			CaptureInterval: 5000, LockAwayAfter: 300000, LockNoContactAfter: 300000, IncidentCaptures: 60, SessionCaptures: 200,
			DefaultGrace: 30000}, UpdatedAt: "2026-10-20T00:00:00Z"})
		return true
	}
	fake.mu.Unlock()
	values, err := f.Service.Settings()
	if err != nil {
		t.Fatal(err)
	}
	if values.HeartbeatInterval != 15000 || values.CaptureInterval != 5000 {
		t.Fatalf("settings = %+v", values)
	}
	// The dashboard is idle again, so the page can save and the poll keeps running.
	if f.Service.Snapshot().Busy {
		t.Fatal("reading the settings left the dashboard busy")
	}
	// A second read works too: the first one did not wedge the service.
	if _, err := f.Service.Settings(); err != nil {
		t.Fatal(err)
	}
	if f.Service.Snapshot().Busy {
		t.Fatal("reading the settings left the dashboard busy")
	}
}

// Saving the settings is a change: it holds the dashboard busy while it runs and releases it afterwards.
func TestSavingTheSettingsReleasesTheDashboard(t *testing.T) {
	f := newPublishFixture(t, "")
	fake := f.server
	fake.mu.Lock()
	fake.reply = func(w http.ResponseWriter, r *http.Request, _ []byte) bool {
		if r.URL.Path != "/admin/settings" {
			return false
		}
		if r.Method == http.MethodPut {
			reply(w, Settings{SettingsValues: SettingsValues{HeartbeatInterval: 10000, OverdueThreshold: 25000, FocusThreshold: 3000, AlertInterval: 2000,
				CaptureInterval: 4000, LockAwayAfter: 600000, LockNoContactAfter: 420000, IncidentCaptures: 150, SessionCaptures: 300,
				DefaultGrace: 45000}, UpdatedAt: "2026-10-20T00:05:00Z"})
			return true
		}
		reply(w, Settings{SettingsValues: SettingsValues{HeartbeatInterval: 15000, OverdueThreshold: 30000, FocusThreshold: 5000, AlertInterval: 2000,
			CaptureInterval: 5000, LockAwayAfter: 300000, LockNoContactAfter: 300000, IncidentCaptures: 60, SessionCaptures: 200,
			DefaultGrace: 30000}, UpdatedAt: "2026-10-20T00:00:00Z"})
		return true
	}
	fake.mu.Unlock()
	saved, err := f.Service.SaveSettings(SettingsValues{HeartbeatInterval: 10000, OverdueThreshold: 25000, FocusThreshold: 3000,
		AlertInterval: 2000, CaptureInterval: 4000, LockAwayAfter: 600000, LockNoContactAfter: 420000, IncidentCaptures: 150,
		SessionCaptures: 300, DefaultGrace: 45000}, "2026-10-20T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if saved.HeartbeatInterval != 10000 {
		t.Fatalf("saved = %+v", saved)
	}
	if f.Service.Snapshot().Busy {
		t.Fatal("saving the settings left the dashboard busy")
	}
	// The values reached the server with the update time the page loaded.
	put := fake.seen(http.MethodPut, "/admin/settings")
	if len(put) != 1 || !strings.Contains(string(put[0].Body), `"expected_updated_at":"2026-10-20T00:00:00Z"`) {
		t.Fatalf("the save did not carry the expected update time: %+v", put)
	}
	// The dashboard keeps working after the change: the poll still refreshes the catalog.
	wait(t, func() bool { return f.Service.Snapshot().CatalogLoaded })
	time.Sleep(50 * time.Millisecond)
	if f.Service.Snapshot().Busy {
		t.Fatal("the dashboard stayed busy after the settings were saved")
	}
}

// A save another proctor's save got ahead of is refused with a message the page recognizes, so it reloads the values.
func TestAStaleSaveSaysAnotherProctorSavedFirst(t *testing.T) {
	f := newPublishFixture(t, "")
	fake := f.server
	fake.mu.Lock()
	fake.reply = func(w http.ResponseWriter, r *http.Request, _ []byte) bool {
		if r.URL.Path != "/admin/settings" || r.Method != http.MethodPut {
			return false
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":"the settings changed; reload them and try again"}`))
		return true
	}
	fake.mu.Unlock()
	_, err := f.Service.SaveSettings(SettingsValues{HeartbeatInterval: 10000}, "2026-10-20T00:00:00Z")
	if err == nil || !strings.HasPrefix(err.Error(), "Another proctor saved the settings first.") {
		t.Fatalf("a stale save = %v", err)
	}
	if f.Service.Snapshot().Busy {
		t.Fatal("a refused save left the dashboard busy")
	}
}
