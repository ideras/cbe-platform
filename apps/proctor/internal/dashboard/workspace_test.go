package dashboard

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const submissionRef = int64(7)

func workspaceRoster() WorkspaceRoster {
	last := at.Add(-time.Minute)
	cpu, memory := 12.5, 180
	return WorkspaceRoster{Students: []WorkspaceStudent{{SessionID: sessionID, Account: "0012", State: "ready", Connected: true, LastActivity: &last, CPUPercent: &cpu, MemoryMB: &memory,
		TodoDone: 2, TodoTotal: 3, Submissions: 1, LastSubmissionAt: &last}}}
}

func sessionsHandler(extra http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/admin/sessions" {
			reply(w, roster())
			return
		}
		extra(w, r)
	}
}

func TestTheWorkspaceRosterComesWithTheSessionsOfAWorkspaceExam(t *testing.T) {
	var asked atomic.Int32
	rosterToServe := workspaceRoster()
	s, _ := start(t, sessionsHandler(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/admin/workspace/exam/"+examID+"/roster" {
			asked.Add(1)
			reply(w, rosterToServe)
			return
		}
		http.NotFound(w, r)
	}))
	wait(t, func() bool { return s.Snapshot().Workspace != nil })
	view := s.Snapshot().Workspace
	if len(view.Students) != 1 || view.Students[0].TodoDone != 2 || view.Students[0].TodoTotal != 3 || view.Students[0].CPUPercent == nil || *view.Students[0].CPUPercent != 12.5 {
		t.Errorf("workspace = %+v", view)
	}
	// It is read again on every pass: what the student ticks shows.
	rosterToServe.Students[0].TodoDone = 3
	wait(t, func() bool { w := s.Snapshot().Workspace; return w != nil && w.Students[0].TodoDone == 3 })
	// Another exam clears it until its own roster is read.
	if _, err := s.SelectExam(otherExam); err != nil {
		t.Fatal(err)
	}
	if s.Snapshot().Workspace != nil {
		t.Error("the roster of the last exam stays after the exam changed")
	}
}

func TestAQuestionExamIsAskedAboutOnceAndNeverAgain(t *testing.T) {
	var asked atomic.Int32
	s, _ := start(t, sessionsHandler(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/admin/workspace/") {
			asked.Add(1)
		}
		http.NotFound(w, r)
	}))
	for range 6 {
		s.Refresh()
		time.Sleep(25 * time.Millisecond)
	}
	if got := asked.Load(); got != 1 {
		t.Errorf("a question exam asked %d times about its workspaces", got)
	}
	if view := s.Snapshot(); view.Workspace != nil || view.Error != "" || view.Stale {
		t.Errorf("a question exam's view = workspace %v, error %q, stale %v", view.Workspace, view.Error, view.Stale)
	}
}

func TestAWorkspaceRosterThatMakesNoSenseIsAnError(t *testing.T) {
	bad := workspaceRoster()
	bad.Students[0].TodoDone = 9
	s, _ := start(t, sessionsHandler(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/roster") {
			reply(w, bad)
			return
		}
		http.NotFound(w, r)
	}))
	wait(t, func() bool { return s.Snapshot().Error != "" })
	if view := s.Snapshot(); !view.Stale || view.Workspace != nil || !strings.Contains(view.Error, "workspace") {
		t.Errorf("view = %+v", view)
	}
}

func TestAStudentsWorkspaceIsReadForTheReviewWindow(t *testing.T) {
	cast := `{"version":2,"width":80,"height":24,"timestamp":1}` + "\n" + `[0.5,"o","hello\r\n"]` + "\n"
	s, _ := start(t, sessionsHandler(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/admin/workspace/session/" + sessionID:
			reply(w, WorkspaceSession{SessionID: sessionID, State: "ready", Snapshots: []WorkspaceSnapshot{{ID: 3, Reason: "submit", At: at, Files: 4}}, Submissions: []WorkspaceSubmission{{ID: 7, SnapshotID: 3, At: at, Files: 3}},
				Terminals: []WorkspaceTerminal{{ID: 5, Kind: "run", StartedAt: at, Bytes: 6}}, Done: map[string]bool{"t1": true}})
		case "/admin/workspace/submission/7/tree":
			reply(w, map[string]any{"entries": []WorkspaceEntry{{Path: "src", Type: "dir"}, {Path: "src/a.c", Type: "file", Size: 5}}})
		case "/admin/workspace/snapshot/3/file":
			if r.URL.Query().Get("path") != "src/a.c" {
				http.NotFound(w, r)
				return
			}
			reply(w, WorkspaceFile{Path: "src/a.c", Size: 5, Content: "int x;"})
		case "/admin/workspace/terminal/5/cast":
			w.Header().Set("Content-Type", "application/x-asciicast")
			_, _ = w.Write([]byte(cast))
		case "/admin/workspace/terminal/6/cast":
			_, _ = w.Write([]byte("not a transcript"))
		default:
			http.NotFound(w, r)
		}
	}))
	session, err := s.WorkspaceSession(sessionID)
	if err != nil || len(session.Snapshots) != 1 || len(session.Submissions) != 1 || session.Terminals[0].Kind != "run" || !session.Done["t1"] {
		t.Fatalf("session = %+v, %v", session, err)
	}
	tree, err := s.WorkspaceTree("submission", 7)
	if err != nil || len(tree) != 2 || tree[1].Path != "src/a.c" {
		t.Fatalf("tree = %+v, %v", tree, err)
	}
	file, err := s.WorkspaceFile("snapshot", 3, "src/a.c")
	if err != nil || file.Content != "int x;" {
		t.Fatalf("file = %+v, %v", file, err)
	}
	if text, err := s.WorkspaceTranscript(5); err != nil || text != cast {
		t.Fatalf("transcript = %q, %v", text, err)
	}
	// What is not a transcript, a file that is not there and a selection that makes no sense are refused here, before or after the server.
	for name, call := range map[string]func() error{
		"not a transcript":  func() error { _, err := s.WorkspaceTranscript(6); return err },
		"a missing file":    func() error { _, err := s.WorkspaceFile("snapshot", 3, "nothing"); return err },
		"a bad kind":        func() error { _, err := s.WorkspaceTree("download", 7); return err },
		"a bad id":          func() error { _, err := s.WorkspaceTree("snapshot", 0); return err },
		"no path":           func() error { _, err := s.WorkspaceFile("snapshot", 3, ""); return err },
		"a bad session":     func() error { _, err := s.WorkspaceSession("../x"); return err },
		"a bad transcript":  func() error { _, err := s.WorkspaceTranscript(-1); return err },
		"another session":   func() error { _, err := s.WorkspaceSession(strings.Replace(sessionID, "2", "9", -1)); return err },
		"a path that exits": func() error { _, err := s.WorkspaceTree("submission", 8); return err },
	} {
		if call() == nil {
			t.Errorf("%s was taken", name)
		}
	}
}

