package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestARefusedLoginIsA409WithTheSecondsToWaitAndShowsOnTheRoster(t *testing.T) {
	f := newAPIFixture(t)
	router := New(f.clock, f.sessions, f.logger, Options{AdminToken: testAdminToken, Proctor: f.sessions})
	first := f.login(t, "0012")
	f.clock.Set(f.clock.Now().Add(12 * time.Second))
	refused := request(router, "POST", "/login", `{"account_number":"0012","exam_pin":"exam-secret"}`, "")
	var body struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(refused.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if refused.Code != http.StatusConflict || refused.Header().Get("Retry-After") != "18" || body.Error != "this account is open on another computer" {
		t.Fatalf("refusal = %d, Retry-After %q, %q", refused.Code, refused.Header().Get("Retry-After"), body.Error)
	}
	if strings.Contains(refused.Body.String(), first.SessionToken) {
		t.Fatal("the refusal leaked the token of the session that is open")
	}
	// A wrong PIN is still a plain credentials failure: the refusal does not tell who has the account open.
	if wrong := request(router, "POST", "/login", `{"account_number":"0012","exam_pin":"guess"}`, ""); wrong.Code != http.StatusUnauthorized {
		t.Fatalf("wrong PIN = %d", wrong.Code)
	}
	list := request(router, "GET", "/admin/sessions?exam_id="+apiExamID, "", "Bearer "+testAdminToken)
	var roster struct {
		Sessions []struct {
			LoginGate *struct {
				Refusals        int        `json:"refusals"`
				LastRefusedAt   *time.Time `json:"last_refused_at"`
				LastRefusedAddr string     `json:"last_refused_addr"`
				ReleasedAt      *time.Time `json:"released_at"`
			} `json:"login_gate"`
		} `json:"sessions"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &roster); err != nil || list.Code != http.StatusOK || len(roster.Sessions) != 1 {
		t.Fatalf("roster = %d %s %v", list.Code, list.Body.String(), err)
	}
	gate := roster.Sessions[0].LoginGate
	// The test request comes from 192.0.2.1:1234; the port is left out, as it is for the heartbeat.
	if gate == nil || gate.Refusals != 1 || gate.LastRefusedAddr != "192.0.2.1" || gate.LastRefusedAt == nil || gate.ReleasedAt != nil {
		t.Fatalf("login_gate = %+v", gate)
	}
}

func TestOnlyAProctorCanReleaseASessionAndItNeedsAReason(t *testing.T) {
	f := newAPIFixture(t)
	router := New(f.clock, f.sessions, f.logger, Options{AdminToken: testAdminToken, Proctor: f.sessions})
	first := f.login(t, "0012")
	path := "/admin/session/" + first.SessionID + "/release-login"
	if w := request(router, "POST", path, `{"reason":"laptop died"}`, ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("release without the admin token = %d", w.Code)
	}
	if w := request(router, "POST", path, `{"reason":"laptop died"}`, "Bearer "+first.SessionToken); w.Code != http.StatusUnauthorized {
		t.Fatalf("release with the student's token = %d", w.Code)
	}
	if w := request(router, "POST", path, `{"reason":"  "}`, "Bearer "+testAdminToken); w.Code != http.StatusBadRequest {
		t.Fatalf("release without a reason = %d", w.Code)
	}
	if w := request(router, "POST", "/admin/session/00000000-0000-4000-8000-0000000000ff/release-login", `{"reason":"x"}`, "Bearer "+testAdminToken); w.Code != http.StatusNotFound {
		t.Fatalf("release of an unknown session = %d", w.Code)
	}
	released := request(router, "POST", path, `{"reason":"laptop died"}`, "Bearer "+testAdminToken)
	var result struct {
		SessionID  string    `json:"session_id"`
		ReleasedAt time.Time `json:"released_at"`
	}
	if err := json.Unmarshal(released.Body.Bytes(), &result); err != nil || released.Code != http.StatusOK || result.SessionID != first.SessionID || !result.ReleasedAt.Equal(f.clock.Now()) {
		t.Fatalf("release = %d %s", released.Code, released.Body.String())
	}
	// The next login is accepted at once and the student application that had the session is cut off.
	again := f.login(t, "0012")
	if again.SessionID != first.SessionID || again.SessionToken == first.SessionToken {
		t.Fatalf("the released login = %+v", again)
	}
	if w := request(router, "GET", "/session-status", "", "Bearer "+first.SessionToken); w.Code != http.StatusUnauthorized {
		t.Fatalf("the replaced token = %d", w.Code)
	}
}

func TestAProctorImplementationWithoutReleaseServesNoReleaseRoute(t *testing.T) {
	f := newAPIFixture(t)
	router := New(f.clock, f.sessions, f.logger, Options{AdminToken: testAdminToken, Proctor: readOnlyProctor{f.sessions}})
	first := f.login(t, "0012")
	if w := request(router, "POST", "/admin/session/"+first.SessionID+"/release-login", `{"reason":"x"}`, "Bearer "+testAdminToken); w.Code != http.StatusNotFound && w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("release route without support = %d", w.Code)
	}
}

// readOnlyProctor hides the optional interfaces of the service, as an older embedded proctor implementation would.
type readOnlyProctor struct{ ProctorAPI }
