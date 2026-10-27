package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"exam-platform/internal/service"
)

// The proctor's Settings: the values are read and stored, and a save that would overwrite another proctor's change is
// refused with 409.
func TestTheSettingsEndpointsStoreAndRefuseAStaleSave(t *testing.T) {
	f := newAPIFixture(t)
	router := New(f.clock, f.sessions, f.logger, Options{AdminToken: testAdminToken, Proctor: f.sessions, Rules: f.sessions})
	read := request(router, "GET", "/admin/settings", "", "Bearer "+testAdminToken)
	if read.Code != http.StatusOK {
		t.Fatalf("settings = %d", read.Code)
	}
	var view service.Settings
	if err := json.Unmarshal(read.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if view.HeartbeatInterval != 15000 || view.UpdatedAt == nil {
		t.Fatalf("settings = %+v", view)
	}
	body := `{"heartbeat_interval_ms":10000,"overdue_threshold_ms":25000,"focus_threshold_ms":3000,"alert_interval_ms":2000,` +
		`"capture_interval_ms":4000,"lock_away_after_ms":600000,"lock_no_contact_after_ms":420000,"incident_captures":30,` +
		`"session_captures":300,"default_grace_ms":45000,"expected_updated_at":"` + view.UpdatedAt.Format(time.RFC3339Nano) + `"}`
	saved := request(router, "PUT", "/admin/settings", body, "Bearer "+testAdminToken)
	if saved.Code != http.StatusOK {
		t.Fatalf("save = %d %s", saved.Code, saved.Body.String())
	}
	var stored service.Settings
	if err := json.Unmarshal(saved.Body.Bytes(), &stored); err != nil {
		t.Fatal(err)
	}
	if stored.HeartbeatInterval != 10000 || stored.CaptureInterval != 4000 {
		t.Fatalf("stored = %+v", stored)
	}
	// The same save again: the update time has moved on, so the server refuses it.
	stale := request(router, "PUT", "/admin/settings", body, "Bearer "+testAdminToken)
	if stale.Code != http.StatusConflict {
		t.Fatalf("a stale save = %d %s", stale.Code, stale.Body.String())
	}
	// The settings need the proctor's credential.
	if anonymous := request(router, "GET", "/admin/settings", "", ""); anonymous.Code != http.StatusUnauthorized {
		t.Fatalf("the settings were public: %d", anonymous.Code)
	}
}

// An invalid setting is refused with every problem at once.
func TestAnInvalidSettingIsRefusedWithEveryProblem(t *testing.T) {
	f := newAPIFixture(t)
	router := New(f.clock, f.sessions, f.logger, Options{AdminToken: testAdminToken, Proctor: f.sessions, Rules: f.sessions})
	body := `{"heartbeat_interval_ms":1000,"overdue_threshold_ms":2000,"focus_threshold_ms":5000,"alert_interval_ms":2000,` +
		`"capture_interval_ms":2000,"lock_away_after_ms":0,"lock_no_contact_after_ms":0,"incident_captures":0,` +
		`"session_captures":2000,"default_grace_ms":30000}`
	w := request(router, "PUT", "/admin/settings", body, "Bearer "+testAdminToken)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("an invalid setting = %d %s", w.Code, w.Body.String())
	}
	var problems struct {
		Error    string   `json:"error"`
		Problems []string `json:"problems"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &problems); err != nil {
		t.Fatal(err)
	}
	if len(problems.Problems) < 3 {
		t.Fatalf("problems = %v", problems.Problems)
	}
}

// The rules the student application reads are public.
func TestTheRulesArePublic(t *testing.T) {
	f := newAPIFixture(t)
	router := New(f.clock, f.sessions, f.logger, Options{AdminToken: testAdminToken, Proctor: f.sessions, Rules: f.sessions})
	w := request(router, "GET", "/rules", "", "")
	if w.Code != http.StatusOK {
		t.Fatalf("rules = %d", w.Code)
	}
	var rules service.Rules
	if err := json.Unmarshal(w.Body.Bytes(), &rules); err != nil {
		t.Fatal(err)
	}
	if rules.AlarmMillis != 20000 || rules.LockAwayMillis != 300000 {
		t.Fatalf("rules = %+v", rules)
	}
}

// A locked exam is refused with 423; the status, the heartbeat and the events still answer.
func TestALockedExamIsRefusedWith423(t *testing.T) {
	f := newAPIFixture(t)
	router := New(f.clock, f.sessions, f.logger, Options{AdminToken: testAdminToken, Proctor: f.sessions, Rules: f.sessions})
	login := request(router, "POST", "/login", `{"account_number":"0012","exam_pin":"exam-secret"}`, "")
	if login.Code != http.StatusOK {
		t.Fatalf("login = %d", login.Code)
	}
	var reply loginResponse
	if err := json.Unmarshal(login.Body.Bytes(), &reply); err != nil {
		t.Fatal(err)
	}
	token := "Bearer " + reply.SessionToken
	// The student application locks itself and tells the server.
	if w := request(router, "POST", "/event", `{"event_type":"away_lock","detail":{"reason":"away"}}`, token); w.Code != http.StatusOK {
		t.Fatalf("away_lock = %d %s", w.Code, w.Body.String())
	}
	for _, probe := range []struct{ method, path, body string }{
		{"GET", "/exam", ""},
		{"POST", "/autosave", `{"answers":{"q1":"text"}}`},
		{"POST", "/submit", `{"answers":{"q1":"text"}}`},
		{"POST", "/advance", `{"answers":{"q1":"text"},"from_exercise":0}`},
	} {
		w := request(router, probe.method, probe.path, probe.body, token)
		if w.Code != http.StatusLocked {
			t.Fatalf("%s while locked = %d %s", probe.path, w.Code, w.Body.String())
		}
	}
	// The status answers and says the exam is locked.
	status := request(router, "GET", "/session-status", "", token)
	if status.Code != http.StatusOK {
		t.Fatalf("status = %d", status.Code)
	}
	var view statusResponse
	if err := json.Unmarshal(status.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if view.Locked == nil || view.Locked.Reason != "away" {
		t.Fatalf("status = %+v", view.Locked)
	}
	// The heartbeat and the events still work.
	if w := request(router, "POST", "/heartbeat", `{"hostname":"desk"}`, token); w.Code != http.StatusOK {
		t.Fatalf("heartbeat = %d", w.Code)
	}
	if w := request(router, "POST", "/event", `{"event_type":"focus_regained"}`, token); w.Code != http.StatusOK {
		t.Fatalf("event = %d", w.Code)
	}
	// A login resumes the locked session, so the application can show the lock screen.
	resumed := request(router, "POST", "/login", `{"account_number":"0012","exam_pin":"exam-secret"}`, "")
	if resumed.Code != http.StatusOK {
		t.Fatalf("resume = %d", resumed.Code)
	}
	var again loginResponse
	if err := json.Unmarshal(resumed.Body.Bytes(), &again); err != nil {
		t.Fatal(err)
	}
	if again.Locked == nil {
		t.Fatal("the login did not report the lock")
	}
}

// The proctor unlocks a locked session, with or without the time back.
func TestTheProctorUnlocksALockedSession(t *testing.T) {
	f := newAPIFixture(t)
	router := New(f.clock, f.sessions, f.logger, Options{AdminToken: testAdminToken, Proctor: f.sessions, Rules: f.sessions})
	login := request(router, "POST", "/login", `{"account_number":"0012","exam_pin":"exam-secret"}`, "")
	var reply loginResponse
	if err := json.Unmarshal(login.Body.Bytes(), &reply); err != nil {
		t.Fatal(err)
	}
	token := "Bearer " + reply.SessionToken
	if w := request(router, "POST", "/event", `{"event_type":"away_lock"}`, token); w.Code != http.StatusOK {
		t.Fatalf("away_lock = %d", w.Code)
	}
	unlock := request(router, "POST", "/admin/session/"+reply.SessionID+"/unlock", `{"reason":"the student came back","give_back_time":false}`, "Bearer "+testAdminToken)
	if unlock.Code != http.StatusOK {
		t.Fatalf("unlock = %d %s", unlock.Code, unlock.Body.String())
	}
	var result service.UnlockResult
	if err := json.Unmarshal(unlock.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.SecondsAdded != 0 {
		t.Fatalf("the time was added without being asked: %+v", result)
	}
	// The exam answers again.
	if w := request(router, "GET", "/exam", "", token); w.Code != http.StatusOK {
		t.Fatalf("the exam is still refused: %d %s", w.Code, w.Body.String())
	}
	// A session that is not locked cannot be unlocked.
	if w := request(router, "POST", "/admin/session/"+reply.SessionID+"/unlock", `{"reason":"again"}`, "Bearer "+testAdminToken); w.Code != http.StatusConflict {
		t.Fatalf("unlocking an unlocked session = %d", w.Code)
	}
	// The unlock needs the proctor's credential.
	if w := request(router, "POST", "/admin/session/"+reply.SessionID+"/unlock", `{"reason":"again"}`, ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("the unlock was public: %d", w.Code)
	}
}