func TestAnEntryThatLeavesTheProjectIsRefused(t *testing.T) {
	s, _ := start(t, sessionsHandler(func(w http.ResponseWriter, r *http.Request) {
		reply(w, map[string]any{"entries": []WorkspaceEntry{{Path: "../etc/passwd", Type: "file"}}})
	}))
	if _, err := s.WorkspaceTree("snapshot", 1); err == nil {
		t.Error("a tree with ../ in it was taken")
	}
}

func TestSubmissionsAreSavedWhereTheProctorChoosesAndNowhereElse(t *testing.T) {
	zipData := "PK-not-really-a-zip-but-bytes"
	var cancelled bool
	s, _ := start(t, sessionsHandler(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/admin/workspace/submission/7/zip":
			w.Header().Set("Content-Type", "application/zip")
			_, _ = w.Write([]byte(zipData))
		case "/admin/workspace/session/" + sessionID + "/report":
			w.Header().Set("Content-Type", "text/markdown")
			_, _ = w.Write([]byte("# Report\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	wait(t, func() bool { return len(s.Snapshot().Roster.Sessions) > 0 })
	dir := t.TempDir()
	var asked []SaveDialog
	s.opts.Save = func(dialog SaveDialog) (string, error) {
		asked = append(asked, dialog)
		if cancelled {
			return "", nil
		}
		return filepath.Join(dir, dialog.Filename), nil
	}
	zipPath, err := s.SaveWorkspaceZip(sessionID, 7)
	if err != nil || filepath.Base(zipPath) != "Ada_untrusted_-files.zip" && !strings.HasSuffix(zipPath, "-files.zip") {
		t.Fatalf("zip = %q, %v", zipPath, err)
	}
	if data, err := os.ReadFile(zipPath); err != nil || string(data) != zipData {
		t.Errorf("the zip file holds %q, %v", data, err)
	}
	if info, _ := os.Stat(zipPath); info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v", info.Mode())
	}
	reportPath, err := s.SaveWorkspaceReport(sessionID)
	if err != nil || !strings.HasSuffix(reportPath, "-report.md") {
		t.Fatalf("report = %q, %v", reportPath, err)
	}
	if data, _ := os.ReadFile(reportPath); string(data) != "# Report\n" {
		t.Errorf("the report file holds %q", data)
	}
	if len(asked) != 2 || asked[0].Pattern != "*.zip" || asked[1].Pattern != "*.md" || strings.ContainsAny(asked[0].Filename, `/\`) {
		t.Errorf("dialogs = %+v", asked)
	}
	// Cancelling writes nothing; a download that fails leaves no half file; nothing stays beside the files.
	cancelled = true
	if path, err := s.SaveWorkspaceReport(sessionID); path != "" || err != nil {
		t.Errorf("cancelled = %q, %v", path, err)
	}
	cancelled = false
	if _, err := s.SaveWorkspaceZip(sessionID, 8); err == nil {
		t.Error("a zip the server does not have was saved")
	}
	if _, err := s.SaveWorkspaceZip(sessionID, 0); err == nil {
		t.Error("a zip of submission 0 was asked for")
	}
	if _, err := s.SaveWorkspaceReport("../x"); err == nil {
		t.Error("a report of a bad session was asked for")
	}
	entries, _ := os.ReadDir(dir)
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".download-") {
			t.Errorf("a temporary file was left: %s", entry.Name())
		}
	}
	// Without a dialog in the build nothing is written.
	s.opts.Save = nil
	if _, err := s.SaveWorkspaceReport(sessionID); err == nil {
		t.Error("saved without a dialog")
	}
}
