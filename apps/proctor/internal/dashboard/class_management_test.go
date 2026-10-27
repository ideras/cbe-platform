package dashboard

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const rosterDocument = "canvas_id,account_number,name,email,sis_user_id\n1,0001,Ada,a@x.invalid,S1\n2,0099,Linus,l@x.invalid,S3\n"

func changeAnswer(w http.ResponseWriter, r *http.Request, admitting bool) bool {
	if r.Method != "PUT" || !strings.HasPrefix(r.URL.Path, "/admin/class/") || !strings.HasSuffix(r.URL.Path, "/roster") {
		return false
	}
	applied := r.URL.Query().Get("dry_run") != "true"
	reply(w, map[string]any{"students": 2, "added": 1, "removed": 3, "warning_count": 1, "applied": applied, "admitting": admitting, "pruned": 2,
		"warnings": []map[string]any{{"row": 4, "account_number": "", "message": "empty account_number; skipped"}}})
	return true
}

func TestRenamingAClassSendsTheNameAndChecksTheAnswer(t *testing.T) {
	f := newPublishFixture(t, examID)
	f.server.reply = func(w http.ResponseWriter, r *http.Request, body []byte) bool {
		if r.Method == "PATCH" && r.URL.Path == "/admin/class/"+classID {
			reply(w, map[string]any{"class_id": classID, "name": "Mathematics 10B", "student_count": 5, "exam_count": 2, "created_at": at, "published_exam": nil})
			return true
		}
		return false
	}
	renamed, err := f.RenameClass(classID, "  Mathematics 10B ")
	if err != nil || renamed.ID != classID || renamed.Name != "Mathematics 10B" || renamed.StudentCount != 5 {
		t.Fatalf("renamed = %+v, %v", renamed, err)
	}
	sent := f.server.seen("PATCH", "/admin/class/"+classID)
	if len(sent) != 1 || string(sent[0].Body) != `{"name":"Mathematics 10B"}` || sent[0].ContentType != "application/json" || sent[0].Authorization != "Bearer "+secret {
		t.Fatalf("request = %+v", sent)
	}
	for name, bad := range map[string][2]string{"bad id": {"nope", "Fine"}, "empty": {classID, "  "}, "control": {classID, "A\x00B"}, "long": {classID, strings.Repeat("é", 121)}} {
		if _, err := f.RenameClass(bad[0], bad[1]); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	if len(f.server.seen("PATCH", "/admin/class/")) != 1 {
		t.Fatal("an invalid rename reached the server")
	}
}

func TestARenameTheServerRefusesOrAnswersWronglyIsReported(t *testing.T) {
	f := newPublishFixture(t, examID)
	f.server.reply = func(w http.ResponseWriter, r *http.Request, body []byte) bool {
		switch string(body) {
		case `{"name":"Taken"}`:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			reply(w, map[string]string{"error": "a class with this name already exists"})
		case `{"name":"Other"}`:
			reply(w, map[string]any{"class_id": classID, "name": "Something else", "student_count": 5, "exam_count": 2, "created_at": at, "published_exam": nil})
		default:
			http.Error(w, "boom "+secret, 500)
		}
		return true
	}
	_, err := f.RenameClass(classID, "Taken")
	var reject *rejection
	if !errors.As(err, &reject) || reject.Status != 409 || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("refusal = %v", err)
	}
	if _, err := f.RenameClass(classID, "Other"); err == nil || !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("a different name in the answer = %v", err)
	}
	if _, err := f.RenameClass(classID, "Third"); err == nil || !strings.Contains(err.Error(), "Look at the class list") || strings.Contains(err.Error(), secret) {
		t.Fatalf("unclear failure = %v", err)
	}
}

func TestDeletingAClassNeedsConfirmationAndIsReportedFaithfully(t *testing.T) {
	f := newPublishFixture(t, examID)
	f.server.reply = func(w http.ResponseWriter, r *http.Request, body []byte) bool {
		if r.Method != "DELETE" {
			return false
		}
		switch r.URL.Path {
		case "/admin/class/" + classID:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			reply(w, map[string]string{"error": "the class has exams; delete its exams first (exams students have taken cannot be deleted)"})
		case "/admin/class/" + otherClass:
			reply(w, map[string]any{"class_id": otherClass, "name": "History 11B", "students": 24, "pruned": 3})
		default:
			http.Error(w, "boom", 500)
		}
		return true
	}
	if _, err := f.DeleteClass(otherClass, false); err == nil || len(f.server.seen("DELETE", "/admin/class/")) != 0 {
		t.Fatalf("an unconfirmed deletion was sent: %v", err)
	}
	if _, err := f.DeleteClass("nope", true); err == nil {
		t.Fatal("an invalid id was sent")
	}
	deleted, err := f.DeleteClass(otherClass, true)
	if err != nil || deleted.Name != "History 11B" || deleted.Students != 24 || deleted.Pruned != 3 {
		t.Fatalf("deleted = %+v, %v", deleted, err)
	}
	if _, err := f.DeleteClass(classID, true); err == nil || !strings.Contains(err.Error(), "still has exams") || !strings.Contains(err.Error(), "Delete its exams first") {
		t.Fatalf("a class with exams = %v", err)
	}
	if _, err := f.DeleteClass("66666666-6666-4666-8666-666666666666", true); err == nil || !strings.Contains(err.Error(), "Look at the class list") {
		t.Fatalf("unclear failure = %v", err)
	}
	// Answering for another class is not a confirmation of this one.
	f.server.reply = func(w http.ResponseWriter, r *http.Request, body []byte) bool {
		reply(w, map[string]any{"class_id": classID, "name": "x", "students": 1, "pruned": 0})
		return r.Method == "DELETE"
	}
	if _, err := f.DeleteClass(otherClass, true); err == nil || !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("a mismatched answer = %v", err)
	}
}

