package httpapi

import (
	"exam-platform/internal/domain"
	"exam-platform/internal/service"
	"net/http"
	"testing"
	"time"
)

func TestLoginAndAuthenticatedStatusAdvertiseConfiguredMonitoringPolicy(t *testing.T) {
	f, _ := fullFixture(t)
	policy := domain.Policy{HeartbeatInterval: 2 * time.Second, OverdueThreshold: 5 * time.Second, FocusThreshold: 3 * time.Second}
	policy.AlertInterval, policy.CaptureInterval = 2*time.Second, 10*time.Second
	policy.IncidentCaptures, policy.SessionCaptures = 30, 200
	policy.LockAwayAfter, policy.LockNoContactAfter = 0, 0
	// The exam is published again with these settings, so the sessions that follow follow them too.
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
	reply := decoded[loginResponse](t, login.Body.Bytes())
	if reply.Policy.HeartbeatMillis != 2000 || reply.Policy.OverdueMillis != 5000 || reply.Policy.FocusMillis != 3000 {
		t.Fatalf("configured login policy missing: %+v", reply.Policy)
	}
	status := request(router, "GET", "/session-status", "", "Bearer "+reply.SessionToken)
	if status.Code != 200 {
		t.Fatal("status unavailable")
	}
	body := decoded[statusResponse](t, status.Body.Bytes())
	if body.Policy != reply.Policy {
		t.Fatal("login/status policy mismatch")
	}
	if w := request(router, "GET", "/session-status", "", ""); w.Code != 401 {
		t.Fatal("status bypassed authentication")
	}
}
