package httpapi

import (
	"net/http"
	"strings"
	"testing"

	"exam-platform/internal/domain"
	"exam-platform/internal/service"
)

// The login names the programs the student application looks for, and the roster carries what the application says it found.
func TestLoginNamesTheRemoteToolsAndTheRosterCarriesWhatWasFound(t *testing.T) {
	f, _ := fullFixture(t)
	storeSettings(f.store, f.clock, domain.DefaultPolicy())
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
	if !strings.Contains(login.Body.String(), `"remote_tools":[`) || !strings.Contains(login.Body.String(), `"anydesk*"`) || !strings.Contains(login.Body.String(), `"mstsc"`) {
		t.Fatalf("the login does not name the programs to look for: %s", login.Body)
	}
	reply := decoded[loginResponse](t, login.Body.Bytes())
	auth := "Bearer " + reply.SessionToken
	if w := request(router, "POST", "/heartbeat", `{"hostname":"student-pc"}`, auth); w.Code != http.StatusOK {
		t.Fatalf("heartbeat=%d %s", w.Code, w.Body)
	}
	event := `{"event_type":"remote_changed","detail":{"source":"remote_watch","active":[{"kind":"remote_tool","name":"AnyDesk.exe"}]}}`
	if w := request(router, "POST", "/event", event, auth); w.Code != http.StatusOK {
		t.Fatalf("event=%d %s", w.Code, w.Body)
	}
	presence, err := sessions.Presence(t.Context(), reply.SessionID)
	if err != nil || len(presence.Remote) != 1 || presence.Remote[0].Name != "AnyDesk.exe" {
		t.Fatalf("presence = %+v, %v", presence.Remote, err)
	}
}

// What the application finds is recorded after the exam has ended, as the service allows: a program that starts or stops just
// after the student submits is still worth knowing, and the refusal of a finished session would stop the application's reporting.
func TestARemoteReportIsAcceptedAfterTheExamEnded(t *testing.T) {
	f, _ := proctorFixture(t)
	login := f.login(t, "0012")
	auth := "Bearer " + login.SessionToken
	if w := request(f.router, "POST", "/submit", `{"answers":{"q1":"final"}}`, auth); w.Code != http.StatusOK {
		t.Fatalf("submit=%d %s", w.Code, w.Body)
	}
	event := `{"event_type":"remote_changed","detail":{"source":"remote_watch","active":[{"kind":"remote_tool","name":"AnyDesk.exe"}]}}`
	if w := request(f.router, "POST", "/event", event, auth); w.Code != http.StatusOK {
		t.Fatalf("a remote report after the end = %d %s", w.Code, w.Body)
	}
	if w := request(f.router, "POST", "/event", `{"event_type":"focus_lost"}`, auth); w.Code != http.StatusForbidden {
		t.Fatalf("an ordinary event after the end = %d, want 403", w.Code)
	}
}