func TestANewRosterIsComparedFirstAndOnlyReplacesTheClassItWasChosenFor(t *testing.T) {
	f := newPublishFixture(t, examID)
	f.server.reply = func(w http.ResponseWriter, r *http.Request, body []byte) bool { return changeAnswer(w, r, true) }
	f.chosen = writeTemp(t, "class-a.csv", append([]byte("\xef\xbb\xbf"), rosterDocument...))
	preview, err := f.ChooseRosterFile(classID)
	if err != nil || !preview.Valid || preview.File != "class-a.csv" || preview.Students != 2 || preview.Added != 1 || preview.Removed != 3 || preview.WarningCount != 1 || preview.Warnings[0].Row != 4 || !preview.Admitting || preview.Applied {
		t.Fatalf("preview = %+v, %v", preview, err)
	}
	dry := f.server.seen("PUT", "/admin/class/"+classID+"/roster?dry_run=true")
	if len(dry) != 1 || string(dry[0].Body) != rosterDocument || !strings.HasPrefix(dry[0].ContentType, "text/csv") {
		t.Fatalf("dry run = %+v", dry)
	}
	if _, err := f.ReplaceClassRoster(classID, false); err == nil || len(f.server.seen("PUT", "/admin/class/")) != 1 {
		t.Fatalf("an unconfirmed replacement was sent: %v", err)
	}
	if _, err := f.ReplaceClassRoster(otherClass, true); err == nil || len(f.server.seen("PUT", "/admin/class/")) != 1 {
		t.Fatalf("a roster previewed for one class was applied to another: %v", err)
	}
	change, err := f.ReplaceClassRoster(classID, true)
	if err != nil || !change.Applied || change.Added != 1 || change.Removed != 3 || change.Pruned != 2 {
		t.Fatalf("change = %+v, %v", change, err)
	}
	if puts := f.server.seen("PUT", "/admin/class/"+classID+"/roster"); len(puts) != 2 || string(puts[1].Body) != rosterDocument {
		t.Fatal("the replaced roster differs from the previewed file")
	}
	if _, err := f.ReplaceClassRoster(classID, true); err == nil || len(f.server.seen("PUT", "/admin/class/")) != 2 {
		t.Fatal("one preview was applied twice")
	}
}

func TestARosterFileTheServerRejectsListsItsProblemsAndCannotBeApplied(t *testing.T) {
	f := newPublishFixture(t, examID)
	f.server.reply = func(w http.ResponseWriter, r *http.Request, body []byte) bool {
		problemsReply(w, 400, "invalid request", "roster CSV missing required column \"email\"")
		return true
	}
	f.chosen = writeTemp(t, "bad.csv", []byte("account_number\n1\n"))
	preview, err := f.ChooseRosterFile(classID)
	if err != nil || preview.Valid || len(preview.Problems) != 1 {
		t.Fatalf("preview = %+v, %v", preview, err)
	}
	if _, err := f.ReplaceClassRoster(classID, true); err == nil {
		t.Fatal("a rejected roster can be applied")
	}
	f.chosen = writeTemp(t, "huge.csv", []byte(strings.Repeat(" ", maxRosterFile+1)))
	if _, err := f.ChooseRosterFile(classID); err == nil {
		t.Fatal("an oversized roster file was accepted")
	}
	if _, err := f.ChooseRosterFile("nope"); err == nil {
		t.Fatal("an invalid class id was accepted")
	}
	f.chosen = ""
	if preview, err := f.ChooseRosterFile(classID); err != nil || !preview.Cancelled {
		t.Fatalf("cancelled dialog = %+v, %v", preview, err)
	}
}

