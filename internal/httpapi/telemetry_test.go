package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"exam-platform/internal/domain"
)

type eventResponse struct {
	ID         int64     `json:"event_log_id"`
	ServerTime time.Time `json:"server_time"`
	Duplicate  bool      `json:"duplicate"`
}

func TestHTTPTelemetryBearerScopeAndPeerMetadata(t *testing.T) {
	f, _ := fullFixture(t)
	first := f.login(t, "0012")
	other := f.login(t, "0034")
	for _, path := range []string{"/heartbeat", "/event"} {
		for _, auth := range []string{"", "Bearer invalid", "Bearer " + testAdminToken} {
			if w := request(f.router, "POST", path, `{}`, auth); w.Code != http.StatusUnauthorized {
				t.Fatal("telemetry exposed to invalid bearer")
			}
		}
	}
	if w := request(f.router, "POST", "/heartbeat", `{"hostname":"pc","id":"0034"}`, "Bearer "+first.SessionToken); w.Code != http.StatusBadRequest {
		t.Fatal("account payload was accepted")
	}
	r := httptest.NewRequest("POST", "/heartbeat?session_id="+other.SessionID, strings.NewReader(`{"hostname":" student-pc "}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+first.SessionToken)
	r.Header.Set("X-Forwarded-For", "203.0.113.99")
	r.Header.Set("Date", "Sun, 01 Jan 2040 00:00:00 GMT")
	r.RemoteAddr = "192.0.2.20:4567"
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("heartbeat = %d", w.Code)
	}
	p, err := f.sessions.Presence(t.Context(), first.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if p.Hostname != "student-pc" || p.RemoteAddr != "192.0.2.20" || p.LastSeen == nil || !p.LastSeen.Equal(f.clock.Now()) {
		t.Fatal("client claimed heartbeat identity/time/peer")
	}
	p, err = f.sessions.Presence(t.Context(), other.SessionID)
	if err != nil || p.LastSeen != nil {
		t.Fatal("heartbeat crossed session boundary")
	}
	w = request(f.router, "POST", "/event", `{"event_type":"focus_lost","detail":{"session_id":"other","account_number":"0034","timestamp":"2099-01-01T00:00:00Z"}}`, "Bearer "+first.SessionToken)
	accepted := decoded[eventResponse](t, w.Body.Bytes())
	if w.Code != http.StatusOK || accepted.ID <= 0 || !accepted.ServerTime.Equal(f.clock.Now()) {
		t.Fatal("event receipt not authoritative")
	}
	p, err = f.sessions.Presence(t.Context(), other.SessionID)
	if err != nil || p.Focus.State != domain.FocusUnknown {
		t.Fatal("detail identity forged another session's focus")
	}
}

func TestHTTPTelemetryPayloadValidationAndLimits(t *testing.T) {
	f, _ := fullFixture(t)
	login := f.login(t, "0012")
	auth := "Bearer " + login.SessionToken
	for _, body := range []string{"null", "[]", `{}`, `{"hostname":null}`, `{"hostname":12}`, `{"hostname":""}`, `{"hostname":"pc","last_seen":"2099"}`, `{"hostname":"pc","remote_addr":"203.0.113.1"}`, `{"hostname":"pc"} {}`, `{"hostname":"pc","volume_percent":101}`, `{"hostname":"pc","volume_percent":-1}`, `{"hostname":"pc","volume_percent":"loud"}`, `{"hostname":"pc","volume_percent":50.5}`, `{"hostname":"pc","volume_muted":"yes"}`, `{"hostname":"pc","volume_muted":1}`} {
		if w := request(f.router, "POST", "/heartbeat", body, auth); w.Code != http.StatusBadRequest {
			t.Fatalf("heartbeat validation = %d", w.Code)
		}
	}
	for _, body := range []string{"null", "[]", `{}`, `{"event_type":"login"}`, `{"event_type":"network_gap"}`, `{"event_type":"submitted"}`, `{"event_type":"force_submitted"}`, `{"event_type":"focus_lost","detail":null}`, `{"event_type":"focus_lost","detail":[]}`, `{"event_type":"focus_lost","detail":"text"}`, `{"event_type":"focus_lost","detail":true}`, `{"event_type":"focus_lost","account_number":"other"}`, `{"event_type":"focus_lost"} {}`} {
		if w := request(f.router, "POST", "/event", body, auth); w.Code != http.StatusBadRequest {
			t.Fatalf("event validation = %d", w.Code)
		}
	}
	for _, body := range []string{`{"event_type":"app_started","detail":{"text":"` + strings.Repeat("x", 5000) + `"}}`, `{"event_type":"app_started","detail":{"text":"` + strings.Repeat("x", 9000) + `"}}`} {
		if w := request(f.router, "POST", "/event", body, auth); w.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("event bound = %d", w.Code)
		}
	}
	if w := request(f.router, "POST", "/heartbeat", `{"hostname":"`+strings.Repeat("x", 9000)+`"}`, auth); w.Code != http.StatusRequestEntityTooLarge {
		t.Fatal("heartbeat body not bounded")
	}
	p, err := f.sessions.Presence(t.Context(), login.SessionID)
	if err != nil || p.LastSeen != nil || p.Focus.State != domain.FocusUnknown {
		t.Fatal("invalid payload wrote partial state")
	}
}

func TestTelemetryRechecksExpiryAfterMiddleware(t *testing.T) {
	for _, path := range []string{"/heartbeat", "/event"} {
		f, _ := fullFixture(t)
		login := f.login(t, "0012")
		if request(f.router, "POST", "/heartbeat", `{"hostname":"pc"}`, "Bearer "+login.SessionToken).Code != http.StatusOK {
			t.Fatal("initial heartbeat failed")
		}
		f.clock.Set(login.DeadlineAt.Add(29 * time.Second))
		payload := `{"hostname":"pc"}`
		if path == "/event" {
			payload = `{"event_type":"focus_lost"}`
		}
		body := &clockAdvancingReader{Reader: strings.NewReader(payload), advance: func() { f.clock.Set(login.DeadlineAt.Add(30 * time.Second)) }}
		r := httptest.NewRequest("POST", path, body)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer "+login.SessionToken)
		w := httptest.NewRecorder()
		f.router.ServeHTTP(w, r)
		if w.Code != http.StatusForbidden {
			t.Fatalf("delayed telemetry = %d", w.Code)
		}
		p, err := f.sessions.Presence(t.Context(), login.SessionID)
		if err != nil || p.Connectivity != "terminal" || p.LastSeen == nil || !p.LastSeen.Equal(login.ServerTime) {
			t.Fatal("late telemetry changed the finalized session")
		}
	}
}

func TestRealHTTPTelemetryAndTerminalLocking(t *testing.T) {
	f, _ := fullFixture(t)
	server := httptest.NewServer(f.router)
	defer server.Close()
	login := f.login(t, "0012")
	if result := realRequest(t, server, "POST", "/heartbeat", `{"hostname":"pc"}`, login.SessionToken); result.Code != http.StatusOK {
		t.Fatal("HTTP heartbeat failed")
	}
	lost := realRequest(t, server, "POST", "/event", `{"event_type":"focus_lost","detail":{"client_time":"2040-01-01T00:00:00Z"}}`, login.SessionToken)
	incident := decoded[eventResponse](t, lost.Body)
	if lost.Code != http.StatusOK || incident.ID <= 0 {
		t.Fatal("HTTP incident failed")
	}
	f.clock.Set(login.ServerTime.Add(10 * time.Second))
	duplicate := realRequest(t, server, "POST", "/event", `{"event_type":"focus_lost"}`, login.SessionToken)
	if result := decoded[eventResponse](t, duplicate.Body); !result.Duplicate || result.ID != incident.ID {
		t.Fatal("HTTP duplicate changed capture incident")
	}
	f.clock.Set(login.ServerTime.Add(20*time.Second + time.Nanosecond))
	p, err := f.sessions.Presence(t.Context(), login.SessionID)
	if err != nil || !p.Focus.ThresholdExceeded || p.Overdue {
		t.Fatal("independent focus/presence thresholds failed")
	}
	regained := realRequest(t, server, "POST", "/event", `{"event_type":"focus_regained"}`, login.SessionToken)
	if regained.Code != http.StatusOK {
		t.Fatal("HTTP regain failed")
	}
	if result := realRequest(t, server, "POST", "/submit", `{"answers":{}}`, login.SessionToken); result.Code != http.StatusOK {
		t.Fatal("telemetry blocked durable answers")
	}
	for _, path := range []string{"/heartbeat", "/event"} {
		if result := realRequest(t, server, "POST", path, `{}`, login.SessionToken); result.Code != http.StatusForbidden {
			t.Fatal("terminal telemetry reopened")
		}
	}
}

func TestHTTPHeartbeatReportsTheSoundLevelToPresence(t *testing.T) {
	f, _ := fullFixture(t)
	login := f.login(t, "0012")
	auth := "Bearer " + login.SessionToken
	level := func() domain.Presence {
		p, err := f.sessions.Presence(t.Context(), login.SessionID)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	if p := level(); p.VolumePercent != nil || p.VolumeMuted != nil {
		t.Fatal("level exists before any heartbeat")
	}
	for _, body := range []string{`{"hostname":"pc","volume_percent":0,"volume_muted":false}`, `{"hostname":"pc","volume_percent":100,"volume_muted":true}`} {
		if w := request(f.router, "POST", "/heartbeat", body, auth); w.Code != http.StatusOK {
			t.Fatalf("heartbeat %s = %d", body, w.Code)
		}
	}
	if p := level(); p.VolumePercent == nil || *p.VolumePercent != 100 || p.VolumeMuted == nil || !*p.VolumeMuted {
		t.Fatalf("level not visible to presence: %+v", p)
	}
	// Older clients, and clients that cannot read the sound system, simply omit the fields or send null.
	for _, body := range []string{`{"hostname":"pc"}`, `{"hostname":"pc","volume_percent":null,"volume_muted":null}`} {
		request(f.router, "POST", "/heartbeat", `{"hostname":"pc","volume_percent":40,"volume_muted":false}`, auth)
		if w := request(f.router, "POST", "/heartbeat", body, auth); w.Code != http.StatusOK {
			t.Fatalf("heartbeat %s = %d", body, w.Code)
		}
		if p := level(); p.VolumePercent != nil || p.VolumeMuted != nil {
			t.Fatalf("unreported level kept a stale value after %s: %+v", body, p)
		}
	}
}
