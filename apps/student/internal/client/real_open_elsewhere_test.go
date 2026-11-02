package client

import (
	"strings"
	"testing"
)

// Two real student applications against the real server: the second one cannot open an account the first has open, the first
// goes on working, and once a proctor releases the session the second takes it over and the first is cut off.
func TestRealServerAnAccountOpensOnOneComputerAtATime(t *testing.T) {
	server := setupRealServer(t)
	exam := server.create(t, 600, 30)
	// A long threshold: the first application is taken to be there for as long as this test runs.
	stop := server.start(t, exam, fastPolicyWith(20000))
	defer stop()

	first := realClient(t, server.base)
	if _, err := first.Login(t.Context(), server.base, "00123", "synthetic PIN"); err != nil {
		t.Fatal(err)
	}
	update(t, first, "typed on the first computer", 1)
	waitView(t, first, func(v View) bool { return v.AcknowledgedRevision == 1 })

	// The second computer is told the account is open, with how long to wait, and starts no session.
	second := realClient(t, server.base)
	_, err := second.Login(t.Context(), server.base, "00123", "synthetic PIN")
	if err == nil || !strings.Contains(err.Error(), "already open on another computer") || failureCode(err) != 409 {
		t.Fatalf("second login = %v", err)
	}
	if view := second.Snapshot(); view.Phase == "exam" {
		t.Fatal("a refused login started a session")
	}
	// The first computer is not disturbed: it goes on saving.
	update(t, first, "still typing on the first computer", 2)
	waitView(t, first, func(v View) bool { return v.AcknowledgedRevision == 2 && v.SaveError == "" })

	// A proctor releases the session: the second computer gets in and sees the work saved from the first.
	var list struct {
		Sessions []struct {
			ID        string `json:"session_id"`
			LoginGate *struct {
				Refusals int `json:"refusals"`
			} `json:"login_gate"`
		} `json:"sessions"`
	}
	server.admin(t, "GET", "/admin/sessions?exam_id="+exam, nil, &list, 200)
	if len(list.Sessions) != 1 || list.Sessions[0].LoginGate == nil || list.Sessions[0].LoginGate.Refusals != 1 {
		t.Fatalf("roster = %+v", list.Sessions)
	}
	server.admin(t, "POST", "/admin/session/"+list.Sessions[0].ID+"/release-login", map[string]string{"reason": "the first computer is being swapped"}, nil, 200)
	view, err := second.Login(t.Context(), server.base, "00123", "synthetic PIN")
	if err != nil {
		t.Fatal(err)
	}
	if view.Answers["q1"] != "still typing on the first computer" {
		t.Fatalf("the second computer did not get the saved work: %q", view.Answers["q1"])
	}

	// The first computer's token is no longer recognised: it cannot save, and says why.
	update(t, first, "typed after the session moved", 3)
	stale := waitView(t, first, func(v View) bool { return v.SaveError != "" })
	if !strings.Contains(stale.SaveError, "opened on another computer") {
		t.Fatalf("the first computer's save error = %q", stale.SaveError)
	}
}
