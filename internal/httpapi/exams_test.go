package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"exam-platform/internal/service"
)

var testAdminToken = strings.Repeat("p", 64)

// examPath imports an exam into the test class.
const examPath = "/admin/class/" + apiClassID + "/exam"

const examBody = `{"title":"Unicode exam","pin":"exam-secret","duration_seconds":60,"content":{"exercises":[{"id":"1","title":"¿Qué imprime 世界? 🧪",` +
	`"body":"<div class=\"slot\" data-slot=\"q1\"></div><p>Explain: <span class=\"slot\" data-slot=\"q2\"></span></p>","slots":[{"id":"q1","kind":"code","language":"go","starter":"package main\n"},{"id":"q2","kind":"blank"}]}]}}`

// oneSlotContent is the smallest valid exam content: one exercise with one blank, q1.
const oneSlotContent = `"content":{"exercises":[{"id":"1","title":"T","body":"<span data-slot=\"q1\"></span>","slots":[{"id":"q1","kind":"blank"}]}]}`

func fullFixture(t *testing.T) (*apiFixture, string) {
	t.Helper()
	f := newAPIFixture(t)
	exams := service.NewExams(f.store, f.clock, f.sessions)
	options := Options{AdminToken: testAdminToken, Exams: exams}
	f.router = New(f.clock, f.sessions, f.logger, options)
	w := request(f.router, "POST", examPath, examBody, "Bearer "+testAdminToken)
	if w.Code != http.StatusCreated {
		t.Fatalf("creation = %d %s", w.Code, w.Body.String())
	}
	var created struct {
		ID string `json:"exam_id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	f.publish(t, created.ID)
	f.router = New(f.clock, f.sessions, f.logger, options)
	return f, created.ID
}

func decoded[T any](t *testing.T, body []byte) T {
	t.Helper()
	var result T
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

type wireResult struct {
	Code   int
	Body   []byte
	Header http.Header
}

func realRequest(t *testing.T, server *httptest.Server, method, path, body, token string) wireResult {
	t.Helper()
	r, err := http.NewRequest(method, server.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Content-Type", "application/json")
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := server.Client().Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return wireResult{response.StatusCode, data, response.Header}
}

func TestRealHTTPCreateSaveSubmitLoop(t *testing.T) {
	f := newAPIFixture(t)
	exams := service.NewExams(f.store, f.clock, f.sessions)
	options := Options{AdminToken: testAdminToken, Exams: exams}
	f.router = New(f.clock, f.sessions, f.logger, options)
	server := httptest.NewServer(f.router)
	defer server.Close()
	creation := realRequest(t, server, "POST", examPath, strings.Replace(examBody, "exam-secret", "new-pin", 1), testAdminToken)
	if creation.Code != http.StatusCreated {
		t.Fatalf("create = %d", creation.Code)
	}
	created := decoded[struct {
		ID string `json:"exam_id"`
	}](t, creation.Body)
	if created.ID == "" || strings.Contains(string(creation.Body), "new-pin") {
		t.Fatal("bad creation response")
	}
	if result := realRequest(t, server, "POST", "/login", `{"account_number":"0012","exam_pin":"new-pin"}`, ""); result.Code != http.StatusUnauthorized {
		t.Fatal("exam creation silently changed admission selection")
	}
	f.publish(t, created.ID)
	loginResult := realRequest(t, server, "POST", "/login", `{"account_number":"0012","exam_pin":"new-pin"}`, "")
	if loginResult.Code != http.StatusOK {
		t.Fatalf("login = %d", loginResult.Code)
	}
	login := decoded[loginResponse](t, loginResult.Body)
	get := realRequest(t, server, "GET", "/exam", "", login.SessionToken)
	view := decoded[examResponse](t, get.Body)
	if get.Code != http.StatusOK || view.ID != created.ID || view.Latest != nil || len(view.Answers) != 2 || view.Content.Exercises[0].Title != "¿Qué imprime 世界? 🧪" || view.Answers["q1"] != "package main\n" || view.Navigation != "free" || strings.Contains(string(get.Body), "new-pin") || strings.Contains(string(get.Body), `"pin"`) {
		t.Fatal("initial exam response incorrect or secret-bearing")
	}
	for i, text := range []string{"first 世界", "second 🧪"} {
		body, err := json.Marshal(map[string]any{"answers": map[string]string{"q1": text}})
		if err != nil {
			t.Fatal(err)
		}
		result := realRequest(t, server, "POST", "/autosave", string(body), login.SessionToken)
		save := decoded[savedResponse](t, result.Body)
		if result.Code != http.StatusOK || save.Sequence != int64(i+1) || !save.SavedAt.Equal(f.clock.Now()) {
			t.Fatal("save feedback not committed")
		}
	}
	resumeResult := realRequest(t, server, "POST", "/login", `{"account_number":"0012","exam_pin":"new-pin"}`, "")
	resume := decoded[loginResponse](t, resumeResult.Body)
	if resume.SessionID != login.SessionID || !resume.DeadlineAt.Equal(login.DeadlineAt) {
		t.Fatal("resume reset session")
	}
	view = decoded[examResponse](t, realRequest(t, server, "GET", "/exam", "", resume.SessionToken).Body)
	if view.Latest.Sequence != 2 || view.Answers["q1"] != "second 🧪" || view.Answers["q2"] != "" {
		t.Fatal("resume lost snapshot")
	}
	f.clock.Set(login.DeadlineAt)
	grace := realRequest(t, server, "POST", "/autosave", `{"answers":{"q2":"grace"}}`, login.SessionToken)
	if grace.Code != http.StatusOK {
		t.Fatal("grace save rejected")
	}
	status := decoded[statusResponse](t, realRequest(t, server, "GET", "/session-status", "", login.SessionToken).Body)
	if status.Status != "grace" || status.SecondsRemaining != 0 {
		t.Fatal("autosave ended grace")
	}
	submitted := realRequest(t, server, "POST", "/submit", `{"answers":{"q1":"final","q2":"done"}}`, login.SessionToken)
	final := decoded[submitResponse](t, submitted.Body)
	if submitted.Code != http.StatusOK || final.Status != "submitted" || final.Sequence != 4 || final.FinalVersionID <= 0 {
		t.Fatal("submit did not choose final version")
	}
	for range 3 {
		repeated := realRequest(t, server, "POST", "/submit", `{"answers":{"q1":"replace"}}`, login.SessionToken)
		if repeated.Code != http.StatusOK || decoded[submitResponse](t, repeated.Body) != final {
			t.Fatal("duplicate submit replaced final state")
		}
	}
	if realRequest(t, server, "POST", "/autosave", `{"answers":{}}`, login.SessionToken).Code != http.StatusForbidden {
		t.Fatal("late save permitted")
	}
	if realRequest(t, server, "GET", "/exam", "", login.SessionToken).Code != http.StatusForbidden {
		t.Fatal("terminal exam permitted")
	}
	status = decoded[statusResponse](t, realRequest(t, server, "GET", "/session-status", "", login.SessionToken).Body)
	if status.Status != "submitted" || status.FinalizationPending {
		t.Fatal("final status still pending")
	}
	if strings.Contains(f.logs.String(), "new-pin") || strings.Contains(f.logs.String(), login.SessionToken) {
		t.Fatal("secret logged")
	}
}

func TestHTTPExpiryIsDurableWithoutWorkerTick(t *testing.T) {
	for _, save := range []bool{false, true} {
		f, _ := fullFixture(t)
		login := f.login(t, "0012")
		if save && request(f.router, "POST", "/autosave", `{"answers":{"q1":"saved"}}`, "Bearer "+login.SessionToken).Code != http.StatusOK {
			t.Fatal("save failed")
		}
		f.clock.Set(login.DeadlineAt.Add(30 * time.Second))
		w := request(f.router, "POST", "/submit", `{"answers":{"q1":"too late"}}`, "Bearer "+login.SessionToken)
		final := decoded[submitResponse](t, w.Body.Bytes())
		if w.Code != http.StatusOK || final.Status != "submitted" || final.Sequence != 1 {
			t.Fatal("expiry append/replacement")
		}
		var answers map[string]string
		if err := f.store.WithSessionTransaction(t.Context(), func(tx service.SessionTransaction) error {
			v, err := tx.Version(t.Context(), login.SessionID, final.FinalVersionID)
			answers = v.Answers
			return err
		}); err != nil {
			t.Fatal(err)
		}
		if save && answers["q1"] != "saved" || !save && len(answers) != 0 {
			t.Fatal("HTTP expiry did not select latest/empty")
		}
		status := request(f.router, "GET", "/session-status", "", "Bearer "+login.SessionToken)
		if strings.Contains(status.Body.String(), "finalization_pending") {
			t.Fatal("pending finalization leaked after commit")
		}
	}
}

func TestHTTPAnswerValidationAndIdentityIsolation(t *testing.T) {
	f, _ := fullFixture(t)
	first := f.login(t, "0012")
	other := f.login(t, "0034")
	for _, student := range []loginResponse{first, other} {
		body, err := json.Marshal(map[string]any{"answers": map[string]string{"q1": student.SessionID}})
		if err != nil {
			t.Fatal(err)
		}
		if request(f.router, "POST", "/autosave", string(body), "Bearer "+student.SessionToken).Code != http.StatusOK {
			t.Fatal("save failed")
		}
	}
	for _, body := range []string{
		"null", "[]", "{", `{}`, `{"answers":null}`, `{"answers":{"q1":null}}`, `{"answers":{"q1":12}}`, `{"answers":{"q1":true}}`,
		`{"answers":{"q1":[]}}`, `{"answers":{"q1":{}}}`, `{"answers":{"other":"x"}}`, `{"answers":{}} {}`,
		`{"answers":{},"session_id":"another"}`, `{"answers":{},"account_number":"0034"}`,
	} {
		for _, path := range []string{"/autosave", "/submit"} {
			if w := request(f.router, "POST", path, body, "Bearer "+first.SessionToken); w.Code != http.StatusBadRequest {
				t.Fatalf("%s validation = %d", path, w.Code)
			}
		}
	}
	view := decoded[examResponse](t, request(f.router, "GET", "/exam?session_id="+other.SessionID, "", "Bearer "+first.SessionToken).Body.Bytes())
	if view.Answers["q1"] != first.SessionID || view.Latest.Sequence != 1 {
		t.Fatal("invalid input appended or cross-session retrieval succeeded")
	}
	if request(f.router, "POST", "/autosave", `{"answers":{}}`, "Bearer "+testAdminToken).Code != http.StatusUnauthorized {
		t.Fatal("admin bearer became student authority")
	}
}

func TestHTTPConfiguredBodyAndAnswerLimits(t *testing.T) {
	f, _ := fullFixture(t)
	var err error
	f.sessions, err = service.NewSessionsWithLimits(f.store, f.clock, service.AnswerLimits{PerAnswerBytes: 4, TotalBytes: 6})
	if err != nil {
		t.Fatal(err)
	}
	f.router = New(f.clock, f.sessions, f.logger, Options{MaxJSONBytes: 128})
	login := f.login(t, "0012")
	for _, body := range []string{`{"answers":{"q1":"12345"}}`, `{"answers":{"q1":"1234","q2":"123"}}`, `{"answers":{"q1":"` + strings.Repeat("x", 200) + `"}}`} {
		for _, path := range []string{"/autosave", "/submit"} {
			if w := request(f.router, "POST", path, body, "Bearer "+login.SessionToken); w.Code != http.StatusRequestEntityTooLarge {
				t.Fatalf("size limit = %d", w.Code)
			}
		}
	}
	view := decoded[examResponse](t, request(f.router, "GET", "/exam", "", "Bearer "+login.SessionToken).Body.Bytes())
	if view.Latest != nil {
		t.Fatal("oversized request partially saved")
	}
}

func TestAdminCreationAuthorizationAndMalformedInput(t *testing.T) {
	f, _ := fullFixture(t)
	login := f.login(t, "0012")
	for _, token := range []string{"", "invalid", login.SessionToken} {
		if w := request(f.router, "POST", examPath, examBody, "Bearer "+token); w.Code != http.StatusUnauthorized {
			t.Fatal("unprotected exam creation")
		}
	}
	r := httptest.NewRequest("POST", examPath, strings.NewReader(examBody))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Add("Authorization", "Bearer "+testAdminToken)
	r.Header.Add("Authorization", "Bearer "+login.SessionToken)
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatal("multiple admin headers accepted")
	}
	for _, body := range []string{"null", `{"duration_seconds":1}`, strings.Replace(examBody, `"duration_seconds":60`, `"duration_seconds":0`, 1), strings.Replace(examBody, `"duration_seconds":60`, `"duration_seconds":60,"grace_seconds":0`, 1), strings.Replace(examBody, `"id":"q2"`, `"id":"q1"`, 1), strings.Replace(examBody, `"content":`, `"exam_id":"client-chosen","content":`, 1), strings.Replace(examBody, `"duration_seconds":60`, `"duration_seconds":60,"navigation":"back"`, 1)} {
		if w := request(f.router, "POST", examPath, body, "Bearer "+testAdminToken); w.Code != http.StatusBadRequest {
			t.Fatalf("invalid exam = %d", w.Code)
		}
	}
}

func TestHTTPExamVolumeOptionReachesTheStudentAtLogin(t *testing.T) {
	for _, tc := range []struct {
		name, option string
		want         int
	}{{"omitted gets the default", "", service.DefaultMinVolumePercent}, {"explicit", `,"min_volume_percent":35`, 35}, {"zero leaves the student's volume alone", `,"min_volume_percent":0`, 0}, {"maximum", `,"min_volume_percent":100`, 100}} {
		t.Run(tc.name, func(t *testing.T) {
			f := newAPIFixture(t)
			exams := service.NewExams(f.store, f.clock, f.sessions)
			options := Options{AdminToken: testAdminToken, Exams: exams}
			router := New(f.clock, f.sessions, f.logger, options)
			body := examBody[:len(examBody)-1] + tc.option + "}"
			w := request(router, "POST", examPath, body, "Bearer "+testAdminToken)
			if w.Code != http.StatusCreated {
				t.Fatalf("creation = %d %s", w.Code, w.Body.String())
			}
			f.publish(t, decoded[struct {
				ID string `json:"exam_id"`
			}](t, w.Body.Bytes()).ID)
			if got := f.login(t, "0012").MinVolumePercent; got != tc.want {
				t.Fatalf("login advertised volume %d, want %d", got, tc.want)
			}
		})
	}
}

func TestHTTPExamVolumeOptionIsValidated(t *testing.T) {
	f, _ := fullFixture(t)
	for _, option := range []string{`-1`, `101`, `"loud"`, `50.5`, `null`, `true`} {
		body := examBody[:len(examBody)-1] + `,"min_volume_percent":` + option + "}"
		w := request(f.router, "POST", examPath, body, "Bearer "+testAdminToken)
		if option == "null" {
			if w.Code != http.StatusCreated { // null means "not given", like other optional fields
				t.Errorf("null volume = %d", w.Code)
			}
			continue
		}
		if w.Code != http.StatusBadRequest {
			t.Errorf("volume %s = %d, want 400", option, w.Code)
		}
	}
}

// The sample exam compiled by shared/examdoc is what the proctor sends; the server must take it as it is, plus the PIN.
func TestCompiledSampleExamAndForwardOnlyNavigation(t *testing.T) {
	f := newAPIFixture(t)
	exams := service.NewExams(f.store, f.clock, f.sessions)
	f.router = New(f.clock, f.sessions, f.logger, Options{AdminToken: testAdminToken, Exams: exams, MaxJSONBytes: 1 << 20})
	compiled, err := os.ReadFile("../../testdata/exam.json")
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(compiled, &document); err != nil {
		t.Fatal(err)
	}
	document["pin"] = "sample-pin"
	body, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	w := request(f.router, "POST", examPath, string(body), "Bearer "+testAdminToken)
	if w.Code != http.StatusCreated {
		t.Fatalf("creation = %d %s", w.Code, w.Body.String())
	}
	f.publish(t, decoded[struct {
		ID string `json:"exam_id"`
	}](t, w.Body.Bytes()).ID)
	server := httptest.NewServer(f.router)
	defer server.Close()
	login := decoded[loginResponse](t, realRequest(t, server, "POST", "/login", `{"account_number":"0012","exam_pin":"sample-pin"}`, "").Body)

	view := decoded[examResponse](t, realRequest(t, server, "GET", "/exam", "", login.SessionToken).Body)
	if view.Navigation != "forward_only" || view.CurrentExercise != 0 || len(view.Content.Exercises) != 5 || len(view.Answers) != 17 || view.Answers["3.code"] != "sum_array:\n" {
		t.Fatalf("view = %+v", view)
	}
	if item := view.Content.Exercises[0].Items[0]; item.ID != "1a" || len(item.Slots) != 6 || !strings.Contains(item.Body, `data-slot="1a.alu"`) {
		t.Fatalf("item 1a = %+v", item)
	}

	advance := realRequest(t, server, "POST", "/advance", `{"from_exercise":0,"answers":{"1a.alu":"18","2a.code":"too early"}}`, login.SessionToken)
	moved := decoded[advanceResponse](t, advance.Body)
	if advance.Code != http.StatusOK || moved.CurrentExercise != 1 || moved.Sequence != 1 || moved.SavedAt.IsZero() {
		t.Fatalf("advance = %d %s", advance.Code, advance.Body)
	}
	if status := decoded[statusResponse](t, realRequest(t, server, "GET", "/session-status", "", login.SessionToken).Body); status.CurrentExercise != 1 {
		t.Fatalf("status = %+v", status)
	}
	if again := realRequest(t, server, "POST", "/advance", `{"from_exercise":0,"answers":{}}`, login.SessionToken); again.Code != http.StatusOK || decoded[advanceResponse](t, again.Body) != moved {
		t.Fatalf("repeated advance = %d %s", again.Code, again.Body)
	}
	if behind := realRequest(t, server, "POST", "/advance", `{"from_exercise":3,"answers":{}}`, login.SessionToken); behind.Code != http.StatusConflict {
		t.Fatalf("advance from another exercise = %d", behind.Code)
	}
	for _, invalid := range []string{`{"answers":{}}`, `{"from_exercise":1}`, `{"from_exercise":1,"answers":{"1a.alu":null}}`, `{"from_exercise":4,"answers":{}}`, `{"from_exercise":-1,"answers":{}}`} {
		if w := realRequest(t, server, "POST", "/advance", invalid, login.SessionToken); w.Code != http.StatusBadRequest {
			t.Fatalf("%s = %d", invalid, w.Code)
		}
	}
	view = decoded[examResponse](t, realRequest(t, server, "GET", "/exam", "", login.SessionToken).Body)
	if view.CurrentExercise != 1 || view.Answers["1a.alu"] != "18" || view.Answers["2a.code"] != "" {
		t.Fatalf("answers after advancing = %q", view.Answers)
	}
}
