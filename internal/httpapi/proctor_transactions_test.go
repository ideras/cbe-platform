package httpapi

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"exam-platform/internal/domain"
)

func TestForceSubmitRechecksTimeAfterReadingBody(t *testing.T) {
	f, _ := proctorFixture(t)
	login := f.login(t, "0012")
	if w := request(f.router, "POST", "/autosave", `{"answers":{"q1":"saved"}}`, "Bearer "+login.SessionToken); w.Code != 200 {
		t.Fatal("save failed")
	}
	f.clock.Set(login.DeadlineAt.Add(29 * time.Second))
	body := &clockAdvancingReader{Reader: strings.NewReader(`{"reason":"late instructor"}`), advance: func() { f.clock.Set(login.DeadlineAt.Add(30 * time.Second)) }}
	r := httptest.NewRequest("POST", "/admin/force-submit/"+login.SessionID, body)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+testAdminToken)
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, r)
	final := decoded[submitResponse](t, w.Body.Bytes())
	if w.Code != 200 || final.Status != domain.Submitted || final.Sequence != 1 {
		t.Fatal("force request used stale timing")
	}
	page, err := f.sessions.ProctorTimeline(t.Context(), login.SessionID, 100, "")
	if err != nil {
		t.Fatal(err)
	}
	if page.Session.Final.Reason != "grace_expired" {
		t.Fatal("late force relabelled expiry")
	}
}

func TestAdminHTTPDoesNotAcknowledgeFailedCommit(t *testing.T) {
	for _, operation := range []string{"list", "timeline", "force"} {
		t.Run(operation, func(t *testing.T) {
			f, id := proctorFixture(t)
			login := f.login(t, "0012")
			failing := testSessions(&preCommitFailure{repository: f.store, calls: 1}, f.clock)
			router := New(f.clock, f.sessions, f.logger, Options{AdminToken: testAdminToken, Proctor: failing})
			method, path, body := "GET", "/admin/sessions?exam_id="+id, ""
			if operation == "timeline" {
				path = "/admin/session/" + login.SessionID + "/timeline"
			}
			if operation == "force" {
				method = "POST"
				path = "/admin/force-submit/" + login.SessionID
				body = `{"reason":"test"}`
			}
			w := request(router, method, path, body, "Bearer "+testAdminToken)
			if w.Code != 500 || strings.Contains(w.Body.String(), "final_version_id") || strings.Contains(w.Body.String(), "next_cursor") {
				t.Fatal("success payload before commit")
			}
			principal, err := f.sessions.Authenticate(t.Context(), login.SessionToken)
			if err != nil || principal.Session.Status != domain.Active || principal.Session.FinalVersionID != nil {
				t.Fatal("failed action persisted final state")
			}
		})
	}
}
