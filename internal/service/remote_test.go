package service_test

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"exam-platform/internal/domain"
	"exam-platform/internal/service"
)

// remoteReport is what the student application sends when it has found something, or nothing any more.
func remoteReport(t *testing.T, f *fixture, token, active string) error {
	t.Helper()
	_, err := f.sessions.ClientEvent(t.Context(), token, service.EventInput{Type: "remote_changed", Detail: json.RawMessage(`{"source":"remote_watch","active":` + active + `}`)})
	return err
}

func remoteNames(findings []domain.RemoteFinding) []string {
	names := []string{}
	for _, finding := range findings {
		names = append(names, finding.Name)
	}
	return names
}

// The login names the programs the application looks for, and a caller cannot change the server's list through the copy it gets.
func TestTheLoginNamesTheProgramsToLookFor(t *testing.T) {
	f := newFixture(t, 0)
	login := f.login(t, "0012")
	for _, name := range []string{"teamviewer*", "anydesk*", "mstsc", "msra", "quickassist", "winvnc*"} {
		if !slices.Contains(login.RemoteTools, name) {
			t.Errorf("the list lacks %q: %v", name, login.RemoteTools)
		}
	}
	login.RemoteTools[0] = "changed"
	if again := f.resume(t, "0012", login.SessionID); again.RemoteTools[0] == "changed" {
		t.Fatal("the server's list was changed through a login result")
	}
}

// What the application finds is on the roster until it says it is gone; before it has said anything there is nothing.
func TestARemoteReportShowsOnTheRosterUntilTheApplicationSaysItIsGone(t *testing.T) {
	f := newFixture(t, 0)
	login := f.login(t, "0012")
	heartbeat(t, f, login.Token)
	if got := presence(t, f, login.SessionID).Remote; got == nil || len(got) != 0 {
		t.Fatalf("remote before any report = %#v, want an empty list", got)
	}
	if err := remoteReport(t, f, login.Token, `[{"kind":"remote_session","name":"Remote Desktop","detail":"LAPTOP-1"},{"kind":"remote_tool","name":"AnyDesk.exe"}]`); err != nil {
		t.Fatal(err)
	}
	got := presence(t, f, login.SessionID).Remote
	if len(got) != 2 || got[0] != (domain.RemoteFinding{Kind: "remote_session", Name: "Remote Desktop", Detail: "LAPTOP-1"}) || got[1].Name != "AnyDesk.exe" {
		t.Fatalf("remote = %+v", got)
	}
	// The latest report is the whole state: one program of two gone leaves the other.
	if err := remoteReport(t, f, login.Token, `[{"kind":"remote_tool","name":"AnyDesk.exe"}]`); err != nil {
		t.Fatal(err)
	}
	if got := remoteNames(presence(t, f, login.SessionID).Remote); !slices.Equal(got, []string{"AnyDesk.exe"}) {
		t.Fatalf("remote = %v", got)
	}
	if err := remoteReport(t, f, login.Token, `[]`); err != nil {
		t.Fatal(err)
	}
	if got := presence(t, f, login.SessionID).Remote; got == nil || len(got) != 0 {
		t.Fatalf("remote after it was gone = %#v, want an empty list", got)
	}
}

// What the application says is its own word: only known kinds with a name are kept, no more than the most, and cut short; a report
// that cannot be read says nothing was found.
func TestWhatTheApplicationSaysIsBoundedAndOfKnownKindsOnly(t *testing.T) {
	f := newFixture(t, 0)
	login := f.login(t, "0012")
	heartbeat(t, f, login.Token)
	var items []string
	items = append(items, `{"kind":"something_new","name":"Unknown"}`, `{"kind":"remote_tool","name":""}`)
	long := strings.Repeat("é", 100)
	items = append(items, `{"kind":"remote_tool","name":"`+long+`","detail":"`+long+`"}`)
	for i := 0; i < 20; i++ {
		items = append(items, `{"kind":"remote_tool","name":"tool`+strings.Repeat("x", i)+`"}`)
	}
	if err := remoteReport(t, f, login.Token, "["+strings.Join(items, ",")+"]"); err != nil {
		t.Fatal(err)
	}
	got := presence(t, f, login.SessionID).Remote
	if len(got) != 10 {
		t.Fatalf("kept %d findings, want 10", len(got))
	}
	if got[0].Name == "Unknown" || got[0].Name == "" {
		t.Fatalf("the first finding is %+v", got[0])
	}
	if len(got[0].Name) > 64 || len(got[0].Detail) > 64 || strings.ContainsRune(got[0].Name, '�') || strings.ContainsRune(got[0].Detail, '�') {
		t.Fatalf("a long name was kept whole or cut in a character: %q / %q", got[0].Name, got[0].Detail)
	}
	for _, bad := range []string{`"not a list"`, `[1,2]`, `{"x":1}`, `null`} {
		if err := remoteReport(t, f, login.Token, bad); err != nil {
			t.Fatalf("a report of %s was refused: %v", bad, err)
		}
		if got := presence(t, f, login.SessionID).Remote; len(got) != 0 {
			t.Fatalf("%s gave %+v", bad, got)
		}
	}
}

