package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"exam-platform/internal/service"
)

func TestAdminTimingRoutesValidateAndRequireAuth(t *testing.T) {
	f := newAPIFixture(t)
	router := New(f.clock, f.sessions, f.logger, Options{AdminToken: testAdminToken, Proctor: f.sessions, Publisher: service.NewPublisher(f.store, f.clock)})
	login := f.login(t, "0012")
	paths := []struct{ method, path, valid string }{
		{"POST", "/admin/session/" + login.SessionID + "/extend-grace", `{"minutes":5,"reason":"interruption"}`},
		{"POST", "/admin/exam/" + apiExamID + "/extend-grace", `{"minutes":5,"reason":"class interruption"}`},
		{"PUT", "/admin/exam/" + apiExamID + "/end-time", `{"ends_at":null}`},
	}
	for _, route := range paths {
		if w := request(router, route.method, route.path, route.valid, ""); w.Code != http.StatusUnauthorized {
			t.Fatalf("public route: %d", w.Code)
		}
		if w := request(router, route.method, route.path, `{"unknown":true}`, "Bearer "+testAdminToken); w.Code != http.StatusBadRequest {
			t.Fatalf("unknown body: %d", w.Code)
		}
		if w := request(router, route.method, route.path, route.valid, "Bearer "+testAdminToken); w.Code != http.StatusOK {
			t.Fatalf("valid request: %d %s", w.Code, w.Body.String())
		}
	}
	for _, body := range []string{`{}`, `{"ends_at":"wrong"}`, `{"ends_at":42}`, `{"ends_at":"` + f.clock.Now().Add(30*time.Second).Format(time.RFC3339Nano) + `"}`, `{"ends_at":"` + f.clock.Now().Add(25*time.Hour).Format(time.RFC3339Nano) + `"}`} {
		if w := request(router, "PUT", paths[2].path, body, "Bearer "+testAdminToken); w.Code != http.StatusBadRequest {
			t.Fatalf("accepted %s: %d", body, w.Code)
		}
	}
	for _, body := range []string{`{"minutes":0,"reason":"x"}`, `{"minutes":61,"reason":"x"}`, `{"minutes":5,"reason":""}`, `{"minutes":5,"reason":"` + strings.Repeat("x", 1025) + `"}`} {
		if w := request(router, "POST", paths[0].path, body, "Bearer "+testAdminToken); w.Code != http.StatusBadRequest {
			t.Fatalf("accepted bad extension: %d", w.Code)
		}
	}
	end := f.clock.Now().Add(65 * time.Second)
	w := request(router, "PUT", paths[2].path, `{"ends_at":"`+end.Format(time.RFC3339Nano)+`"}`, "Bearer "+testAdminToken)
	if w.Code != http.StatusOK {
		t.Fatal(w.Body.String())
	}
	w = request(router, "GET", "/session-status", "", "Bearer "+login.SessionToken)
	var status statusResponse
	if err := json.Unmarshal(w.Body.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if status.EndsAt == nil || !status.GraceUntil.Equal(end) || !status.DeadlineAt.Equal(login.DeadlineAt) || !status.OwnGraceUntil.After(end) {
		t.Fatalf("%+v", status)
	}
	f.clock.Set(end)
	w = request(router, "GET", "/session-status", "", "Bearer "+login.SessionToken)
	if err := json.Unmarshal(w.Body.Bytes(), &status); err != nil || status.Reason != "end_time_reached" {
		t.Fatalf("%+v %v", status, err)
	}
	w = request(router, "POST", "/login", `{"account_number":"0034","exam_pin":"exam-secret"}`, "")
	if w.Code != http.StatusGone || !strings.Contains(w.Body.String(), "This exam has ended") {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
}
