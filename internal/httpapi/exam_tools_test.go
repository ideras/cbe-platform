package httpapi

import (
	"net/http"
	"strings"
	"testing"

	"exam-platform/internal/service"
	"exam-platform/shared/calc"
)

// examBodyWithTools builds an import of the smallest exam with, or without, a tools object. The PIN is the one
// the fixture's login helper uses.
func examBodyWithTools(tools string) string {
	body := `{"title":"Tools exam","pin":"exam-secret","duration_seconds":60,` + oneSlotContent
	if tools != "" {
		body += `,"tools":` + tools
	}
	return body + `}`
}

// toolsFixture imports and publishes the exam, and logs a student in.
func toolsFixture(t *testing.T, tools string) (*apiFixture, loginResponse) {
	t.Helper()
	f := newAPIFixture(t)
	exams := service.NewExams(f.store, f.clock, f.sessions)
	f.router = New(f.clock, f.sessions, f.logger, Options{AdminToken: testAdminToken, Exams: exams})
	w := request(f.router, "POST", examPath, examBodyWithTools(tools), "Bearer "+testAdminToken)
	if w.Code != http.StatusCreated {
		t.Fatalf("creation = %d %s", w.Code, w.Body.String())
	}
	created := decoded[struct {
		ID string `json:"exam_id"`
	}](t, w.Body.Bytes())
	f.publish(t, created.ID)
	return f, f.login(t, "0012")
}

// A valid config validates on the server, is stored, and reaches the student as the effective configuration.
func TestExamToolsRoundTripToTheStudent(t *testing.T) {
	f, login := toolsFixture(t, `{"version":1,"calculator":{"enabled":true,"modes":["scientific","neural"],"angle_mode":"rad","disabled_functions":["softmax"]}}`)
	body := request(f.router, "GET", "/exam", "", "Bearer "+login.SessionToken).Body.Bytes()
	view := decoded[examResponse](t, body)
	if view.Tools == nil || view.Tools.Calculator == nil {
		t.Fatalf("GET /exam sent no tools: %s", body)
	}
	effective := view.Tools.Effective()
	if effective.Version != 1 || !effective.Calculator.Enabled || len(effective.Calculator.Modes) != 2 || effective.Calculator.Modes[0] != "scientific" || effective.Calculator.Modes[1] != "neural" {
		t.Fatalf("the modes were not preserved: %+v", effective.Calculator)
	}
	if effective.Calculator.AngleMode != "rad" || len(effective.Calculator.DisabledFunctions) != 1 || effective.Calculator.DisabledFunctions[0] != "softmax" {
		t.Fatalf("the settings were not preserved: %+v", effective.Calculator)
	}
	// The defaults are applied, not left to the frontend: a bare calculator arrives complete.
	f, login = toolsFixture(t, `{"version":1,"calculator":{"enabled":true}}`)
	view = decoded[examResponse](t, request(f.router, "GET", "/exam", "", "Bearer "+login.SessionToken).Body.Bytes())
	effective = view.Tools.Effective()
	if len(effective.Calculator.Modes) != 1 || effective.Calculator.Modes[0] != "scientific" || effective.Calculator.AngleMode != calc.AngleUser {
		t.Fatalf("the defaults were not applied: %+v", effective.Calculator)
	}
}

// An exam file without a calculator sends no tools at all.
func TestExamWithoutToolsSendsNoTools(t *testing.T) {
	f, login := toolsFixture(t, "")
	body := request(f.router, "GET", "/exam", "", "Bearer "+login.SessionToken).Body.Bytes()
	if strings.Contains(string(body), `"tools"`) {
		t.Fatalf("GET /exam invented tools: %s", body)
	}
}

