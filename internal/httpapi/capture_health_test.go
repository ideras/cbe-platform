package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestTerminalCaptureHealthAllowanceIsTypeAndSizeBounded(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		allowed    bool
	}{
		{"ready", `{"event_type":"capture_ready"}`, true},
		{"failure", `{"event_type":"capture_failed"}`, true},
		{"recovery", `{"event_type":"capture_restored"}`, true},
		{"ordinary focus", `{"event_type":"focus_lost"}`, false},
		{"missing type", `{}`, false},
		{"trailing document", `{"event_type":"capture_failed"} {}`, false},
		{"oversized", `{"event_type":"capture_failed","detail":{"reason":"` + strings.Repeat("x", 8<<10) + `"}}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "/event", strings.NewReader(tc.body))
			if got := terminalAccess(r); got != tc.allowed {
				t.Fatalf("terminal access = %v; want %v", got, tc.allowed)
			}
		})
	}
}

func TestCaptureHealthLogsAreScopedProtectedAndDoNotAlterFinalOrPresence(t *testing.T) {
	f, _ := proctorFixture(t)
	login := f.login(t, "0012")
	other := f.login(t, "0034")
	auth := "Bearer " + login.SessionToken
	for _, kind := range []string{"capture_ready", "capture_failed", "capture_restored"} {
		body := `{"event_type":"` + kind + `","detail":{"client_report_id":"test-report","session_id":"` + other.SessionID + `","client_time":"2099-01-01T00:00:00Z"}}`
		if w := request(f.router, "POST", "/event", body, ""); w.Code != http.StatusUnauthorized {
			t.Fatal("capture health accepted without student authorization")
		}
		w := request(f.router, "POST", "/event", body, auth)
		if w.Code != http.StatusOK || !decoded[eventResponse](t, w.Body.Bytes()).ServerTime.Equal(f.clock.Now()) {
			t.Fatalf("capture log = %d %s", w.Code, w.Body.String())
		}
	}
	p, err := f.sessions.Presence(t.Context(), login.SessionID)
	if err != nil || p.LastSeen != nil {
		t.Fatal("capture log impersonated a heartbeat")
	}
	p, err = f.sessions.Presence(t.Context(), other.SessionID)
	if err != nil || p.LastSeen != nil {
		t.Fatal("capture detail forged another student's presence")
	}
	final := request(f.router, "POST", "/submit", `{"answers":{"q1":"preserved final"}}`, auth)
	if final.Code != http.StatusOK {
		t.Fatal(final.Body.String())
	}
	f.clock.Set(f.clock.Now().Add(2 * time.Minute))
	for _, kind := range []string{"capture_failed", "capture_restored"} {
		w := request(f.router, "POST", "/event", `{"event_type":"`+kind+`","detail":{"client_report_id":"delayed-report"}}`, auth)
		if w.Code != http.StatusOK {
			t.Fatalf("delayed post-final capture log = %d %s", w.Code, w.Body.String())
		}
	}
	for _, kind := range []string{"focus_lost", "focus_regained", "app_started", "app_closed"} {
		if w := request(f.router, "POST", "/event", `{"event_type":"`+kind+`"}`, auth); w.Code != http.StatusForbidden {
			t.Fatalf("ordinary terminal telemetry accepted: %s %d", kind, w.Code)
		}
	}
	retry := request(f.router, "POST", "/submit", `{"answers":{"q1":"must not replace final"}}`, auth)
	if decoded[submitResponse](t, retry.Body.Bytes()) != decoded[submitResponse](t, final.Body.Bytes()) {
		t.Fatal("capture logging changed final answer identity")
	}
	timeline := request(f.router, "GET", "/admin/session/"+login.SessionID+"/timeline", "", "Bearer "+testAdminToken)
	if timeline.Code != http.StatusOK || !json.Valid(timeline.Body.Bytes()) {
		t.Fatal("protected history unavailable")
	}
	for _, value := range []string{"capture_ready", "capture_failed", "capture_restored", "test-report", "delayed-report", "preserved final"} {
		if !strings.Contains(timeline.Body.String(), value) {
			t.Fatalf("protected history omitted %q", value)
		}
	}
}
