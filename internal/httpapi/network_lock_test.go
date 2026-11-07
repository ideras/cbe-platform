package httpapi

import (
	"net/http"
	"strings"
	"testing"

	"exam-platform/internal/domain"
	"exam-platform/internal/service"
)

// The login and the status tell the student application how many times it may still lock the exam to change network, and
// a lock it asked for shows as such.
func TestLoginAndStatusReportTheNetworkLocksLeft(t *testing.T) {
	f, _ := fullFixture(t)
	policy := domain.DefaultPolicy()
	storeSettings(f.store, f.clock, policy)
	f.publish(t, apiExamID)
	sessions, err := service.NewSessionsWithLimits(f.store, f.clock, service.DefaultAnswerLimits())
	if err != nil {
		t.Fatal(err)
	}
	router := New(f.clock, sessions, f.logger)
	login := request(router, "POST", "/login", `{"account_number":"0012","exam_pin":"exam-secret"}`, "")
	if login.Code != http.StatusOK {
		t.Fatalf("login=%d", login.Code)
	}
	if !strings.Contains(login.Body.String(), `"network_locks_left":2`) {
		t.Fatalf("the login does not say how many network locks are left: %s", login.Body)
	}
	reply := decoded[loginResponse](t, login.Body.Bytes())
	auth := "Bearer " + reply.SessionToken
	if w := request(router, "POST", "/heartbeat", `{"hostname":"student-pc"}`, auth); w.Code != http.StatusOK {
		t.Fatalf("heartbeat=%d %s", w.Code, w.Body)
	}
	// The application locks the exam at the student's request and tells the server.
	if w := request(router, "POST", "/event", `{"event_type":"away_lock","detail":{"reason":"network"}}`, auth); w.Code != http.StatusOK {
		t.Fatalf("event=%d %s", w.Code, w.Body)
	}
	status := request(router, "GET", "/session-status", "", auth)
	if status.Code != http.StatusOK {
		t.Fatalf("status=%d", status.Code)
	}
	if !strings.Contains(status.Body.String(), `"network_locks_left":1`) {
		t.Fatalf("the status does not say how many network locks are left: %s", status.Body)
	}
	body := decoded[statusResponse](t, status.Body.Bytes())
	if body.Locked == nil || body.Locked.Reason != service.LockReasonNetwork || body.NetworkLocksLeft != domain.NetworkLockLimit-1 {
		t.Fatalf("status = %+v", body)
	}
	// It names the record of the lock too, which the application sends the screenshots of the lock under.
	if body.Locked.EventID <= 0 || !strings.Contains(status.Body.String(), `"event_id":`) {
		t.Fatalf("the status does not name the record of the lock: %s", status.Body)
	}
}

// The rules a student reads before login say how many times the exam may be locked to change network.
func TestRulesSayHowManyNetworkLocksAreAllowed(t *testing.T) {
	f, _ := fullFixture(t)
	router := New(f.clock, f.sessions, f.logger, Options{Rules: f.sessions})
	w := request(router, "GET", "/rules", "", "")
	if w.Code != http.StatusOK {
		t.Fatalf("rules=%d", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"network_lock_limit":2`) {
		t.Fatalf("the rules do not say the network lock limit: %s", w.Body)
	}
}
