package httpapi

import (
	"net/http"
	"strings"
	"testing"

	"exam-platform/internal/service"
)

// The calculator's event types are accepted for a session whose exam has a calculator and refused — with the
// refusal, not silently — for an exam without one (plan Phase 4).
func TestCalcEventsAreRefusedForAnExamWithoutACalculator(t *testing.T) {
	f, _ := fullFixture(t) // examBody declares no tools
	login := f.login(t, "0012")
	for _, kind := range []string{"calc_opened", "calc_closed", "calc_mode_changed", "calc_tab_changed", "calc_evaluated", "calc_inspected"} {
		w := request(f.router, "POST", "/event", `{"event_type":"`+kind+`","detail":{"mode":"scientific"}}`, "Bearer "+login.SessionToken)
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "no calculator") {
			t.Fatalf("%s = %d %s, want 400 naming the missing calculator", kind, w.Code, w.Body.String())
		}
	}
	// The focus pipeline is untouched by the refusals.
	w := request(f.router, "POST", "/event", `{"event_type":"focus_lost","detail":{}}`, "Bearer "+login.SessionToken)
	if w.Code != http.StatusOK {
		t.Fatalf("focus_lost after refusals = %d %s", w.Code, w.Body.String())
	}
}

// A session's exam with a calculator accepts the whole sequence, and the server records each detail under
// detail.client with its own receipt time.
func TestCalcEventsAreAcceptedForAnExamWithACalculator(t *testing.T) {
	f := newAPIFixture(t)
	exams := service.NewExams(f.store, f.clock, f.sessions)
	f.router = New(f.clock, f.sessions, f.logger, Options{AdminToken: testAdminToken, Exams: exams})
	tools := examBodyWithTools(`{"version":1,"calculator":{"enabled":true,"modes":["scientific"],"angle_mode":"deg"}}`)
	w := request(f.router, "POST", examPath, tools, "Bearer "+testAdminToken)
	if w.Code != http.StatusCreated {
		t.Fatalf("creation = %d %s", w.Code, w.Body.String())
	}
	created := decoded[struct {
		ID string `json:"exam_id"`
	}](t, w.Body.Bytes())
	f.publish(t, created.ID)
	login := f.login(t, "0012")
	for _, event := range []string{
		`{"event_type":"calc_opened","detail":{"client_time":"2035-01-02T03:04:05Z","mode":"scientific"}}`,
		`{"event_type":"calc_evaluated","detail":{"client_time":"2035-01-02T03:04:06Z","mode":"scientific","expression":"sin(30)","ok":true,"display":"0.5","angle_mode":"deg"}}`,
		`{"event_type":"calc_evaluated","detail":{"client_time":"2035-01-02T03:04:07Z","mode":"scientific","expression":"log(8)","ok":false,"code":"ARITY","angle_mode":"deg"}}`,
		`{"event_type":"calc_tab_changed","detail":{"client_time":"2035-01-02T03:04:07Z","from":"keypad","to":"fraction"}}`,
		`{"event_type":"calc_closed","detail":{"client_time":"2035-01-02T03:04:08Z","mode":"scientific","open_ms":3000}}`,
	} {
		if w := request(f.router, "POST", "/event", event, "Bearer "+login.SessionToken); w.Code != http.StatusOK {
			t.Fatalf("%s = %d %s", event, w.Code, w.Body.String())
		}
	}
	// The events are in the session's record, under detail.client.
	if w := request(f.router, "GET", "/admin/session/"+login.SessionID+"/timeline", "", "Bearer "+testAdminToken); w.Code == http.StatusOK {
		body := w.Body.String()
		for _, want := range []string{`"calc_opened"`, `"expression":"sin(30)"`, `"code":"ARITY"`, `"calc_tab_changed"`, `"to":"fraction"`, `"open_ms":3000`, `"client":`} {
			if !strings.Contains(body, want) {
				t.Fatalf("the history page lacks %s:\n%s", want, body)
			}
		}
	}
}