// An exam whose calculator is off, as a config may declare it, sends no tools either.
func TestExamWithTheCalculatorOffSendsNoTools(t *testing.T) {
	f, login := toolsFixture(t, `{"version":1,"calculator":{"enabled":false,"modes":["neural"]}}`)
	body := request(f.router, "GET", "/exam", "", "Bearer "+login.SessionToken).Body.Bytes()
	if strings.Contains(string(body), `"tools"`) {
		t.Fatalf("an off calculator still sent tools: %s", body)
	}
}

func TestExamToolsRefusalsNameTheProblem(t *testing.T) {
	for name, test := range map[string]struct{ tools, want string }{
		"unknown disabled name": {`{"version":1,"calculator":{"enabled":true,"disabled_functions":["solve"]}}`, `unknown name \"solve\"`},
		"unknown mode":          {`{"version":1,"calculator":{"enabled":true,"modes":["quaternion"]}}`, `unknown mode \"quaternion\"`},
		"empty mode list":       {`{"version":1,"calculator":{"enabled":true,"modes":[]}}`, "modes is empty"},
		"bad angle mode":        {`{"version":1,"calculator":{"enabled":true,"angle_mode":"gradians"}}`, "angle_mode must be deg, rad or user"},
		"wrong version":         {`{"version":2,"calculator":{"enabled":true}}`, "tools.version must be 1"},
	} {
		t.Run(name, func(t *testing.T) {
			f := newAPIFixture(t)
			exams := service.NewExams(f.store, f.clock, f.sessions)
			f.router = New(f.clock, f.sessions, f.logger, Options{AdminToken: testAdminToken, Exams: exams})
			w := request(f.router, "POST", examPath, examBodyWithTools(test.tools), "Bearer "+testAdminToken)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("create = %d %s", w.Code, w.Body.String())
			}
			if !strings.Contains(w.Body.String(), test.want) {
				t.Fatalf("the refusal does not name the problem: %s", w.Body.String())
			}
		})
	}
}

// A config that pretends logging can be turned off must fail loudly: logging is never configurable (D4).
func TestExamToolsRefuseLogExpressions(t *testing.T) {
	f := newAPIFixture(t)
	exams := service.NewExams(f.store, f.clock, f.sessions)
	f.router = New(f.clock, f.sessions, f.logger, Options{AdminToken: testAdminToken, Exams: exams})
	w := request(f.router, "POST", examPath, examBodyWithTools(`{"version":1,"calculator":{"enabled":true,"log_expressions":false}}`), "Bearer "+testAdminToken)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("log_expressions = %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "log_expressions") {
		t.Fatalf("the refusal does not name the field: %s", w.Body.String())
	}
}

// An unknown key anywhere in the tools object fails, not only in the calculator.
func TestExamToolsRefuseUnknownKeysAtEveryLevel(t *testing.T) {
	f := newAPIFixture(t)
	exams := service.NewExams(f.store, f.clock, f.sessions)
	f.router = New(f.clock, f.sessions, f.logger, Options{AdminToken: testAdminToken, Exams: exams})
	for _, tools := range []string{
		`{"version":1,"logging":true}`,
		`{"version":1,"calculator":{"enabled":true},"extra":1}`,
	} {
		if w := request(f.router, "POST", examPath, examBodyWithTools(tools), "Bearer "+testAdminToken); w.Code != http.StatusBadRequest {
			t.Fatalf("%s = %d %s", tools, w.Code, w.Body.String())
		}
	}
}

// A proctor cannot import an exam whose JSON is malformed beyond shape: the error names the field.
func TestExamToolsWrongTypesAreNamed(t *testing.T) {
	f := newAPIFixture(t)
	exams := service.NewExams(f.store, f.clock, f.sessions)
	f.router = New(f.clock, f.sessions, f.logger, Options{AdminToken: testAdminToken, Exams: exams})
	w := request(f.router, "POST", examPath, examBodyWithTools(`{"version":"one"}`), "Bearer "+testAdminToken)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "tools.version") {
		t.Fatalf("wrong type = %d %s", w.Code, w.Body.String())
	}
}