func TestAReplacementTheServerRefusesKeepsTheFileAndAnUnclearOneDropsIt(t *testing.T) {
	f := newPublishFixture(t, examID)
	mode := "refuse"
	f.server.reply = func(w http.ResponseWriter, r *http.Request, body []byte) bool {
		if r.URL.Query().Get("dry_run") == "true" {
			return changeAnswer(w, r, false)
		}
		if mode == "refuse" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			reply(w, map[string]string{"error": "the situation changed"})
			return true
		}
		http.Error(w, "boom "+secret, 500)
		return true
	}
	f.chosen = writeTemp(t, "class.csv", []byte(rosterDocument))
	if _, err := f.ChooseRosterFile(classID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.ReplaceClassRoster(classID, true); err == nil || !definite(err) {
		t.Fatalf("a refusal = %v", err)
	}
	mode = "boom"
	_, err := f.ReplaceClassRoster(classID, true) // the file was kept, so this attempt is made
	if err == nil || !strings.Contains(err.Error(), "look at its roster") || strings.Contains(err.Error(), secret) {
		t.Fatalf("unclear failure = %v", err)
	}
	if _, err := f.ReplaceClassRoster(classID, true); err == nil || len(f.server.seen("PUT", "/admin/class/"+classID+"/roster")) != 3 {
		t.Fatalf("an unclear failure was retried without a new preview: %v", err)
	}
}

func TestClassManagementNeedsAConnection(t *testing.T) {
	opts := DefaultOptions()
	opts.Choose = func(FileDialog) (string, error) {
		t.Fatal("dialog opened while disconnected")
		return "", nil
	}
	s, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.ServiceShutdown)
	if _, err := s.RenameClass(classID, "Fine"); err == nil {
		t.Error("RenameClass")
	}
	if _, err := s.DeleteClass(classID, true); err == nil {
		t.Error("DeleteClass")
	}
	if _, err := s.ChooseRosterFile(classID); err == nil {
		t.Error("ChooseRosterFile")
	}
	if _, err := s.ReplaceClassRoster(classID, true); err == nil {
		t.Error("ReplaceClassRoster")
	}
}

func TestDisconnectingEndsTheConnectionAndKeepsTheWayBack(t *testing.T) {
	f := newConfigFixture(t, nil)
	f.chosen[tokenTitle] = writeToken(t, secret, 0o600)
	if _, err := f.ChooseCredential(); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Connect(f.server.URL); err != nil {
		t.Fatal(err)
	}
	wait(t, func() bool { v := f.Snapshot(); return v.Connected && v.ClassesLoaded })

	view, err := f.Disconnect()
	if err != nil || view.Connected || view.Connecting || len(view.Classes) != 0 || view.ClassesLoaded || view.CatalogLoaded || view.ExamID != examID || view.Server != f.server.URL || !view.CredentialAvailable || view.CredentialFile != tokenLabel {
		t.Fatalf("view after disconnecting = %+v, %v", view, err)
	}
	// Nothing more is asked of the server.
	for len(f.seen) > 0 {
		<-f.seen
	}
	time.Sleep(120 * time.Millisecond)
	if len(f.seen) != 0 {
		t.Fatalf("%d requests after disconnecting", len(f.seen))
	}
	// Administrator actions are refused, and the way back needs no file to be chosen again.
	if _, err := f.RenameClass(classID, "Fine"); err == nil {
		t.Error("a class was renamed while disconnected")
	}
	if view, err := f.Connect(f.server.URL); err != nil || !view.Connected {
		t.Fatalf("reconnecting: %v, %+v", err, view)
	}
	wait(t, func() bool { return f.Snapshot().ClassesLoaded })
	// The settings are left alone: the next start still connects by itself.
	if got := f.remembered(t); got.Server != f.server.URL {
		t.Fatalf("remembered = %+v", got)
	}
}

func TestDisconnectingWhenNotConnectedIsHarmlessAndAnAttemptOrActionBlocksIt(t *testing.T) {
	f := newConfigFixture(t, nil)
	if view, err := f.Disconnect(); err != nil || view.Connected {
		t.Fatalf("disconnecting while disconnected = %+v, %v", view, err)
	}
	release := make(chan struct{})
	entered := make(chan struct{}, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		entered <- struct{}{}
		<-release
		serveAdminState(w, r)
	}))
	t.Cleanup(server.Close)
	opts := DefaultOptions()
	opts.Server, opts.Token, opts.Poll = server.URL, secret, 15*time.Millisecond
	s, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
		s.ServiceShutdown()
	})
	var failed atomic.Bool
	go func() { _, err := s.Connect(server.URL); failed.Store(err != nil) }()
	<-entered
	if _, err := s.Disconnect(); err == nil || !strings.Contains(err.Error(), "connection attempt") {
		t.Fatalf("disconnecting during an attempt = %v", err)
	}
	close(release)
	wait(t, func() bool { return s.Snapshot().Connected })
	if failed.Load() {
		t.Fatal("the attempt failed")
	}
}