// What was found matters while the exam is locked, and once it has ended: it is recorded then too, and the roster no longer shows it
// when the exam is over.
func TestARemoteReportIsRecordedWhileLockedAndAfterTheEnd(t *testing.T) {
	f := newFixture(t, 0)
	locked := f.login(t, "0012")
	heartbeat(t, f, locked.Token)
	if _, err := f.sessions.ClientEvent(t.Context(), locked.Token, service.EventInput{Type: "away_lock", Detail: json.RawMessage(`{"reason":"no_contact"}`)}); err != nil {
		t.Fatal(err)
	}
	if err := remoteReport(t, f, locked.Token, `[{"kind":"remote_tool","name":"AnyDesk.exe"}]`); err != nil {
		t.Fatalf("refused while the exam was locked: %v", err)
	}
	if got := remoteNames(presence(t, f, locked.SessionID).Remote); !slices.Equal(got, []string{"AnyDesk.exe"}) {
		t.Fatalf("remote while locked = %v", got)
	}
	over := f.login(t, "0034")
	heartbeat(t, f, over.Token)
	if _, err := f.sessions.Submit(t.Context(), over.Token, domain.Answers{}); err != nil {
		t.Fatal(err)
	}
	if err := remoteReport(t, f, over.Token, `[{"kind":"remote_tool","name":"AnyDesk.exe"}]`); err != nil {
		t.Fatalf("refused after the end: %v", err)
	}
	if got := presence(t, f, over.SessionID).Remote; len(got) != 0 {
		t.Fatalf("a finished exam shows %+v on the roster", got)
	}
	var stored string
	if err := f.raw.QueryRow(`SELECT detail FROM event_log WHERE session_id = ? AND event_type = 'remote_changed'`, over.SessionID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stored, `"client"`) || !strings.Contains(stored, "AnyDesk.exe") {
		t.Fatalf("the report was recorded as %s", stored)
	}
}

// What the application found is its own word, and it goes with it. A session that is logged in again has an application that has
// said nothing yet, so what the one before it reported is not shown as if it were current; the new one tells what it finds.
func TestWhatAnEarlierRunOfTheApplicationReportedIsNotShownAfterANewLogin(t *testing.T) {
	f := newFixture(t, 0)
	login := f.login(t, "0012")
	heartbeat(t, f, login.Token)
	if err := remoteReport(t, f, login.Token, `[{"kind":"remote_tool","name":"AnyDesk.exe"}]`); err != nil {
		t.Fatal(err)
	}
	if got := remoteNames(presence(t, f, login.SessionID).Remote); !slices.Equal(got, []string{"AnyDesk.exe"}) {
		t.Fatalf("remote = %v", got)
	}
	again := f.resume(t, "0012", login.SessionID)
	heartbeat(t, f, again.Token)
	if got := presence(t, f, login.SessionID).Remote; len(got) != 0 {
		t.Fatalf("the roster shows %+v of the run that was closed", got)
	}
	if err := remoteReport(t, f, again.Token, `[{"kind":"remote_tool","name":"TeamViewer.exe"}]`); err != nil {
		t.Fatal(err)
	}
	if got := remoteNames(presence(t, f, login.SessionID).Remote); !slices.Equal(got, []string{"TeamViewer.exe"}) {
		t.Fatalf("remote = %v", got)
	}
}

// An event of another name is still refused.
func TestOnlyTheKnownEventNamesAreAccepted(t *testing.T) {
	f := newFixture(t, 0)
	login := f.login(t, "0012")
	if _, err := f.sessions.ClientEvent(t.Context(), login.Token, service.EventInput{Type: "remote_cleared", Detail: json.RawMessage(`{}`)}); !errors.Is(err, service.ErrInvalidInput) {
		t.Fatalf("err = %v, want ErrInvalidInput", err)
	}
}
